package usagehistory

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

// CUSTOM (cpamc-custom): OpenAI speed modes. OpenAI publishes Fast at 2x and
// Ultrafast at 6x the Standard tariff (cache and long-context rates included).
const (
	ModeFast      = "fast"
	ModeUltrafast = "ultrafast"
)

var modeMultipliers = map[string]float64{"": 1, ModeFast: 2, ModeUltrafast: 6}

// ServiceMode is the speed mode a record ran with; "" means standard.
// The requested tier wins over the response because OpenAI reports "default"
// even when ultrafast runs (openai/codex#54952).
func ServiceMode(record usage.Record) string {
	requested := normalizeMode(record.ServiceTier)
	if requested == "" {
		requested = normalizeMode(record.RequestServiceTier)
	}
	if requested != "" {
		return requested
	}
	return normalizeMode(record.ResponseServiceTier)
}

func normalizeMode(tier string) string {
	switch tier = strings.ToLower(strings.TrimSpace(tier)); tier {
	case "", "auto", "default", "standard":
		return ""
	case "priority", "fast":
		return ModeFast
	default:
		return tier
	}
}

// modeMultiplier reports the tariff multiplier; unknown modes are not priced.
func modeMultiplier(record usage.Record) (float64, bool) {
	mode := ServiceMode(record)
	if mode == "" {
		return 1, true
	}
	if !openAIProvider(record) {
		return 0, false
	}
	multiplier, known := modeMultipliers[mode]
	return multiplier, known
}

func openAIProvider(record usage.Record) bool {
	provider := strings.ToLower(record.Provider + " " + record.ExecutorType)
	return strings.Contains(provider, "codex") || strings.ToLower(record.Provider) == "openai"
}

func scaleRates(rate *Rates, multiplier float64) *Rates {
	if rate == nil || multiplier == 1 {
		return rate
	}
	scaled := *rate
	scaled.Input *= multiplier
	scaled.Output *= multiplier
	if rate.CacheRead != nil {
		value := *rate.CacheRead * multiplier
		scaled.CacheRead = &value
	}
	if rate.CacheWrite != nil {
		value := *rate.CacheWrite * multiplier
		scaled.CacheWrite = &value
	}
	scaled.LongContext = scaleRates(rate.LongContext, multiplier)
	return &scaled
}
