package daemon

import (
	"slices"
	"strings"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/engine"
)

var popular = []api.Provider{
	{Type: "drive", Name: "Google Drive", Hint: "Sign in with Google", Group: "cloud"},
	{Type: "onedrive", Name: "OneDrive", Hint: "Sign in with Microsoft", Group: "cloud"},
	{Type: "dropbox", Name: "Dropbox", Hint: "Sign in with Dropbox", Group: "cloud"},
	{Type: "mega", Name: "MEGA", Hint: "Email and password", Group: "cloud"},
	{Type: "protondrive", Name: "Proton Drive", Hint: "Proton account", Group: "cloud"},
	{Type: "pcloud", Name: "pCloud", Hint: "Sign in with pCloud", Group: "cloud"},
	{Type: "box", Name: "Box", Hint: "Sign in with Box", Group: "cloud"},
	{Type: "sftp", Name: "SSH / SFTP", Hint: "Your server", Group: "own"},
	{Type: "smb", Name: "Network share", Hint: "NAS, Windows", Group: "own"},
	{Type: "s3", Name: "S3 storage", Hint: "AWS, R2, MinIO", Group: "own"},
	{Type: "webdav", Name: "Nextcloud", Hint: "Or any WebDAV", Group: "own"},
}

// wrappers over other remotes, and local disks, make no sense as a drive
var notDrives = []string{"alias", "archive", "cache", "chunker", "combine", "compress", "crypt", "hasher", "local", "memory", "union"}

func catalog() ([]api.Provider, error) {
	ps, err := engine.Providers()
	if err != nil {
		return nil, err
	}
	out := make([]api.Provider, 0, len(ps))
	for _, p := range popular {
		if slices.ContainsFunc(ps, func(x engine.Provider) bool { return x.Type == p.Type }) {
			p.Popular = true
			out = append(out, p)
		}
	}
	var rest []api.Provider
	for _, p := range ps {
		if slices.Contains(notDrives, p.Type) || slices.ContainsFunc(popular, func(x api.Provider) bool { return x.Type == p.Type }) {
			continue
		}
		name, hint, _ := strings.Cut(p.Description, " (")
		rest = append(rest, api.Provider{Type: p.Type, Name: name, Hint: strings.TrimSuffix(hint, ")"), Group: "cloud"})
	}
	slices.SortFunc(rest, func(a, b api.Provider) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return append(out, rest...), nil
}
