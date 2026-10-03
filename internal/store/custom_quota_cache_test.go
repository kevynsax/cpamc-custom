package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

// CUSTOM (cpamc-custom): mirrored auth stores must ignore internal quota state too.
func TestTokenStoresListSkipsCustomQuotaCache(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T) (string, func(context.Context) ([]*cliproxyauth.Auth, error))
	}{
		{
			name: "git",
			setup: func(t *testing.T) (string, func(context.Context) ([]*cliproxyauth.Auth, error)) {
				root := t.TempDir()
				remote := setupGitRemoteRepository(t, root, "master", testBranchSpec{name: "master", contents: "quota cache test\n"})
				store := NewGitTokenStore(remote, "", "", "")
				store.SetBaseDir(filepath.Join(root, "workspace", "auths"))
				if err := store.EnsureRepository(); err != nil {
					t.Fatal(err)
				}
				return store.AuthDir(), store.List
			},
		},
		{
			name: "object",
			setup: func(t *testing.T) (string, func(context.Context) ([]*cliproxyauth.Auth, error)) {
				store := &ObjectTokenStore{authDir: t.TempDir()}
				return store.AuthDir(), store.List
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, list := tt.setup(t)
			cache := []byte(`{"entries":{},"last_run":"2026-10-02T12:00:00Z"}`)
			for name, data := range map[string][]byte{
				"account.json":                                               []byte(`{"type":"claude"}`),
				filepath.Join("nested", "account.json"):                      []byte(`{"type":"codex"}`),
				filepath.Join(".cpamc-custom", "quota-cache.json"):           cache,
				filepath.Join("nested", ".cpamc-custom", "quota-cache.json"): cache,
				filepath.Join(".cpamc-custom", "nested", "credential.json"):  []byte(`{"type":"claude"}`),
			} {
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			auths, err := list(context.Background())
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
			data, err := os.ReadFile(filepath.Join(dir, ".cpamc-custom", "quota-cache.json"))
			if err != nil || string(data) != string(cache) {
				t.Fatalf("quota cache was changed or removed: %s, %v", data, err)
			}
		})
	}
}
