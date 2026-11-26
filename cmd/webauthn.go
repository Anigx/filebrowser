package cmd

import (
	"errors"

	"github.com/filebrowser/filebrowser/v2/auth"
	fberrors "github.com/filebrowser/filebrowser/v2/errors"
	"github.com/filebrowser/filebrowser/v2/settings"
	"github.com/filebrowser/filebrowser/v2/users"
)

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
