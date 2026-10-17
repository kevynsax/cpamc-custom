package management

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/usagehistory"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

func TestClientUsageReturnsSeparateSparkEstimateWhileCollectionPaused(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := usagehistory.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	record := usage.Record{Provider: "openai-compatible-spark", Model: "qwen-vl", ResponseModel: "/models/Qwen3-VL-30B-A3B-Instruct-Q4_K_M.gguf", APIKey: "fixture-key", AuthID: "fixture-spark", Detail: usage.Detail{InputTokens: 1000, CacheReadTokens: 800, OutputTokens: 200, TotalTokens: 1200}}
	if err := store.RecordPriced(record, nil); err != nil {
		t.Fatal(err)
	}
	h := &Handler{cfg: &config.Config{}, clientUsage: store}
	router := gin.New()
	router.GET("/usage", h.GetClientUsage)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/usage", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d", response.Code)
	}
	var payload struct {
		Enabled bool                        `json:"collection_enabled"`
		Rows    []usagehistory.ReferenceRow `json:"rows"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Enabled || len(payload.Rows) != 1 || payload.Rows[0].CostUSD != nil || payload.Rows[0].ReferenceCostUSD == nil || math.Abs(*payload.Rows[0].ReferenceCostUSD-.00027) > 1e-12 || payload.Rows[0].ModelAlias != "qwen-vl" || payload.Rows[0].UnpricedRequests != 1 {
		t.Fatalf("incorrect reference contract: %+v", payload)
	}
}
