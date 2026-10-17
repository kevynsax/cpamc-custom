package usagehistory

import (
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	"testing"
	"time"
)

func TestQuotaWindowsPreserveIndependentDurationsAndLegacyWeekIDs(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	body := []byte(`{"five_hour":{"utilization":20,"resets_at":"2026-10-17T14:00:00Z"},"seven_day":{"utilization":40,"resets_at":"2026-10-20T12:00:00Z"},"seven_day_opus":{"utilization":50,"resets_at":"2026-10-20T12:00:00Z"}}`)
	windows, _ := ParseQuotaWindows("claude", body)
	if len(windows) != 3 {
		t.Fatalf("windows=%+v", windows)
	}
	dir := t.TempDir()
	store, err := Open(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	for _, window := range windows {
		if errObserve := store.ObserveQuotaWindow("account", "claude", window.Scope, "Max 20x", window.Reset, now, window.Used, window.Period); errObserve != nil {
			t.Fatal(errObserve)
		}
	}
	reopened, errOpen := Open(dir, func() time.Time { return now })
	if errOpen != nil {
		t.Fatal(errOpen)
	}
	reports, errReports := reopened.Reports("2026-10-01", "2026-10-31")
	if errReports != nil || len(reports) != 3 {
		t.Fatalf("reports=%+v err=%v", reports, errReports)
	}
	foundSession := false
	for _, report := range reports {
		if report.PeriodSeconds == 18000 {
			foundSession = true
		}
		if report.Scope == "account" && report.PeriodSeconds == 604800 && report.ID != Fingerprint("account")+"|account|2026-10-20T12:00:00Z" {
			t.Fatal("legacy weekly ID changed")
		}
	}
	if !foundSession {
		t.Fatal("session history lost")
	}
	if !quotaScopeMatches("family:opus", "claude-opus-5-5") || quotaScopeMatches("family:opus", "claude-sonnet-5") {
		t.Fatal("model-scoped quotas mixed")
	}
}

func TestResearchedInventoryAndNonTokenPrices(t *testing.T) {
	models := ReferenceModels()
	if len(models) != 68 {
		t.Fatalf("models=%d", len(models))
	}
	var pricing *Pricing
	ref := pricing.ModelReference("grok-imagine-video-1.5", "xai", "", emptySettings())
	if ref.Kind != "second" || ref.Prices["per_second"] != .08 {
		t.Fatalf("wrong media unit: %+v", ref)
	}
	ref.Prices["per_second"] = 123
	if pricing.ModelReference(ref.ID, "xai", "", emptySettings()).Prices["per_second"] != .08 {
		t.Fatal("reference mutated through response")
	}
	for _, id := range []string{"MiniMax-M3.1-Flash-Preview", "muse-wonder-full-1.0", "qwen3-8b", "codex-auto-review"} {
		info := pricing.ModelReference(id, "", "", emptySettings())
		if len(info.Prices) != 0 {
			t.Fatalf("unpublished price invented for %s", id)
		}
	}
	rate := pricing.Resolve(usage.Record{Provider: "claude", Model: "claude-mythos-5-1"}, nil)
	if rate == nil || rate.Input != 10 || rate.Output != 50 || rate.CacheRead == nil || *rate.CacheRead != .25 {
		t.Fatalf("missing official Mythos rate: %+v", rate)
	}
}

func TestPriceEditsPreserveContextRatesAndAliasOverrides(t *testing.T) {
	store, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	settings := emptySettings()
	settings.Prices["route-alias"] = Rates{Input: 2, Output: 10, ContextOver: 100, ContextInclusive: true, LongContext: &Rates{Input: 4, Output: 15}}
	settings.UnitPrices["route-alias"] = map[string]float64{"input": 2, "output": 10, "long_input": 4, "long_output": 15}
	if errSave := store.UpdateSettings(settings); errSave != nil {
		t.Fatal(errSave)
	}
	var pricing *Pricing
	record := usage.Record{Alias: "route-alias", Model: "upstream-name", Provider: "openai", Detail: usage.Detail{InputTokens: 100}}
	rate := store.PriceFor(pricing, record)
	if rate == nil || rate.Input != 4 || rate.Output != 15 {
		t.Fatalf("alias/context edit lost: %+v", rate)
	}
	settings.UnitPrices["grok-imagine-image"] = map[string]float64{"per_image": .02}
	if errSave := store.UpdateSettings(settings); errSave != nil {
		t.Fatal(errSave)
	}
	if store.PriceFor(pricing, usage.Record{Model: "grok-imagine-image"}) != nil {
		t.Fatal("image count rate incorrectly used as token rate")
	}
	settings.UnitPrices["bad"] = map[string]float64{"invented_unit": 1}
	if store.UpdateSettings(settings) == nil {
		t.Fatal("invalid price dimension accepted")
	}
}

func TestIncompleteContextTierNeverBecomesFree(t *testing.T) {
	catalog, err := ParsePublicPrices([]byte(`{"openai":{"models":{"example":{"cost":{"input":2,"output":10,"tiers":[{"input":4,"tier":{"type":"context","size":100}}]}}}}}`), "2026-10-17")
	if err != nil {
		t.Fatal(err)
	}
	pricing := &Pricing{catalog: catalog}
	if pricing.Resolve(usage.Record{Model: "example", Provider: "openai", Detail: usage.Detail{InputTokens: 101}}, nil) != nil {
		t.Fatal("missing long-context output silently guessed")
	}
}
