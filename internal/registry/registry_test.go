package registry

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func testToken(t *testing.T, email, subject, refresh string, expiry time.Time) []byte {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"email": email, "sub": subject})
	if err != nil {
		t.Fatal(err)
	}
	idToken := "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
	document := map[string]any{
		"token": map[string]any{
			"access_token":  "access-" + refresh,
			"token_type":    "Bearer",
			"refresh_token": refresh,
			"expiry":        expiry.Format(time.RFC3339Nano),
		},
		"auth_method": "consumer",
		"id_token":    idToken,
	}
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestUpsertCreateAndDedupe(t *testing.T) {
	store := NewStore(t.TempDir())
	expiry := time.Now().Add(time.Hour)
	first := testToken(t, "user@example.com", "subject-1", "refresh-1", expiry)

	result, err := store.Upsert(first, "personal", true)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created {
		t.Error("expected a new account")
	}
	if result.Account.Email != "user@example.com" {
		t.Errorf("email = %q", result.Account.Email)
	}
	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reg.Active != result.Account.ID {
		t.Errorf("active = %q", reg.Active)
	}
	if len(reg.Accounts) != 1 {
		t.Fatalf("accounts = %d", len(reg.Accounts))
	}

	second := testToken(t, "user@example.com", "subject-1", "refresh-2", expiry)
	result, err = store.Upsert(second, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Created {
		t.Error("same subject should update, not create")
	}
	reg, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Accounts) != 1 {
		t.Fatalf("accounts = %d", len(reg.Accounts))
	}
	if reg.Accounts[0].Alias != "personal" {
		t.Errorf("alias should be preserved, got %q", reg.Accounts[0].Alias)
	}
	raw, err := store.LoadToken(reg.Accounts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(second) {
		t.Error("snapshot was not refreshed")
	}
}

func TestUpsertDoesNotActivateWhenRequested(t *testing.T) {
	store := NewStore(t.TempDir())
	expiry := time.Now().Add(time.Hour)
	first, err := store.Upsert(testToken(t, "a@example.com", "subject-a", "refresh-a", expiry), "", true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Upsert(testToken(t, "b@example.com", "subject-b", "refresh-b", expiry), "", false)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reg.Active != first.Account.ID {
		t.Errorf("active = %q, want %q", reg.Active, first.Account.ID)
	}
	if reg.FindByID(second.Account.ID) == nil {
		t.Error("second account missing")
	}
}

func TestResolveQueries(t *testing.T) {
	store := NewStore(t.TempDir())
	expiry := time.Now().Add(time.Hour)
	a, _ := store.Upsert(testToken(t, "alpha@one.com", "subject-a", "refresh-a", expiry), "work", true)
	b, _ := store.Upsert(testToken(t, "beta@two.com", "subject-b", "refresh-b", expiry), "personal", true)
	reg, _ := store.Load()

	cases := map[string]string{
		"1":              a.Account.ID,
		"2":              b.Account.ID,
		"work":           a.Account.ID,
		"PERSONAL":       b.Account.ID,
		"alpha@one.com":  a.Account.ID,
		"two.com":        b.Account.ID,
		a.Account.ID[:8]: a.Account.ID,
	}
	for query, want := range cases {
		matches := reg.Resolve(query)
		if len(matches) != 1 {
			t.Errorf("Resolve(%q) returned %d matches", query, len(matches))
			continue
		}
		if matches[0].ID != want {
			t.Errorf("Resolve(%q) = %q, want %q", query, matches[0].ID, want)
		}
	}
	if matches := reg.Resolve("missing"); len(matches) != 0 {
		t.Errorf("expected no matches, got %d", len(matches))
	}
	if matches := reg.Resolve("example.com"); len(matches) != 0 {
		t.Errorf("expected no matches for unrelated domain, got %d", len(matches))
	}
}

func TestAliasConflictAndClear(t *testing.T) {
	store := NewStore(t.TempDir())
	expiry := time.Now().Add(time.Hour)
	a, _ := store.Upsert(testToken(t, "a@example.com", "subject-a", "refresh-a", expiry), "work", true)
	b, _ := store.Upsert(testToken(t, "b@example.com", "subject-b", "refresh-b", expiry), "personal", true)
	reg, _ := store.Load()
	if err := reg.SetAlias(b.Account.ID, "work"); err == nil {
		t.Error("expected alias conflict")
	}
	if err := reg.SetAlias(a.Account.ID, "123"); err == nil {
		t.Error("numeric aliases should be rejected")
	}
	if err := reg.ClearAlias(a.Account.ID); err != nil {
		t.Fatal(err)
	}
	if reg.FindByID(a.Account.ID).Alias != "" {
		t.Error("alias was not cleared")
	}
	if err := store.Save(reg); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveAndPermissions(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	result, err := store.Upsert(testToken(t, "a@example.com", "subject-a", "refresh-a", time.Now().Add(time.Hour)), "", true)
	if err != nil {
		t.Fatal(err)
	}
	path, err := store.AccountTokenPath(result.Account.ID)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("account permissions = %o", info.Mode().Perm())
	}
	reg, _ := store.Load()
	if !reg.Remove(result.Account.ID) {
		t.Error("expected remove to succeed")
	}
	if reg.Active != "" {
		t.Errorf("active = %q, want empty", reg.Active)
	}
	if err := store.DeleteToken(result.Account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("token file still exists")
	}
	if err := store.Save(reg); err != nil {
		t.Fatal(err)
	}
	reloaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Accounts) != 0 {
		t.Errorf("accounts = %d", len(reloaded.Accounts))
	}
}

func TestLoadRejectsTraversalIDs(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	raw := []byte(`{"version":1,"accounts":[{"id":"../../escape"}]}`)
	if err := os.WriteFile(store.Registry, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("expected invalid account id error")
	}
}
