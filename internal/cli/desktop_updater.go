package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/fikrilal/agy-auth/internal/agy"
	"github.com/fikrilal/agy-auth/internal/desktop"
	"github.com/fikrilal/agy-auth/internal/registry"
)

type desktopSyncResult struct {
	Switched        bool
	RestoredProfile bool
	Relaunched      bool
	Warnings        []string
}

func (a *App) desktopDataDirPresent() bool {
	if a.Desktop == nil || a.Desktop.DataDir == "" {
		return false
	}
	if info, err := os.Stat(a.Desktop.DataDir); err == nil && info.IsDir() {
		return true
	}
	_, err := os.Stat(desktop.InstallationIDPath(a.Desktop.DataDir))
	return err == nil
}

// syncDesktopForAccount synchronizes Antigravity Desktop state for account.
// For --target=all it handles both accounts with a captured desktop profile
// snapshot and accounts added via CLI login (syncing Desktop's fallback token,
// user scope, app_storage.json metadata, .updaterId, and Desktop installation_id).
func (a *App) syncDesktopForAccount(account *registry.Account, raw []byte, isGCP bool, opts switchOptions, keeperHint string) (desktopSyncResult, int) {
	var res desktopSyncResult
	if a.Desktop == nil || opts.Target == "cli" {
		return res, 0
	}

	deskProfile := a.Store.DesktopProfilePath(account.ID)
	hasDeskProfile := desktop.HasProfile(deskProfile)
	running, pid, _ := a.Desktop.IsRunning()
	livePresent := desktop.HasProfile(a.Desktop.ConfigDir) || running || a.desktopDataDirPresent()

	if !hasDeskProfile {
		if opts.Target == "desktop" {
			return res, a.failCode("desktop_profile_not_found", "Run `agy-auth add` while logged into Antigravity Desktop to capture its profile.", "no desktop profile stored for %s", account.DisplayName())
		}
		if !livePresent {
			return res, 0
		}
	}

	// For --target=all, keep Desktop's standalone fallback token file in sync
	// alongside the shared OS keyring, even if Desktop profile mutation is
	// deferred while Desktop is running.
	if opts.Target == "all" && len(raw) > 0 && a.Storage != nil {
		if err := a.Storage.WriteDesktopToken(raw); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("could not write desktop fallback token: %v", err))
		}
	}

	if running {
		if opts.Relaunch || opts.CloseDesktop {
			if err := a.Desktop.Terminate(pid, 5*time.Second); err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("failed to stop Antigravity Desktop (PID %d): %v", pid, err))
			}
		} else {
			if opts.Target == "desktop" {
				return res, a.failCode("desktop_running", "Close Antigravity Desktop first, or run with --relaunch.", "Antigravity Desktop is currently running (PID %d)", pid)
			}
			res.Warnings = append(res.Warnings, fmt.Sprintf("Antigravity Desktop is currently running (PID %d); desktop profile was not modified to prevent database corruption. Run with --relaunch to restart Desktop.", pid))
			return res, 0
		}
	}

	if stillRunning, stillPID, _ := a.Desktop.IsRunning(); stillRunning {
		if opts.Target == "desktop" {
			return res, a.failCode("desktop_running", "Close Antigravity Desktop manually and try again.", "Antigravity Desktop is still running (PID %d)", stillPID)
		}
		res.Warnings = append(res.Warnings, fmt.Sprintf("Antigravity Desktop is still running (PID %d); desktop profile was not modified.", stillPID))
		return res, 0
	}

	if opts.Target == "desktop" && len(raw) > 0 && a.Storage != nil {
		if err := a.Storage.WriteDesktopToken(raw); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("could not write desktop fallback token: %v", err))
		}
	}

	if hasDeskProfile {
		claimUpdater := a.retainDesktopUpdaterID(keeperHint)
		if err := a.ensureDesktopUpdaterID(account.ID, claimUpdater, false); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("could not set the desktop updater id: %v", err))
		}
		if err := a.Desktop.Restore(deskProfile); err != nil {
			if opts.Target == "desktop" {
				return res, a.failCode("desktop_restore_failed", "Check directory permissions for Antigravity desktop profile.", "could not restore desktop profile: %v", err)
			}
			res.Warnings = append(res.Warnings, fmt.Sprintf("could not restore desktop profile: %v", err))
			return res, 0
		}
		claimInstall := a.retainDesktopInstallationID(keeperHint)
		if err := a.ensureDesktopInstallationID(account.ID, claimInstall); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("could not set the desktop installation id: %v", err))
		}
		res.Switched = true
		res.RestoredProfile = true
	} else {
		if err := a.Desktop.ClearUserScope(); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("could not clear desktop credential scope: %v", err))
		}
		if err := a.Desktop.SyncAppStorage(account.Email, isGCP); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("could not sync desktop app storage: %v", err))
		}
		if desktop.HasProfile(a.Desktop.ConfigDir) {
			claimUpdater := a.retainDesktopUpdaterID(keeperHint)
			if err := a.ensureDesktopUpdaterID(account.ID, claimUpdater, true); err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("could not set the desktop updater id: %v", err))
			}
		}
		claimInstall := a.retainDesktopInstallationID(keeperHint)
		if err := a.ensureDesktopInstallationID(account.ID, claimInstall); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("could not set the desktop installation id: %v", err))
		}
		res.Switched = true
	}

	if opts.Relaunch {
		if err := a.Desktop.Relaunch(); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("could not relaunch Antigravity Desktop: %v", err))
		} else {
			res.Relaunched = true
		}
	}
	return res, 0
}

// retainDesktopUpdaterID binds an unowned live .updaterId to the active
// account, or to the previous account when the active one has no desktop
// profile. Call it before replacing the live desktop profile.
//
// claimLive is false when a live id exists but could not be bound. The
// incoming account must mint instead of taking that id.
func (a *App) retainDesktopUpdaterID(keeperHint string) (claimLive bool) {
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
	keeper, err := a.desktopStateKeeper(keeperHint)
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

// desktopStateKeeper returns the account that should retain an unowned live
// Desktop identifier (.updaterId or Desktop installation_id).
func (a *App) desktopStateKeeper(keeperHint string) (string, error) {
	if keeperHint != "" {
		return keeperHint, nil
	}
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
	if reg.Active != "" {
		return reg.Active, nil
	}
	return "", nil
}

// ensureDesktopUpdaterID stores accountID's Desktop updater id inside its
// profile. A stored id is reused unless another account already has it.
// Otherwise an unowned live id is claimed when claimLive is set. Otherwise
// a new id is minted.
//
// writeLive copies the id into the Desktop user-data directory. switch with a
// stored profile leaves that to Restore so a failed restore does not change
// the live file.
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

// retainDesktopInstallationID binds an unowned live Desktop language_server
// installation_id (~/.gemini/antigravity/installation_id) to the current
// Desktop account before switching away.
func (a *App) retainDesktopInstallationID(keeperHint string) (claimLive bool) {
	if !a.desktopDataDirPresent() || a.Store == nil {
		return true
	}
	live, err := agy.ReadInstallationID(desktop.InstallationIDPath(a.Desktop.DataDir))
	if err != nil || live == "" {
		return true
	}
	owner, err := a.Store.DesktopInstallationOwner(live, "")
	if err != nil {
		a.warn("could not keep the current desktop installation id: %v", err)
		return false
	}
	if owner != "" {
		return true
	}
	keeper, err := a.desktopStateKeeper(keeperHint)
	if err != nil {
		a.warn("could not keep the current desktop installation id: %v", err)
		return false
	}
	if keeper == "" {
		return false
	}
	existing, err := a.Store.LoadDesktopInstallationID(keeper)
	if err == nil && existing != "" {
		if existing == live {
			return true
		}
		a.warn("could not keep the current desktop installation id: the current account already has a different one")
		return false
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, agy.ErrInvalidInstallationID) {
		a.warn("could not keep the current desktop installation id: %v", err)
		return false
	}
	if err := a.Store.SaveDesktopInstallationID(keeper, live); err != nil {
		a.warn("could not keep the current desktop installation id: %v", err)
		return false
	}
	return true
}

// ensureDesktopInstallationID ensures accountID has its own Desktop
// language_server installation UUID and writes it to DataDir/installation_id
// when Desktop's data directory exists.
func (a *App) ensureDesktopInstallationID(accountID string, claimLive bool) error {
	if !a.desktopDataDirPresent() || a.Store == nil {
		return nil
	}
	stored, err := a.Store.LoadDesktopInstallationID(accountID)
	if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, agy.ErrInvalidInstallationID) {
		return err
	}
	if stored != "" {
		other, err := a.Store.DesktopInstallationOwner(stored, accountID)
		if err != nil {
			return err
		}
		if other != "" {
			stored = ""
		}
	}
	id := stored
	if id == "" {
		id, err = a.chooseDesktopInstallationID(accountID, claimLive)
		if err != nil {
			return err
		}
		if err := a.Store.SaveDesktopInstallationID(accountID, id); err != nil {
			return err
		}
	}
	if err := agy.WriteInstallationID(desktop.InstallationIDPath(a.Desktop.DataDir), id); err != nil {
		return fmt.Errorf("write desktop installation id: %w", err)
	}
	return nil
}

func (a *App) chooseDesktopInstallationID(accountID string, claimLive bool) (string, error) {
	if claimLive && a.Desktop != nil && a.Desktop.DataDir != "" {
		live, err := agy.ReadInstallationID(desktop.InstallationIDPath(a.Desktop.DataDir))
		if err == nil {
			owner, err := a.Store.DesktopInstallationOwner(live, accountID)
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
