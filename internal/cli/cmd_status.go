package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/fikrilal/agy-auth/internal/agy"
)

func (a *App) cmdStatus(args []string) int {
	fs := a.newFlagSet("status")
	if ok, code := a.parseFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() > 0 {
		return a.usageFor("status", "status takes no arguments")
	}

	reg, err := a.Store.Load()
	if err != nil {
		return a.failCode("registry_error", "Run `agy-auth status` or `agy-auth path` to check storage.", "could not read the account registry")
	}

	var currentRaw []byte
	var current *agy.TokenFile
	var currentSource agy.Source
	read, readErr := a.Storage.Read()
	if readErr == nil {
		currentRaw = read.Raw
		currentSource = read.Source
		current, _ = agy.ParseTokenFile(currentRaw)
	} else if !errors.Is(readErr, agy.ErrNoCredentials) {
		a.warn("%v", readErr)
	}

	var matched = (*accountView)(nil)
	if current != nil {
		if account, err := reg.Match(currentRaw); err != nil {
			a.warn("could not match the live account: %v", err)
		} else if account != nil {
			matched = &accountView{ID: account.ID, Name: account.DisplayName()}
		}
	}

	keyringRaw, fileRaw, keyringErr, fileErr := a.Storage.ReadBoth()
	mirror := mirrorState(keyringRaw, fileRaw, keyringErr, fileErr)
	running := runningAgyCount()
	var desktopRunning bool
	var desktopPid int
	if a.Desktop != nil {
		desktopRunning, desktopPid, _ = a.Desktop.IsRunning()
	}

	now := a.now()
	var expiryAt time.Time
	var hasExpiry bool
	var expiryText string
	if current != nil {
		if expiry, ok := current.ExpiryTime(); ok {
			expiryAt = expiry
			hasExpiry = true
			expiryText = fmt.Sprintf("%s (%s)", expiry.Format("2006-01-02 15:04 MST"), humanUntil(expiry, now))
		}
	}

	if a.JSON {
		payload := map[string]any{
			"registry":          a.Store.Dir,
			"registry_file":     a.Store.Registry,
			"accounts_dir":      a.Store.AccountsDir,
			"token_file":        a.Storage.FilePath,
			"keyring_service":   a.Storage.Service,
			"keyring_user":      a.Storage.User,
			"keyring_available": a.Storage.KeyringAvailable(),
			"mirror":            mirror,
			"running_agy":       running,
			"stored_accounts":   len(reg.Accounts),
		}
		if a.Desktop != nil {
			payload["desktop_config"] = a.Desktop.ConfigDir
			payload["desktop_running"] = desktopRunning
			if desktopRunning {
				payload["desktop_pid"] = desktopPid
			}
		}
		if current != nil {
			row := map[string]any{
				"email":       current.Email(),
				"source":      string(currentSource),
				"auth_method": current.AuthMethod,
				"has_refresh": current.HasRefreshToken(),
				"stored":      matched != nil,
			}
			if hasExpiry {
				row["expires_at"] = expiryAt.Format(time.RFC3339Nano)
				row["expires_in"] = humanUntil(expiryAt, now)
				row["expired"] = !expiryAt.After(now)
			} else {
				row["expires_at"] = nil
			}
			if matched != nil {
				row["account_id"] = matched.ID
			}
			payload["current"] = row
		} else {
			payload["current"] = nil
		}
		if err := a.emitJSON(payload); err != nil {
			return a.failCode("output_error", "", "could not write JSON output")
		}
		return 0
	}

	fmt.Fprintf(a.Out, "Registry:   %s\n", a.Store.Dir)
	fmt.Fprintf(a.Out, "Token file: %s\n", a.Storage.FilePath)
	keyringState := a.green("available")
	if !a.Storage.KeyringAvailable() {
		keyringState = a.yellow("unavailable")
	}
	fmt.Fprintf(a.Out, "Keyring:    %s / %s (%s)\n", a.Storage.Service, a.Storage.User, keyringState)
	fmt.Fprintln(a.Out)

	if current == nil {
		fmt.Fprintf(a.Out, "Current:    %s\n", a.yellow("not signed in"))
	} else {
		name := current.Email()
		if name == "" {
			name = "(unknown account)"
		}
		fmt.Fprintf(a.Out, "Current:    %s (%s)\n", name, currentSource)
		fmt.Fprintf(a.Out, "Method:     %s\n", valueOr(current.AuthMethod, "-"))
		if expiryText != "" {
			fmt.Fprintf(a.Out, "Expires:    %s\n", expiryText)
		}
		fmt.Fprintf(a.Out, "Refresh:    %s\n", yesNo(current.HasRefreshToken()))
		if matched != nil {
			fmt.Fprintf(a.Out, "Stored as:  %s\n", matched.Name)
		} else {
			fmt.Fprintf(a.Out, "Stored as:  %s\n", a.yellow("not stored (run \"agy-auth add\")"))
		}
	}
	fmt.Fprintf(a.Out, "Mirror:     %s\n", mirror)
	switch {
	case running > 0:
		fmt.Fprintf(a.Out, "agy:        %d process(es) running; restart after switching\n", running)
	case running == 0:
		fmt.Fprintln(a.Out, "agy:        not running")
	default:
		fmt.Fprintln(a.Out, "agy:        running state unknown on this platform")
	}
	if a.Desktop != nil {
		switch {
		case desktopRunning:
			fmt.Fprintf(a.Out, "Desktop:    running (PID %d); restart with --relaunch on switch\n", desktopPid)
		default:
			fmt.Fprintln(a.Out, "Desktop:    not running")
		}
	}
	fmt.Fprintf(a.Out, "Accounts:   %d stored\n", len(reg.Accounts))
	return 0
}

type accountView struct {
	ID   string
	Name string
}

func mirrorState(keyringRaw, fileRaw []byte, keyringErr, fileErr error) string {
	keyringToken, keyringOK := parseStoredToken(keyringRaw, keyringErr)
	fileToken, fileOK := parseStoredToken(fileRaw, fileErr)
	switch {
	case keyringOK && fileOK:
		if keyringToken.Fingerprint() == fileToken.Fingerprint() {
			return "in sync"
		}
		return "keyring and file contain different credentials (run switch again)"
	case keyringOK:
		return "keyring only (file mirror missing)"
	case fileOK:
		return "file only (keyring unavailable or empty)"
	default:
		return "no credentials"
	}
}

func parseStoredToken(raw []byte, err error) (*agy.TokenFile, bool) {
	if err != nil || len(raw) == 0 {
		return nil, false
	}
	token, parseErr := agy.ParseTokenFile(raw)
	if parseErr != nil {
		return nil, false
	}
	return token, true
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
