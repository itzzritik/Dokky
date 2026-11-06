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
		wasRemoved := d.Local[m.ID].Removing
		if wasRemoved {
			l := out.Local[m.ID]
			l.Removing = false
			out.Local[m.ID] = l
		}
		switch {
		case !ok:
			mark(m, "added", false)
		case o.Version != m.Version:
			mark(m, "changed", mounted && MountNeedsRemount(o, m))
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
		// removed elsewhere: kept until this device has finished with it, then purged here
		l := d.Local[m.ID]
		out.Mounts = append(out.Mounts, m)
		needed[m.ServiceID] = true
		if !l.Removing {
			l.Removing = true
			out.Local[m.ID] = l
			changes = append(changes, Change{Kind: "removing", MountID: m.ID, ServiceID: m.ServiceID})
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

func MountNeedsRemount(o, n store.Mount) bool {
	return o.Name != n.Name || o.ServiceID != n.ServiceID || o.RemotePath != n.RemotePath || SharedVFS(o.VFS) != SharedVFS(n.VFS)
}

func serviceNeedsRemount(o, n store.Service) bool {
	return o.Type != n.Type || !maps.Equal(o.Options, n.Options) || !maps.Equal(withoutToken(o.Secrets), withoutToken(n.Secrets))
}

func withoutToken(m map[string]string) map[string]string {
	c := maps.Clone(m)
	delete(c, "token")
	return c
}

// cache size and age are set per device, so a shared change to them is no reason to remount
func SharedVFS(v store.VFS) store.VFS {
	v.CacheMaxSize, v.CacheMaxAge = "", ""
	return v
}
