package auth

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// CUSTOM (cpamc-custom): internal quota state must not become an auth credential.
func TestFileTokenStoreListSkipsCustomQuotaCache(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, ".cpamc-custom", "quota-cache.json")
	cache := []byte(`{"entries":{},"last_run":"2026-10-02T12:00:00Z"}`)
	files := map[string][]byte{
		"account.json":                          []byte(`{"type":"claude"}`),
		filepath.Join("nested", "account.json"): []byte(`{"type":"codex"}`),
		filepath.Join("nested", ".cpamc-custom", "quota-cache.json"): cache,
		filepath.Join(".cpamc-custom", "quota-cache.json"):           cache,
		filepath.Join(".cpamc-custom", "nested", "credential.json"):  []byte(`{"type":"claude"}`),
	}
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store := NewFileTokenStore()
	store.SetBaseDir(dir)
	auths, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(auths) != 2 {
		t.Fatalf("List returned %d credentials, want only the 2 real credentials", len(auths))
	}
	want := map[string]bool{"account.json": true, filepath.Join("nested", "account.json"): true}
	for _, auth := range auths {
		if !want[auth.ID] {
			t.Errorf("unexpected credential %q", auth.ID)
		}
		delete(want, auth.ID)
	}
	if len(want) != 0 {
		t.Errorf("missing real credentials: %v", want)
	}
	data, err := os.ReadFile(cachePath)
	if err != nil || string(data) != string(cache) {
		t.Fatalf("quota cache was changed or removed: %s, %v", data, err)
	}
}
