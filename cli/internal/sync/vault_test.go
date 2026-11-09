package sync

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/itzzritik/dokky/cli/internal/vault"
	"github.com/stretchr/testify/require"
)

const oauth = `{"access_token":"a2","refresh_token":"r2","expiry":"2026-11-01T10:00:00Z"}`

func testSealer() *Sealer {
	ak := make([]byte, vault.KeySize)
	_, _ = rand.Read(ak)
	return &Sealer{AK: ak, KeyID: vault.KeyID(ak)}
}

func sealedClient(t *testing.T, s *Sealer, h http.HandlerFunc) *Client {
	t.Helper()
	c := newClient(t, h)
	c.Sealer = s
	return c
}

func wireOf(t *testing.T, s *Sealer, svc store.Service) map[string]any {
	t.Helper()
	sealed, tok, err := vault.SealService(s.AK, svc.ID, svc.Options, svc.Secrets)
	require.NoError(t, err)
	return map[string]any{"id": svc.ID, "name": svc.Name, "type": svc.Type, "version": svc.Version, "updatedAt": "2026-10-30T10:00:00.000Z",
		"keyId": s.KeyID, "sealed": sealed, "sealedToken": tok, "tokenExpiry": nil}
}

func TestSealedPullOpensEverything(t *testing.T) {
	s := testSealer()
	var header string
	c := sealedClient(t, s, func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Get("X-Dokky-Vault")
		w.Header().Set("ETag", `"v4"`)
		_ = json.NewEncoder(w).Encode(map[string]any{"rev": 4, "vault": map[string]any{"keyId": s.KeyID, "version": 2},
			"services": []any{
				wireOf(t, s, store.Service{ID: "s1", Name: "nas", Type: "sftp", Options: map[string]string{"host": "h"}, Secrets: map[string]string{"pass": "p", "token": oauth}, Version: 3}),
				map[string]any{"id": "s2", "name": "old", "type": "webdav", "version": 1, "updatedAt": "2026-10-30T10:00:00.000Z", "keyId": nil, "sealed": nil, "sealedToken": nil, "tokenExpiry": nil,
					"legacy": map[string]any{"options": map[string]string{"url": "http://x"}, "secrets": map[string]string{"pass": "q"}}},
			},
			"mounts": []any{}})
	})
	snap, _, err := c.Pull(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, "1", header)
	require.Equal(t, &VaultRef{KeyID: s.KeyID, Version: 2}, snap.Vault)
	require.Equal(t, []string{"s2"}, snap.Legacy)
	require.Equal(t, "p", snap.Services[0].Secrets["pass"])
	require.Equal(t, oauth, snap.Services[0].Secrets["token"])
	require.Equal(t, "h", snap.Services[0].Options["host"])
	require.Equal(t, int64(3), snap.Services[0].Version)
	require.Equal(t, "q", snap.Services[1].Secrets["pass"])
}

func TestSealedPullRefusesAnotherKey(t *testing.T) {
	s, other := testSealer(), testSealer()
	for name, body := range map[string]map[string]any{
		"the account's key differs": {"rev": 1, "vault": map[string]any{"keyId": other.KeyID, "version": 1}, "services": []any{}, "mounts": []any{}},
		"a service sealed with another key": {"rev": 1, "vault": map[string]any{"keyId": s.KeyID, "version": 1}, "mounts": []any{},
			"services": []any{wireOf(t, other, store.Service{ID: "s1", Name: "nas", Type: "sftp"})}},
		"a service that does not open": {"rev": 1, "vault": map[string]any{"keyId": s.KeyID, "version": 1}, "mounts": []any{},
			"services": []any{func() map[string]any {
				w := wireOf(t, other, store.Service{ID: "s1", Name: "nas", Type: "sftp"})
				w["keyId"] = s.KeyID
				return w
			}()}},
	} {
		t.Run(name, func(t *testing.T) {
			c := sealedClient(t, s, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("ETag", `"v1"`)
				_ = json.NewEncoder(w).Encode(body)
			})
			_, _, err := c.Pull(context.Background(), "")
			require.ErrorIs(t, err, ErrKeyMismatch)
		})
	}
	c := sealedClient(t, s, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(`{"rev":1,"vault":null,"services":[],"mounts":[]}`))
	})
	_, _, err := c.Pull(context.Background(), "")
	require.ErrorIs(t, err, ErrNoVault)
}

func TestSealedPutSendsNoPlaintext(t *testing.T) {
	s := testSealer()
	var raw string
	c := sealedClient(t, s, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		raw = string(b)
		var body sealedBody
		require.NoError(t, json.Unmarshal(b, &body))
		reply := wireOf(t, s, store.Service{ID: "s1", Name: body.Name, Type: body.Type, Version: body.BaseVersion + 1, Options: map[string]string{"host": "h"}, Secrets: map[string]string{"pass": "p", "token": oauth}})
		_ = json.NewEncoder(w).Encode(reply)
	})
	saved, err := c.PutService(context.Background(), store.Service{ID: "s1", Name: "nas", Type: "sftp", Options: map[string]string{"host": "h"}, Secrets: map[string]string{"pass": "p-secret", "token": oauth}}, 2)
	require.NoError(t, err)
	require.Equal(t, int64(3), saved.Version)
	require.Equal(t, oauth, saved.Secrets["token"])
	for _, plain := range []string{"p-secret", "access_token", `"host"`} {
		require.NotContains(t, raw, plain)
	}
	var body sealedBody
	require.NoError(t, json.Unmarshal([]byte(raw), &body))
	require.Equal(t, s.KeyID, body.KeyID)
	require.Equal(t, "2026-11-01T10:00:00Z", *body.TokenExpiry)
	opts, secrets, err := vault.OpenService(s.AK, "s1", body.Sealed, body.SealedToken)
	require.NoError(t, err)
	require.Equal(t, "h", opts["host"])
	require.Equal(t, oauth, secrets["token"])
}

func TestSealedConflictCurrentIsOpened(t *testing.T) {
	s := testSealer()
	c := sealedClient(t, s, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "conflict", "message": "first"},
			"current": wireOf(t, s, store.Service{ID: "s1", Name: "nas", Type: "sftp", Version: 5, Options: map[string]string{"host": "moved"}, Secrets: map[string]string{"pass": "theirs"}})})
	})
	_, err := c.PutService(context.Background(), store.Service{ID: "s1", Name: "nas", Type: "sftp"}, 4)
	var ce *ConflictError
	require.True(t, errors.As(err, &ce))
	var cur store.Service
	require.NoError(t, json.Unmarshal(ce.Current, &cur))
	require.Equal(t, "moved", cur.Options["host"])
	require.Equal(t, int64(5), cur.Version)

	// a retried save that landed is recognised after opening
	landed, err := SaveService(context.Background(), c, store.Service{ID: "s1", Name: "nas", Type: "sftp", Version: 4, Options: map[string]string{"host": "moved"}, Secrets: map[string]string{"pass": "theirs"}})
	require.NoError(t, err)
	require.Equal(t, int64(5), landed.Version)
}

func TestSealedTokenWriteBack(t *testing.T) {
	s := testSealer()
	var body map[string]string
	c := sealedClient(t, s, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/services/s1/token", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		_, _ = w.Write([]byte(`{"kept":"new"}`))
	})
	require.NoError(t, c.WriteToken(context.Background(), "s1", "token", oauth, time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)))
	require.Equal(t, s.KeyID, body["keyId"])
	require.Equal(t, "2026-11-01T10:00:00Z", body["expiry"])
	require.NotContains(t, body, "value")
	tok, err := vault.Open(s.AK, body["sealedToken"], vault.AADServiceToken("s1"))
	require.NoError(t, err)
	require.Equal(t, oauth, string(tok))
}

func TestVaultErrors(t *testing.T) {
	s := testSealer()
	current := map[string]any{"keyId": "0123456789abcdef", "kdf": map[string]any{"alg": "argon2id", "salt": strings.Repeat("A", 43) + "=", "t": 3, "m": 65536, "p": 4},
		"wrappedKey": "w", "recoveryWrappedKey": "r", "version": 3}
	cases := map[string]struct {
		status int
		code   string
		check  func(error)
	}{
		"key mismatch": {http.StatusConflict, "key_mismatch", func(err error) { require.ErrorIs(t, err, ErrKeyMismatch) }},
		"no vault 409": {http.StatusConflict, "no_vault", func(err error) { require.ErrorIs(t, err, ErrNoVault) }},
		"no vault 404": {http.StatusNotFound, "no_vault", func(err error) { require.ErrorIs(t, err, ErrNoVault) }},
		"426":          {http.StatusUpgradeRequired, "update_required", func(err error) { require.ErrorIs(t, err, ErrUpdateRequired) }},
		"vault exists": {http.StatusConflict, "vault_exists", func(err error) {
			var ve *VaultExistsError
			require.True(t, errors.As(err, &ve))
			require.Equal(t, "0123456789abcdef", ve.Current.KeyID)
			require.Equal(t, 3, ve.Current.Version)
		}},
	}
	for name, cs := range cases {
		t.Run(name, func(t *testing.T) {
			c := sealedClient(t, s, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "1", r.Header.Get("X-Dokky-Vault"))
				w.WriteHeader(cs.status)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": cs.code, "message": "m"}, "current": current})
			})
			_, err := c.CreateVault(context.Background(), vault.Remote{})
			cs.check(err)
		})
	}
	c := sealedClient(t, s, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "conflict", "message": "m"}, "current": current})
	})
	_, err := c.UpdateVault(context.Background(), 2, vault.Remote{})
	var vc *VaultConflictError
	require.True(t, errors.As(err, &vc))
	require.Equal(t, 3, vc.Current.Version)
}

func TestVaultCallsSayTheyUnderstandVaults(t *testing.T) {
	var got []string
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("X-Dokky-Vault"))
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"no_vault","message":"none"}}`))
	})
	_, err := c.GetVault(context.Background())
	require.ErrorIs(t, err, ErrNoVault)
	_, _, _ = c.Pull(context.Background(), "")
	require.Equal(t, []string{"1", ""}, got, "a client without a sealer speaks the old protocol except to /vault")
}
