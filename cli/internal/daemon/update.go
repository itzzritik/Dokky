package daemon

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-semver/semver"
	"github.com/itzzritik/dokky/cli/internal/buildinfo"
	"github.com/itzzritik/dokky/cli/internal/store"
)

const (
	lookupTimeout   = 20 * time.Second
	npmTimeout      = 15 * time.Minute
	selftestTimeout = time.Minute
	maxMeta         = 1 << 20
	checkTicks      = int(time.Hour / tickInterval)
	openLookup      = 3 * time.Second
)

var (
	registry    = "https://registry.npmjs.org"
	releaseBase = "https://github.com/itzzritik/dokky/releases/download/"
	executable  = os.Executable
	runNPM      = func(ctx context.Context, state string, args ...string) error {
		// npm 11.19+ skips install scripts it wasn't told to allow, and dokky's postinstall is what puts the new binary in place
		cmd, err := NPM(ctx, state, append(args, "--allow-scripts=dokky", "--no-fund", "--no-audit", "--loglevel=error")...)
		if err != nil {
			return err
		}
		cmd.WaitDelay = time.Second
		if out, err := cmd.CombinedOutput(); err != nil {
			out = bytes.TrimSpace(out)
			return fmt.Errorf("npm %s: %w: %s", strings.Join(args, " "), err, out[max(0, len(out)-400):])
		}
		return nil
	}
	runSelftest = func(ctx context.Context, bin, state string) error {
		ctx, cancel := context.WithTimeout(ctx, selftestTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, "selftest", "--state", state)
		cmd.WaitDelay = time.Second
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("the new dokky failed its selftest: %w: %s", err, bytes.TrimSpace(out))
		}
		return nil
	}
)

type Release struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
}

type updater struct {
	mu      sync.Mutex
	paths   Paths
	blocked string // decided at start: after a swap, Linux reports the running exe as dokky.old
	ticks   int
	off     string
}

type updateParams struct {
	To string `json:"to"`
}

type updateReply struct {
	Version string `json:"version"`
	Updated bool   `json:"updated"`
}

func Latest(ctx context.Context, hc *http.Client, version, key string) (Release, error) {
	if version != "latest" && !valid(version) {
		return Release{}, fmt.Errorf("%q is not a dokky version", version)
	}
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	res, err := get(ctx, hc, registry+"/dokky/"+version)
	if err != nil {
		return Release{}, fmt.Errorf("npm registry: %w", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxMeta+1))
	if err == nil && len(body) > maxMeta {
		err = errors.New("reply too large")
	}
	var doc struct {
		Version string `json:"version"`
		Dokky   struct {
			Binaries map[string]Release `json:"binaries"`
		} `json:"dokky"`
	}
	if err == nil {
		err = json.Unmarshal(body, &doc)
	}
	if err != nil {
		return Release{}, fmt.Errorf("npm registry: %w", err)
	}
	if !valid(doc.Version) || version != "latest" && doc.Version != version {
		return Release{}, fmt.Errorf("npm registry sent version %q for %s", doc.Version, version)
	}
	r := doc.Dokky.Binaries[key]
	if r.URL == "" {
		return Release{}, fmt.Errorf("dokky %s has no binary for %s", doc.Version, key)
	}
	if !strings.HasPrefix(r.URL, releaseBase+"v"+doc.Version+"/") {
		return Release{}, fmt.Errorf("dokky %s: %s is not a dokky release download", doc.Version, r.URL)
	}
	if sum, err := hex.DecodeString(r.SHA256); err != nil || len(sum) != sha256.Size {
		return Release{}, fmt.Errorf("dokky %s: bad sha256 %q", doc.Version, r.SHA256)
	}
	r.Version, r.SHA256 = doc.Version, strings.ToLower(r.SHA256)
	return r, nil
}

// npm installs the package; its postinstall downloads that version's binary and writes the version file
func Install(ctx context.Context, r Release, p Paths) error {
	// the CLI that asked may go away; a half-done global install would break npm's copy
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), npmTimeout)
	defer cancel()
	prev := installedVersion(p.State)
	pin, _ := os.ReadFile(filepath.Join(p.State, "pin"))
	// with neither file, every published postinstall, old ones too, replaces the binary
	for _, name := range []string{"version", "pin"} {
		_ = os.Remove(filepath.Join(p.State, name))
	}
	err := runNPM(ctx, p.State, "install", "-g", "dokky@"+r.Version)
	if err == nil && installedVersion(p.State) != r.Version {
		err = fmt.Errorf("npm installed dokky %s but its binary did not change", r.Version)
	}
	if err == nil {
		if err = runSelftest(ctx, p.Bin, p.State); err != nil && valid(prev) {
			if rerr := runNPM(ctx, p.State, "install", "-g", "dokky@"+prev); rerr != nil {
				err = fmt.Errorf("%w; going back to %s also failed: %v", err, prev, rerr)
			}
		}
	}
	if err != nil {
		if installedVersion(p.State) == "" && prev != "" {
			_ = writeAtomic(p.State, "version", prev)
		}
		if len(pin) > 0 {
			_ = writeAtomic(p.State, "pin", string(pin))
		}
	}
	return err
}

func get(ctx context.Context, hc *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		_ = res.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, res.Status)
	}
	return res, nil
}

// the npm launcher leaves the binary alone while <State>/pin exists
func writePin(state, version string) error {
	if version != "" {
		return writeAtomic(state, "pin", version)
	}
	if err := os.Remove(filepath.Join(state, "pin")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func writeAtomic(state, name, content string) error {
	f, err := os.CreateTemp(state, name+"-*")
	if err != nil {
		return err
	}
	_, err = f.WriteString(content)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(state, name))
	}
	if err != nil {
		_ = os.Remove(f.Name())
	}
	return err
}

func installedVersion(state string) string {
	b, _ := os.ReadFile(filepath.Join(state, "version"))
	return strings.TrimSpace(string(b))
}

func valid(v string) bool {
	_, err := semver.NewVersion(v)
	return err == nil
}

func Newer(a, than string) bool {
	va, err := semver.NewVersion(a)
	if err != nil {
		return false
	}
	vb, err := semver.NewVersion(than)
	return err == nil && vb.LessThan(*va)
}

func updateBlocked(bin string) string {
	v, err := semver.NewVersion(buildinfo.Version)
	if err != nil || strings.Contains(string(v.PreRelease), "dev") {
		return "dev build " + buildinfo.Version
	}
	exe, err := executable()
	if err != nil {
		return "cannot find the running dokky: " + err.Error()
	}
	a, aerr := os.Stat(exe)
	b, berr := os.Stat(bin)
	if aerr != nil || berr != nil || !os.SameFile(a, b) {
		return "running " + exe + ", not " + bin
	}
	return ""
}

// unblocked means the exe is Bin, so a version file that disagrees is wrong and would loop restarts
func (d *Daemon) startUpdater(p Paths) {
	d.upd.paths, d.upd.blocked = p, updateBlocked(p.Bin)
	if v := installedVersion(p.State); d.upd.blocked == "" && v != buildinfo.Version {
		d.log.Warn("the version file does not match the running dokky, fixing it", "file", v, "running", buildinfo.Version)
		if err := writeAtomic(p.State, "version", buildinfo.Version); err != nil {
			d.log.Error("write version file", "err", err)
		}
	}
}

func autoOff(s store.Settings) string {
	switch {
	case os.Getenv("DOKKY_NO_UPDATE") == "1":
		return "DOKKY_NO_UPDATE=1"
	case !s.AutoUpdate:
		return "turned off in settings"
	}
	return ""
}

// true: restart onto the newer installed dokky now
func (d *Daemon) autoUpdate(ctx context.Context, now time.Time) bool {
	u := &d.upd
	due := u.ticks%checkTicks == 0
	u.ticks++
	if u.blocked != "" {
		d.updateOff(u.blocked)
		return false
	}
	if Newer(installedVersion(u.paths.State), buildinfo.Version) {
		return d.idle.IsIdle(now)
	}
	why := autoOff(d.settings())
	d.updateOff(why)
	if why == "" && due && u.mu.TryLock() {
		_, _, err := d.install(ctx, "latest")
		u.mu.Unlock()
		if err != nil {
			d.log.Warn("auto update", "err", err)
		}
	}
	// idle was observed before any download; the next tick decides on a fresh observation
	return false
}

func (d *Daemon) updateOff(why string) {
	if why != "" && why != d.upd.off {
		d.log.Info("auto update is off", "why", why)
	}
	d.upd.off = why
}

func (d *Daemon) update(ctx context.Context, p updateParams) (updateReply, error) {
	if p.To != "" && !valid(p.To) {
		return updateReply{}, fmt.Errorf("%q is not a dokky version", p.To)
	}
	d.upd.mu.Lock()
	defer d.upd.mu.Unlock()
	v, ok, err := d.install(ctx, cmp.Or(p.To, "latest"))
	if err != nil {
		return updateReply{}, err
	}
	s := d.settings()
	switch {
	case p.To != "":
		s = store.Settings{PinnedVersion: p.To}
	case s.PinnedVersion != "":
		s = store.Settings{AutoUpdate: true}
	}
	return updateReply{Version: v, Updated: ok}, d.setSettings(s)
}

type checkReply struct {
	Latest string `json:"latest"`
	Newer  bool   `json:"newer"`
}

// the newest dokky on npm and whether it beats the running or installed one; empty while updates can't install
func (d *Daemon) updateCheck(ctx context.Context) (checkReply, error) {
	if d.upd.blocked != "" {
		return checkReply{}, nil
	}
	r, err := Latest(ctx, http.DefaultClient, "latest", runtime.GOOS+"-"+runtime.GOARCH)
	if err != nil {
		return checkReply{}, err
	}
	cur := buildinfo.Version
	if v := installedVersion(d.upd.paths.State); Newer(v, cur) {
		cur = v
	}
	return checkReply{Latest: r.Version, Newer: Newer(r.Version, cur)}, nil
}

type installReply struct {
	Version string `json:"version"` // the installed dokky, newer or not
}

// the TUI's own update: install the newest with npm and leave the restart to it, so it can say so first
func (d *Daemon) installNow(ctx context.Context) (installReply, error) {
	u := &d.upd
	if u.blocked != "" {
		return installReply{}, nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if _, _, err := d.install(ctx, "latest"); err != nil {
		return installReply{}, err
	}
	return installReply{Version: installedVersion(u.paths.State)}, nil
}

type openReply struct {
	Restarting bool   `json:"restarting"`
	Version    string `json:"version"`
}

// dokky is opening: fetch a newer release when auto update is on, then restart onto any newer installed one unless uploads run
func (d *Daemon) updateOnOpen(ctx context.Context) openReply {
	u := &d.upd
	if u.blocked != "" {
		return openReply{}
	}
	if autoOff(d.settings()) == "" && u.mu.TryLock() {
		lctx, cancel := context.WithTimeout(ctx, openLookup)
		r, err := Latest(lctx, http.DefaultClient, "latest", runtime.GOOS+"-"+runtime.GOARCH)
		cancel()
		if err == nil && Newer(r.Version, buildinfo.Version) && Newer(r.Version, installedVersion(u.paths.State)) {
			if _, _, err := d.install(ctx, "latest"); err != nil {
				d.log.Warn("update on open", "err", err)
			}
		}
		u.mu.Unlock()
	}
	v := installedVersion(u.paths.State)
	return openReply{Restarting: Newer(v, buildinfo.Version) && d.uploading() == 0, Version: v}
}

func (d *Daemon) uploading() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, s := range d.stats {
		n += s.UploadsQueued + s.UploadsInProgress
	}
	return n
}

// caller holds upd.mu; "latest" never installs an older or equal version
func (d *Daemon) install(ctx context.Context, version string) (string, bool, error) {
	r, err := Latest(ctx, http.DefaultClient, version, runtime.GOOS+"-"+runtime.GOARCH)
	if err != nil {
		return "", false, err
	}
	if version == "latest" {
		cur := buildinfo.Version
		if v := installedVersion(d.upd.paths.State); Newer(v, cur) {
			cur = v
		}
		if !Newer(r.Version, cur) {
			return cur, false, nil
		}
	}
	if err := Install(ctx, r, d.upd.paths); err != nil {
		return "", false, err
	}
	d.log.Info("installed dokky", "version", r.Version)
	return r.Version, true, nil
}
