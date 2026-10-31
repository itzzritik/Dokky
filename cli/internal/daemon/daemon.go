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
	Offline      bool          `json:"offline"`
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

	// serialises changes; status only TryLocks it
	opMu        sync.Mutex
	unmountedAt map[string]time.Time
	edits       map[string]string // configure id → the saved service whose secrets it was given

	// guards data and states; never held across an engine call or an external command
	mu     sync.Mutex
	data   store.Data
	states map[string]*mountState
	stats  map[string]engine.MountStats
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

	eng, err := engine.Start(p.Cache, nil)
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

	d := newDaemon(eng, st, data, home, p.Cache, log)
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
			d.tick()
		}
	}
}

func newDaemon(eng mountEngine, st *store.Store, data store.Data, home, cacheDir string, log *slog.Logger) *Daemon {
	return &Daemon{
		eng: eng, st: st, data: data, home: home, cacheDir: cacheDir, log: log,
		isMounted: engine.IsMounted, forceUnmount: forceUnmount,
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

func (d *Daemon) tick() {
	d.opMu.Lock()
	d.refresh()
	d.opMu.Unlock()
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
			return r, errors.New(logx.Redact(err.Error()))
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
		"service.put": decode(func(p putParams) (idParams, error) {
			s := p.Service
			s.ID = cmp.Or(s.ID, uuid.NewString())
			if p.EditOf != "" && p.EditOf != s.ID {
				return idParams{}, fmt.Errorf("editOf %q must be the id of the service being saved", p.EditOf)
			}
			s.Options, _ = d.keepSecrets(p.EditOf, s.Type, s.Options)
			return idParams{ID: s.ID}, d.putService(s)
		}),
		"service.delete": decode(func(p idParams) (any, error) { return nil, d.deleteService(p.ID) }),
		"service.list":   func(context.Context, json.RawMessage) (any, error) { return d.serviceList(), nil },
		"service.get":    decode(d.serviceGet),
		"oauth.status":   func(context.Context, json.RawMessage) (any, error) { return oauthStatus() },
		"mount.put": decode(func(m store.Mount) (idParams, error) {
			m.ID = cmp.Or(m.ID, uuid.NewString())
			return idParams{ID: m.ID}, d.putMount(m)
		}),
		"mount.delete":      decode(func(p idParams) (any, error) { return nil, d.deleteMount(p.ID) }),
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
		// lets the reply out before shutdown closes the connection
		"daemon.restart": func(context.Context, json.RawMessage) (any, error) {
			time.AfterFunc(restartDelay, func() {
				select {
				case restart <- struct{}{}:
				default:
				}
			})
			return nil, nil
		},
	}
}

func (d *Daemon) status() StatusReply {
	// Stats waits on rclone's mount lock while a mount runs; serve the cache then
	if d.opMu.TryLock() {
		d.refresh()
		d.opMu.Unlock()
	}
	r := StatusReply{Version: buildinfo.Version, Protocol: ipc.Protocol, Mounts: []MountStatus{}}
	if du, err := diskusage.New(d.cacheDir); err == nil {
		r.FreeBytes = du.Available
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	r.LoggedIn = d.data.Account != nil
	for _, m := range d.data.Mounts {
		st := d.state(m.ID)
		ms := MountStatus{ID: m.ID, Name: m.Name, ServiceID: m.ServiceID, State: st.state, Error: logx.Redact(st.err), MountPoint: d.mountPoint(m)}
		if st.state == stateMounted {
			s := d.stats[st.name]
			ms.MountPoint, ms.CacheBytes, ms.UploadsQueued, ms.UploadsInProgress = st.point, s.CacheBytes, s.UploadsQueued, s.UploadsInProgress
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

func (d *Daemon) putService(s store.Service) error {
	s, err := splitSecrets(s)
	if err != nil {
		return err
	}
	d.opMu.Lock()
	defer d.opMu.Unlock()
	if err := d.eng.PutService(s); err != nil {
		return err
	}
	d.mu.Lock()
	if i := slices.IndexFunc(d.data.Services, func(x store.Service) bool { return x.ID == s.ID }); i >= 0 {
		d.data.Services[i] = s
	} else {
		d.data.Services = append(d.data.Services, s)
	}
	d.setSecrets()
	err = d.save()
	d.mu.Unlock()
	d.publish()
	return err
}

func (d *Daemon) deleteService(id string) error {
	d.opMu.Lock()
	defer d.opMu.Unlock()
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
	d.setSecrets()
	err := d.save()
	d.mu.Unlock()
	d.eng.DeleteService(id)
	d.publish()
	return err
}

func (d *Daemon) putMount(m store.Mount) error {
	if err := engine.ValidName(m.Name); err != nil {
		return err
	}
	d.opMu.Lock()
	defer d.opMu.Unlock()
	d.mu.Lock()
	err := d.upsertMount(m)
	if err == nil {
		err = d.save()
	}
	d.mu.Unlock()
	d.publish()
	return err
}

func (d *Daemon) upsertMount(m store.Mount) error {
	if !slices.ContainsFunc(d.data.Services, func(s store.Service) bool { return s.ID == m.ServiceID }) {
		return fmt.Errorf("no service with id %q", m.ServiceID)
	}
	i := -1
	for j, x := range d.data.Mounts {
		switch {
		case x.ID == m.ID:
			i = j
		case x.Name == m.Name:
			return fmt.Errorf("a mount named %q already exists", m.Name)
		}
	}
	if i < 0 {
		d.data.Mounts = append(d.data.Mounts, m)
	} else {
		d.data.Mounts[i] = m
	}
	return nil
}

func (d *Daemon) deleteMount(id string) error {
	d.opMu.Lock()
	defer d.opMu.Unlock()
	m, ok := d.findMount(byID(id))
	if !ok {
		return nil
	}
	if err := d.release(m); err != nil {
		return err
	}
	d.mu.Lock()
	d.dropMount(id)
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
	logx.SetSecrets(values)
}
