package usagehistory

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

const qwenVLResponseFile = "/models/qwen-vl/Qwen3-VL-30B-A3B-Instruct-Q4_K_M.gguf"

func TestInHouseCacheUsesRegularInputWithoutInventingDiscount(t *testing.T) {
	var pricing *Pricing
	record := usage.Record{Provider: "openai-compatible-spark", Alias: "qwen-vl", Model: "qwen-vl", ResponseModel: qwenVLResponseFile, Detail: usage.Detail{InputTokens: 1000, CacheReadTokens: 800, OutputTokens: 200, TotalTokens: 1200}}
	rate := pricing.Resolve(record, nil)
	if rate == nil || rate.CacheRead == nil || *rate.CacheRead != rate.Input || rate.Input != .15 {
		t.Fatalf("missing regular-input fallback: %+v", rate)
	}
	detail := usage.EnsureTokenBreakdownForProvider(record.Detail, record.Provider, "OpenAICompatExecutor")
	cost := EstimateCost(detail, rate)
	if cost == nil || math.Abs(*cost-.00027) > 1e-12 {
		t.Fatalf("cache double-counted or omitted: %v", cost)
	}
	record.Alias = ""
	record.Model = "opaque-route"
	if rate := pricing.Resolve(record, nil); rate == nil || rate.Input != .15 {
		t.Fatal("exact response filename did not resolve")
	}
	discount := pricing.Resolve(usage.Record{Provider: "spark", Model: "gpt-oss-120b"}, nil)
	if discount == nil || discount.CacheRead == nil || *discount.CacheRead != .075 {
		t.Fatal("published cache discount overwritten")
	}
}

func TestInHouseReferenceEstimatesDoNotRewriteRecordedHistory(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	store, err := Open(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	record := usage.Record{RequestedAt: now, Provider: "openai-compatible-spark", APIKey: "fixture-client", AuthID: "fixture-spark-account", Model: "qwen-vl", ResponseModel: qwenVLResponseFile, Detail: usage.Detail{InputTokens: 1000, CacheReadTokens: 800, OutputTokens: 200, TotalTokens: 1200}}
	if err := store.RecordPriced(record, nil); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "2026-10-17.json")
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := store.Snapshot("2026-10-17", "2026-10-17")
	if err != nil {
		t.Fatal(err)
	}
	reference := WithInHouseReferenceEstimates(rows)
	if len(reference) != 1 || reference[0].ReferenceCostUSD == nil || math.Abs(*reference[0].ReferenceCostUSD-.00027) > 1e-12 {
		t.Fatalf("missing historical comparison: %+v", reference)
	}
	if reference[0].ModelAlias != "qwen-vl" || reference[0].ReferencePrice.CacheRead == nil || *reference[0].ReferencePrice.CacheRead != .15 {
		t.Fatal("reference identity/cache policy lost")
	}
	if rows[0].CostUSD != nil || rows[0].Price != nil || reference[0].CostUSD != nil || reference[0].UnpricedRequests != 1 {
		t.Fatal("original accounting was modified")
	}
	encoded, _ := json.Marshal(reference[0])
	if !bytes.Contains(encoded, []byte(`"reference_cost_usd"`)) || !bytes.Contains(encoded, []byte(`"cost_usd":null`)) {
		t.Fatal("recorded and reference costs were not separate")
	}
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("read-time estimate rewrote storage")
	}
	reopened, err := Open(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	retained, _ := reopened.Snapshot("2026-10-17", "2026-10-17")
	if retained[0].CostUSD != nil || retained[0].UnpricedRequests != 1 {
		t.Fatal("historical accounting changed on restart")
	}
}

func TestInHouseReferenceEstimatesRespectFrozenCostsAndIncompleteData(t *testing.T) {
	base := Row{Provider: "spark", Model: qwenVLResponseFile, Requests: 1, UnpricedRequests: 1, InputTokens: 1000, OutputTokens: 200, CachedTokens: 800, TotalTokens: 1200}
	paid := base
	paid.CostUSD = ptr(42)
	invalid := base
	invalid.CachedTokens = 2000
	wrongSize := base
	wrongSize.Model = "/models/Qwen3-VL-8B-Instruct-Q4_K_M.gguf"
	native := base
	native.Provider = "codex"
	manual := base
	manual.Price = &Rates{Input: 9, Output: 10, Source: "manual"}
	for _, row := range []Row{paid, invalid, wrongSize, native, manual} {
		if WithInHouseReferenceEstimates([]Row{row})[0].ReferenceCostUSD != nil {
			t.Fatalf("unsupported history was repriced: %s", row.Model)
		}
	}
	if WithInHouseReferenceEstimates([]Row{paid})[0].ModelAlias != "qwen-vl" {
		t.Fatal("priced and estimated aliases cannot merge")
	}
}

func TestInHouseReferencePreservesRecordedOpenRouterRates(t *testing.T) {
	row := Row{Provider: "openai-compatible-spark", Model: "spark/qwen-vl", Requests: 1, UnpricedRequests: 1, InputTokens: 1000, OutputTokens: 200, CachedTokens: 800, TotalTokens: 1200, Price: &Rates{Input: 2, Output: 3, Source: "https://openrouter.ai/qwen/qwen3-vl-30b-a3b-instruct", AsOf: "2026-01-01"}}
	reference := WithInHouseReferenceEstimates([]Row{row})[0]
	if reference.ReferenceCostUSD == nil || math.Abs(*reference.ReferenceCostUSD-.0026) > 1e-12 {
		t.Fatal("recorded reference rate was replaced with today's tariff")
	}
	if row.Price.CacheRead != nil || reference.ModelAlias != "qwen-vl" {
		t.Fatal("rate was mutated or public alias was not normalized")
	}
}
