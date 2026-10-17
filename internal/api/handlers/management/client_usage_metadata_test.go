package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/usagehistory"
)

func TestClientKeyNamesWorkWithTrackingPaused(t *testing.T) {
	store, err := usagehistory.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.APIKeys = []string{"fixture-key"}
	h := &Handler{cfg: cfg, clientUsage: store}
	id := usagehistory.Fingerprint("fixture-key")
	if h.clientKeyMetadata().Kinds[id] != "person" {
		t.Fatal("existing key did not default to Person")
	}
	router := gin.New()
	router.GET("/names", h.GetClientKeyNames)
	router.PATCH("/names", h.PatchClientKeyNames)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, "/names", strings.NewReader(`{"names":{"`+id+`":"Alice"}}`)))
	if recorder.Code != http.StatusOK {
		t.Fatal(recorder.Body.String())
	}
	if store.KeyNames()[id] != "Alice" {
		t.Fatal("paused tracking blocked metadata")
	}
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, "/names", strings.NewReader(`{"kinds":{"`+id+`":"system"}}`)))
	if recorder.Code != http.StatusOK || h.clientKeyMetadata().Kinds[id] != "system" {
		t.Fatal("cannot save System while tracking paused")
	}
	if store.KeyNames()[id] != "Alice" {
		t.Fatal("type-only patch discarded name")
	}
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/names", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "Alice") || strings.Contains(recorder.Body.String(), "fixture-key") {
		t.Fatal("invalid names response")
	}
}

func TestCachedClientQuotaIsReadOnlyAndIndependentOfHistory(t *testing.T) {
	previous := customQuota
	customQuota = &customQuotaState{loaded: true, entries: map[string]*customQuotaEntry{}}
	t.Cleanup(func() { customQuota = previous })
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	for index, observation := range []struct {
		used int
		age  time.Duration
	}{{10, time.Minute * 10}, {30, time.Minute}} {
		body, _ := json.Marshal(map[string]any{"plan_type": "pro", "rate_limit": map[string]any{"secondary_window": map[string]any{"used_percent": observation.used, "reset_at": now.Add(time.Hour).Unix(), "limit_window_seconds": 604800}}})
		customQuota.entries[string(rune('a'+index))] = &customQuotaEntry{Recipe: customQuotaRecipe{AuthID: "raw-auth-id", Provider: "codex", URL: "https://example.test/quota"}, StatusCode: 200, FetchedAt: now.Add(-observation.age), Body: string(body)}
	}
	h := &Handler{cfg: &config.Config{}}
	cached := h.clientCachedQuotas()
	account := cached[usagehistory.Fingerprint("raw-auth-id")]
	if account.Plan != "pro" || len(account.Quotas) != 1 || account.Quotas[0].Used != 30 || !account.Quotas[0].ObservedAt.Equal(now.Add(-time.Minute)) {
		t.Fatalf("incorrect cache summary: %+v", account)
	}
	if len(customQuota.entries) != 2 || h.clientUsage != nil {
		t.Fatal("cache read mutated history")
	}
	serialized, _ := json.Marshal(cached)
	if strings.Contains(string(serialized), "raw-auth-id") || strings.Contains(string(serialized), "example.test") {
		t.Fatal("cache recipe exposed")
	}
}
