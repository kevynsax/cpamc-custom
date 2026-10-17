package management

import (
	"context"
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

func TestClientUsageHistoryContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := usagehistory.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.APIKeys = []string{"test-client-secret-abcd"}
	cfg.UsageStatisticsEnabled = true
	h := &Handler{cfg: cfg, clientUsage: store}
	plugin := clientUsagePlugin{handler: h, store: store}
	record := usage.Record{APIKey: cfg.APIKeys[0], AuthID: "subscription", Provider: "codex", AuthType: "oauth", Detail: usage.Detail{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}}
	plugin.HandleUsage(context.Background(), record)
	cfg.UsageStatisticsEnabled = false
	plugin.HandleUsage(context.Background(), record)
	router := gin.New()
	router.GET("/usage", h.GetClientUsage)
	for _, test := range []struct {
		query  string
		status int
	}{
		{"", http.StatusOK},
		{"?from=invalid", http.StatusBadRequest},
		{"?from=2020-01-01", http.StatusBadRequest},
		{"?to=2999-01-01", http.StatusBadRequest},
		{"?from=" + time.Now().UTC().Format(usagehistory.DateLayout) + "&to=2020-01-01", http.StatusBadRequest},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/usage"+test.query, nil))
		if recorder.Code != test.status {
			t.Fatalf("query %s status=%d body=%s", test.query, recorder.Code, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), cfg.APIKeys[0]) {
			t.Fatal("client secret exposed")
		}
		if test.query != "" {
			continue
		}
		var payload struct {
			Rows      []usagehistory.Row `json:"rows"`
			Enabled   bool               `json:"collection_enabled"`
			Labels    map[string]string  `json:"key_labels"`
			Timezone  string             `json:"timezone"`
			Retention int                `json:"retention_days"`
		}
		if errDecode := json.Unmarshal(recorder.Body.Bytes(), &payload); errDecode != nil {
			t.Fatal(errDecode)
		}
		if len(payload.Rows) != 1 || payload.Rows[0].Requests != 1 || payload.Rows[0].TotalTokens != 15 || payload.Enabled || payload.Timezone != "UTC" || payload.Retention != 365 {
			t.Fatalf("wrong payload: %+v", payload)
		}
		if payload.Labels[usagehistory.Fingerprint(cfg.APIKeys[0])] != "••••abcd" {
			t.Fatalf("wrong masked labels: %+v", payload.Labels)
		}
	}
}

func TestClientUsageUnavailable(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	(&Handler{}).GetClientUsage(ctx)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", recorder.Code)
	}
}
