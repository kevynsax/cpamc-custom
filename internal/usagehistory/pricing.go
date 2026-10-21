package usagehistory

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

//go:embed prices.json
var seedPrices []byte

// Rates are USD per million tokens. Missing cache rates are not assumed free.
type Rates struct {
	Input            float64  `json:"input"`
	Output           float64  `json:"output"`
	CacheRead        *float64 `json:"cache_read,omitempty"`
	CacheWrite       *float64 `json:"cache_write,omitempty"`
	Source           string   `json:"source,omitempty"`
	AsOf             string   `json:"as_of,omitempty"`
	ContextOver      int64    `json:"context_over,omitempty"`
	ContextInclusive bool     `json:"context_inclusive,omitempty"`
	LongContext      *Rates   `json:"long_context,omitempty"`
}

// UnmarshalJSON distinguishes an explicit free rate from a missing price.
func (r *Rates) UnmarshalJSON(data []byte) error {
	var required struct {
		Input  *float64 `json:"input"`
		Output *float64 `json:"output"`
	}
	if err := json.Unmarshal(data, &required); err != nil {
		return err
	}
	if required.Input == nil || required.Output == nil {
		return fmt.Errorf("input and output prices are required")
	}
	type plain Rates
	return json.Unmarshal(data, (*plain)(r))
}

type PriceCatalog struct {
	AsOf   string           `json:"as_of"`
	Models map[string]Rates `json:"models"`
}

type Pricing struct {
	mu      sync.RWMutex
	catalog PriceCatalog
	path    string
}

func OpenPricing(dir string) (*Pricing, error) {
	p := &Pricing{path: filepath.Join(dir, "prices.json")}
	data, err := os.ReadFile(p.path)
	if os.IsNotExist(err) {
		data = seedPrices
	} else if err != nil {
		return nil, fmt.Errorf("read price catalog: %w", err)
	}
	if errDecode := json.Unmarshal(data, &p.catalog); errDecode != nil {
		return nil, fmt.Errorf("decode price catalog: %w", errDecode)
	}
	if len(p.catalog.Models) == 0 {
		return nil, fmt.Errorf("empty price catalog")
	}
	for key, rate := range p.catalog.Models {
		if strings.HasPrefix(key, "zai/") && rate.CacheWrite != nil && *rate.CacheWrite == 0 {
			rate.CacheWrite = nil
			p.catalog.Models[key] = rate
		}
	}
	for _, rate := range p.catalog.Models {
		if !validRates(rate) || rate.LongContext != nil && !validRates(*rate.LongContext) {
			return nil, fmt.Errorf("invalid price catalog rates")
		}
	}
	return p, nil
}

func (p *Pricing) AsOf() string { p.mu.RLock(); defer p.mu.RUnlock(); return p.catalog.AsOf }

// Run refreshes public prices daily without adding upstream inference deadlines.
func (p *Pricing) Run(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.Refresh(ctx); err != nil {
				log.WithError(err).Warn("Refresh public API price catalog")
			}
		}
	}
}

func (p *Pricing) Refresh(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://models.dev/api.json", nil)
	if err != nil {
		return err
	}
	response, errDo := http.DefaultClient.Do(request)
	if errDo != nil {
		return fmt.Errorf("fetch public price catalog: %w", errDo)
	}
	defer func() {
		if errClose := response.Body.Close(); errClose != nil {
			log.WithError(errClose).Debug("Close public price catalog")
		}
	}()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("public price catalog HTTP %d", response.StatusCode)
	}
	data, errRead := io.ReadAll(io.LimitReader(response.Body, 16*1024*1024+1))
	if errRead != nil {
		return fmt.Errorf("read public price catalog: %w", errRead)
	}
	if len(data) > 16*1024*1024 {
		return fmt.Errorf("public price catalog exceeds size limit")
	}
	catalog, errParse := ParsePublicPrices(data, time.Now().UTC().Format(DateLayout))
	if errParse != nil {
		return errParse
	}
	encoded, errEncode := json.Marshal(catalog)
	if errEncode != nil {
		return errEncode
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if errWrite := atomicJSON(p.path, encoded); errWrite != nil {
		return errWrite
	}
	p.catalog = catalog
	return nil
}

// ParsePublicPrices keeps native vendor rates, avoiding reseller-specific discounts.
func ParsePublicPrices(data []byte, asOf string) (PriceCatalog, error) {
	var providers map[string]struct {
		Models map[string]struct {
			Cost *struct {
				Input      *float64 `json:"input"`
				Output     *float64 `json:"output"`
				CacheRead  *float64 `json:"cache_read"`
				CacheWrite *float64 `json:"cache_write"`
				Tiers      []struct {
					Input      *float64 `json:"input"`
					Output     *float64 `json:"output"`
					CacheRead  *float64 `json:"cache_read"`
					CacheWrite *float64 `json:"cache_write"`
					Tier       struct {
						Type string `json:"type"`
						Size int64  `json:"size"`
					} `json:"tier"`
				} `json:"tiers"`
			} `json:"cost"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &providers); err != nil {
		return PriceCatalog{}, fmt.Errorf("parse public price catalog: %w", err)
	}
	out := PriceCatalog{AsOf: asOf, Models: map[string]Rates{}}
	for _, vendor := range nativePriceVendors {
		for model, entry := range providers[vendor].Models {
			cost := entry.Cost
			if cost == nil || cost.Input == nil || cost.Output == nil {
				continue
			}
			rate := Rates{Input: *cost.Input, Output: *cost.Output, CacheRead: cost.CacheRead, CacheWrite: cost.CacheWrite, Source: "models.dev/" + vendor, AsOf: asOf}
			// Z.ai's free cache storage is not a published per-token cache-write tariff.
			if vendor == "zai" && rate.CacheWrite != nil && *rate.CacheWrite == 0 {
				rate.CacheWrite = nil
			}
			if !validRates(rate) {
				continue
			}
			for _, tier := range cost.Tiers {
				if tier.Tier.Type == "context" && tier.Tier.Size > 0 && (rate.ContextOver == 0 || tier.Tier.Size < rate.ContextOver) {
					rate.ContextOver = tier.Tier.Size
					rate.ContextInclusive = vendor == "xai"
					if tier.Input == nil || tier.Output == nil {
						continue
					}
					extra := Rates{Input: *tier.Input, Output: *tier.Output, CacheRead: tier.CacheRead, CacheWrite: tier.CacheWrite, Source: rate.Source, AsOf: asOf}
					if validRates(extra) {
						rate.LongContext = &extra
					}
				}
			}
			out.Models[vendor+"/"+model] = rate
		}
	}
	if len(out.Models) == 0 {
		return PriceCatalog{}, fmt.Errorf("public price catalog contains no usable rates")
	}
	return out, nil
}

var nativePriceVendors = []string{"anthropic", "openai", "google", "xai", "moonshotai", "minimax", "zai", "deepseek", "mistral", "cohere", "perplexity", "alibaba", "qwen", "nvidia", "meta", "amazon-bedrock"}

// Resolve returns the effective rate for the record's service mode. OpenAI fast and
// ultrafast are scaled from the standard tariff; other non-standard tiers stay unpriced.
func (p *Pricing) Resolve(record usage.Record, overrides map[string]Rates) *Rates {
	if !usage.GenerateEnabled(record.Generate) {
		return nil
	}
	multiplier, known := modeMultiplier(record)
	if override := overrideRate(record, overrides); override != nil {
		if !known {
			return override
		}
		return scaleRates(override, multiplier)
	}
	if !known {
		return nil
	}
	return scaleRates(p.resolveStandard(record), multiplier)
}

func overrideRate(record usage.Record, overrides map[string]Rates) *Rates {
	model := strings.TrimSpace(record.ResponseModel)
	if model == "" {
		model = strings.TrimSpace(record.Model)
	}
	providerName := strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(record.Provider), "openai-compatible-"), "openai-compatibility-")
	for _, candidate := range []string{strings.TrimSpace(record.Alias), model, strings.TrimSpace(record.Model)} {
		if candidate == "" {
			continue
		}
		for _, key := range []string{record.Provider + "/" + candidate, providerName + "/" + candidate, candidate} {
			if rate, ok := overrides[key]; ok {
				return effectiveRate(rate, record)
			}
		}
	}
	return nil
}

func (p *Pricing) resolveStandard(record usage.Record) *Rates {
	model := strings.TrimSpace(record.ResponseModel)
	if model == "" {
		model = strings.TrimSpace(record.Model)
	}
	for _, candidate := range []string{record.Alias, model} {
		if info, found := referenceModels()[candidate]; found && (info.Status == "self_hosted" || info.Status == "subscription_only" || info.Status == "unpublished") {
			return nil
		}
	}
	for _, unsupported := range []string{"audio", "realtime", "image", "video", "tts", "transcrib"} {
		if strings.Contains(strings.ToLower(model), unsupported) {
			return nil
		}
	}
	// CUSTOM (cpamc-custom): exact in-house models use the sourced OpenRouter
	// counterfactual, not a native-vendor tariff or a guessed neighboring model.
	for _, candidate := range []string{record.Alias, model, record.Model, inHouseReferenceModel(record.Provider, model)} {
		if info, ok := referenceModels()[candidate]; ok && info.Status == "openrouter" {
			if rate := researchedTokenRate(candidate); rate != nil {
				if inHouseProvider(record.Provider) != "" {
					rate = inHouseCacheRate(*rate)
				}
				return effectiveRate(*rate, record)
			}
		}
	}
	if fallback := researchedTokenRate(model); fallback != nil {
		if p == nil || p.AsOf() <= fallback.AsOf {
			return effectiveRate(*fallback, record)
		}
	}
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	vendor := ""
	provider := strings.ToLower(record.Provider + " " + record.ExecutorType)
	switch {
	case strings.Contains(provider, "claude"):
		vendor = "anthropic"
	case strings.Contains(provider, "codex"):
		vendor = "openai"
	case strings.Contains(provider, "gemini"):
		vendor = "google"
	case strings.Contains(provider, "xai"):
		vendor = "xai"
	}
	if vendor != "" {
		if rate, ok := p.catalog.Models[vendor+"/"+model]; ok {
			return effectiveRate(rate, record)
		}
	}
	if rate, ok := p.catalog.Models[model]; ok {
		return effectiveRate(rate, record)
	}
	// Canonical model names may be routed through compatible/custom providers.
	var candidates []string
	for key := range p.catalog.Models {
		if strings.HasSuffix(key, "/"+model) {
			candidates = append(candidates, key)
		}
	}
	sort.Strings(candidates)
	if len(candidates) == 1 {
		return effectiveRate(p.catalog.Models[candidates[0]], record)
	}
	if fallback := researchedTokenRate(model); fallback != nil {
		return effectiveRate(*fallback, record)
	}
	return nil
}

func effectiveRate(rate Rates, record usage.Record) *Rates {
	detail := usage.EnsureTokenBreakdownForProvider(record.Detail, record.Provider, record.ExecutorType)
	if rate.ContextOver > 0 && (detail.TokenBreakdown.Input.TotalTokens > rate.ContextOver || rate.ContextInclusive && detail.TokenBreakdown.Input.TotalTokens == rate.ContextOver) {
		if rate.LongContext == nil {
			return nil
		}
		rate = *rate.LongContext
	}
	rate.LongContext = nil
	rate.ContextOver = 0
	return &rate
}

func EstimateCost(detail usage.Detail, rate *Rates) *float64 {
	b := detail.TokenBreakdown
	if rate == nil || !validRates(*rate) || !b.Valid() || b.Quality != usage.TokenAccountingQualityComplete {
		return nil
	}
	if b.Input.CacheReadTokens > 0 && rate.CacheRead == nil || b.Input.CacheWriteTokens > 0 && rate.CacheWrite == nil {
		return nil
	}
	cost := float64(b.Input.UncachedTokens)*rate.Input + float64(b.Output.TotalTokens)*rate.Output
	if rate.CacheRead != nil {
		cost += float64(b.Input.CacheReadTokens) * *rate.CacheRead
	}
	if rate.CacheWrite != nil {
		cost += float64(b.Input.CacheWriteTokens) * *rate.CacheWrite
	}
	cost /= 1e6
	return &cost
}
