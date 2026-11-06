package sync

import (
	"testing"

	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/stretchr/testify/require"
)

func svc(id string, v int64) store.Service {
	return store.Service{ID: id, Name: id, Type: "sftp", Options: map[string]string{"host": "h"}, Secrets: map[string]string{"pass": "p", "token": "t"}, Version: v}
}

func mnt(id, service string, v int64) store.Mount {
	return store.Mount{ID: id, Name: id, ServiceID: service, RemotePath: "/", VFS: store.DefaultVFS(), Version: v}
}

func mountedLocal(ids ...string) map[string]store.LocalMount {
	l := map[string]store.LocalMount{}
	for _, id := range ids {
		l[id] = store.LocalMount{MountPoint: "/m/" + id, Mounted: true}
	}
	return l
}

func find(cs []Change, kind, mountID, serviceID string) *Change {
	for i := range cs {
		if cs[i].Kind == kind && cs[i].MountID == mountID && cs[i].ServiceID == serviceID {
			return &cs[i]
		}
	}
	return nil
}

func TestApplyAddsNewItemsUnmounted(t *testing.T) {
	d, cs := Apply(store.Data{}, Snapshot{Services: []store.Service{svc("s1", 1)}, Mounts: []store.Mount{mnt("m1", "s1", 1)}})
	require.Len(t, d.Services, 1)
	require.Len(t, d.Mounts, 1)
	require.False(t, d.Local["m1"].Mounted)
	require.NotNil(t, find(cs, "added", "m1", "s1"))
	require.NotNil(t, find(cs, "added", "", "s1"))
	for _, c := range cs {
		require.False(t, c.NeedsRemount)
	}
}

func TestApplyUnchangedVersionIsNoChange(t *testing.T) {
	old := store.Data{Services: []store.Service{svc("s1", 1)}, Mounts: []store.Mount{mnt("m1", "s1", 1)}, Local: mountedLocal("m1")}
	d, cs := Apply(old, Snapshot{Services: old.Services, Mounts: old.Mounts})
	require.Empty(t, cs)
	require.Equal(t, old.Local, d.Local)
}

func TestApplyChangedMountedMountNeedsRemount(t *testing.T) {
	old := store.Data{Services: []store.Service{svc("s1", 1)}, Mounts: []store.Mount{mnt("m1", "s1", 1)}, Local: mountedLocal("m1")}
	m := mnt("m1", "s1", 2)
	m.RemotePath = "/other"
	d, cs := Apply(old, Snapshot{Services: old.Services, Mounts: []store.Mount{m}})
	require.Equal(t, "/other", d.Mounts[0].RemotePath)
	c := find(cs, "changed", "m1", "s1")
	require.NotNil(t, c)
	require.True(t, c.NeedsRemount)
}

func TestApplyChangedUnmountedMountNoRemount(t *testing.T) {
	old := store.Data{Services: []store.Service{svc("s1", 1)}, Mounts: []store.Mount{mnt("m1", "s1", 1)}}
	m := mnt("m1", "s1", 2)
	m.Name = "renamed"
	_, cs := Apply(old, Snapshot{Services: old.Services, Mounts: []store.Mount{m}})
	c := find(cs, "changed", "m1", "s1")
	require.NotNil(t, c)
	require.False(t, c.NeedsRemount)
}

func TestApplyServiceChangeMarksMountedMounts(t *testing.T) {
	old := store.Data{
		Services: []store.Service{svc("s1", 1)},
		Mounts:   []store.Mount{mnt("m1", "s1", 1), mnt("m2", "s1", 1)},
		Local:    mountedLocal("m1"),
	}
	s := svc("s1", 2)
	s.Options = map[string]string{"host": "new"}
	_, cs := Apply(old, Snapshot{Services: []store.Service{s}, Mounts: old.Mounts})
	c := find(cs, "changed", "m1", "s1")
	require.NotNil(t, c)
	require.True(t, c.NeedsRemount)
	require.Nil(t, find(cs, "changed", "m2", "s1"))
	require.NotNil(t, find(cs, "changed", "", "s1"))
}

func TestApplyTokenOnlyChangeNoRemount(t *testing.T) {
	old := store.Data{Services: []store.Service{svc("s1", 1)}, Mounts: []store.Mount{mnt("m1", "s1", 1)}, Local: mountedLocal("m1")}
	s := svc("s1", 2)
	s.Secrets = map[string]string{"pass": "p", "token": "newer"}
	d, cs := Apply(old, Snapshot{Services: []store.Service{s}, Mounts: old.Mounts})
	require.Equal(t, "newer", d.Services[0].Secrets["token"])
	for _, c := range cs {
		require.False(t, c.NeedsRemount)
	}
}

func TestApplyOtherSecretChangeNeedsRemount(t *testing.T) {
	old := store.Data{Services: []store.Service{svc("s1", 1)}, Mounts: []store.Mount{mnt("m1", "s1", 1)}, Local: mountedLocal("m1")}
	s := svc("s1", 2)
	s.Secrets = map[string]string{"pass": "changed", "token": "t"}
	_, cs := Apply(old, Snapshot{Services: []store.Service{s}, Mounts: old.Mounts})
	require.True(t, find(cs, "changed", "m1", "s1").NeedsRemount)
}

func TestApplyMissingUnmountedMountStartsRemoving(t *testing.T) {
	old := store.Data{
		Services: []store.Service{svc("s1", 1)},
		Mounts:   []store.Mount{mnt("m1", "s1", 1)},
		Local:    map[string]store.LocalMount{"m1": {MountPoint: "/m/m1"}},
	}
	d, cs := Apply(old, Snapshot{})
	require.Len(t, d.Mounts, 1)
	require.Len(t, d.Services, 1)
	require.True(t, d.Local["m1"].Removing)
	require.NotNil(t, find(cs, "removing", "m1", "s1"))
}

func TestApplyMissingMountedMountKeptWithService(t *testing.T) {
	old := store.Data{Services: []store.Service{svc("s1", 1)}, Mounts: []store.Mount{mnt("m1", "s1", 1)}, Local: mountedLocal("m1")}
	d, cs := Apply(old, Snapshot{})
	require.Len(t, d.Mounts, 1)
	require.Len(t, d.Services, 1)
	require.Equal(t, "p", d.Services[0].Secrets["pass"])
	require.True(t, d.Local["m1"].Removing)
	require.True(t, d.Local["m1"].Mounted)
	require.NotNil(t, find(cs, "removing", "m1", "s1"))
	_, again := Apply(d, Snapshot{})
	require.Empty(t, again)
}

func TestApplyOrphanSnapshotMountIgnored(t *testing.T) {
	d, cs := Apply(store.Data{}, Snapshot{Mounts: []store.Mount{mnt("m1", "gone", 1)}})
	require.Empty(t, d.Mounts)
	require.Empty(t, cs)
}

func TestApplyOrphanMountOfMountedLocalIsKept(t *testing.T) {
	old := store.Data{Services: []store.Service{svc("s1", 1)}, Mounts: []store.Mount{mnt("m1", "s1", 1)}, Local: mountedLocal("m1")}
	d, _ := Apply(old, Snapshot{Mounts: []store.Mount{mnt("m1", "s1", 2)}})
	require.Len(t, d.Mounts, 1)
	require.Equal(t, int64(1), d.Mounts[0].Version)
	require.True(t, d.Local["m1"].Removing)
}

func TestApplyNeverAutoMounts(t *testing.T) {
	d, _ := Apply(store.Data{Local: map[string]store.LocalMount{}}, Snapshot{Services: []store.Service{svc("s1", 1)}, Mounts: []store.Mount{mnt("m1", "s1", 1)}})
	require.False(t, d.Local["m1"].Mounted)
}

func TestApplyDoesNotMutateInput(t *testing.T) {
	old := store.Data{
		Services: []store.Service{svc("s1", 1), svc("s2", 1)},
		Mounts:   []store.Mount{mnt("m1", "s1", 1), mnt("m2", "s2", 1)},
		Local:    mountedLocal("m1", "m2"),
	}
	Apply(old, Snapshot{})
	require.Len(t, old.Services, 2)
	require.Len(t, old.Mounts, 2)
	require.False(t, old.Local["m1"].Removing)
	require.Len(t, old.Local, 2)
}

func TestApplyMountMovedToOtherServiceNeedsRemount(t *testing.T) {
	old := store.Data{Services: []store.Service{svc("s1", 1), svc("s2", 1)}, Mounts: []store.Mount{mnt("m1", "s1", 1)}, Local: mountedLocal("m1")}
	_, cs := Apply(old, Snapshot{Services: old.Services, Mounts: []store.Mount{mnt("m1", "s2", 2)}})
	require.True(t, find(cs, "changed", "m1", "s2").NeedsRemount)
}

func TestApplyReappearedRemovedMountEmitsChanged(t *testing.T) {
	old := store.Data{Services: []store.Service{svc("s1", 1)}, Mounts: []store.Mount{mnt("m1", "s1", 1)}, Local: mountedLocal("m1")}
	d, _ := Apply(old, Snapshot{})
	d, cs := Apply(d, Snapshot{Services: old.Services, Mounts: old.Mounts})
	c := find(cs, "changed", "m1", "s1")
	require.NotNil(t, c)
	require.False(t, c.NeedsRemount)
	require.False(t, d.Local["m1"].Removing)
}
