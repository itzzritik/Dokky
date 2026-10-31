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
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
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

	idle   Idle
	upd    updater
	loop   dsync.Loop
	syncMu sync.Mutex // one pull at a time; taken before opMu

	// serialises changes; status only TryLocks it
	opMu        sync.Mutex
	unmountedAt map[string]time.Time
	edits       map[string]string // configure id → the saved service whose secrets it was given

	// guards data and states; never held across an engine call or an external command
	mu      sync.Mutex
	data    store.Data
	states  map[string]*mountState
	stats   map[string]engine.MountStats
	flow    *loginFlow
	clashes []dsync.Clash
	saves   uint64 // a pull that started before a save is dropped

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
	log.Info("daemon started", "version", buildinfo.Version)
	d.autoUpdate(ctx, time.Now())

	tick := time.NewTicker(tickInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Info("daemon stopping")
			return nil
		case <-restart:
			log.Info("daemon restarting")
			return ErrRestart
		case serr := <-served:
			serving = false
			return fmt.Errorf("ipc server stopped: %v", serr)
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
		unmountedAt: map[string]time.Time{}, states: map[string]*mountState{}, edits: map[string]string{},
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
	d.remountFlagged()
}

func (d *Daemon) tick(ctx context.Context) {
	d.syncNow(ctx)
	d.opMu.Lock()
	statsErr := d.refresh()
	d.opMu.Unlock()
	d.observeIdle(time.Now(), statsErr)
	d.remountFlagged()
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

type nameParams struct {
	Name string `json:"name"`
}

type idParams struct {
	ID string `json:"id"`
}

type localParams struct {
	ID         string `json:"id"`
	MountPoint string `json:"mountPoint"`
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
	return map[string]ipc.Handler{
		"status": func(context.Context, json.RawMessage) (any, error) { return d.status(), nil },
		"mount": decode(func(p nameParams) (any, error) {
			if err := d.mount(p.Name); err != nil {
				return nil, err
			}
			return d.mountStatus(p.Name), nil
		}),
		"unmount": decode(func(p nameParams) (any, error) {
			if err := d.unmount(p.Name); err != nil {
				return nil, err
			}
			return d.mountStatus(p.Name), nil
		}),
		"service.put": decodeCtx(func(ctx context.Context, p putParams) (idParams, error) {
			s := p.Service
			s.ID = cmp.Or(s.ID, uuid.NewString())
			if p.EditOf != "" && p.EditOf != s.ID {
				return idParams{}, fmt.Errorf("editOf %q must be the id of the service being saved", p.EditOf)
			}
			s.Options, _ = d.keepSecrets(p.EditOf, s.Type, s.Options)
			return idParams{ID: s.ID}, d.putService(ctx, s)
		}),
		"service.delete": decodeCtx(func(ctx context.Context, p idParams) (any, error) { return nil, d.deleteService(ctx, p.ID) }),
		"service.list":   func(context.Context, json.RawMessage) (any, error) { return d.serviceList(), nil },
		"service.get":    decode(d.serviceGet),
		"oauth.status":   func(context.Context, json.RawMessage) (any, error) { return oauthStatus() },
		"mount.put": decodeCtx(func(ctx context.Context, m store.Mount) (idParams, error) {
			m.ID = cmp.Or(m.ID, uuid.NewString())
			return idParams{ID: m.ID}, d.putMount(ctx, m)
		}),
		"mount.delete":      decodeCtx(func(ctx context.Context, p idParams) (any, error) { return nil, d.deleteMount(ctx, p.ID) }),
		"mount.setLocal":    decode(func(p localParams) (any, error) { return nil, d.setLocal(p.ID, p.MountPoint) }),
		"providers":         func(context.Context, json.RawMessage) (any, error) { return engine.Providers() },
		"service.configure": decodeCtx(d.configure),
		"service.test": decode(func(p testParams) (testReply, error) {
			p.Service.Options, _ = d.keepSecrets(p.EditOf, p.Service.Type, p.Service.Options)
			return d.testService(p.Service), nil
		}),
		"browse":       decode(d.browse),
		"transfers":    func(context.Context, json.RawMessage) (any, error) { return d.transfers(), nil },
		"settings.get": func(context.Context, json.RawMessage) (any, error) { return d.settings(), nil },
		"settings.set": decode(func(s store.Settings) (any, error) { return nil, d.setSettings(s) }),
		"login.start":  decodeCtx(func(ctx context.Context, _ struct{}) (loginStartReply, error) { return d.loginStart(ctx) }),
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
		"sync.now":       decodeCtx(func(ctx context.Context, _ struct{}) (any, error) { d.syncNow(ctx); return nil, nil }),
		"devices.list":   decodeCtx(func(ctx context.Context, _ struct{}) ([]dsync.Device, error) { return d.devices(ctx) }),
		"devices.revoke": decodeCtx(func(ctx context.Context, p idParams) (any, error) { return nil, d.revokeDevice(ctx, p.ID) }),
		"update": decodeCtx(func(ctx context.Context, p updateParams) (updateReply, error) {
			r, err := d.update(ctx, p)
			if err == nil && r.Updated {
				requestRestart(restart)
			}
			return r, err
		}),
		"daemon.restart": func(context.Context, json.RawMessage) (any, error) {
			requestRestart(restart)
			return nil, nil
		},
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

func (d *Daemon) status() StatusReply {
	// Stats waits on rclone's mount lock while a mount runs; serve the cache then
	if d.opMu.TryLock() {
		_ = d.refresh()
		d.opMu.Unlock()
	}
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
		ms := MountStatus{ID: m.ID, Name: m.Name, ServiceID: m.ServiceID, State: st.state, Error: logx.Redact(st.err), MountPoint: d.mountPoint(m)}
		if st.state == stateMounted {
			s := d.stats[st.name]
			ms.MountPoint, ms.CacheBytes, ms.UploadsQueued, ms.UploadsInProgress = st.point, s.CacheBytes, s.UploadsQueued, s.UploadsInProgress
			ms.RemountToApply = st.remount
		}
		if d.data.Local[m.ID].RemovedRemotely {
			ms.State = stateRemoved
		}
		var free fs.SizeSuffix
		if free.Set(m.VFS.CacheMinFreeSpace) == nil && free > 0 {
			r.MinFreeBytes = max(r.MinFreeBytes, uint64(free))
		}
		r.Mounts = append(r.Mounts, ms)
	}
	return r
}

func (d *Daemon) mountStatus(name string) MountStatus {
	for _, m := range d.status().Mounts {
		if m.Name == name {
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
	if err := engine.ValidName(m.Name); err != nil {
		return err
	}
	d.opMu.Lock()
	defer d.opMu.Unlock()
	d.mu.Lock()
	_, err := d.mountSlot(m)
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
	i := -1
	for j, x := range d.data.Mounts {
		switch {
		case x.ID == m.ID:
			i = j
		case x.Name == m.Name:
			return -1, fmt.Errorf("a mount named %q already exists", m.Name)
		}
	}
	return i, nil
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

func (d *Daemon) deleteMount(ctx context.Context, id string) error {
	d.opMu.Lock()
	defer d.opMu.Unlock()
	if err := d.serverFirst(func(c *dsync.Client) error { return notFoundOK(c.DeleteMount(ctx, id)) }); err != nil {
		return err
	}
	m, ok := d.findMount(byID(id))
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
