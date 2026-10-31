package sync

import (
	"maps"
	"slices"

	"github.com/itzzritik/dokky/cli/internal/store"
)

type Change struct {
	Kind         string
	MountID      string
	ServiceID    string
	NeedsRemount bool
}

func Apply(d store.Data, s Snapshot) (store.Data, []Change) {
	oldSvc := map[string]store.Service{}
	for _, v := range d.Services {
		oldSvc[v.ID] = v
	}
	oldMnt := map[string]store.Mount{}
	for _, m := range d.Mounts {
		oldMnt[m.ID] = m
	}
	snapSvc := map[string]store.Service{}
	for _, v := range s.Services {
		snapSvc[v.ID] = v
	}
	var snapMnt []store.Mount
	inSnap := map[string]bool{}
	for _, m := range s.Mounts {
		if _, ok := snapSvc[m.ServiceID]; ok {
			snapMnt = append(snapMnt, m)
			inSnap[m.ID] = true
		}
	}

	out := d
	out.Local = maps.Clone(d.Local)
	if out.Local == nil {
		out.Local = map[string]store.LocalMount{}
	}
	out.Services = slices.Clone(s.Services)
	out.Mounts = slices.Clone(snapMnt)
	var changes []Change
	mountChange := map[string]int{}
	mark := func(m store.Mount, kind string, remount bool) {
		if i, ok := mountChange[m.ID]; ok {
			changes[i].NeedsRemount = changes[i].NeedsRemount || remount
			return
		}
		changes = append(changes, Change{Kind: kind, MountID: m.ID, ServiceID: m.ServiceID, NeedsRemount: remount})
		mountChange[m.ID] = len(changes) - 1
	}

	for _, v := range s.Services {
		o, ok := oldSvc[v.ID]
		switch {
		case !ok:
			changes = append(changes, Change{Kind: "added", ServiceID: v.ID})
		case o.Version != v.Version:
			changes = append(changes, Change{Kind: "changed", ServiceID: v.ID})
		}
	}
	svcRemount := map[string]bool{}
	for _, v := range s.Services {
		if o, ok := oldSvc[v.ID]; ok && o.Version != v.Version && serviceNeedsRemount(o, v) {
			svcRemount[v.ID] = true
		}
	}

	for _, m := range snapMnt {
		o, ok := oldMnt[m.ID]
		mounted := d.Local[m.ID].Mounted
		wasRemoved := d.Local[m.ID].RemovedRemotely
		if wasRemoved {
			l := out.Local[m.ID]
			l.RemovedRemotely = false
			out.Local[m.ID] = l
		}
		switch {
		case !ok:
			mark(m, "added", false)
		case o.Version != m.Version:
			mark(m, "changed", mounted && mountNeedsRemount(o, m))
		}
		if wasRemoved {
			mark(m, "changed", false)
		}
		if ok && mounted && svcRemount[m.ServiceID] {
			mark(m, "changed", true)
		}
	}

	needed := map[string]bool{}
	for _, m := range d.Mounts {
		if inSnap[m.ID] {
			continue
		}
		l := d.Local[m.ID]
		if !l.Mounted {
			delete(out.Local, m.ID)
			changes = append(changes, Change{Kind: "removed", MountID: m.ID, ServiceID: m.ServiceID})
			continue
		}
		out.Mounts = append(out.Mounts, m)
		needed[m.ServiceID] = true
		if !l.RemovedRemotely {
			l.RemovedRemotely = true
			out.Local[m.ID] = l
			changes = append(changes, Change{Kind: "removed_kept", MountID: m.ID, ServiceID: m.ServiceID})
		}
	}
	for _, v := range d.Services {
		if _, ok := snapSvc[v.ID]; ok {
			continue
		}
		if needed[v.ID] {
			out.Services = append(out.Services, v)
		} else {
			changes = append(changes, Change{Kind: "removed", ServiceID: v.ID})
		}
	}
	if d.Account != nil {
		a := *d.Account
		a.Rev = s.Rev
		out.Account = &a
	}
	return out, changes
}

func DropRemoved(d store.Data, mountID string) store.Data {
	if !d.Local[mountID].RemovedRemotely {
		return d
	}
	out := d
	out.Local = maps.Clone(d.Local)
	delete(out.Local, mountID)
	out.Mounts = nil
	used := map[string]bool{}
	var serviceID string
	for _, m := range d.Mounts {
		if m.ID == mountID {
			serviceID = m.ServiceID
			continue
		}
		out.Mounts = append(out.Mounts, m)
		used[m.ServiceID] = true
	}
	if !used[serviceID] && d.Account != nil {
		a := *d.Account
		a.Rev = ""
		out.Account = &a
	}
	return out
}

func mountNeedsRemount(o, n store.Mount) bool {
	return o.Name != n.Name || o.ServiceID != n.ServiceID || o.RemotePath != n.RemotePath || o.VFS != n.VFS
}

func serviceNeedsRemount(o, n store.Service) bool {
	return o.Type != n.Type || !maps.Equal(o.Options, n.Options) || !maps.Equal(withoutToken(o.Secrets), withoutToken(n.Secrets))
}

func withoutToken(m map[string]string) map[string]string {
	c := maps.Clone(m)
	delete(c, "token")
	return c
}
