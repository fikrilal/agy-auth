package contracts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fikrilal/agy-auth/internal/agy"
	"github.com/fikrilal/agy-auth/internal/registry"
)

// Check locks the agy credential and registry contracts with independent
// fixtures. These are intentionally separate from package unit tests.
func Check(root string) error {
	fixturesDir := filepath.Join(root, "harness", "fixtures")
	var errs []error

	checkCredential := func(name string, wantOK bool, check func(*agy.TokenFile) error) {
		path := filepath.Join(fixturesDir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			return
		}
		tokenFile, err := agy.ParseTokenFile(raw)
		if wantOK && err != nil {
			errs = append(errs, fmt.Errorf("%s: expected valid credential: %w", name, err))
			return
		}
		if !wantOK {
			if err == nil {
				errs = append(errs, fmt.Errorf("%s: expected invalid credential", name))
			}
			return
		}
		if err := check(tokenFile); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}

	checkCredential("credential.consumer.json", true, func(tokenFile *agy.TokenFile) error {
		if tokenFile.Email() != "user@example.com" {
			return fmt.Errorf("email = %q", tokenFile.Email())
		}
		if tokenFile.Subject() != "subject-1" {
			return fmt.Errorf("subject = %q", tokenFile.Subject())
		}
		if tokenFile.Token.RefreshToken != "test-refresh" {
			return fmt.Errorf("refresh token = %q", tokenFile.Token.RefreshToken)
		}
		return nil
	})
	checkCredential("credential.minimal.json", true, func(tokenFile *agy.TokenFile) error {
		if tokenFile.Token.AccessToken != "test-access" {
			return fmt.Errorf("access token = %q", tokenFile.Token.AccessToken)
		}
		if tokenFile.Email() != "" {
			return fmt.Errorf("expected no email, got %q", tokenFile.Email())
		}
		return nil
	})
	checkCredential("credential.invalid.json", false, nil)

	if err := checkRegistry(filepath.Join(fixturesDir, "registry.valid.json"), true); err != nil {
		errs = append(errs, err)
	}
	if err := checkRegistry(filepath.Join(fixturesDir, "registry.invalid.json"), false); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

func checkRegistry(path string, wantOK bool) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	dir, err := os.MkdirTemp("", "agy-auth-contract-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "registry.json"), raw, 0o600); err != nil {
		return err
	}
	store := registry.NewStore(dir)
	reg, err := store.Load()
	if wantOK && err != nil {
		return fmt.Errorf("%s: expected valid registry: %w", filepath.Base(path), err)
	}
	if !wantOK {
		if err == nil {
			return fmt.Errorf("%s: expected invalid registry", filepath.Base(path))
		}
		return nil
	}
	if len(reg.Accounts) != 2 {
		return fmt.Errorf("%s: accounts = %d", filepath.Base(path), len(reg.Accounts))
	}
	if reg.Active != "acct_test000001" {
		return fmt.Errorf("%s: active = %q", filepath.Base(path), reg.Active)
	}
	return nil
}
