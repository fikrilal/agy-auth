package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/fikrilal/agy-auth/internal/agy"
)

// saveLiveAccount stores the credential that is signed in now, without
// activating it. Login and import --activate call this before retain so an
// account that was never added still keeps its installation id.
func (a *App) saveLiveAccount() {
	if a.Storage == nil || a.Store == nil {
		return
	}
	read, err := a.Storage.Read()
	if err != nil {
		return
	}
	result, err := a.Store.Upsert(read.Raw, "", false)
	if err != nil {
		a.warn("could not save the current account: %v", err)
		return
	}
	if result.Created {
		fmt.Fprintf(a.promptWriter(), "Saved current account %s.\n", result.Account.DisplayName())
	}
}

// retainLiveInstallationID binds the live installation id to the account that
// owns the live credential. Call it before replacing that credential.
//
// claimLive is false when a live id exists but could not be bound. The caller
// must mint for the incoming account instead of giving it that id.
func (a *App) retainLiveInstallationID() (claimLive bool) {
	if a.Storage == nil || a.Store == nil {
		return true
	}
	live, err := agy.ReadInstallationID(a.Storage.InstallationPath())
	if err != nil || live == "" {
		return true
	}
	owner, err := a.Store.InstallationOwner(live)
	if err != nil {
		a.warn("could not keep the current installation id: %v", err)
		return false
	}
	if owner != "" {
		return true
	}
	read, err := a.Storage.Read()
	if err != nil {
		a.warn("could not keep the current installation id: %v", err)
		return false
	}
	reg, err := a.Store.Load()
	if err != nil {
		a.warn("could not keep the current installation id: %v", err)
		return false
	}
	matched, err := reg.Match(read.Raw)
	if err != nil || matched == nil {
		a.warn("could not keep the current installation id: the signed-in account is not stored")
		return false
	}
	existing, err := a.Store.LoadInstallationID(matched.ID)
	if err == nil && existing != "" {
		return true
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, agy.ErrInvalidInstallationID) {
		a.warn("could not keep the current installation id: %v", err)
		return false
	}
	if err := a.Store.SaveInstallationID(matched.ID, live); err != nil {
		a.warn("could not keep the current installation id: %v", err)
		return false
	}
	return true
}

// ensureLiveInstallationID makes accountID the live CLI installation id.
// A stored id is reused. Otherwise an unowned live id is claimed when
// claimLive is set. Otherwise a new id is minted.
func (a *App) ensureLiveInstallationID(accountID string, claimLive bool) error {
	if a.Storage == nil || a.Store == nil {
		return errors.New("credential storage is not configured")
	}
	stored, err := a.Store.LoadInstallationID(accountID)
	if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, agy.ErrInvalidInstallationID) {
		return err
	}
	id := stored
	if id == "" {
		id, err = a.chooseInstallationID(accountID, claimLive)
		if err != nil {
			return err
		}
		if err := a.Store.SaveInstallationID(accountID, id); err != nil {
			return err
		}
	}
	if err := agy.WriteInstallationID(a.Storage.InstallationPath(), id); err != nil {
		return fmt.Errorf("write installation id: %w", err)
	}
	return nil
}

func (a *App) chooseInstallationID(accountID string, claimLive bool) (string, error) {
	if claimLive {
		live, err := agy.ReadInstallationID(a.Storage.InstallationPath())
		if err == nil {
			owner, err := a.Store.InstallationOwner(live)
			if err != nil {
				return "", err
			}
			if owner == "" || owner == accountID {
				return live, nil
			}
		}
	}
	return agy.NewInstallationID()
}
