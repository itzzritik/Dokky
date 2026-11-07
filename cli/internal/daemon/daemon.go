package daemon

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/buildinfo"
	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/ipc"
	"github.com/itzzritik/dokky/cli/internal/logx"
	"github.com/itzzritik/dokky/cli/internal/platform"
	"github.com/itzzritik/dokky/cli/internal/store"
	dsync "github.com/itzzritik/dokky/cli/internal/sync"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/lib/diskusage"
	"github.com/rclone/rclone/lib/oauthutil"
)

const (
	tickInterval = 30 * time.Second
	restartDelay = 200 * time.Millisecond
)

var ErrRestart = errors.New("daemon restart requested")

type Paths struct {
	State, Cache, Bin string
}

type StatusReply struct {
	Version      string        `json:"version"`
	Protocol     int           `json:"protocol"`
	Mounts       []MountStatus `json:"mounts"`
	FreeBytes    uint64        `json:"freeBytes"`
	MinFreeBytes uint64        `json:"minFreeBytes"`
	LoggedIn     bool          `json:"loggedIn"`
	Pending      bool          `json:"pending"`
	Offline      bool          `json:"offline"`
	SignedOut    bool          `json:"signedOut"`
}

type MountStatus struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	ServiceID         string `json:"serviceId"`
	State             string `json:"state"`
	Error             string `json:"error"`
	MountPoint        string `json:"mountPoint"`
	CacheBytes        int64  `json:"cacheBytes"`
	UploadsQueued     int    `json:"uploadsQueued"`
	UploadsInProgress int    `json:"uploadsInProgress"`
	RemountToApply    bool   `json:"remountToApply"`
	CacheMaxAge       string `json:"cacheMaxAge"`
}

type Daemon struct {
	eng          mountEngine
	st           *store.Store
	home         string
	cacheDir     string
	log          *slog.Logger
	srv          *ipc.Server
	isMounted    func(point string) bool
	forceUnmount func(point string) error
	openURL      func(url string) error

	unregister  func() error
	stopService func() error
	removePath  func(home, state string) error
	removeLater func(left []string, state string) error
	notify      func(title, body string) error

	idle       Idle
	act        activity
	upd        updater
	loop       dsync.Loop
	syncMu     sync.Mutex // one pull at a time; taken before opMu
	reportMu   sync.Mutex
	kicks      chan struct{}
	reportDue  atomic.Bool
	reported   string                 // guarded by mu
	unsent     map[string]unsentCount // guarded by mu
	purging    map[string]float64     // guarded by mu; removal progress
	draining   map[string]bool        // guarded by mu
	stuckSince map[string]time.Time   // guarded by mu
	// this device was removed: leave() runs, and what it waits for shows in the app; guarded by mu
	departing     bool
	leaveWaiting  string
	leaveProgress float64
	lost          map[string]lostCheck
	reached       lostCheck
	newestEvent   time.Time // guarded by mu
	flushMu       sync.Mutex
	peers         []dsync.Device // guarded by mu
	// guarded by mu; mounts made now are the ones at login
	loginMounts bool

	// serialises changes; status only TryLocks it
	opMu        sync.Mutex
	unmountedAt map[string]time.Time
	cleaned     map[string]time.Time
	edits       map[string]string // configure id → the saved service whose secrets it was given

	// guards data and states; never held across an engine call or an external command
	mu      sync.Mutex
	data    store.Data
	states  map[string]*mountState
	stats   map[string]engine.MountStats
	space   map[string]api.Space
	spaceAt map[string]time.Time
	// failed checks in a row for a drive with no space yet
	spaceFails map[string]int
	flow       *loginFlow
	clashes    []dsync.Clash
	saves      uint64 // a pull that started before a save is dropped

	offline, signedOut bool
}

func DefaultPaths() Paths {
	state, _ := platform.StateDir()
	cache, _ := platform.CacheDir()
	var bin string
	if home, err := os.UserHomeDir(); err == nil {
		bin = filepath.Join(home, ".local", "bin", "dokky")
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
	}
	return Paths{State: state, Cache: cache, Bin: bin}
}

func Run(ctx context.Context, p Paths) (err error) {
	if p.State == "" || p.Cache == "" {
		return errors.New("cannot find the dokky state or cache folder")
	}
	if err := os.MkdirAll(p.State, 0o700); err != nil {
		return err
	}
	unlock, err := platform.Lock(filepath.Join(p.State, "daemon.lock"))
	if errors.Is(err, platform.ErrLocked) {
		return errors.New("another dokky daemon is already running")
	}
	if err != nil {
		return err
	}
	defer unlock()
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	var d *Daemon
	eng, err := engine.Start(p.Cache, func(serviceID, token string) {
		if d != nil {
			d.onToken(serviceID, token)
		}
	})
	if err != nil {
		return err
	}
	defer eng.Stop()
	oauthutil.OpenURL = platform.OpenURL
	// after engine.Start: librclone.Initialize resets slog's default
	log := logx.New(filepath.Join(p.State, "dokky.log"))
	defer func() {
		if err != nil && !errors.Is(err, ErrRestart) {
			log.Error("daemon stopped", "err", err)
		}
	}()
	if restart, rerr := refreshServiceFile(p.Bin); rerr != nil {
		log.Warn("refresh service file", "err", rerr)
	} else if restart {
		log.Info("service file updated, restarting")
		return ErrRestart
	}
	st, err := store.Open(p.State, store.SystemKeyring(p.State))
	if err != nil {
		return err
	}
	data, err := st.Load()
	if err != nil {
		return err
	}

	d = newDaemon(eng, st, data, home, p.Cache, log)
	d.startUpdater(p)
	d.start()
	defer d.shutdown()

	ln, err := platform.Listen(platform.SocketAddr(p.State))
	if err != nil {
		return err
	}
	restart := make(chan struct{}, 1)
	d.srv = ipc.NewServer(ln, d.handlers(restart))
	go d.syncLoop(ctx)
	d.resumeRemovals()
	d.resumeLeaving()
	srvCtx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- d.srv.Serve(srvCtx) }()
	serving := true
	defer func() {
		cancel()
		if serving {
			<-served
		}
	}()
	sampleCtx, stopSample := context.WithCancel(ctx)
	sampled := make(chan struct{})
	go func() { d.sample(sampleCtx); close(sampled) }()
	defer func() { stopSample(); <-sampled }()
	log.Info("daemon started", "version", buildinfo.Version)
	d.autoUpdate(ctx, time.Now())
	wipe := make(chan *InstallWatch)
	go d.watchInstalls(ctx, wipe)

	tick := time.NewTicker(tickInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Info("daemon stopping")
			return nil
		case w := <-wipe:
			cancel()
			<-served
			serving = false
			// draining commands can take a while; a reinstall meanwhile wins
			if d.checkInstalls(w, time.Now(), pathExists) != Wipe {
				log.Info("dokky is installed again, not uninstalling")
				return ErrRestart
			}
			cleaned := make(chan struct{})
			go func() { DeepClean(d); close(cleaned) }()
			// stopping the service ends this process before DeepClean returns on macOS and Linux
			select {
			case <-cleaned:
			case <-ctx.Done():
			}
			return nil
		case <-restart:
			log.Info("daemon restarting")
			return ErrRestart
		case serr := <-served:
			serving = false
			return fmt.Errorf("ipc server stopped: %v", serr)
		case <-eng.Ejected():
			_ = d.refresh()
			d.publish()
		case <-tick.C:
			d.tick(ctx)
			if d.autoUpdate(ctx, time.Now()) {
				log.Info("daemon restarting onto the new dokky", "version", installedVersion(p.State))
				return ErrRestart
			}
		}
	}
}

func newDaemon(eng mountEngine, st *store.Store, data store.Data, home, cacheDir string, log *slog.Logger) *Daemon {
	return &Daemon{
		eng: eng, st: st, data: data, home: home, cacheDir: cacheDir, log: log, loop: dsync.Loop{Log: log},
		isMounted: engine.IsMounted, forceUnmount: forceUnmount, openURL: platform.OpenURL,
		unregister: unregisterService, stopService: StopService, removePath: platform.RemovePath, removeLater: deleteAfterExit, notify: platform.Notify,
		unmountedAt: map[string]time.Time{}, cleaned: map[string]time.Time{}, states: map[string]*mountState{}, edits: map[string]string{},
		space: map[string]api.Space{}, spaceAt: map[string]time.Time{}, spaceFails: map[string]int{},
		kicks: make(chan struct{}, 1), unsent: map[string]unsentCount{}, purging: map[string]float64{}, draining: map[string]bool{}, stuckSince: map[string]time.Time{}, lost: map[string]lostCheck{},
	}
}

func (d *Daemon) start() {
	d.mu.Lock()
	services := slices.Clone(d.data.Services)
	d.setSecrets()
	d.mu.Unlock()
	for _, s := range services {
		if err := d.eng.PutService(s); err != nil {
			d.log.Error("load service", "service", s.Name, "err", err)
		}
	}
	// a restart for an update remounts too, but only a start soon after boot is a login
	d.mu.Lock()
	d.loginMounts = bootedRecently()
	d.mu.Unlock()
	d.remountFlagged()
	d.mu.Lock()
	d.loginMounts = false
	d.mu.Unlock()
}

// a removal from another device picks up where it stopped before a restart
func (d *Daemon) resumeRemovals() {
	d.mu.Lock()
	var leaving []string
	for id, lm := range d.data.Local {
		if lm.Removing {
			leaving = append(leaving, id)
		}
	}
	d.mu.Unlock()
	for _, id := range leaving {
		go d.drain(id)
	}
}

func (d *Daemon) tick(ctx context.Context) {
	d.opMu.Lock()
	statsErr := d.refresh()
	d.opMu.Unlock()
	d.observeIdle(time.Now(), statsErr)
	d.cleanupUploads(time.Now())
	d.remountFlagged()
	d.applyPulled()
	d.reportIfChanged()
	d.publish()
}

func (d *Daemon) shutdown() {
	d.opMu.Lock()
	defer d.opMu.Unlock()
	d.mu.Lock()
	var live []mountState
	for _, st := range d.states {
		if st.name != "" {
			live = append(live, *st)
		}
	}
	d.mu.Unlock()
	for _, st := range live {
		if err := d.detach(st.name, st.point, true); err != nil {
			d.log.Error("unmount at shutdown", "name", st.name, "err", err)
		}
	}
}

type namedParams struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type idParams struct {
	ID string `json:"id"`
}

type deleteParams struct {
	ID    string `json:"id"`
	Force bool   `json:"force"`
}

func decode[T, R any](fn func(T) (R, error)) ipc.Handler {
	return decodeCtx(func(_ context.Context, p T) (R, error) { return fn(p) })
}

func decodeCtx[T, R any](fn func(context.Context, T) (R, error)) ipc.Handler {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p T
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, fmt.Errorf("bad params: %w", err)
			}
		}
		r, err := fn(ctx, p)
		if err != nil {
			code := "internal"
			var ce ipc.CodedError
			if errors.As(err, &ce) {
				code = ce.Code()
			}
			return r, &codedError{code, logx.Redact(err.Error())}
		}
		return r, nil
	}
}

func (d *Daemon) handlers(restart chan<- struct{}) map[string]ipc.Handler {
	h := map[string]ipc.Handler{
		"status": func(context.Context, json.RawMessage) (any, error) { return d.status(), nil },
		api.MState: func(context.Context, json.RawMessage) (any, error) {
			d.tryRefresh()
			return d.snapshot(), nil
		},
		"mount": decode(func(p namedParams) (any, error) {
			id, err := d.resolve(p)
			if err == nil {
				err = d.mountByPerson(id)
			}
			if err != nil {
				return nil, err
			}
			return d.mountStatus(id), nil
		}),
		"unmount": decode(func(p namedParams) (any, error) {
			id, err := d.resolve(p)
			if err == nil {
				err = d.unmountIdle(id)
			}
			if err != nil {
				return nil, err
			}
			return d.mountStatus(id), nil
		}),
		"service.put": decodeCtx(func(ctx context.Context, p putParams) (idParams, error) {
			s := p.Service
			s.ID = cmp.Or(s.ID, uuid.NewString())
			if p.EditOf != "" && p.EditOf != s.ID {
				return idParams{}, fmt.Errorf("editOf %q must be the id of the service being saved", p.EditOf)
			}
			s.Options, _ = d.keepSecrets(p.EditOf, s.Type, s.Options)
			if err := d.putService(ctx, s); err != nil {
				return idParams{}, err
			}
			if p.EditOf != "" {
				d.event("signin.fixed", s.ID, s.Name, "", true)
			} else {
				d.event("signin.added", s.ID, s.Name, "", true)
			}
			return idParams{ID: s.ID}, nil
		}),
		"service.delete": decodeCtx(func(ctx context.Context, p idParams) (any, error) { return nil, d.deleteService(ctx, p.ID) }),
		"service.list":   func(context.Context, json.RawMessage) (any, error) { return d.serviceList(), nil },
		"service.get":    decode(d.serviceGet),
		"oauth.status":   func(context.Context, json.RawMessage) (any, error) { return oauthStatus() },
		"mount.put": decodeCtx(func(ctx context.Context, m store.Mount) (idParams, error) {
			m.ID = cmp.Or(m.ID, uuid.NewString())
			return idParams{ID: m.ID}, d.putMount(ctx, m)
		}),
		"mount.get": decode(func(p idParams) (store.Mount, error) {
			m, ok := d.findMount(byID(p.ID))
			if !ok {
				return m, fmt.Errorf("no mount with id %q", p.ID)
			}
			return m, nil
		}),
		"mount.delete":      decodeCtx(func(ctx context.Context, p deleteParams) (any, error) { return nil, d.deleteMount(ctx, p.ID, p.Force) }),
		"providers":         func(context.Context, json.RawMessage) (any, error) { return engine.Providers() },
		"service.configure": decodeCtx(d.configure),
		"service.test": decode(func(p testParams) (testReply, error) {
			p.Service.Options, _ = d.keepSecrets(p.EditOf, p.Service.Type, p.Service.Options)
			return d.testService(p.Service), nil
		}),
		"browse":             decode(d.browse),
		api.MActivity:        func(context.Context, json.RawMessage) (any, error) { return d.activity(), nil },
		api.MTransferRetry:   decode(d.retry),
		api.MCatalog:         func(context.Context, json.RawMessage) (any, error) { return catalog() },
		api.MAccounts:        func(context.Context, json.RawMessage) (any, error) { return d.accounts(), nil },
		api.MDriveAdd:        decodeCtx(d.addDrive),
		api.MDriveUpdate:     decodeCtx(d.updateDrive),
		api.MDriveRemove:     decodeCtx(d.removeDrive),
		api.MDriveCheck:      decodeCtx(d.checkDrive),
		api.MEvents:          decodeCtx(func(ctx context.Context, _ struct{}) (api.Events, error) { return d.events(ctx) }),
		api.MEventsSeen:      func(context.Context, json.RawMessage) (any, error) { return nil, d.eventsSeen() },
		api.MDriveConnect:    decode(d.connectDrive),
		api.MDriveDisconnect: decode(d.disconnectDrive),
		api.MCacheClear:      decodeCtx(func(context.Context, struct{}) (any, error) { return nil, d.clearCachedFiles() }),
		"settings.get":       func(context.Context, json.RawMessage) (any, error) { return d.settings(), nil },
		"settings.set":       decode(func(s store.Settings) (any, error) { return nil, d.setSettings(s) }),
		"login.start":        decodeCtx(func(ctx context.Context, _ struct{}) (loginStartReply, error) { return d.loginStart(ctx) }),
		"login.poll": decodeCtx(func(ctx context.Context, _ struct{}) (loginPollReply, error) {
			r, err := d.loginPoll(ctx)
			d.syncNow(ctx)
			return r, err
		}),
		"login.resolve": decodeCtx(func(ctx context.Context, p resolveParams) (resolveReply, error) {
			r, err := d.loginResolve(ctx, p)
			d.syncNow(ctx)
			return r, err
		}),
		"logout":         decodeCtx(d.logout),
		"sync.now":       decodeCtx(func(ctx context.Context, _ struct{}) (any, error) { _ = d.syncNow(ctx); return nil, nil }),
		api.MSyncKick:    func(context.Context, json.RawMessage) (any, error) { d.kick(); return nil, nil },
		"devices.list":   decodeCtx(func(ctx context.Context, _ struct{}) ([]dsync.Device, error) { return d.devices(ctx) }),
		"devices.revoke": decodeCtx(func(ctx context.Context, p idParams) (any, error) { return nil, d.revokeDevice(ctx, p.ID) }),
		"devices.rename": decodeCtx(func(ctx context.Context, p namedParams) (any, error) { return nil, d.renameDevice(ctx, p.ID, p.Name) }),
		"update.check":   decodeCtx(func(ctx context.Context, _ struct{}) (checkReply, error) { return d.updateCheck(ctx) }),
		"update": decodeCtx(func(ctx context.Context, p updateParams) (updateReply, error) {
			r, err := d.update(ctx, p)
			if err == nil && r.Updated {
				requestRestart(restart)
			}
			return r, err
		}),
		"update.install": decodeCtx(func(ctx context.Context, _ struct{}) (installReply, error) { return d.installNow(ctx) }),
		"update.restart": decodeCtx(func(ctx context.Context, _ struct{}) (openReply, error) {
			r := d.updateOnOpen(ctx)
			if r.Restarting {
				requestRestart(restart)
			}
			return r, nil
		}),
		"daemon.restart": func(context.Context, json.RawMessage) (any, error) {
			requestRestart(restart)
			return nil, nil
		},
	}
	for _, m := range kickAfter {
		h[m] = d.kicked(h[m])
	}
	for m, fn := range h {
		if !slices.Contains(whileRemoved, m) {
			h[m] = d.refuseRemoved(fn)
		}
	}
	return h
}

// a change made here pulls right after, so this device also sees what others changed
var kickAfter = []string{"mount", "unmount", "service.put", "service.delete", "mount.put", "mount.delete", "service.configure", "settings.set", "devices.revoke", "devices.rename",
	api.MDriveAdd, api.MDriveUpdate, api.MDriveRemove, api.MDriveConnect, api.MDriveDisconnect}

func (d *Daemon) kicked(h ipc.Handler) ipc.Handler {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		out, err := h(ctx, raw)
		if err == nil {
			d.kick()
		}
		return out, err
	}
}

// lets the reply out before shutdown closes the connection
func requestRestart(restart chan<- struct{}) {
	time.AfterFunc(restartDelay, func() {
		select {
		case restart <- struct{}{}:
		default:
		}
	})
}

// Stats waits on rclone's mount lock while a mount runs; serve the cache then
func (d *Daemon) tryRefresh() {
	if d.opMu.TryLock() {
		_ = d.refresh()
		d.opMu.Unlock()
	}
}

func (d *Daemon) status() StatusReply {
	d.tryRefresh()
	r := StatusReply{Version: buildinfo.Version, Protocol: ipc.Protocol, Mounts: []MountStatus{}}
	if du, err := diskusage.New(d.cacheDir); err == nil {
		r.FreeBytes = du.Available
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	r.LoggedIn = d.data.Account != nil
	r.Pending = r.LoggedIn && d.data.Account.Pending
	r.Offline, r.SignedOut = d.offline, d.signedOut
	for _, m := range d.data.Mounts {
		st := d.state(m.ID)
		ms := MountStatus{ID: m.ID, Name: m.Name, ServiceID: m.ServiceID, State: st.state, Error: logx.Redact(st.err), MountPoint: d.mountPoint(m), CacheMaxAge: d.effective(m).VFS.CacheMaxAge}
		if st.state == stateMounted {
			s := d.stats[st.name]
			ms.MountPoint, ms.CacheBytes, ms.UploadsQueued, ms.UploadsInProgress = st.point, s.CacheBytes, s.UploadsQueued, s.UploadsInProgress
			ms.RemountToApply = st.remount
		}
		if d.data.Local[m.ID].Removing {
			ms.State = stateLeaving
		}
		if _, ok := d.purging[m.ID]; ok {
			ms.State = stateRemoving
		}
		var free fs.SizeSuffix
		if free.Set(m.VFS.CacheMinFreeSpace) == nil && free > 0 {
			r.MinFreeBytes = max(r.MinFreeBytes, uint64(free))
		}
		r.Mounts = append(r.Mounts, ms)
	}
	return r
}

func (d *Daemon) mountStatus(id string) MountStatus {
	for _, m := range d.status().Mounts {
		if m.ID == id {
			return m
		}
	}
	return MountStatus{}
}

func (d *Daemon) publish() {
	if d.srv == nil {
		return
	}
	raw, _ := json.Marshal(d.status())
	d.srv.Publish(ipc.Event{Event: "status", Data: raw})
	raw, _ = json.Marshal(d.snapshot())
	d.srv.Publish(ipc.Event{Event: api.EState, Data: raw})
}

func (d *Daemon) putService(ctx context.Context, s store.Service) error {
	s, err := splitSecrets(s)
	if err != nil {
		return err
	}
	d.opMu.Lock()
	defer d.opMu.Unlock()
	if err := d.serverFirst(func(c *dsync.Client) (err error) {
		s, err = dsync.SaveService(ctx, c, s)
		return err
	}); err != nil {
		return err
	}
	if err := d.eng.PutService(s); err != nil {
		return err
	}
	d.mu.Lock()
	if i := slices.IndexFunc(d.data.Services, func(x store.Service) bool { return x.ID == s.ID }); i >= 0 {
		d.data.Services[i] = s
	} else {
		d.data.Services = append(d.data.Services, s)
	}
	d.saves++
	d.setSecrets()
	err = d.save()
	d.mu.Unlock()
	d.publish()
	return err
}

func (d *Daemon) deleteService(ctx context.Context, id string) error {
	d.opMu.Lock()
	defer d.opMu.Unlock()
	if err := d.serverFirst(func(c *dsync.Client) error { return notFoundOK(c.DeleteService(ctx, id)) }); err != nil {
		return err
	}
	d.mu.Lock()
	var mounts []store.Mount
	for _, m := range d.data.Mounts {
		if m.ServiceID == id {
			mounts = append(mounts, m)
		}
	}
	d.mu.Unlock()
	for _, m := range mounts {
		if err := d.release(m); err != nil {
			return err
		}
	}
	d.mu.Lock()
	for _, m := range mounts {
		d.dropMount(m.ID)
	}
	d.data.Services = slices.DeleteFunc(d.data.Services, func(s store.Service) bool { return s.ID == id })
	d.saves++
	d.setSecrets()
	err := d.save()
	d.mu.Unlock()
	d.eng.DeleteService(id)
	d.publish()
	return err
}

func (d *Daemon) putMount(ctx context.Context, m store.Mount) error {
	m.Name = engine.Title(strings.TrimSpace(m.Name))
	if err := engine.ValidName(m.Name); err != nil {
		return err
	}
	if err := engine.ValidCacheMaxAge(m.VFS.CacheMaxAge); err != nil {
		return err
	}
	d.opMu.Lock()
	defer d.opMu.Unlock()
	d.mu.Lock()
	i, err := d.mountSlot(m)
	var old store.Mount
	if i >= 0 {
		old = d.data.Mounts[i]
	}
	d.mu.Unlock()
	if err == nil {
		err = d.serverFirst(func(c *dsync.Client) (err error) {
			m, err = dsync.SaveMount(ctx, c, m)
			return err
		})
	}
	if err != nil {
		return err
	}
	d.mu.Lock()
	err = d.upsertMount(m)
	if err == nil {
		if st := d.state(m.ID); i >= 0 && st.state == stateMounted && dsync.MountNeedsRemount(old, m) {
			st.remount = true
		}
		d.saves++
		err = d.save()
	}
	d.mu.Unlock()
	d.publish()
	return err
}

func (d *Daemon) mountSlot(m store.Mount) (int, error) {
	if !slices.ContainsFunc(d.data.Services, func(s store.Service) bool { return s.ID == m.ServiceID }) {
		return -1, fmt.Errorf("no service with id %q", m.ServiceID)
	}
	if err := d.folderClash(m.ID, m.Name); err != nil {
		return -1, err
	}
	return slices.IndexFunc(d.data.Mounts, byID(m.ID)), nil
}

func (d *Daemon) resolve(p namedParams) (string, error) {
	if p.ID != "" {
		return p.ID, nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	name := strings.TrimSpace(p.Name)
	var exact, ids []string
	for _, m := range d.data.Mounts {
		switch {
		case m.ID == name || m.Name == name:
			exact = append(exact, m.ID)
		case strings.EqualFold(m.Name, name):
			ids = append(ids, m.ID)
		}
	}
	if len(exact) > 0 {
		ids = exact
	}
	switch len(ids) {
	case 0:
		return "", fmt.Errorf("no drive named %q", p.Name)
	case 1:
		return ids[0], nil
	}
	return "", fmt.Errorf("more than one drive is named %q: %s", p.Name, strings.Join(ids, ", "))
}

func (d *Daemon) upsertMount(m store.Mount) error {
	i, err := d.mountSlot(m)
	if err != nil {
		return err
	}
	if i < 0 {
		d.data.Mounts = append(d.data.Mounts, m)
	} else {
		d.data.Mounts[i] = m
	}
	return nil
}

func (d *Daemon) deleteMount(ctx context.Context, id string, force bool) error {
	d.opMu.Lock()
	defer d.opMu.Unlock()
	m, ok := d.findMount(byID(id))
	if ok && !force {
		if err := d.refuseUnsentOf(m); err != nil {
			return err
		}
	}
	if err := d.serverFirst(func(c *dsync.Client) error { return notFoundOK(c.DeleteMount(ctx, id)) }); err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if err := d.release(m); err != nil {
		return err
	}
	d.mu.Lock()
	d.dropMount(id)
	d.saves++
	err := d.save()
	d.mu.Unlock()
	d.publish()
	return err
}

func (d *Daemon) setLocal(id, point string) error {
	drive := runtime.GOOS == "windows" && len(point) == 2 && point[1] == ':'
	if point != "" && !filepath.IsAbs(point) && !drive {
		return fmt.Errorf("mount point %q must be an absolute path", point)
	}
	d.opMu.Lock()
	defer d.opMu.Unlock()
	m, ok := d.findMount(byID(id))
	if !ok {
		return fmt.Errorf("no mount with id %q", id)
	}
	d.mu.Lock()
	lm := d.data.Local[id]
	if lm.Mounted || d.state(id).name != "" {
		d.mu.Unlock()
		return fmt.Errorf("unmount %s before changing its mount point", m.Name)
	}
	lm.MountPoint = point
	d.data.Local[id] = lm
	err := d.save()
	d.mu.Unlock()
	d.publish()
	return err
}

func (d *Daemon) release(m store.Mount) error {
	d.mu.Lock()
	busy := d.data.Local[m.ID].Mounted || d.state(m.ID).name != ""
	d.mu.Unlock()
	if !busy {
		return nil
	}
	return d.unmountLocked(m)
}

func (d *Daemon) dropMount(id string) {
	d.data.Mounts = slices.DeleteFunc(d.data.Mounts, func(m store.Mount) bool { return m.ID == id })
	delete(d.data.Local, id)
	delete(d.states, id)
}

func (d *Daemon) save() error {
	return d.st.Save(d.data)
}

func (d *Daemon) setSecrets() {
	var values []string
	for _, s := range d.data.Services {
		for _, v := range s.Secrets {
			values = append(values, v)
		}
	}
	if d.data.Account != nil {
		values = append(values, d.data.Account.Token)
	}
	if d.flow != nil {
		values = append(values, d.flow.deviceCode)
	}
	logx.SetSecrets(values)
}
