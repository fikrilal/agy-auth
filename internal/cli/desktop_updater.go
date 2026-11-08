package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/fikrilal/agy-auth/internal/agy"
	"github.com/fikrilal/agy-auth/internal/desktop"
)

// retainDesktopUpdaterID binds an unowned live .updaterId to the active
// account, or to the previous account when the active one has no desktop
// profile. Call it before replacing the live desktop profile.
//
// claimLive is false when a live id exists but could not be bound. The
// incoming account must mint instead of taking that id.
func (a *App) retainDesktopUpdaterID() (claimLive bool) {
	if a.Desktop == nil || a.Store == nil {
		return true
	}
	live, err := agy.ReadInstallationID(desktop.UpdaterIDPath(a.Desktop.ConfigDir))
	if err != nil || live == "" {
		return true
	}
	owner, err := a.Store.DesktopUpdaterOwner(live, "")
	if err != nil {
		a.warn("could not keep the current desktop updater id: %v", err)
		return false
	}
	if owner != "" {
		return true
	}
	keeper, err := a.desktopUpdaterKeeper()
	if err != nil {
		a.warn("could not keep the current desktop updater id: %v", err)
		return false
	}
	if keeper == "" {
		a.warn("could not keep the current desktop updater id: no stored desktop profile owns it")
		return false
	}
	existing, err := a.Store.LoadDesktopUpdaterID(keeper)
	if err == nil && existing != "" {
		if existing == live {
			return true
		}
		a.warn("could not keep the current desktop updater id: the current account already has a different one")
		return false
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, agy.ErrInvalidInstallationID) {
		a.warn("could not keep the current desktop updater id: %v", err)
		return false
	}
	if err := a.Store.SaveDesktopUpdaterID(keeper, live); err != nil {
		a.warn("could not keep the current desktop updater id: %v", err)
		return false
	}
	return true
}

// desktopUpdaterKeeper is the account that should keep an unowned live updater id.
func (a *App) desktopUpdaterKeeper() (string, error) {
	reg, err := a.Store.Load()
	if err != nil {
		return "", err
	}
	for _, id := range []string{reg.Active, reg.Previous} {
		if id == "" || !a.Store.HasDesktopProfile(id) {
			continue
		}
		return id, nil
	}
	return "", nil
}

// ensureDesktopUpdaterID stores accountID's Desktop updater id inside its
// profile. A stored id is reused unless another account already has it.
// Otherwise an unowned live id is claimed when claimLive is set. Otherwise
// a new id is minted.
//
// writeLive copies the id into the Desktop user-data directory. switch
// leaves that to Restore so a failed restore does not change the live file.
func (a *App) ensureDesktopUpdaterID(accountID string, claimLive bool, writeLive bool) error {
	if a.Desktop == nil || a.Store == nil {
		return errors.New("desktop storage is not configured")
	}
	stored, err := a.Store.LoadDesktopUpdaterID(accountID)
	if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, agy.ErrInvalidInstallationID) {
		return err
	}
	if stored != "" {
		other, err := a.Store.DesktopUpdaterOwner(stored, accountID)
		if err != nil {
			return err
		}
		if other != "" {
			stored = ""
		}
	}
	id := stored
	if id == "" {
		var err error
		id, err = a.chooseDesktopUpdaterID(accountID, claimLive)
		if err != nil {
			return err
		}
		if err := a.Store.SaveDesktopUpdaterID(accountID, id); err != nil {
			return err
		}
	} else if err := a.Store.SaveDesktopUpdaterID(accountID, id); err != nil {
		return err
	}
	if !writeLive {
		return nil
	}
	if err := agy.WriteInstallationID(desktop.UpdaterIDPath(a.Desktop.ConfigDir), id); err != nil {
		return fmt.Errorf("write desktop updater id: %w", err)
	}
	return nil
}

func (a *App) chooseDesktopUpdaterID(accountID string, claimLive bool) (string, error) {
	if claimLive {
		live, err := agy.ReadInstallationID(desktop.UpdaterIDPath(a.Desktop.ConfigDir))
		if err == nil {
			owner, err := a.Store.DesktopUpdaterOwner(live, accountID)
			if err != nil {
				return "", err
			}
			if owner == "" {
				return live, nil
			}
		}
	}
	return agy.NewInstallationID()
}
