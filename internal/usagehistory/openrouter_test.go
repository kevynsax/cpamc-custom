package usagehistory

import (
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

func TestInHouseOpenRouterReferencesUseExactServingModels(t *testing.T) {
	var pricing *Pricing
	for _, tc := range []struct {
		model         string
		input, output float64
	}{
		{"gemma-4-31b", .2, .4}, {"gpt-oss-120b", .15, .6},
		{"muse-glimmer-30b", .3, 1.2}, {"nemotron-120b", .08, .45},
		{"nemotron-3.5-lightning", .07, .2}, {"qwen-coder", .24, 2.2},
		{"qwen-vl", .15, .6}, {"qwen3.5-122b-a10b", .26, 2.08},
	} {
		for _, id := range []string{tc.model, "spark/" + tc.model} {
			rate := pricing.Resolve(usage.Record{Model: id, Provider: "openai-compatibility"}, nil)
			if rate == nil || rate.Input != tc.input || rate.Output != tc.output || !strings.HasPrefix(rate.Source, "https://openrouter.ai/") {
				t.Fatalf("%s: wrong OpenRouter reference: %+v", id, rate)
			}
			ref := pricing.ModelReference(id, "spark", "", emptySettings())
			if ref.Status != "openrouter" || ref.Prices["input"] != tc.input {
				t.Fatalf("missing card reference for %s", id)
			}
		}
	}
	if ref := pricing.ModelReference("qwen-coder", "spark", "", emptySettings()); ref.Sources[0] != "https://openrouter.ai/qwen/qwen3.8-27b" {
		t.Fatal("coder alias incorrectly priced as Coder Next")
	}
	for _, model := range []string{"qwen2.5:3b", "qwen3-8b", "qwen2.5-vl-7b", "sweep-next-edit-v2-7b", "qwen2.5-coder-7b-base"} {
		if pricing.Resolve(usage.Record{Model: model, Provider: "openai-compatibility"}, nil) != nil {
			t.Fatalf("unlisted OpenRouter model guessed: %s", model)
		}
	}
}

func TestOpenRouterAliasPrecedenceAndUnknownCacheRates(t *testing.T) {
	pricing := &Pricing{catalog: PriceCatalog{AsOf: "2099-01-01", Models: map[string]Rates{"qwen-coder": {Input: 999, Output: 999}}}}
	record := usage.Record{Alias: "spark/qwen-coder", Model: "Qwen3.8-27B-quantized", Provider: "openai-compatibility"}
	rate := pricing.Resolve(record, nil)
	if rate == nil || rate.Input != .24 {
		t.Fatalf("exact OpenRouter alias lost: %+v", rate)
	}
	if manual := pricing.Resolve(record, map[string]Rates{"spark/qwen-coder": {Input: 7, Output: 9}}); manual == nil || manual.Input != 7 {
		t.Fatal("manual override lost")
	}
	missingCache := pricing.Resolve(usage.Record{Model: "gemma-4-31b"}, nil)
	detail := usage.EnsureTokenBreakdownForProvider(usage.Detail{InputTokens: 100, OutputTokens: 20, CacheReadTokens: 50, CachedTokens: 50}, "openai", "openai")
	if EstimateCost(detail, missingCache) != nil {
		t.Fatal("unpublished cache discount invented")
	}
	record.ServiceTier = "priority"
	if pricing.Resolve(record, nil) != nil {
		t.Fatal("nonstandard service tier priced as default")
	}
}
