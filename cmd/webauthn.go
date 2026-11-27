package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/viper"

	"github.com/filebrowser/filebrowser/v2/auth"
	fberrors "github.com/filebrowser/filebrowser/v2/errors"
	"github.com/filebrowser/filebrowser/v2/settings"
	"github.com/filebrowser/filebrowser/v2/users"
)

func validateStartupWebAuthn(v *viper.Viper, databaseExists bool) error {
	value := v.Get("webauthn.origin")
	if value == nil {
		return nil
	}
	raw, ok := value.(string)
	if !ok {
		return errors.New("startup WebAuthn origin must be a string")
	}
	if raw == "" {
		return nil
	}
	if !databaseExists && v.GetBool("noauth") {
		return errors.New("WebAuthn requires JSON authentication; remove --noauth or FB_WEBAUTHN_ORIGIN")
	}
	webauthn, err := auth.NewWebAuthn(raw)
	if err != nil {
		return fmt.Errorf("startup WebAuthn configuration: %w", err)
	}
	v.Set("webauthn.origin", webauthn.Config.RPOrigins[0])
	return nil
}

func stageStartupWebAuthn(st *store, raw string) (*settings.Settings, error) {
	if raw == "" {
		return nil, nil
	}
	current, err := st.Settings.Get()
	if err != nil {
		return nil, err
	}
	next := *current
	next.WebAuthnOrigin = raw
	if err := validateWebAuthnConfig(st, current, &next); err != nil {
		return nil, fmt.Errorf("startup WebAuthn configuration: %w", err)
	}
	if current.WebAuthnOrigin == next.WebAuthnOrigin {
		return nil, nil
	}
	return &next, nil
}

func validateWebAuthnConfig(st *store, previous, next *settings.Settings) error {
	var rp string
	if next.WebAuthnOrigin != "" {
		if next.AuthMethod != auth.MethodJSONAuth {
			return errors.New("WebAuthn requires JSON authentication")
		}
		origin, nextRP, err := auth.NormalizeWebAuthnOrigin(next.WebAuthnOrigin)
		if err != nil {
			return err
		}
		if _, err := auth.NewWebAuthn(origin); err != nil {
			return err
		}
		next.WebAuthnOrigin, rp = origin, nextRP
	}
	enrolled, err := st.SecurityKeys.Any()
	if err != nil || !enrolled {
		return err
	}
	if previous != nil {
		_, oldRP, oldErr := auth.NormalizeWebAuthnOrigin(previous.WebAuthnOrigin)
		if oldErr == nil && oldRP == rp && next.AuthMethod == auth.MethodJSONAuth {
			return nil
		}
	}
	return errors.New("switch enrolled accounts back to passwords before disabling WebAuthn or changing its hostname")
}

func guardUserExport(st *store) error {
	enrolled, err := st.SecurityKeys.Any()
	if err == nil && enrolled {
		return errors.New("user exports cannot preserve security keys; back up the database instead")
	}
	return err
}

func guardUserImport(st *store, list []*users.User, replace, overwrite bool) error {
	if replace {
		enrolled, err := st.SecurityKeys.Any()
		if err == nil && enrolled {
			return users.ErrKeyOnlyAccount
		}
		return err
	}
	if overwrite {
		for _, candidate := range list {
			if candidate.ID == 0 {
				continue
			}
			u, err := st.Users.Get("", false, candidate.ID)
			switch {
			case errors.Is(err, fberrors.ErrNotExist):
			case err != nil:
				return err
			case u.KeyOnly:
				return users.ErrKeyOnlyAccount
			}
		}
	}
	return nil
}
