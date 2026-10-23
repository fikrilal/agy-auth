package registry

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fikrilal/agy-auth/internal/backup"
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
	b, _ := store.Upsert(testToken(t, "beta@two.com", "subject-b", "refresh-b", expiry), "personal", false)
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

func TestResolvePrecedence(t *testing.T) {
	reg := &Registry{Accounts: []Account{
		{ID: "acct_a", Email: "alpha@one.com", Alias: "work"},
		{ID: "acct_b", Email: "beta@two.com", Alias: "personal"},
		{ID: "acct_c", Email: "work@elsewhere.com", Alias: "other"},
	}}

	cases := []struct {
		query string
		want  []string
	}{
		{"work", []string{"acct_a"}},
		{"WORK", []string{"acct_a"}},
		{"personal", []string{"acct_b"}},
		{"alpha@one.com", []string{"acct_a"}},
		{"acct_b", []string{"acct_b"}},
		{"acct_", []string{"acct_a", "acct_b", "acct_c"}},
		{"2", []string{"acct_b"}},
		{"com", []string{"acct_a", "acct_b", "acct_c"}},
		{"elsewhere", []string{"acct_c"}},
		{"missing", nil},
	}
	for _, tc := range cases {
		matches := reg.Resolve(tc.query)
		var got []string
		for _, account := range matches {
			got = append(got, account.ID)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Resolve(%q) = %v, want %v", tc.query, got, tc.want)
		}
	}
}

func TestPreviousTracking(t *testing.T) {
	store := NewStore(t.TempDir())
	expiry := time.Now().Add(time.Hour)
	first, _ := store.Upsert(testToken(t, "first@example.com", "subject-1", "refresh-1", expiry), "first", true)
	reg, _ := store.Load()
	if reg.Previous != "" {
		t.Errorf("previous = %q after first account", reg.Previous)
	}

	second, _ := store.Upsert(testToken(t, "second@example.com", "subject-2", "refresh-2", expiry), "second", true)
	reg, _ = store.Load()
	if reg.Active != second.Account.ID || reg.Previous != first.Account.ID {
		t.Errorf("active = %q, previous = %q", reg.Active, reg.Previous)
	}

	_, _ = store.Upsert(testToken(t, "first@example.com", "subject-1", "refresh-1", expiry), "first", true)
	reg, _ = store.Load()
	if reg.Active != first.Account.ID || reg.Previous != second.Account.ID {
		t.Errorf("active = %q, previous = %q after reactivating first", reg.Active, reg.Previous)
	}

	reg.Remove(second.Account.ID)
	if reg.Previous != "" {
		t.Errorf("previous = %q after removing previous account", reg.Previous)
	}

	stale := &Registry{
		Version:  Version,
		Active:   "acct_missing",
		Previous: "acct_missing",
		Accounts: []Account{{ID: "acct_a", Alias: "only"}},
	}
	if err := store.Save(stale); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Active != "" || loaded.Previous != "" {
		t.Errorf("stale active/previous not cleared: active = %q, previous = %q", loaded.Active, loaded.Previous)
	}

	same := &Registry{
		Version:  Version,
		Active:   "acct_a",
		Previous: "acct_a",
		Accounts: []Account{{ID: "acct_a", Alias: "only"}},
	}
	if err := store.Save(same); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Previous != "" {
		t.Errorf("previous = %q when it equals active", loaded.Previous)
	}
}

func TestSaveBacksUpChangedRegistry(t *testing.T) {
	store := NewStore(t.TempDir())
	expiry := time.Now().Add(time.Hour)
	if _, err := store.Upsert(testToken(t, "first@example.com", "subject-1", "refresh-1", expiry), "first", true); err != nil {
		t.Fatal(err)
	}
	if managed, err := store.Backups.Managed(backup.Registry); err != nil {
		t.Fatal(err)
	} else if len(managed) != 0 {
		t.Fatalf("first save created backups: %d", len(managed))
	}

	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(reg); err != nil {
		t.Fatal(err)
	}
	if managed, err := store.Backups.Managed(backup.Registry); err != nil {
		t.Fatal(err)
	} else if len(managed) != 0 {
		t.Fatalf("unchanged save created backups: %d", len(managed))
	}

	reg.Accounts[0].Alias = "changed"
	if err := store.Save(reg); err != nil {
		t.Fatal(err)
	}
	managed, err := store.Backups.Managed(backup.Registry)
	if err != nil {
		t.Fatal(err)
	}
	if len(managed) != 1 {
		t.Fatalf("changed save backups = %d, want 1", len(managed))
	}
	raw, err := os.ReadFile(managed[0])
	if err != nil {
		t.Fatal(err)
	}
	var backedUp Registry
	if err := json.Unmarshal(raw, &backedUp); err != nil {
		t.Fatal(err)
	}
	if len(backedUp.Accounts) != 1 || backedUp.Accounts[0].Alias != "first" {
		t.Errorf("backup does not contain the previous registry: %+v", backedUp.Accounts)
	}
}

func TestClean(t *testing.T) {
	store := NewStore(t.TempDir())
	expiry := time.Now().Add(time.Hour)
	first, err := store.Upsert(testToken(t, "first@example.com", "subject-1", "refresh-1", expiry), "first", true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Upsert(testToken(t, "second@example.com", "subject-2", "refresh-2", expiry), "second", true)
	if err != nil {
		t.Fatal(err)
	}
	orphanID := "acct_orphan"
	orphanPath := filepath.Join(store.AccountsDir, orphanID+".json")
	if err := os.WriteFile(orphanPath, []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	unknownPath := filepath.Join(store.AccountsDir, "notes.txt")
	if err := os.WriteFile(unknownPath, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	unknownJSON := filepath.Join(store.AccountsDir, "notes.json")
	if err := os.WriteFile(unknownJSON, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if err := store.Backups.Snapshot(backup.Registry, []byte(fmt.Sprintf("backup-%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	before, err := store.Backups.Managed(backup.Registry)
	if err != nil {
		t.Fatal(err)
	}

	report, err := store.Clean(true)
	if err != nil {
		t.Fatal(err)
	}
	if report.Preserved {
		t.Error("registry should not be preserved")
	}
	if !reflect.DeepEqual(report.Snapshots, []string{orphanID + ".json"}) {
		t.Errorf("planned snapshots = %v", report.Snapshots)
	}
	if len(report.Backups) != len(before)-backup.Retention {
		t.Errorf("planned backups = %d, want %d", len(report.Backups), len(before)-backup.Retention)
	}
	if _, err := os.Stat(orphanPath); err != nil {
		t.Errorf("dry run removed orphan: %v", err)
	}

	if _, err := store.Clean(false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Errorf("orphan still exists: %v", err)
	}
	if _, err := os.Stat(unknownPath); err != nil {
		t.Errorf("unknown file was removed: %v", err)
	}
	if _, err := os.Stat(unknownJSON); err != nil {
		t.Errorf("unknown JSON file was removed: %v", err)
	}
	for _, account := range []Account{first.Account, second.Account} {
		if _, err := os.Stat(filepath.Join(store.AccountsDir, account.ID+".json")); err != nil {
			t.Errorf("known snapshot removed: %v", err)
		}
	}
	managed, err := store.Backups.Managed(backup.Registry)
	if err != nil {
		t.Fatal(err)
	}
	if len(managed) != backup.Retention {
		t.Errorf("backups after clean = %d, want %d", len(managed), backup.Retention)
	}
}

func TestCleanPreservesSnapshotsWithoutRegistry(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := os.MkdirAll(store.AccountsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	orphanPath := filepath.Join(store.AccountsDir, "acct_orphan.json")
	if err := os.WriteFile(orphanPath, []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := store.Clean(false)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Preserved {
		t.Error("expected snapshots to be preserved")
	}
	if _, err := os.Stat(orphanPath); err != nil {
		t.Errorf("snapshot was removed without a registry: %v", err)
	}
}

func TestCleanMalformedRegistryKeepsSnapshots(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := os.MkdirAll(store.AccountsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Registry, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	orphanPath := filepath.Join(store.AccountsDir, "acct_orphan.json")
	if err := os.WriteFile(orphanPath, []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Clean(false); err == nil {
		t.Fatal("expected malformed registry error")
	}
	if _, err := os.Stat(orphanPath); err != nil {
		t.Errorf("snapshot was removed after registry error: %v", err)
	}
}

func TestSortAccountsOrder(t *testing.T) {
	reg := &Registry{
		Active: "acct_b",
		Accounts: []Account{
			{ID: "acct_c", Email: "c@example.com"},
			{ID: "acct_a", Email: "a@example.com"},
			{ID: "acct_b", Email: "b@example.com"},
		},
	}
	reg.SortAccounts()
	want := []string{"acct_b", "acct_a", "acct_c"}
	for i, id := range want {
		if reg.Accounts[i].ID != id {
			t.Fatalf("order[%d] = %q, want %q", i, reg.Accounts[i].ID, id)
		}
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

func TestSaveTokenEncryptsAtRest(t *testing.T) {
	store := NewStore(t.TempDir())
	raw := testToken(t, "user@example.com", "subject-encrypt", "refresh-secret", time.Now().Add(time.Hour))
	result, err := store.Upsert(raw, "", true)
	if err != nil {
		t.Fatal(err)
	}
	path, err := store.AccountTokenPath(result.Account.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "refresh-secret") || strings.Contains(string(data), "access-refresh-secret") {
		t.Fatalf("snapshot contains plaintext credentials: %q", data)
	}
	if !store.Vault.IsEncrypted(data) {
		t.Fatalf("snapshot is not encrypted: %q", data)
	}
	loaded, err := store.LoadToken(result.Account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(loaded) != string(raw) {
		t.Error("round trip changed the credential")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !store.Vault.IsEncrypted(after) {
		t.Error("reading the snapshot decrypted it on disk")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("snapshot permissions = %o", info.Mode().Perm())
	}
	if _, err := os.Stat(store.Vault.KeyPath); err != nil {
		t.Errorf("key file missing: %v", err)
	}
}

func TestLoadTokenMigratesPlaintextSnapshot(t *testing.T) {
	store := NewStore(t.TempDir())
	raw := testToken(t, "legacy@example.com", "subject-legacy", "refresh-legacy", time.Now().Add(time.Hour))
	result, err := store.Upsert(raw, "", true)
	if err != nil {
		t.Fatal(err)
	}
	path, err := store.AccountTokenPath(result.Account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadToken(result.Account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(loaded) != string(raw) {
		t.Error("plaintext migration changed the credential")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !store.Vault.IsEncrypted(data) {
		t.Error("plaintext snapshot was not encrypted on load")
	}
}

func TestLoadMigratesV1Registry(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"version":1,"config":{"live_interval_seconds":42},"accounts":[]}`)
	if err := os.WriteFile(store.Registry, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reg.Version != Version {
		t.Errorf("version = %d", reg.Version)
	}
	if reg.Config == nil || reg.Config.SnapshotEncryption != snapshotEncryption || reg.Config.LiveIntervalSeconds != 42 {
		t.Errorf("config = %+v", reg.Config)
	}
	persisted, err := os.ReadFile(store.Registry)
	if err != nil {
		t.Fatal(err)
	}
	var saved Registry
	if err := json.Unmarshal(persisted, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Version != Version || saved.Config == nil || saved.Config.SnapshotEncryption != snapshotEncryption {
		t.Errorf("persisted registry = %+v", saved)
	}
}

func TestLoadMigratesPlaintextBackups(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if err := os.MkdirAll(store.Backups.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(store.Backups.Dir, "active-20250101T000000.000000000Z.json")
	if err := os.WriteFile(backupPath, []byte(`{"token":{"refresh_token":"backup-secret"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"version":1,"accounts":[]}`)
	if err := os.WriteFile(store.Registry, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if !store.Vault.IsEncrypted(data) || strings.Contains(string(data), "backup-secret") {
		t.Fatalf("backup was not encrypted: %q", data)
	}
}

func TestLoadRejectsNewerRegistryVersion(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := os.MkdirAll(store.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Registry, []byte(`{"version":99,"accounts":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("newer registry version was accepted")
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
