package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"time"

	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/itzzritik/dokky/cli/internal/vault"
)

const vaultPath = "/vault"

// Sealer seals drive logins with the account key on the way out and opens them on the way in, so the server
// only ever holds ciphertext. a client with one says so on every request
type Sealer struct {
	AK    []byte
	KeyID string
}

var (
	ErrKeyMismatch    = errors.New("this device's key is not the account's key")
	ErrNoVault        = errors.New("the account has no vault yet")
	ErrUpdateRequired = errors.New("the dokky server wants a newer dokky")
)

// another device made the account's vault first; Current is it
type VaultExistsError struct{ Current RemoteVault }

func (*VaultExistsError) Error() string { return "the account already has a vault" }

type VaultConflictError struct{ Current RemoteVault }

func (*VaultConflictError) Error() string { return "the account's vault was changed elsewhere first" }

// RemoteVault is the account's vault as the server keeps it: the wraps plus a version for rewraps
type RemoteVault struct {
	vault.Remote
	Version           int       `json:"version"`
	CreatedAt         time.Time `json:"createdAt"`
	PasswordChangedAt time.Time `json:"passwordChangedAt"`
}

// VaultRef is what a pull says about the account's vault, so a rekey or another key shows without asking
type VaultRef struct {
	KeyID   string `json:"keyId"`
	Version int    `json:"version"`
}

var vaultHeader = map[string]string{"X-Dokky-Vault": "1"}

func (c *Client) GetVault(ctx context.Context) (RemoteVault, error) {
	var out struct {
		Vault RemoteVault `json:"vault"`
	}
	err := c.callWith(ctx, http.MethodGet, vaultPath, vaultHeader, nil, &out, http.StatusOK)
	return out.Vault, err
}

// a retried create whose first try landed comes back as VaultExistsError with this key's id
func (c *Client) CreateVault(ctx context.Context, r vault.Remote) (RemoteVault, error) {
	var out struct {
		Vault RemoteVault `json:"vault"`
	}
	err := c.callWith(ctx, http.MethodPost, vaultPath, vaultHeader, r, &out, http.StatusCreated)
	return out.Vault, err
}

func (c *Client) UpdateVault(ctx context.Context, baseVersion int, r vault.Remote) (RemoteVault, error) {
	body := struct {
		vault.Remote
		BaseVersion int `json:"baseVersion"`
	}{r, baseVersion}
	var out struct {
		Vault RemoteVault `json:"vault"`
	}
	err := c.callWith(ctx, http.MethodPut, vaultPath, vaultHeader, body, &out, http.StatusOK)
	var ce *ConflictError
	if errors.As(err, &ce) {
		var cur RemoteVault
		if json.Unmarshal(ce.Current, &cur) != nil {
			return RemoteVault{}, fmt.Errorf("the vault conflict came without a readable current vault: %w", err)
		}
		return RemoteVault{}, &VaultConflictError{Current: cur}
	}
	return out.Vault, err
}

// the service shape a vault-aware client sends and gets back
type sealedService struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Type        string     `json:"type"`
	Version     int64      `json:"version"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	KeyID       *string    `json:"keyId"`
	Sealed      *string    `json:"sealed"`
	SealedToken *string    `json:"sealedToken"`
	TokenExpiry *time.Time `json:"tokenExpiry"`
	// only while the server still holds the service encrypted with its own key
	Legacy *struct {
		Options map[string]string `json:"options"`
		Secrets map[string]string `json:"secrets"`
	} `json:"legacy"`
}

type sealedBody struct {
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	KeyID       string  `json:"keyId"`
	Sealed      string  `json:"sealed"`
	SealedToken *string `json:"sealedToken"`
	TokenExpiry *string `json:"tokenExpiry"`
	BaseVersion int64   `json:"baseVersion"`
}

// open turns the wire shape back into a plain service. legacy is true for one the server still has to give up;
// a service sealed with another key, or one that does not open, is ErrKeyMismatch: dropping it would delete it here
func (s *Sealer) open(w sealedService) (svc store.Service, legacy bool, err error) {
	svc = store.Service{ID: w.ID, Name: w.Name, Type: w.Type, Version: w.Version, UpdatedAt: w.UpdatedAt}
	mismatch := func(cause error) error {
		return fmt.Errorf("%w: drive login %s: %v", ErrKeyMismatch, w.Name, cause)
	}
	if (w.Sealed != nil || w.SealedToken != nil) && (w.KeyID == nil || *w.KeyID != s.KeyID) {
		return svc, false, mismatch(errors.New("sealed with another key"))
	}
	switch {
	case w.Sealed != nil:
		svc.Options, svc.Secrets, err = vault.OpenService(s.AK, w.ID, *w.Sealed, w.SealedToken)
		if err != nil {
			return svc, false, mismatch(err)
		}
	case w.Legacy != nil:
		svc.Options, svc.Secrets = maps.Clone(nonNil(w.Legacy.Options)), maps.Clone(nonNil(w.Legacy.Secrets))
		// a token refreshed by a vault device is sealed even while the rest still waits
		if w.SealedToken != nil {
			tok, err := vault.Open(s.AK, *w.SealedToken, vault.AADServiceToken(w.ID))
			if err != nil {
				return svc, false, mismatch(err)
			}
			svc.Secrets[tokenField] = string(tok)
			vault.Zero(tok)
		}
		legacy = true
	default:
		return svc, false, fmt.Errorf("drive login %s came without its settings", w.Name)
	}
	return svc, legacy, nil
}

func (s *Sealer) body(svc store.Service, baseVersion int64) (sealedBody, error) {
	sealed, token, err := vault.SealService(s.AK, svc.ID, svc.Options, svc.Secrets)
	if err != nil {
		return sealedBody{}, err
	}
	b := sealedBody{Name: svc.Name, Type: svc.Type, KeyID: s.KeyID, Sealed: sealed, SealedToken: token, BaseVersion: baseVersion}
	if token != nil {
		if exp, ok := TokenExpiry(svc.Secrets[tokenField]); ok {
			v := exp.UTC().Format(time.RFC3339)
			b.TokenExpiry = &v
		}
	}
	return b, nil
}

const tokenField = "token"

// TokenExpiry reads the expiry rclone writes into an OAuth token
func TokenExpiry(token string) (time.Time, bool) {
	var t struct {
		Expiry time.Time `json:"expiry"`
	}
	if json.Unmarshal([]byte(token), &t) != nil || t.Expiry.IsZero() {
		return time.Time{}, false
	}
	return t.Expiry, true
}

// a conflict's current item is sealed like the rest; callers compare and show it opened, without secrets leaving
func (s *Sealer) openConflict(err error) error {
	var ce *ConflictError
	if !errors.As(err, &ce) {
		return err
	}
	var w sealedService
	if json.Unmarshal(ce.Current, &w) != nil {
		return err
	}
	cur, _, oerr := s.open(w)
	if oerr != nil {
		return oerr
	}
	raw, merr := json.Marshal(cur)
	if merr != nil {
		return err
	}
	return &ConflictError{Current: raw}
}
