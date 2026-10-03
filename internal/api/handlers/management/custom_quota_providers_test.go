package management

// CUSTOM (cpamc-custom): API-key quota discovery never exposes credentials.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestCustomQuotaAPIKeyProvider(t *testing.T) {
	for _, tc := range []struct {
		name, base, key, want string
	}{
		{"MiniMax", "", "test-key", "minimax"},
		{"other", "https://api.minimax.io/v1", "test-key", "minimax"},
		{"other", "https://api.minimaxi.com/v1", "test-key", "minimax"},
		{"Z.ai", "", "test-key", "zai"},
		{"zhipu", "", "test-key", "zai"},
		{"other", "https://api.z.ai/api/coding/paas/v4", "test-key", "zai"},
		{"other", "https://open.bigmodel.cn/api/paas/v4", "test-key", "zai"},
		{"other", "https://api.z.ai.example.test/v1", "test-key", ""},
		{"other", "https://example.test/z.ai", "test-key", ""},
		{"other", "https://z.ai@example.test/v1", "test-key", ""},
		{"minimax", "https://api.minimax.io/v1", "", ""},
		{"xai", "https://api.x.ai/v1", "test-key", ""},
	} {
		t.Run(tc.name+tc.base+tc.key, func(t *testing.T) {
			auth := &coreauth.Auth{Attributes: map[string]string{"compat_name": tc.name, "base_url": tc.base, "api_key": tc.key}}
			if got := customQuotaAPIKeyProvider(auth); got != tc.want {
				t.Fatalf("provider = %q, want %q", got, tc.want)
			}
		})
	}
	if customQuotaAPIKeyProvider(nil) != "" {
		t.Fatal("nil auth must not support quota")
	}
}

func TestCustomQuotaCredentialsListsDistinctKeysWithoutSecrets(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	for _, auth := range []*coreauth.Auth{
		{ID: "minimax-1", Provider: "minimax", Attributes: map[string]string{"compat_name": "minimax", "api_key": "secret-minimax-1"}},
		{ID: "minimax-2", Provider: "minimax", Attributes: map[string]string{"compat_name": "minimax", "api_key": "secret-minimax-2"}},
		{ID: "zai-1", Provider: "zai", Attributes: map[string]string{"compat_name": "zai", "api_key": "secret-zai"}},
		{ID: "disabled", Provider: "zai", Disabled: true, Attributes: map[string]string{"compat_name": "zai", "api_key": "secret-disabled"}},
		{ID: "disabled-status", Provider: "minimax", Status: coreauth.StatusDisabled, Attributes: map[string]string{"compat_name": "minimax", "api_key": "secret-disabled-status"}},
		{ID: "keyless", Provider: "zai", Attributes: map[string]string{"compat_name": "zai"}},
		{ID: "xai", Provider: "xai", Attributes: map[string]string{"api_key": "secret-xai"}},
	} {
		if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
			t.Fatalf("register credential: %v", errRegister)
		}
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v8/management/custom/quota/credentials", nil)
	h.CustomQuotaCredentials(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret-") || strings.Contains(rec.Body.String(), "api_key") {
		t.Fatal("credential list exposed a secret field")
	}
	var payload struct {
		Files []struct {
			Index  string `json:"auth_index"`
			Type   string `json:"type"`
			Source string `json:"quota_source"`
		} `json:"files"`
	}
	if errUnmarshal := json.Unmarshal(rec.Body.Bytes(), &payload); errUnmarshal != nil {
		t.Fatalf("decode credentials: %v", errUnmarshal)
	}
	if len(payload.Files) != 3 {
		t.Fatalf("listed %d credentials, want 3", len(payload.Files))
	}
	seen := map[string]bool{}
	for _, file := range payload.Files {
		if file.Index == "" || seen[file.Index] || file.Source != "api-key" {
			t.Fatal("API-key credentials need distinct public indexes and source")
		}
		seen[file.Index] = true
	}
}
