package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/platform"
	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/rclone/rclone/fs"
	"github.com/stretchr/testify/require"
)

var (
	npmRoot = filepath.Join(string(filepath.Separator), "usr", "local", "lib", "node_modules")
	npmPkg  = filepath.Join(npmRoot, "dokky")
)

func existsIn(paths ...string) func(string) bool {
	return func(p string) bool { return slices.Contains(paths, p) }
}

func TestCheckUpdateGapKeeps(t *testing.T) {
	var w InstallWatch
	recs := []string{npmPkg}
	up, down := existsIn(npmRoot, npmPkg), existsIn(npmRoot)
	require.Equal(t, Keep, w.Check(t0, recs, up))
	require.Equal(t, Keep, w.Check(t0.Add(30*time.Second), recs, down))
	require.Equal(t, Keep, w.Check(t0.Add(time.Minute), recs, up))
	require.Equal(t, Keep, w.Check(t0.Add(11*time.Minute), recs, down), "the update gap must not count toward a later removal")
	require.Equal(t, Keep, w.Check(t0.Add(21*time.Minute-time.Second), recs, down))
	require.Equal(t, Wipe, w.Check(t0.Add(21*time.Minute), recs, down))
}

func TestCheckRemovedTenMinutesWipes(t *testing.T) {
	var w InstallWatch
	recs, down := []string{npmPkg}, existsIn(npmRoot)
	require.Equal(t, Keep, w.Check(t0, recs, down))
	require.Equal(t, Keep, w.Check(t0.Add(10*time.Minute-time.Second), recs, down))
	require.Equal(t, Wipe, w.Check(t0.Add(10*time.Minute), recs, down))
}

func TestCheckLauncherRunResetsTimer(t *testing.T) {
	recs, down := []string{npmPkg}, existsIn(npmRoot)

	t.Run("installs.json rewritten", func(t *testing.T) {
		var w InstallWatch
		w.noteMtime(t0)
		require.Equal(t, Keep, w.Check(t0, recs, down))
		require.Equal(t, Keep, w.Check(t0.Add(9*time.Minute), recs, down))
		w.noteMtime(t0.Add(9 * time.Minute))
		require.Equal(t, Keep, w.Check(t0.Add(10*time.Minute), recs, down))
		require.Equal(t, Keep, w.Check(t0.Add(20*time.Minute-time.Second), recs, down))
		require.Equal(t, Wipe, w.Check(t0.Add(20*time.Minute), recs, down))
	})

	t.Run("new record", func(t *testing.T) {
		var w InstallWatch
		otherRoot := filepath.Join(string(filepath.Separator), "opt", "node", "lib", "node_modules")
		more := []string{npmPkg, filepath.Join(otherRoot, "dokky")}
		bothDown := existsIn(npmRoot, otherRoot)
		require.Equal(t, Keep, w.Check(t0, recs, down))
		require.Equal(t, Keep, w.Check(t0.Add(9*time.Minute), recs, down))
		require.Equal(t, Keep, w.Check(t0.Add(9*time.Minute+30*time.Second), more, bothDown))
		require.Equal(t, Keep, w.Check(t0.Add(19*time.Minute), more, bothDown))
		require.Equal(t, Wipe, w.Check(t0.Add(19*time.Minute+30*time.Second), more, bothDown))
	})
}

func TestCheckNodeRemovedDropsRecordNeverWipes(t *testing.T) {
	var w InstallWatch
	gone := existsIn()
	require.Equal(t, DropRecord, w.Check(t0, []string{npmPkg}, gone))
	require.Equal(t, []string{npmPkg}, w.drop)
	require.Equal(t, DropRecord, w.Check(t0.Add(time.Hour), []string{npmPkg}, gone))
	require.Equal(t, Keep, w.Check(t0.Add(2*time.Hour), []string{}, gone))
	require.Equal(t, Keep, w.Check(t0.Add(48*time.Hour), []string{}, gone))
}

func TestCheckOneRecordStillPresentKeeps(t *testing.T) {
	var w InstallWatch
	otherRoot := filepath.Join(string(filepath.Separator), "home", "me", ".bun", "install", "global", "node_modules")
	otherPkg := filepath.Join(otherRoot, "dokky")
	recs, exists := []string{npmPkg, otherPkg}, existsIn(npmRoot, otherRoot, otherPkg)
	require.Equal(t, Keep, w.Check(t0, recs, exists))
	require.Equal(t, Keep, w.Check(t0.Add(10*time.Minute-time.Second), recs, exists))

	require.Equal(t, DropRecord, w.Check(t0.Add(10*time.Minute), recs, exists), "a removal while another install has dokky is forgotten")
	require.Equal(t, []string{npmPkg}, w.drop)
	require.Equal(t, Keep, w.Check(t0.Add(10*time.Minute), []string{otherPkg}, exists))

	nodeGone := existsIn(npmRoot)
	require.Equal(t, DropRecord, w.Check(t0.Add(time.Hour), []string{otherPkg}, nodeGone))
	require.Equal(t, Keep, w.Check(t0.Add(time.Hour), []string{}, nodeGone))
	require.Equal(t, Keep, w.Check(t0.Add(48*time.Hour), []string{}, nodeGone), "removing that Node later is not an uninstall")
}

func TestCheckRemovalReappearingKeepsRecord(t *testing.T) {
	var w InstallWatch
	otherRoot := filepath.Join(string(filepath.Separator), "opt", "node", "lib", "node_modules")
	recs := []string{npmPkg, filepath.Join(otherRoot, "dokky")}
	oneGone, both := existsIn(npmRoot, otherRoot, recs[1]), existsIn(npmRoot, npmPkg, otherRoot, recs[1])
	require.Equal(t, Keep, w.Check(t0, recs, oneGone))
	require.Equal(t, Keep, w.Check(t0.Add(5*time.Minute), recs, both))
	require.Equal(t, Keep, w.Check(t0.Add(6*time.Minute), recs, oneGone))
	require.Equal(t, Keep, w.Check(t0.Add(16*time.Minute-time.Second), recs, oneGone))
	require.Equal(t, DropRecord, w.Check(t0.Add(16*time.Minute), recs, oneGone))
}

func TestCheckJunkRecordsDropped(t *testing.T) {
	var w InstallWatch
	junk := []string{"", "dokky", filepath.Join("lib", "node_modules", "dokky")}
	anything := func(string) bool { return true }
	require.Equal(t, DropRecord, w.Check(t0, junk, existsIn(".", "lib", filepath.Join("lib", "node_modules"))))
	require.Equal(t, junk, w.drop)
	require.Equal(t, DropRecord, w.Check(t0, append([]string{npmPkg}, junk...), anything))
	require.Equal(t, junk, w.drop)
	require.Equal(t, Keep, w.Check(t0.Add(time.Hour), []string{}, existsIn(".")))
}

func TestCheckNoRecordsNeverWipes(t *testing.T) {
	var w InstallWatch
	require.Equal(t, Keep, w.Check(t0, nil, existsIn()))
	require.Equal(t, Keep, w.Check(t0.Add(24*time.Hour), nil, existsIn()))
}

func TestCheckPnpmUpdateKeeps(t *testing.T) {
	var w InstallWatch
	root := filepath.Join(string(filepath.Separator), "home", "me", ".local", "share", "pnpm", "global", "5", "node_modules")
	old := filepath.Join(root, ".pnpm", "dokky@1.0.0", "node_modules", "dokky")
	link := filepath.Join(root, "dokky")
	updated := existsIn(root, link)
	require.Equal(t, Keep, w.Check(t0, []string{old}, updated))
	require.Equal(t, Keep, w.Check(t0.Add(time.Hour), []string{old}, updated), "the versioned folder going is an update while the global link stays")
	removed := existsIn(root)
	require.Equal(t, Keep, w.Check(t0.Add(2*time.Hour), []string{old}, removed))
	require.Equal(t, Wipe, w.Check(t0.Add(2*time.Hour+10*time.Minute), []string{old}, removed))
}

func writeRecords(t *testing.T, state string, recs ...string) {
	t.Helper()
	b, err := json.Marshal(recs)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(state, "installs.json"), b, 0o644))
}

func readRecords(t *testing.T, state string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(state, "installs.json"))
	require.NoError(t, err)
	var recs []string
	require.NoError(t, json.Unmarshal(b, &recs))
	return recs
}

func TestCheckInstallsDropsGoneRecords(t *testing.T) {
	d, _, _ := newTestDaemon(t, store.Fresh())
	state, base := d.upd.paths.State, t.TempDir()
	npm := filepath.Join(base, "npm", "node_modules")
	nvm := filepath.Join(base, "nvm", "node_modules")
	require.NoError(t, os.MkdirAll(npm, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(nvm, "dokky"), 0o755))
	recs := []string{filepath.Join(npm, "dokky"), filepath.Join(nvm, "dokky")}
	writeRecords(t, state, recs...)

	var w InstallWatch
	require.Equal(t, Keep, d.checkInstalls(&w, t0, pathExists))
	require.NoError(t, os.Remove(recs[1]))
	require.Equal(t, Keep, d.checkInstalls(&w, t0.Add(time.Minute), pathExists))
	require.NoError(t, os.RemoveAll(nvm))
	require.Equal(t, Keep, d.checkInstalls(&w, t0.Add(5*time.Minute), pathExists))
	require.Equal(t, recs[:1], readRecords(t, state))
	require.Equal(t, Wipe, d.checkInstalls(&w, t0.Add(11*time.Minute), pathExists), "dokky's own rewrite is not a launcher run")
}

func TestCheckInstallsWithoutRecordsKeeps(t *testing.T) {
	d, _, _ := newTestDaemon(t, store.Fresh())
	var w InstallWatch
	require.Equal(t, Keep, d.checkInstalls(&w, t0, pathExists))
	require.Equal(t, Keep, d.checkInstalls(&w, t0.Add(24*time.Hour), pathExists))

	require.NoError(t, os.WriteFile(filepath.Join(d.upd.paths.State, "installs.json"), []byte(`["half`), 0o644))
	require.Equal(t, Keep, d.checkInstalls(&w, t0.Add(48*time.Hour), pathExists))
}

func TestCheckInstallsChecksOnceMoreBeforeWipe(t *testing.T) {
	d, _, _ := newTestDaemon(t, store.Fresh())
	root := filepath.Join(t.TempDir(), "node_modules")
	pkg := filepath.Join(root, "dokky")
	require.NoError(t, os.MkdirAll(root, 0o755))
	writeRecords(t, d.upd.paths.State, pkg)

	var w InstallWatch
	require.Equal(t, Keep, d.checkInstalls(&w, t0, pathExists))
	asked := 0
	reappears := func(p string) bool {
		if p == root {
			if asked++; asked == 2 {
				require.NoError(t, os.Mkdir(pkg, 0o755))
			}
		}
		return pathExists(p)
	}
	require.Equal(t, Keep, d.checkInstalls(&w, t0.Add(10*time.Minute), reappears))
	require.Equal(t, 2, asked, "a final check runs right before wiping")

	require.NoError(t, os.Remove(pkg))
	require.Equal(t, Keep, d.checkInstalls(&w, t0.Add(11*time.Minute), pathExists))
	require.Equal(t, Wipe, d.checkInstalls(&w, t0.Add(21*time.Minute), pathExists))
}

type cleanEngine struct {
	*fakeEngine
	queue    map[string][]engine.QueueItem
	events   []string
	partials map[string][]string
}

func (c *cleanEngine) Queue(name string) ([]engine.QueueItem, error) {
	c.events = append(c.events, "queue "+name)
	return c.queue[name], nil
}

func (c *cleanEngine) StopJobs() error {
	c.events = append(c.events, "stop jobs")
	return nil
}

func (c *cleanEngine) Unmount(name string) error {
	c.events = append(c.events, "unmount "+name)
	return c.fakeEngine.Unmount(name)
}

func (c *cleanEngine) RemovePartials(_ context.Context, m store.Mount, names []string) error {
	c.events = append(c.events, "partials "+m.Name)
	c.partials[m.ID] = names
	return nil
}

type effects struct {
	steps      []string
	paths      []string
	later      []string
	laterState string
	title      string
	body       string
}

type install struct {
	d                 *Daemon
	eng               *cleanEngine
	st                *store.Store
	fx                *effects
	state, cache, bin string
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
}

func setupInstall(t *testing.T, data store.Data) install {
	t.Helper()
	d, f, st := newTestDaemon(t, data)
	in := install{d: d, st: st, fx: &effects{}, state: d.upd.paths.State, cache: t.TempDir()}
	in.bin = filepath.Join(d.home, ".local", "bin", "dokky")
	d.cacheDir, d.upd.paths = in.cache, Paths{State: in.state, Cache: in.cache, Bin: in.bin}
	for _, p := range []string{
		in.bin,
		filepath.Join(in.state, "dokky.old"),
		filepath.Join(in.state, "updates", "dokky-1.2.3"),
		filepath.Join(in.state, "version"),
		filepath.Join(in.state, "pin"),
		filepath.Join(in.state, "installs.json"),
		filepath.Join(in.state, "path.json"),
		filepath.Join(in.state, "dokky.log"),
		filepath.Join(in.state, "dokky-2026-10-30T10-00-00.000.log"),
		filepath.Join(in.state, "daemon.lock"),
		filepath.Join(in.cache, "vfs", "s1", "queued.mkv"),
		filepath.Join(in.cache, "vfsMeta", "s1", "queued.mkv"),
	} {
		writeFile(t, p)
	}
	if sock := platform.SocketAddr(in.state); filepath.Dir(sock) == in.state {
		writeFile(t, sock)
	}
	in.eng = &cleanEngine{fakeEngine: f, queue: map[string][]engine.QueueItem{}, partials: map[string][]string{}}
	d.eng = in.eng
	step := func(s string) { in.fx.steps = append(in.fx.steps, s) }
	d.unregister = func() error {
		if _, err := os.Lstat(in.bin); err != nil {
			step("unregister after the binary went")
			return nil
		}
		step("unregister")
		return nil
	}
	d.stopService = func() error { step("stop"); return nil }
	d.removePath = func(home, _ string) error { in.fx.paths = append(in.fx.paths, home); return nil }
	d.removeLater = func(left []string, state string) error {
		step("later")
		in.fx.later, in.fx.laterState = left, state
		return nil
	}
	d.notify = func(title, body string) error {
		step("notify")
		in.fx.title, in.fx.body = title, body
		return nil
	}
	return in
}

func entries(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	require.NoError(t, err)
	var names []string
	for _, e := range list {
		names = append(names, e.Name())
	}
	return names
}

func requireOwnedGone(t *testing.T, in install) {
	t.Helper()
	require.NoFileExists(t, in.bin)
	require.NoDirExists(t, in.cache)
	require.Empty(t, in.eng.mounted)
	require.NoDirExists(t, in.d.defaultPoint("media"))
	require.NoDirExists(t, filepath.Join(in.d.home, "dokky"))
	require.Equal(t, []string{"unregister", "notify", "later", "stop"}, in.fx.steps, "the service points at no deleted binary, and stopping it comes last")
	require.Equal(t, "dokky", in.fx.title)
	require.Equal(t, "dokky was uninstalled.", in.fx.body)
	require.Equal(t, []string{in.d.home}, in.fx.paths)
	require.Empty(t, in.fx.later)
	require.Equal(t, in.state, in.fx.laterState)
}

func TestDeepCleanLoggedInWipesEverything(t *testing.T) {
	api := newFakeAPI(t)
	in := setupInstall(t, loggedIn(api.srv.URL))
	in.d.start()
	require.Contains(t, in.eng.mounted, "media")

	DeepClean(in.d)

	require.True(t, api.wasRevoked())
	require.NoDirExists(t, in.state, "the store, its key file and every dokky file are gone")
	requireOwnedGone(t, in)
}

func TestDeepCleanLocalOnlyKeepsStore(t *testing.T) {
	in := setupInstall(t, sample("m1"))
	in.d.start()
	require.Contains(t, in.eng.mounted, "media")

	DeepClean(in.d)

	require.ElementsMatch(t, []string{"store.enc", "store.key"}, entries(t, in.state))
	kept := load(t, in.st)
	require.Equal(t, sample().Services, kept.Services)
	require.Equal(t, sample().Mounts, kept.Mounts)
	require.False(t, kept.Local["m1"].Mounted, "a reinstall brings every mount back unmounted")
	requireOwnedGone(t, in)
}

func TestDeepCleanStopsUploadsAndReportsThem(t *testing.T) {
	in := setupInstall(t, sample("m1"))
	in.d.start()
	items := []engine.QueueItem{
		{Name: "a.mov", Size: 1 << 30, Uploading: true},
		{Name: "dir/b.mov", Size: 1 << 29},
		{Name: "c.mov", Size: 1 << 20},
		{Name: "d.mov", Size: 1 << 20},
	}
	in.eng.queue["media"], in.eng.events = items, nil

	DeepClean(in.d)

	require.Equal(t, []string{"queue media", "stop jobs", "unmount media", "partials media"}, in.eng.events)
	require.Equal(t, map[string][]string{"m1": {"a.mov", "dir/b.mov", "c.mov", "d.mov"}}, in.eng.partials)
	size := fs.SizeSuffix(1<<30 + 1<<29 + 2<<20).ByteUnit()
	require.Equal(t, "dokky was uninstalled. 4 files ("+size+") were not uploaded: a.mov, dir/b.mov, c.mov and 1 more.", in.fx.body)
}

func TestDeepCleanForceUnmountsStuckMount(t *testing.T) {
	in := setupInstall(t, sample("m1"))
	in.d.start()
	in.eng.failUnmount = 1 << 20

	DeepClean(in.d)

	require.Contains(t, in.eng.forced, in.d.defaultPoint("media"))
	require.False(t, in.eng.isMounted(in.d.defaultPoint("media")))
	require.NoDirExists(t, in.d.defaultPoint("media"))
	require.False(t, load(t, in.st).Local["m1"].Mounted, "a reinstall must not remount it")
}

func TestDeepCleanKeepsUserFilesInMountFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows mounts on drive letters")
	}
	in := setupInstall(t, sample())
	media, docs := in.d.defaultPoint("media"), in.d.defaultPoint("docs")
	writeFile(t, filepath.Join(media, "notes.txt"))
	require.NoError(t, os.MkdirAll(docs, 0o755))

	DeepClean(in.d)

	require.FileExists(t, filepath.Join(media, "notes.txt"))
	require.NoDirExists(t, docs)
	require.DirExists(t, filepath.Join(in.d.home, "dokky"))
}

func TestDeepCleanNeverFollowsSymlinks(t *testing.T) {
	in := setupInstall(t, sample())
	outside := t.TempDir()
	keep := filepath.Join(outside, "keep.txt")
	writeFile(t, keep)
	updates := filepath.Join(in.state, "updates")
	require.NoError(t, os.RemoveAll(updates))
	if err := os.Symlink(outside, updates); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("creating symlinks needs developer mode on Windows")
		}
		require.NoError(t, err)
	}
	require.NoError(t, os.Remove(in.bin))
	require.NoError(t, os.Symlink(keep, in.bin))
	home := filepath.Join(in.d.home, "dokky")
	require.NoError(t, os.Symlink(t.TempDir(), home))

	DeepClean(in.d)

	require.FileExists(t, keep)
	_, err := os.Lstat(updates)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Lstat(in.bin)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Lstat(home)
	require.NoError(t, err, "~/dokky goes only when it is an empty folder")
}

func TestDeepCleanLeavesFolderAtFilePath(t *testing.T) {
	in := setupInstall(t, sample())
	version := filepath.Join(in.state, "version")
	require.NoError(t, os.Remove(version))
	writeFile(t, filepath.Join(version, "mine.txt"))
	require.NoError(t, os.Remove(in.bin))
	writeFile(t, filepath.Join(in.bin, "tool"))

	DeepClean(in.d)

	require.FileExists(t, filepath.Join(version, "mine.txt"))
	require.FileExists(t, filepath.Join(in.bin, "tool"))
	require.NoDirExists(t, filepath.Join(in.state, "updates"))
	require.NoFileExists(t, filepath.Join(in.state, "pin"))
}

func TestDeepCleanPathKeptUnlessOnlyOurs(t *testing.T) {
	t.Run("other tools in ~/.local/bin", func(t *testing.T) {
		in := setupInstall(t, sample())
		writeFile(t, filepath.Join(filepath.Dir(in.bin), "uv"))
		DeepClean(in.d)
		require.Empty(t, in.fx.paths)
		require.NoFileExists(t, filepath.Join(in.state, "path.json"))
		require.FileExists(t, filepath.Join(filepath.Dir(in.bin), "uv"))
	})
	t.Run("dokky did not add it", func(t *testing.T) {
		in := setupInstall(t, sample())
		require.NoError(t, os.Remove(filepath.Join(in.state, "path.json")))
		DeepClean(in.d)
		require.Empty(t, in.fx.paths)
	})
}
