package management

// CUSTOM (cpamc-custom): tests for the non-management quota view.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestQuotaViewScrubDropsIdentity(t *testing.T) {
	raw := "{" +
		`"email":"ada@example.com","plan_type":"pro",` +
		`"account":{"name":"Ada Lovelace","has_claude_pro":true},` +
		`"rate_limit":{"primary_window":{"used_percent":12,"note":"mail ada@example.com"}},` +
		`"access_token":"secret"}`
	scrubbed, _ := quotaViewBody(raw).(map[string]any)
	got, _ := json.Marshal(scrubbed)
	body := string(got)
	for _, leaked := range []string{"ada@example.com", "Ada Lovelace", "secret"} {
		if strings.Contains(body, leaked) {
			t.Errorf("view leaked %q in %s", leaked, body)
		}
	}
	if _, kept := scrubbed["access_token"]; kept {
		t.Errorf("access_token key kept: %s", body)
	}
	if scrubbed["plan_type"] != "pro" || scrubbed["rate_limit"] == nil {
		t.Errorf("view dropped quota data: %s", body)
	}
	account, _ := scrubbed["account"].(map[string]any)
	if account["has_claude_pro"] != true || account["name"] != nil {
		t.Errorf("account scrub wrong: %v", account)
	}
	if quotaViewBody("not json") != nil {
		t.Fatal("non-JSON bodies must be dropped")
	}
}

func TestQuotaViewURLStripsQuery(t *testing.T) {
	got := quotaViewURL("https://chatgpt.com/backend-api/subscriptions?account_id=acct_123")
	if got != "https://chatgpt.com/backend-api/subscriptions" {
		t.Fatalf("url = %q", got)
	}
	weekly := quotaViewURL("https://cli-chat-proxy.grok.com/v1/billing?format=credits&account_id=acct_123")
	if weekly != "https://cli-chat-proxy.grok.com/v1/billing?format=credits" {
		t.Fatalf("weekly url = %q", weekly)
	}
	monthly := quotaViewURL("https://cli-chat-proxy.grok.com/v1/billing?account_id=acct_123")
	if monthly != "https://cli-chat-proxy.grok.com/v1/billing" {
		t.Fatalf("monthly url = %q", monthly)
	}
}

func TestCustomQuotaViewReadOnlyAndSanitized(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")

	manager := coreauth.NewManager(nil, nil, nil)
	if _, err := manager.Register(nil, &coreauth.Auth{ID: "codex-auth", Provider: "codex"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := manager.Register(nil, &coreauth.Auth{ID: "minimax-auth", Provider: "openai-compatibility",
		Attributes: map[string]string{"compat_name": "MiniMax"}}); err != nil {
		t.Fatalf("register minimax: %v", err)
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)

	customQuota.mu.Lock()
	customQuota.loaded = true
	customQuota.path = ""
	customQuota.entries = map[string]*customQuotaEntry{
		"a": {Recipe: customQuotaRecipe{AuthID: "codex-auth", Provider: "codex", URL: "https://chatgpt.com/backend-api/wham/usage"},
			StatusCode: 200, Body: `{"plan_type":"plus","email":"ada@example.com","rate_limit":{"primary_window":{"used_percent":40}}}`, FetchedAt: time.Now().UTC()},
		"b": {Recipe: customQuotaRecipe{AuthID: "codex-auth", Provider: "codex", URL: "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume"},
			StatusCode: 200, Body: `{"consumed":true}`, FetchedAt: time.Now().UTC()},
		"c": {Recipe: customQuotaRecipe{AuthID: "minimax-auth", Provider: "openai-compatibility", URL: "https://api.minimax.io/v1/api/openplatform/coding_plan/remains"},
			StatusCode: 200, Body: `{"model_remains":[{"current_interval_remaining_percent":70}]}`, FetchedAt: time.Now().UTC()},
	}
	customQuota.mu.Unlock()
	t.Cleanup(func() {
		customQuota.mu.Lock()
		customQuota.entries = map[string]*customQuotaEntry{}
		customQuota.loaded = false
		customQuota.mu.Unlock()
	})

	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	ginCtx.Request = httptest.NewRequest(http.MethodGet, "/v1/quota/view", nil)
	h.CustomQuotaView(ginCtx)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "ada@example.com") || strings.Contains(body, "codex-auth") || strings.Contains(body, "consume") {
		t.Fatalf("view leaked identity, auth id, or unsafe recipe: %s", body)
	}
	var out struct {
		Credentials []quotaViewCredential `json:"credentials"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Credentials) != 2 {
		t.Fatalf("credentials = %+v", out.Credentials)
	}
	var sawMinimax bool
	for _, cred := range out.Credentials {
		if cred.Provider == "minimax" {
			sawMinimax = true
		}
		if strings.Contains(cred.Provider, "compatib") {
			t.Fatalf("compat provider leaked: %+v", cred)
		}
	}
	if !sawMinimax {
		t.Fatalf("minimax credential missing: %+v", out.Credentials)
	}
	if out.Credentials[0].ID == "" || out.Credentials[0].Provider != "codex" {
		t.Fatalf("credential = %+v", out.Credentials[0])
	}
}
