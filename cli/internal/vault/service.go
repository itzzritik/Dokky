package vault

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
)

// the OAuth token is sealed on its own because devices refresh it far more often than the rest of a login
const tokenKey = "token"

type serviceConfig struct {
	Options map[string]string `json:"options"`
	Secrets map[string]string `json:"secrets"`
}

// SealService seals a drive login for the server: options and secrets in one envelope, the token in another.
// sealedToken is nil when there is no token
func SealService(ak []byte, id string, options, secrets map[string]string) (sealed string, sealedToken *string, err error) {
	if id == "" {
		return "", nil, errors.New("the drive login has no id")
	}
	cfg := serviceConfig{Options: maps.Clone(options), Secrets: map[string]string{}}
	if cfg.Options == nil {
		cfg.Options = map[string]string{}
	}
	var token *string
	for k, v := range secrets {
		if k == tokenKey {
			token = &v
			continue
		}
		cfg.Secrets[k] = v
	}
	plain, err := json.Marshal(cfg)
	if err != nil {
		return "", nil, err
	}
	defer Zero(plain)
	if sealed, err = Seal(ak, plain, AADServiceConfig(id)); err != nil {
		return "", nil, err
	}
	if token != nil {
		s, err := Seal(ak, []byte(*token), AADServiceToken(id))
		if err != nil {
			return "", nil, err
		}
		sealedToken = &s
	}
	return sealed, sealedToken, nil
}

// OpenService is SealService backwards: the token goes back into secrets
func OpenService(ak []byte, id, sealed string, sealedToken *string) (options, secrets map[string]string, err error) {
	plain, err := Open(ak, sealed, AADServiceConfig(id))
	if err != nil {
		return nil, nil, fmt.Errorf("drive login %s: %w", id, err)
	}
	defer Zero(plain)
	var cfg serviceConfig
	if err := json.Unmarshal(plain, &cfg); err != nil {
		return nil, nil, fmt.Errorf("drive login %s is damaged: %w", id, err)
	}
	options, secrets = cfg.Options, cfg.Secrets
	if options == nil {
		options = map[string]string{}
	}
	if secrets == nil {
		secrets = map[string]string{}
	}
	delete(secrets, tokenKey)
	if sealedToken != nil {
		tok, err := Open(ak, *sealedToken, AADServiceToken(id))
		if err != nil {
			return nil, nil, fmt.Errorf("drive login %s token: %w", id, err)
		}
		secrets[tokenKey] = string(tok)
		Zero(tok)
	}
	return options, secrets, nil
}
