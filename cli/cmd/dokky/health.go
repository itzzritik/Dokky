package main

import (
	"cmp"
	"context"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/buildinfo"
	"github.com/itzzritik/dokky/cli/internal/daemon"
	"github.com/itzzritik/dokky/cli/internal/doctor"
	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/platform"
	"github.com/itzzritik/dokky/cli/internal/tui"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
)

// this computer's checks for the health tab; drives, uploads and sync come live from the daemon
func doctorChecks(ctx context.Context) []tui.Check {
	env := realDoctorEnv()
	var s api.State
	_ = callOnce(api.MState, nil, &s)
	var rel daemon.Release
	var relErr error
	looked := make(chan struct{})
	go func() {
		defer close(looked)
		lctx, cancel := context.WithTimeout(ctx, doctorHTTPTimeout)
		defer cancel()
		rel, relErr = daemon.Latest(lctx, http.DefaultClient, "latest", runtime.GOOS+"-"+runtime.GOARCH)
	}()
	by := map[string]doctor.Check{}
	for _, c := range doctor.Run(ctx, env) {
		by[c.Name] = c
	}
	<-looked
	out := []tui.Check{serviceCheck(env, by), updatesCheck(s, env.Version, rel, relErr, by["github.com"]),
		cacheCheck(env, s, by["disk space"]), keychainCheck(by["keychain"])}
	if runtime.GOOS != "darwin" {
		return append(out, driverCheck(by))
	}
	out = append(out, finderCheck(s))
	if m := by["macos version"]; !m.OK && !m.Warn {
		out = append(out, tui.Check{Name: "macOS", Status: "Too old", Detail: upper(m.Detail) + ".", Fix: upper(m.Fix) + ".", Tone: tui.Bad})
	}
	return out
}

func serviceCheck(env doctor.Env, by map[string]doctor.Check) tui.Check {
	c := tui.Check{Name: "Background service", Status: "Running", Detail: "Keeps your drives open and uploads going, even with this window closed.", Tone: tui.Good}
	run, same := by["daemon running"], by["daemon version"]
	v, login := strings.TrimPrefix(run.Detail, "version "), "Yes"
	if !env.DaemonInstalled {
		login = "No"
	}
	switch {
	case !run.OK:
		v = ""
		c.Status, c.Tone, c.Auto = "Not running", tui.Bad, "Fix all starts the background service."
	// a dev build never matches the installed service, and restarting it every open would only disturb the drives
	case !same.OK && !strings.Contains(env.Version, "dev"):
		c.Status, c.Tone, c.Auto, c.Reopen = "Out of date", tui.Warn, "Fix all restarts dokky so both run the same version.", true
	case !env.DaemonInstalled:
		c.Status, c.Tone, c.Auto = "Off at login", tui.Warn, "Fix all sets the background service to start at login."
	}
	if c.Auto != "" {
		c.Repair = restartService
	}
	c.Facts = [][2]string{{"Version", v}, {"At login", login}, {"Log", core.Home(env.LogPath)}}
	if run.OK && !same.OK {
		c.Facts = append(c.Facts, [2]string{"This window", env.Version})
	}
	return c
}

func updatesCheck(s api.State, cli string, r daemon.Release, err error, gh doctor.Check) tui.Check {
	c := tui.Check{Name: "Updates", Status: "Up to date", Detail: "dokky updates with npm in the background and switches when nothing is uploading.", Tone: tui.Good}
	cur, auto := cmp.Or(s.Version, cli), "Off"
	if s.Settings.AutoUpdate {
		auto = "On"
	}
	c.Facts = [][2]string{{"Installed", cur}, {"Latest", r.Version}, {"Automatic", auto}}
	switch {
	case err != nil:
		c.Status, c.Tone, c.Fix = "Can't check", tui.Warn, "Check your internet connection, proxy and firewall."
		c.Facts = append(c.Facts, [2]string{"Error", err.Error()})
	case !gh.OK:
		c.Status, c.Tone, c.Fix = "Can't download", tui.Warn, "github.com can't be reached. Check your internet connection, proxy and firewall."
	case buildinfo.Newer(r.Version, cur) && s.Settings.AutoUpdate:
		c.Status, c.Detail = r.Version+" on the way", "dokky installs "+r.Version+" in the background and switches when nothing is uploading."
	case buildinfo.Newer(r.Version, cur):
		c.Status, c.Detail = r.Version+" is out", "Press u to update, or turn on Automatic updates in Settings."
	}
	return c
}

func cacheCheck(env doctor.Env, s api.State, disk doctor.Check) tui.Check {
	free := core.Size(int64(min(env.FreeBytes, 1<<62)))
	c := tui.Check{Name: "Cache", Status: free + " free", Detail: "Files you open are kept on this computer, so they open fast and work offline.", Tone: tui.Good}
	used, floor := "", ""
	if s.CacheBytes > 0 {
		used = core.Size(s.CacheBytes)
	}
	if env.MinFreeBytes > 0 {
		floor = core.Size(int64(env.MinFreeBytes))
	}
	c.Facts = [][2]string{{"In use", used}, {"Free on disk", free}, {"Kept free", floor}, {"Folder", core.Home(s.CacheDir)}}
	if disk.Warn {
		c.Status, c.Tone = "Low on space", tui.Warn
		c.Auto, c.Repair = "Fix all clears cached files. Files not uploaded yet stay.", func() error { return callOnce(api.MCacheClear, nil, nil) }
	}
	return c
}

func keychainCheck(k doctor.Check) tui.Check {
	where := map[string]string{"darwin": "macOS keychain", "windows": "Windows Credential Manager"}[runtime.GOOS]
	where = cmp.Or(where, "system keyring")
	c := tui.Check{Name: "Keychain", Status: "Key saved", Detail: "Your drive passwords are stored encrypted. The key that opens them lives in the " + where + ".", Tone: tui.Good}
	state, _ := platform.StateDir()
	c.Facts = [][2]string{{"Key in", where}, {"Passwords", core.Home(filepath.Join(state, "store.enc"))}}
	switch {
	case k.Warn:
		c.Status, c.Tone, c.Fix = "Key in a file", tui.Warn, upper(k.Fix)+"."
		c.Facts[0][1] = "A file, no keyring found"
	case !k.OK:
		c.Status, c.Tone, c.Fix = "Can't read key", tui.Bad, upper(k.Fix)+"."
		c.Facts = append(c.Facts, [2]string{"Error", k.Detail})
	}
	return c
}

// every drive sits under Dokky in Finder; a hosts line with more than Dokky on it is left from per-drive names
func finderCheck(s api.State) tui.Check {
	c := tui.Check{Name: "Finder name", Status: "Shows as " + platform.FinderHost, Detail: "Your drives show under " + platform.FinderHost + " in Finder, not localhost.", Tone: tui.Good,
		Facts: [][2]string{{"Name", platform.FinderHost + " is 127.0.0.1"}, {"Set in", "/etc/hosts"}}}
	if platform.FinderHostClean() {
		return c
	}
	c.Status, c.Tone = "Old drive names", tui.Warn
	c.Auto = "Fix all puts back the single Dokky name. macOS asks for your password."
	if !platform.FinderHostReady() {
		c.Status, c.Auto = "Shows as localhost", "Fix all adds the Dokky name. macOS asks for your password."
	}
	c.Repair = func() error {
		if err := platform.EnsureFinderHost(); err != nil {
			return err
		}
		// a connected drive keeps its old server until it connects again; idle ones do that now
		for _, d := range s.Drives {
			if d.State == api.Connected && d.Uploads == 0 {
				_ = callOnce("unmount", map[string]string{"id": d.ID}, nil)
				_ = callOnce("mount", map[string]string{"id": d.ID}, nil)
			}
		}
		return nil
	}
	return c
}

func driverCheck(by map[string]doctor.Check) tui.Check {
	d, name := by["fuse3"], "FUSE 3"
	if runtime.GOOS == "windows" {
		// the same lookup mounting uses, so this can't pass while mounts fail
		d, name = doctor.Check{OK: true}, "WinFsp"
		if err := engine.CheckDriver(); err != nil {
			d = doctor.Check{Detail: err.Error(), Fix: "Install it with: winget install -e --id WinFsp.WinFsp"}
		}
	}
	c := tui.Check{Name: "File system driver", Status: "Installed", Detail: "Lets your drives show up on this computer.", Tone: tui.Good, Facts: [][2]string{{"Driver", name}}}
	if !d.OK {
		c.Status, c.Tone, c.Fix = "Missing", tui.Bad, upper(d.Fix)
		c.Facts = append(c.Facts, [2]string{"Error", d.Detail})
		if runtime.GOOS == "windows" {
			c.Auto, c.Repair = "Fix all installs WinFsp. Windows asks for permission.", platform.InstallWinFsp
		}
	}
	return c
}

func upper(s string) string {
	if s == "" || strings.HasPrefix(s, "macOS") {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
