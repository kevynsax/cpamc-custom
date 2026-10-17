package usagehistory

import (
	"path"
	"regexp"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

// ReferenceRow adds a read-time comparison without changing persisted accounting.
type ReferenceRow struct {
	Row
	ReferenceCostUSD *float64 `json:"reference_cost_usd,omitempty"`
	ModelAlias       string   `json:"model_alias,omitempty"`
	ReferencePrice   *Rates   `json:"reference_price,omitempty"`
}

func inHouseProvider(provider string) string {
	name := strings.ToLower(strings.TrimSpace(provider))
	for _, prefix := range []string{"openai-compatible-", "openai-compatibility-"} {
		name = strings.TrimPrefix(name, prefix)
	}
	switch name {
	case "spark", "home-gpu", "macbook":
		return name
	}
	return ""
}

var inHouseQuantization = regexp.MustCompile(`(?i)-(?:UD-)?(?:Q[0-9][A-Z0-9_]*|IQ[0-9][A-Z0-9_]*|F16|BF16|NVFP4)$`)

// GGUF response filenames identify the verified serving models, not a neighboring size.
func inHouseReferenceModel(provider, model string) string {
	if inHouseProvider(provider) == "" {
		return ""
	}
	model = strings.TrimSpace(model)
	if info, ok := referenceModels()[model]; ok && info.Status == "openrouter" {
		return strings.TrimPrefix(model, "spark/")
	}
	if inHouseProvider(provider) != "spark" || !strings.HasSuffix(strings.ToLower(model), ".gguf") {
		return ""
	}
	base := path.Base(strings.ReplaceAll(model, "\\", "/"))
	base = inHouseQuantization.ReplaceAllString(base[:len(base)-5], "")
	return map[string]string{
		"qwen3-vl-30b-a3b-instruct":             "qwen-vl",
		"qwen3.8-27b":                           "qwen-coder",
		"gpt-oss-120b":                          "gpt-oss-120b",
		"qwen3.5-122b-a10b":                     "qwen3.5-122b-a10b",
		"gemma-4-31b-it-qat":                    "gemma-4-31b",
		"nemotron-3-super-120b":                 "nemotron-120b",
		"nvidia-nemotron-3.5-lightning-30b-a3b": "nemotron-3.5-lightning",
		"muse-glimmer-30b":                      "muse-glimmer-30b",
	}[strings.ToLower(base)]
}

// A local cache hit does not imply a hosted discount. Without a published read
// tariff, the approved OpenRouter comparison uses the regular input tariff.
func inHouseCacheRate(rate Rates) *Rates {
	if rate.CacheRead == nil {
		input := rate.Input
		rate.CacheRead = &input
	}
	return &rate
}

func WithInHouseReferenceEstimates(rows []Row) []ReferenceRow {
	out := make([]ReferenceRow, len(rows))
	for index, row := range rows {
		out[index].Row = row
		model := inHouseReferenceModel(row.Provider, row.Model)
		out[index].ModelAlias = model
		if model == "" || row.CostUSD != nil || row.UnpricedRequests <= 0 || row.Requests <= 0 {
			continue
		}
		var rate *Rates
		if row.Price != nil {
			if !strings.HasPrefix(row.Price.Source, "https://openrouter.ai/") {
				continue
			}
			rate = inHouseCacheRate(*row.Price)
		} else {
			if published := researchedTokenRate(model); published != nil {
				rate = inHouseCacheRate(*published)
			}
		}
		if rate == nil || rate.ContextOver > 0 || rate.LongContext != nil {
			continue
		}
		detail := usage.EnsureTokenBreakdownForProvider(usage.Detail{
			InputTokens: row.InputTokens, OutputTokens: row.OutputTokens,
			CacheReadTokens: row.CachedTokens, TotalTokens: row.TotalTokens,
		}, row.Provider, "OpenAICompatExecutor")
		if cost := EstimateCost(detail, rate); cost != nil {
			out[index].ReferenceCostUSD = cost
			out[index].ReferencePrice = rate
		}
	}
	return out
}
