package management

// CUSTOM (cpamc-custom): tests for the quota cache helpers and reset extraction.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestCustomQuotaInWindow(t *testing.T) {
	cases := map[string]bool{
		"2026-10-01T12:29:00Z": false, // 09:29 UTC-3
		"2026-10-01T12:30:00Z": true,  // 09:30 UTC-3
		"2026-10-02T00:29:00Z": true,  // 21:29 UTC-3
		"2026-10-02T00:30:00Z": false, // 21:30 UTC-3
		"2026-10-02T05:00:00Z": false, // 02:00 UTC-3
	}
	for raw, want := range cases {
		now, _ := time.Parse(time.RFC3339, raw)
		if got := customQuotaInWindow(now); got != want {
			t.Errorf("customQuotaInWindow(%s) = %v, want %v", raw, got, want)
		}
	}
}

func collect(t *testing.T, body string, fetched time.Time) []time.Time {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatal(err)
	}
	return customQuotaCollectResets(v, "", fetched)
}

func TestCustomQuotaCollectResetsAcrossProviders(t *testing.T) {
	fetched := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		body string
		want time.Time
	}{
		{"claude resets_at", `{"five_hour":{"utilization":10,"resets_at":"2026-10-01T17:00:00Z"}}`, time.Date(2026, 10, 1, 17, 0, 0, 0, time.UTC)},
		{"codex reset_after_seconds", `{"rate_limit":{"primary_window":{"reset_after_seconds":3600}}}`, fetched.Add(time.Hour)},
		{"xai currentPeriod.end", `{"config":{"currentPeriod":{"start":"2026-09-24T23:55:00Z","end":"2026-10-01T23:55:00Z"}}}`, time.Date(2026, 10, 1, 23, 55, 0, 0, time.UTC)},
		{"xai billingPeriodEnd", `{"config":{"billingPeriodEnd":"2026-10-08T16:48:00Z"}}`, time.Date(2026, 10, 8, 16, 48, 0, 0, time.UTC)},
		{"devin unix seconds", `{"userStatus":{"planStatus":{"weeklyQuotaResetAtUnix":"1790899200"}}}`, time.Unix(1790899200, 0)},
		{"meta resets_at seconds", `{"subs_usage":{"weekly":{"resets_at":1790899200}}}`, time.Unix(1790899200, 0)},
		{"zai nextResetTime", `{"data":{"limits":[{"usage":10000,"nextResetTime":1791410021984}]}}`, time.UnixMilli(1791410021984)},
		{"minimax weekly_end_time", `{"model_remains":[{"weekly_end_time":1791158400000}]}`, time.UnixMilli(1791158400000)},
		{"antigravity resetTime", `{"groups":[{"buckets":[{"resetTime":"2026-10-08T00:00:00Z"}]}]}`, time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		got := collect(t, tc.body, fetched)
		if len(got) != 1 || !got[0].Equal(tc.want) {
			t.Errorf("%s: got %v, want [%v]", tc.name, got, tc.want)
		}
	}
	if got := collect(t, `{"currentPeriod":{"end":"x"},"end":"2026-10-01T00:00:00Z"}`, fetched); len(got) != 0 {
		t.Errorf("unrelated 'end' must be ignored, got %v", got)
	}
}

func TestCustomQuotaKeyIgnoresRotatingHeaders(t *testing.T) {
	a := customQuotaRecipe{AuthID: "x", Method: "GET", URL: "https://u", Header: map[string]string{"Authorization": "Bearer $TOKEN$"}}
	b := a
	b.Header = map[string]string{"Authorization": "Bearer other"}
	if customQuotaKey(a) != customQuotaKey(b) {
		t.Fatal("header values must not change the recipe key")
	}
	c := a
	c.URL = "https://v"
	if customQuotaKey(a) == customQuotaKey(c) {
		t.Fatal("different URLs must map to different keys")
	}
}

func TestCustomQuotaUnsafeURLs(t *testing.T) {
	for _, u := range []string{
		"https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume",
		"https://api.x.ai/v1/chat/completions",
	} {
		if !customQuotaUnsafeURL.MatchString(u) {
			t.Errorf("%s must not be cached/replayed", u)
		}
	}
	if customQuotaUnsafeURL.MatchString("https://cli-chat-proxy.grok.com/v1/billing") {
		t.Error("billing must be cacheable")
	}
}

func TestUsageSnapshotMasksRawKeys(t *testing.T) {
	payload := map[string]map[string]apiKeyUsageEntry{
		"codex": {"https://codex.example.com|secret-key": {Success: 3, Failed: 1}},
	}
	maskUsageSnapshotKeys(payload)
	for key := range payload["codex"] {
		if key == "https://codex.example.com|secret-key" {
			t.Fatal("snapshot must not contain the raw key")
		}
		if got := key; len(got) == 0 || got[:len("https://codex.example.com|sha256:")] != "https://codex.example.com|sha256:" {
			t.Fatalf("unexpected masked key %q", got)
		}
	}
}

func TestGetUsageSnapshotReadOnly(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")

	manager := coreauth.NewManager(nil, nil, nil)
	if _, err := manager.Register(context.Background(), &coreauth.Auth{
		ID:       "codex-auth",
		Provider: "codex",
		Attributes: map[string]string{
			"api_key":  "secret-key",
			"base_url": "https://codex.example.com",
		},
	}); err != nil {
		t.Fatalf("register codex auth: %v", err)
	}
	manager.MarkResult(context.Background(), coreauth.Result{AuthID: "codex-auth", Provider: "codex", Model: "gpt-5", Success: true})

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)
	h.refreshUsageSnapshot()

	first := func() (string, int64) {
		rec := httptest.NewRecorder()
		ginCtx, _ := gin.CreateTestContext(rec)
		ginCtx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/usage-snapshot", nil)
		h.GetUsageSnapshot(ginCtx)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var out struct {
			FetchedAt time.Time                              `json:"fetched_at"`
			Usage     map[string]map[string]apiKeyUsageEntry `json:"usage"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		for key := range out.Usage["codex"] {
			if key == "https://codex.example.com|secret-key" {
				t.Fatal("snapshot response must not contain the raw key")
			}
			return key, out.Usage["codex"][key].Success
		}
		t.Fatal("missing codex bucket")
		return "", 0
	}
	key1, success1 := first()

	manager.MarkResult(context.Background(), coreauth.Result{AuthID: "codex-auth", Provider: "codex", Model: "gpt-5", Success: true})
	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	ginCtx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/usage-snapshot", nil)
	h.GetUsageSnapshot(ginCtx)
	var out struct {
		Usage map[string]map[string]apiKeyUsageEntry `json:"usage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if got := out.Usage["codex"][key1].Success; got != success1 {
		t.Fatalf("endpoint refreshed data: success=%d, want stale %d", got, success1)
	}
}

func decodeBody(t *testing.T, body string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestCustomQuotaBodyExhausted(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		// Bodies captured from the live xAI billing endpoint on 2026-10-13.
		{"xai used up", `{"config":{"currentPeriod":{"end":"2026-10-15T16:48:47Z"},"creditUsagePercent":100.0,"productUsage":[{"product":"GrokBuild","usagePercent":100.0}]}}`, true},
		{"xai quota left", `{"config":{"currentPeriod":{"end":"2026-10-15T23:55:01Z"},"creditUsagePercent":41.0,"productUsage":[{"product":"GrokBuild","usagePercent":41.0}]}}`, false},
		{"codex limit reached", `{"rate_limit":{"limit_reached":true,"primary_window":{"used_percent":100}}}`, true},
		{"codex quota left", `{"rate_limit":{"limit_reached":false,"primary_window":{"used_percent":0}}}`, false},
		{"codex per-model window ignored", `{"rate_limit":{"limit_reached":false},"additional_rate_limits":[{"limit_reached":true,"primary_window":{"used_percent":100}}]}`, false},
		{"claude weekly used up", `{"five_hour":{"utilization":7.0},"seven_day":{"utilization":100.0}}`, true},
		{"claude weekly nearly used", `{"five_hour":{"utilization":7.0},"seven_day":{"utilization":97.0}}`, false},
		{"meta weekly used up", `{"subs_usage":{"window":{"used_percent":6},"weekly":{"used_percent":100}}}`, true},
		{"minimax per-model ignored", `{"model_remains":[{"current_interval_remaining_percent":0}]}`, false},
	}
	for _, tc := range cases {
		if got := customQuotaBodyExhausted(decodeBody(t, tc.body), ""); got != tc.want {
			t.Errorf("%s: exhausted = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestCustomQuotaRankProviderSkipsUsedUpCredential(t *testing.T) {
	soon := time.Date(2026, 10, 15, 16, 48, 0, 0, time.UTC)
	later := time.Date(2026, 10, 15, 23, 55, 0, 0, time.UTC)

	// The live incident: the soonest-reset xAI account is used up, the other has 59% left.
	boosts, _, ok := customQuotaRankProvider([]customQuotaCandidate{
		{id: "xai-kevynsax", label: "kevynsax", reset: soon, exhausted: true},
		{id: "xai-kirschner", label: "kirschner", reset: later},
	})
	if !ok {
		t.Fatal("two xAI credentials must be ranked")
	}
	if boosts["xai-kirschner"] <= boosts["xai-kevynsax"] {
		t.Fatalf("credential with quota left must outrank the used-up one: %v", boosts)
	}
	if boosts["xai-kevynsax"] >= 0 {
		t.Fatalf("used-up credential must sit below unboosted credentials: %v", boosts)
	}

	// With quota left on both, the soonest reset still wins.
	boosts, _, _ = customQuotaRankProvider([]customQuotaCandidate{
		{id: "a", label: "a", reset: soon},
		{id: "b", label: "b", reset: later},
	})
	if boosts["a"] <= boosts["b"] {
		t.Fatalf("soonest reset must win when both have quota: %v", boosts)
	}

	// An unknown reset with quota left beats a used-up credential.
	boosts, _, ok = customQuotaRankProvider([]customQuotaCandidate{
		{id: "used", label: "used", reset: soon, exhausted: true},
		{id: "fresh", label: "fresh"},
	})
	if !ok || boosts["fresh"] <= boosts["used"] {
		t.Fatalf("fresh credential must outrank used-up one: ok=%v %v", ok, boosts)
	}

	// A single credential is never ranked: it keeps serving and takes its cooldown.
	if _, _, ok = customQuotaRankProvider([]customQuotaCandidate{{id: "only", reset: soon, exhausted: true}}); ok {
		t.Fatal("a single credential must not be ranked")
	}
}

func TestCustomQuotaExhaustedByAuthIgnoresRolledOverWindow(t *testing.T) {
	customQuota.mu.Lock()
	saved, savedLoaded := customQuota.entries, customQuota.loaded
	customQuota.entries = map[string]*customQuotaEntry{
		"used": {Recipe: customQuotaRecipe{AuthID: "used"}, StatusCode: 200,
			Body: `{"config":{"currentPeriod":{"end":"2026-10-15T16:48:47Z"},"creditUsagePercent":100}}`},
		"rolled": {Recipe: customQuotaRecipe{AuthID: "rolled"}, StatusCode: 200,
			Body: `{"config":{"currentPeriod":{"end":"2026-10-12T16:48:47Z"},"creditUsagePercent":100}}`},
		"left": {Recipe: customQuotaRecipe{AuthID: "left"}, StatusCode: 200,
			Body: `{"config":{"currentPeriod":{"end":"2026-10-15T23:55:01Z"},"creditUsagePercent":41}}`},
	}
	customQuota.loaded = true
	customQuota.mu.Unlock()
	t.Cleanup(func() {
		customQuota.mu.Lock()
		customQuota.entries, customQuota.loaded = saved, savedLoaded
		customQuota.mu.Unlock()
	})

	now := time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)
	got := (&Handler{}).customQuotaExhaustedByAuth(now)
	if !got["used"] || got["rolled"] || got["left"] {
		t.Fatalf("exhausted = %v, want only \"used\"", got)
	}
}
