package daemon

import (
	"net/url"
	"slices"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/buildinfo"
	"github.com/itzzritik/dokky/cli/internal/logx"
	"github.com/itzzritik/dokky/cli/internal/store"
)

var driveStates = map[string]api.DriveState{
	stateMounted:   api.Connected,
	stateMounting:  api.Connecting,
	stateUnmounted: api.Off,
	stateError:     api.Failed,
}

func (d *Daemon) snapshot() api.State {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := api.State{Version: buildinfo.Version, Build: buildinfo.Stamp(), Drives: []api.Drive{}, Clashes: []api.Clash{}, CacheDir: d.cacheDir, Settings: apiSettings(d.data.Settings)}
	if a := d.data.Account; a != nil {
		s.Account = api.Account{LoggedIn: true, Pending: a.Pending, Name: a.Name, Email: a.Email, LastSyncAt: a.LastSyncAt}
	}
	s.Account.Offline, s.Account.SignedOut = d.offline, d.signedOut
	for _, c := range d.clashes {
		s.Clashes = append(s.Clashes, api.Clash{Kind: c.Kind, Name: c.Name, Local: c.Local, Account: c.Account})
	}
	for _, m := range d.data.Mounts {
		dr := d.drive(m)
		s.CacheBytes += dr.CacheBytes
		s.Drives = append(s.Drives, dr)
	}
	return s
}

// caller holds mu
func (d *Daemon) drive(m store.Mount) api.Drive {
	st, lm := d.state(m.ID), d.data.Local[m.ID]
	dr := api.Drive{
		ID: m.ID, Name: m.Name, ServiceID: m.ServiceID, State: driveStates[st.state], Error: logx.Redact(st.err),
		Folder: d.mountPoint(m), AtLogin: lm.Mounted, AddedAt: m.CreatedAt, LocalFolder: lm.MountPoint,
		CacheMaxSize: d.effective(m).VFS.CacheMaxSize, CacheMaxAge: d.effective(m).VFS.CacheMaxAge, SubPath: m.RemotePath, AlsoOn: d.alsoOn(m.ID),
	}
	if i := slices.IndexFunc(d.data.Services, func(s store.Service) bool { return s.ID == m.ServiceID }); i >= 0 {
		svc := d.data.Services[i]
		dr.ServiceType, dr.ServiceName, dr.Account = svc.Type, svc.Name, accountLabel(svc)
	}
	for _, x := range d.data.Mounts {
		if x.ServiceID == m.ServiceID {
			dr.AccountDrives++
		}
	}
	if st.state == stateMounted {
		ms := d.stats[st.name]
		dr.Folder, dr.CacheBytes, dr.Uploads = st.point, ms.CacheBytes, ms.UploadsQueued+ms.UploadsInProgress
		if st.remount {
			dr.State = api.Reconnect
		}
	}
	if sp, ok := d.space[m.ID]; ok {
		dr.Space = &sp
	}
	if lm.Removing {
		dr.State = api.Leaving
	}
	if p, ok := d.purging[m.ID]; ok {
		dr.State, dr.Progress = api.Removing, p
	}
	return dr
}

func accountLabel(s store.Service) string {
	host := s.Options["host"]
	switch s.Type {
	case "webdav":
		if u, err := url.Parse(s.Options["url"]); err == nil {
			host = u.Hostname()
		}
	case "sftp", "smb", "ftp":
	default:
		return s.Name
	}
	if host == "" {
		return s.Name
	}
	if u := s.Options["user"]; u != "" {
		return u + "@" + host
	}
	return host
}

func apiSettings(s store.Settings) api.Settings {
	return api.Settings{AutoUpdate: s.AutoUpdate, PinnedVersion: s.PinnedVersion, NotifyDisconnect: s.NotifyDisconnect, Chosen: s.Chosen}
}
