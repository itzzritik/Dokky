package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/buildinfo"
	"github.com/itzzritik/dokky/cli/internal/ipc"
	"github.com/itzzritik/dokky/cli/internal/platform"
	"github.com/itzzritik/dokky/cli/internal/store"
	dsync "github.com/itzzritik/dokky/cli/internal/sync"
	"github.com/itzzritik/dokky/cli/internal/vault"
	"github.com/rclone/rclone/lib/diskusage"
)

// the notification waits for the desktop to settle after login
var promptDelay = 5 * time.Second

var errLocking = errors.New("dokky is locking")

// what both phases share for the life of the process
type phaseEnv struct {
	p       Paths
	eng     mountEngine
	ejected <-chan struct{}
	k       *keeper
	home    string
	log     *slog.Logger
	cur     *atomic.Pointer[Daemon] // the unlocked Daemon, for the engine's token callback
	adjust  func(*Daemon)           // tests swap in fakes
	// the stop watchdog, armed as the unlocked phase ends; nil in tests
	watchdog *atomic.Pointer[time.Timer]
}

func (e phaseEnv) arm(restart bool) {
	if e.watchdog == nil {
		return
	}
	log := e.log
	e.watchdog.Store(time.AfterFunc(shutdownLimit, func() {
		log.Error("dokky took too long to stop, exiting anyway", "restart", restart)
		StuckExit(restart)
	}))
}

func (e phaseEnv) disarm() {
	if e.watchdog == nil {
		return
	}
	if t := e.watchdog.Swap(nil); t != nil {
		t.Stop()
	}
}

// the locked phase: the same socket with a few methods, no drives, until the vault opens
type lockedPhase struct {
	e      phaseEnv
	shell  *Daemon // no store: only its updater, uninstall watch and notifier are used
	srv    *ipc.Server
	opened chan []byte
}

// runLocked serves until the vault opens and returns its key, or returns nil with the error that ends the
// process. ask shows a notification once, as at login; a manual lock does not
func runLocked(ctx context.Context, e phaseEnv, ask bool) ([]byte, error) {
	shell := newDaemon(e.eng, nil, store.Fresh(), e.home, e.p.Cache, e.log, e.k)
	if e.adjust != nil {
		e.adjust(shell)
	}
	shell.startUpdater(e.p)
	l := &lockedPhase{e: e, shell: shell, opened: make(chan []byte, 1)}
	ln, err := platform.Listen(platform.SocketAddr(e.p.State))
	if err != nil {
		return nil, err
	}
	restart := make(chan struct{}, 1)
	l.srv = ipc.NewServer(ln, l.handlers(restart))
	e.k.onPublish(l.publish)
	defer e.k.onPublish(nil)
	srvCtx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- l.srv.Serve(srvCtx) }()
	serving := true
	defer func() {
		cancel()
		if serving {
			<-served
		}
	}()
	pctx, pcancel := context.WithCancel(ctx)
	defer pcancel()
	if ask {
		go l.ask(pctx)
	}
	wipe := make(chan *InstallWatch)
	go shell.watchInstalls(pctx, wipe)
	tick := time.NewTicker(tickInterval)
	defer tick.Stop()
	e.log.Info("dokky is locked, waiting for the master password", "version", buildinfo.Version)
	l.publish()
	for {
		select {
		case <-ctx.Done():
			e.log.Info("daemon stopping")
			return nil, nil
		case ak := <-l.opened:
			e.log.Info("dokky unlocked")
			return ak, nil
		case <-restart:
			e.log.Info("daemon restarting")
			return nil, ErrRestart
		case serr := <-served:
			serving = false
			return nil, fmt.Errorf("ipc server stopped: %v", serr)
		case w := <-wipe:
			cancel()
			<-served
			serving = false
			if shell.checkInstalls(w, time.Now(), pathExists) != Wipe {
				e.log.Info("dokky is installed again, not uninstalling")
				return nil, ErrRestart
			}
			// nothing is mounted and the account cannot be told without the key, so the vault and store stay
			// for a reinstall, like a device with no account
			shell.log.Warn("dokky was removed with npm while locked, uninstalling it")
			shell.removeInstall(nil)
			return nil, nil
		case <-e.ejected:
		case <-tick.C:
			// a newer dokky installed meanwhile takes over; settings are locked, so nothing downloads here
			if buildinfo.Newer(installedVersion(e.p.State), buildinfo.Version) {
				e.log.Info("daemon restarting onto the new dokky", "version", installedVersion(e.p.State))
				return nil, ErrRestart
			}
		}
	}
}

// locked answers state, status, session.open and close, vault.unlock, vault.recover, daemon.restart,
// update.check and the event stream. opening the session opens the vault, which starts the unlocked phase
func (l *lockedPhase) handlers(restart chan<- struct{}) map[string]ipc.Handler {
	k := l.e.k
	h := map[string]ipc.Handler{}
	h["status"] = func(context.Context, json.RawMessage) (any, error) { return l.status(), nil }
	h[api.MState] = func(context.Context, json.RawMessage) (any, error) { return l.snapshot(), nil }
	open := func(ctx context.Context, p api.SessionParams) (struct{}, error) {
		ak, err := k.openSession(ctx, p, unlockReason)
		// nil with no error: a session opened moments ago, whose key is already on its way
		if ak != nil {
			l.hand(ak)
		}
		return struct{}{}, err
	}
	h[api.MSessionOpen] = decodeCtx(open)
	h[api.MSessionClose] = func(context.Context, json.RawMessage) (any, error) {
		k.endSession("closed")
		return nil, nil
	}
	h[api.MVaultUnlock] = decodeCtx(func(ctx context.Context, p api.UnlockParams) (struct{}, error) {
		return open(ctx, api.SessionParams{Method: p.Method, Password: p.Password, Remote: p.Remote, Force: true})
	})
	h[api.MVaultRecover] = proving(k, decodeCtx(func(ctx context.Context, p api.RecoverParams) (struct{}, error) {
		ak, err := l.recover(ctx, p)
		if err != nil {
			return struct{}{}, err
		}
		l.hand(ak)
		return struct{}{}, nil
	}))
	h["daemon.restart"] = func(context.Context, json.RawMessage) (any, error) {
		requestRestart(restart)
		return nil, nil
	}
	h["update.check"] = decodeCtx(func(ctx context.Context, _ struct{}) (checkReply, error) { return l.shell.updateCheck(ctx) })
	for m, fn := range h {
		h[m] = sessionGate(k, m, fn)
	}
	// every other method the unlocked daemon has answers locked rather than unknown
	refuse := func(context.Context, json.RawMessage) (any, error) { return nil, errVaultLocked }
	for m := range (&Daemon{}).handlers(nil) {
		if _, ok := h[m]; !ok {
			h[m] = refuse
		}
	}
	return h
}

func (l *lockedPhase) status() StatusReply {
	r := StatusReply{Version: buildinfo.Version, Protocol: ipc.Protocol, Mounts: []MountStatus{}, Vault: api.VaultLocked, Locked: true}
	if du, err := diskusage.New(l.e.p.Cache); err == nil {
		r.FreeBytes = du.Available
	}
	return r
}

func (l *lockedPhase) snapshot() api.State {
	return api.State{Version: buildinfo.Version, Build: buildinfo.Stamp(), Drives: []api.Drive{}, Clashes: []api.Clash{}, CacheDir: l.e.p.Cache, Vault: l.e.k.view(false)}
}

func (l *lockedPhase) publish() {
	raw, _ := json.Marshal(l.status())
	l.srv.Publish(ipc.Event{Event: "status", Data: raw})
	raw, _ = json.Marshal(l.snapshot())
	l.srv.Publish(ipc.Event{Event: api.EState, Data: raw})
}

// the reply goes out before the locked socket closes
func (l *lockedPhase) hand(ak []byte) {
	time.AfterFunc(restartDelay, func() {
		select {
		case l.opened <- ak:
		default:
			vault.Zero(ak)
		}
	})
}

// recover works locked too. a device linked to an account changes the account's password first, reading the
// account from the store the recovered key opens; if the server refuses, nothing changes and dokky stays locked
func (l *lockedPhase) recover(ctx context.Context, p api.RecoverParams) ([]byte, error) {
	k := l.e.k
	k.op.Lock()
	defer k.op.Unlock()
	nf, ak, err := k.recovered(p)
	if err != nil {
		return nil, err
	}
	if nf.Account {
		if err := l.pushAccount(ctx, nf, ak); err != nil {
			vault.Zero(ak)
			return nil, coded(err)
		}
	}
	if err := k.save(nf); err != nil {
		vault.Zero(ak)
		return nil, err
	}
	return ak, nil
}

func (l *lockedPhase) pushAccount(ctx context.Context, nf *vault.File, ak []byte) error {
	st, err := store.OpenWithKey(l.e.k.dir, ak)
	if err != nil {
		return err
	}
	data, err := st.Load()
	st.Close()
	if err != nil {
		return err
	}
	a := data.Account
	if a == nil || a.Pending || data.Removal != nil {
		return nil
	}
	c := &dsync.Client{Base: a.APIBase, Token: a.Token, Sealer: &dsync.Sealer{AK: ak, KeyID: nf.KeyID}}
	_, err = l.e.k.push(ctx, c, nf, 0)
	return err
}

// once, about five seconds after start: only a lost device key gets here, and a system prompt can't open the vault
// without it, so the person is told to open dokky and type the master password
func (l *lockedPhase) ask(ctx context.Context) {
	if l.e.k.current() == nil {
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(promptDelay):
		notifyLocked(l.e.log, l.shell.notify)
	}
}
