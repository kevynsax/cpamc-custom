package usagehistory

import (
	"math"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

func ptr(v float64) *float64 { return &v }

func TestPricingUsesCanonicalNonOverlappingTokens(t *testing.T) {
	rate := &Rates{Input: 3, Output: 15, CacheRead: ptr(.3), CacheWrite: ptr(3.75)}
	detail := usage.EnsureTokenBreakdownForProvider(usage.Detail{InputTokens: 100, OutputTokens: 20, CacheReadTokens: 40, CacheCreationTokens: 10}, "claude", "claude")
	cost := EstimateCost(detail, rate)
	// Claude input is uncached; cache read/write are independent buckets.
	expected := (100*3 + 20*15 + 40*.3 + 10*3.75) / 1e6
	if cost == nil || math.Abs(*cost-expected) > 1e-12 {
		t.Fatalf("cost=%v expected=%v breakdown=%+v", cost, expected, detail.TokenBreakdown)
	}
	rate.CacheWrite = nil
	if EstimateCost(detail, rate) != nil {
		t.Fatal("unknown cache price assumed free")
	}
	detail.TokenBreakdown = usage.NewUnclassifiedTokenBreakdown(100)
	if EstimateCost(detail, rate) != nil {
		t.Fatal("unclassified tokens were priced")
	}
}

func TestCatalogResolutionAndLongContext(t *testing.T) {
	data := []byte(`{"openai":{"models":{"model":{"cost":{"input":2,"output":10,"cache_read":0.2,"tiers":[{"input":4,"output":15,"cache_read":0.4,"tier":{"type":"context","size":100}}]}}}},"anthropic":{"models":{"ambiguous":{"cost":{"input":3,"output":15}}}},"google":{"models":{"ambiguous":{"cost":{"input":1,"output":2}}}}}`)
	catalog, err := ParsePublicPrices(data, "2026-06-10")
	if err != nil {
		t.Fatal(err)
	}
	pricing := &Pricing{catalog: catalog}
	record := usage.Record{Provider: "codex", Model: "model", Detail: usage.Detail{InputTokens: 101, OutputTokens: 1}}
	rate := pricing.Resolve(record, nil)
	if rate == nil || rate.Input != 4 || rate.Output != 15 {
		t.Fatalf("wrong context tier: %+v", rate)
	}
	record.Model = "ambiguous"
	record.Provider = "compatible"
	if pricing.Resolve(record, nil) != nil {
		t.Fatal("ambiguous vendor price guessed")
	}
	record.Model = "missing"
	if pricing.Resolve(record, nil) != nil {
		t.Fatal("unknown model price guessed")
	}
	record.ServiceTier = "priority"
	if pricing.Resolve(record, map[string]Rates{"missing": {Input: 9, Output: 10}}) == nil {
		t.Fatal("explicit override not used")
	}
	record.Model = "model"
	record.Provider = "codex"
	if rate := pricing.Resolve(record, nil); rate == nil || rate.Input != 8 || rate.Output != 30 {
		t.Fatalf("fast tier not priced at 2x standard: %+v", rate)
	}
}

func TestPricingSettingsAndHistoricalRatePersistence(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	store, err := Open(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	settings := emptySettings()
	settings.Names[Fingerprint("client")] = "Ada"
	settings.Prices["custom"] = Rates{Input: 2, Output: 10, CacheRead: ptr(.2)}
	if errSave := store.UpdateSettings(settings); errSave != nil {
		t.Fatal(errSave)
	}
	pricing, errPricing := OpenPricing(dir)
	if errPricing != nil {
		t.Fatal(errPricing)
	}
	record := usage.Record{APIKey: "client", AuthID: "account", Model: "custom", Provider: "openai", RequestedAt: now, Detail: usage.Detail{InputTokens: 1_000_000, OutputTokens: 100_000}}
	rate := pricing.Resolve(record, store.Settings().Prices)
	if errRecord := store.RecordPriced(record, rate); errRecord != nil {
		t.Fatal(errRecord)
	}
	settings.Prices["custom"] = Rates{Input: 100, Output: 100}
	if errSave := store.UpdateSettings(settings); errSave != nil {
		t.Fatal(errSave)
	}
	if errClose := store.Close(); errClose != nil {
		t.Fatal(errClose)
	}
	reopened, errOpen := Open(dir, func() time.Time { return now })
	if errOpen != nil {
		t.Fatal(errOpen)
	}
	rows, _ := reopened.Snapshot("2026-06-10", "2026-06-10")
	if len(rows) != 1 || rows[0].CostUSD == nil || *rows[0].CostUSD != 3 || rows[0].Price.Input != 2 {
		t.Fatalf("historical rate changed: %+v", rows)
	}
	if reopened.Settings().Names[Fingerprint("client")] != "Ada" {
		t.Fatal("friendly name not persisted")
	}
	bad := emptySettings()
	bad.Prices["bad"] = Rates{Input: math.NaN(), Output: 2}
	if reopened.UpdateSettings(bad) == nil {
		t.Fatal("invalid price accepted")
	}
}
