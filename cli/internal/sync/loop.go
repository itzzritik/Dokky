package sync

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/itzzritik/dokky/cli/internal/store"
)

const (
	KindService = "service"
	KindMount   = "mount"
	KeepLocal   = "local"
	KeepAccount = "account"
)

type Clash struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	LocalID  string `json:"localId"`
	RemoteID string `json:"remoteId"`
	Local    string `json:"local"`
	Account  string `json:"account"`
}

func APIBase() string {
	return strings.TrimRight(cmp.Or(os.Getenv("DOKKY_API_BASE"), defaultBase), "/")
}

// a mount whose service clashes waits until that clash is resolved
func Upload(ctx context.Context, c *Client, d store.Data) (store.Data, []Clash, error) {
	u := &uploader{ctx: ctx, c: c, local: d.Services}
	out := d
	out.Services = slices.Clone(d.Services)
	out.Mounts = slices.Clone(d.Mounts)
	held := map[string]bool{}
	for i, s := range out.Services {
		saved, err := create(ctx, s, c.PutService, func(x store.Service) string { return x.ID })
		switch {
		case errors.Is(err, ErrNameTaken):
			held[s.ID] = true
			err = u.clashService(s)
		case err == nil:
			out.Services[i] = saved
		}
		if err != nil {
			return out, u.clashes, fmt.Errorf("upload service %s: %w", s.Name, err)
		}
	}
	err := u.mounts(out.Mounts, func(m store.Mount) bool { return !held[m.ServiceID] })
	return out, u.clashes, err
}

// keeping local saves it over the account's copy under that id, so the account's mounts stay
func Resolve(ctx context.Context, c *Client, d store.Data, cl Clash, keep string) (store.Data, []Clash, error) {
	if keep != KeepLocal && keep != KeepAccount {
		return d, nil, fmt.Errorf("keep must be %q or %q", KeepLocal, KeepAccount)
	}
	out := d
	out.Services = slices.Clone(d.Services)
	out.Mounts = slices.Clone(d.Mounts)
	u := &uploader{ctx: ctx, c: c, local: d.Services}
	gone := fmt.Errorf("%s %q is no longer in the account, run dokky login again", cl.Kind, cl.Name)

	switch cl.Kind {
	case KindService:
		i := slices.IndexFunc(out.Services, func(s store.Service) bool { return s.ID == cl.LocalID })
		if i < 0 {
			return d, nil, fmt.Errorf("no local service %q", cl.Name)
		}
		if keep == KeepLocal {
			snap, err := u.snapshot()
			if err != nil {
				return d, nil, err
			}
			j := slices.IndexFunc(snap.Services, func(s store.Service) bool { return s.ID == cl.RemoteID })
			if j < 0 {
				return d, nil, gone
			}
			s := out.Services[i]
			s.ID = cl.RemoteID
			saved, err := c.PutService(ctx, s, snap.Services[j].Version)
			if err != nil {
				return d, nil, fmt.Errorf("save service %s: %w", cl.Name, err)
			}
			out.Services[i] = saved
		} else {
			out.Services = slices.Delete(out.Services, i, i+1)
		}
		moved := map[string]bool{}
		for j, m := range out.Mounts {
			if m.ServiceID == cl.LocalID {
				out.Mounts[j].ServiceID = cl.RemoteID
				moved[m.ID] = true
			}
		}
		if err := u.mounts(out.Mounts, func(m store.Mount) bool { return moved[m.ID] }); err != nil {
			return d, nil, err
		}
		return out, u.clashes, nil

	case KindMount:
		i := slices.IndexFunc(out.Mounts, func(m store.Mount) bool { return m.ID == cl.LocalID })
		if i < 0 {
			return d, nil, fmt.Errorf("no local mount %q", cl.Name)
		}
		if keep == KeepAccount {
			out.Mounts = slices.Delete(out.Mounts, i, i+1)
			return out, nil, nil
		}
		snap, err := u.snapshot()
		if err != nil {
			return d, nil, err
		}
		j := slices.IndexFunc(snap.Mounts, func(m store.Mount) bool { return m.ID == cl.RemoteID })
		if j < 0 {
			return d, nil, gone
		}
		m := out.Mounts[i]
		m.ID = cl.RemoteID
		saved, err := c.PutMount(ctx, m, snap.Mounts[j].Version)
		if err != nil {
			return d, nil, fmt.Errorf("save mount %s: %w", cl.Name, err)
		}
		out.Mounts[i] = saved
		return out, nil, nil
	}
	return d, nil, fmt.Errorf("unknown clash kind %q", cl.Kind)
}

type uploader struct {
	ctx     context.Context
	c       *Client
	local   []store.Service
	snap    *Snapshot
	clashes []Clash
}

func (u *uploader) snapshot() (*Snapshot, error) {
	if u.snap == nil {
		s, _, err := u.c.Pull(u.ctx, "")
		if err != nil {
			return nil, fmt.Errorf("read the account: %w", err)
		}
		u.snap = s
	}
	return u.snap, nil
}

func (u *uploader) clashService(local store.Service) error {
	s, err := u.snapshot()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(s.Services, func(x store.Service) bool { return x.Name == local.Name })
	if i < 0 {
		return fmt.Errorf("the name %q is taken but not in the account, try again", local.Name)
	}
	u.clashes = append(u.clashes, Clash{
		Kind: KindService, Name: local.Name, LocalID: local.ID, RemoteID: s.Services[i].ID,
		Local: describeService(local), Account: describeService(s.Services[i]),
	})
	return nil
}

func (u *uploader) clashMount(local store.Mount) error {
	s, err := u.snapshot()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(s.Mounts, func(x store.Mount) bool { return x.Name == local.Name })
	if i < 0 {
		return fmt.Errorf("the name %q is taken but not in the account, try again", local.Name)
	}
	services := slices.Concat(u.local, s.Services)
	u.clashes = append(u.clashes, Clash{
		Kind: KindMount, Name: local.Name, LocalID: local.ID, RemoteID: s.Mounts[i].ID,
		Local: describeMount(local, services), Account: describeMount(s.Mounts[i], s.Services),
	})
	return nil
}

func (u *uploader) mounts(ms []store.Mount, upload func(store.Mount) bool) error {
	for i, m := range ms {
		if !upload(m) {
			continue
		}
		saved, err := create(u.ctx, m, u.c.PutMount, func(x store.Mount) string { return x.ID })
		switch {
		case errors.Is(err, ErrNameTaken):
			err = u.clashMount(m)
		case err == nil:
			ms[i] = saved
		}
		if err != nil {
			return fmt.Errorf("upload mount %s: %w", m.Name, err)
		}
	}
	return nil
}

// a conflict means the id is already in the account; the account's copy wins, as in a pull
func create[T any](ctx context.Context, item T, put func(context.Context, T, int64) (T, error), id func(T) string) (T, error) {
	saved, err := put(ctx, item, 0)
	var ce *ConflictError
	if errors.As(err, &ce) {
		var cur T
		if json.Unmarshal(ce.Current, &cur) == nil && id(cur) == id(item) {
			return cur, nil
		}
	}
	return saved, err
}

func describeService(s store.Service) string {
	parts := []string{s.Type}
	for _, k := range slices.Sorted(maps.Keys(s.Options)) {
		parts = append(parts, k+"="+s.Options[k])
	}
	var set []string
	for _, k := range slices.Sorted(maps.Keys(s.Secrets)) {
		if s.Secrets[k] != "" {
			set = append(set, k)
		}
	}
	if len(set) > 0 {
		parts = append(parts, strings.Join(set, ", ")+" set")
	}
	return strings.Join(parts, ", ")
}

func describeMount(m store.Mount, services []store.Service) string {
	service := m.ServiceID
	if i := slices.IndexFunc(services, func(s store.Service) bool { return s.ID == m.ServiceID }); i >= 0 {
		service = services[i].Name
	}
	return fmt.Sprintf("service %s, path /%s", service, strings.TrimPrefix(m.RemotePath, "/"))
}
