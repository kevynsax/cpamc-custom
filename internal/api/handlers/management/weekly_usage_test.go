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
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

func TestWeeklyUsageQuotaObserverAndOvernightSchedule(t *testing.T) {
	now := time.Date(2026, 6, 10, 5, 0, 0, 0, time.UTC)
	store, err := usagehistory.Open(t.TempDir(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.UsageStatisticsEnabled = true
	h := &Handler{cfg: cfg, clientUsage: store}
	if !h.clientQuotaRefreshAllowed(now) {
		t.Fatal("nighttime collection disabled")
	}
	recipe := customQuotaRecipe{AuthID: "subscription", Provider: "claude", Method: "GET"}
	reset := now.Add(48 * time.Hour).Format(time.RFC3339)
	first := apiCallResponse{StatusCode: 200, Body: `{"plan_type":"Claude Max","seven_day":{"utilization":20,"resets_at":"` + reset + `"}}`}
	h.observeClientQuota(recipe, first, now)
	record := usage.Record{RequestedAt: now.Add(5 * time.Minute), APIKey: "person", AuthID: "subscription", Provider: "claude", Model: "model", Detail: usage.Detail{InputTokens: 100, OutputTokens: 20}}
	if errRecord := store.RecordPriced(record, &usagehistory.Rates{Input: 2, Output: 10}); errRecord != nil {
		t.Fatal(errRecord)
	}
	now = now.Add(10 * time.Minute)
	second := apiCallResponse{StatusCode: 200, Body: `{"seven_day":{"utilization":40,"resets_at":"` + reset + `"}}`}
	h.observeClientQuota(recipe, second, now)
	weeks, errWeeks := store.Weeks()
	if errWeeks != nil {
		t.Fatal(errWeeks)
	}
	if len(weeks) != 1 || weeks[0].Used != 40 || weeks[0].Unattributed != 20 || len(weeks[0].People) != 1 || weeks[0].People[0].Percent != 20 {
		t.Fatalf("weeks=%+v", weeks)
	}
	cfg.UsageStatisticsEnabled = false
	if h.clientQuotaRefreshAllowed(now) {
		t.Fatal("disabled history still enables overnight polls")
	}
	h.observeClientQuota(recipe, apiCallResponse{StatusCode: 200, Body: `{"seven_day":{"utilization":60,"resets_at":"` + reset + `"}}`}, now.Add(10*time.Minute))
	weeks, _ = store.Weeks()
	if weeks[0].Used != 40 {
		t.Fatal("disabled quota observation collected")
	}
}

func TestWeeklyUsageSettingsEndpoint(t *testing.T) {
	store, err := usagehistory.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{clientUsage: store}
	router := gin.New()
	router.PUT("/settings", h.PutClientUsageSettings)
	key := usagehistory.Fingerprint("client-secret")
	body := `{"names":{"` + key + `":"Ada"},"plans":{},"prices":{"model":{"input":2,"output":10}}}`
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(body)))
	if recorder.Code != 200 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var settings usagehistory.Settings
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &settings); errDecode != nil {
		t.Fatal(errDecode)
	}
	if settings.Names[key] != "Ada" || settings.Prices["model"].Input != 2 {
		t.Fatalf("settings=%+v", settings)
	}
	for _, bad := range []string{`{"names":{"raw-secret":"Ada"}}`, `{"prices":{"bad":{"input":-1,"output":10}}}`} {
		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(bad)))
		if recorder.Code != 400 {
			t.Fatalf("bad settings status=%d", recorder.Code)
		}
	}
	if store.Settings().Names[key] != "Ada" {
		t.Fatal("invalid settings replaced valid names")
	}
}
