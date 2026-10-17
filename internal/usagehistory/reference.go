package usagehistory

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)

//go:embed model_reference.json
var referenceData []byte

type ModelReference struct {
	ID               string             `json:"id"`
	Provider         string             `json:"provider"`
	Kind             string             `json:"kind"`
	Status           string             `json:"status"`
	Prices           map[string]float64 `json:"prices"`
	Sources          []string           `json:"sources"`
	CheckedAt        string             `json:"checked_at"`
	ContextThreshold int64              `json:"context_threshold,omitempty"`
	ContextInclusive bool               `json:"context_inclusive,omitempty"`
	Overridden       bool               `json:"overridden"`
}

var referencesOnce sync.Once
var references map[string]ModelReference

func referenceModels() map[string]ModelReference {
	referencesOnce.Do(func() {
		var data struct {
			Models []ModelReference `json:"models"`
		}
		references = map[string]ModelReference{}
		if json.Unmarshal(referenceData, &data) != nil {
			return
		}
		for _, model := range data.Models {
			references[model.ID] = model
		}
	})
	return references
}

func ReferenceModels() []ModelReference {
	out := make([]ModelReference, 0, len(referenceModels()))
	for _, model := range referenceModels() {
		out = append(out, cloneReference(model))
	}
	return out
}
func cloneReference(model ModelReference) ModelReference {
	prices := map[string]float64{}
	for key, value := range model.Prices {
		prices[key] = value
	}
	model.Prices = prices
	model.Sources = append([]string{}, model.Sources...)
	return model
}

func (p *Pricing) ModelReference(id, provider, canonical string, settings Settings) ModelReference {
	model, ok := referenceModels()[id]
	if !ok && canonical != "" {
		model, ok = referenceModels()[canonical]
	}
	if !ok {
		model = ModelReference{ID: id, Provider: provider, Kind: "tokens", Status: "unpublished", Prices: map[string]float64{}, Sources: []string{}}
	}
	model = cloneReference(model)
	if p != nil && model.Kind == "tokens" && model.Status == "published" {
		p.mu.RLock()
		rate, found := p.catalog.Models[model.Provider+"/"+model.ID]
		if found && p.catalog.AsOf > model.CheckedAt {
			fields := map[string]float64{"input": rate.Input, "output": rate.Output}
			if rate.CacheRead != nil {
				fields["cache_read"] = *rate.CacheRead
			}
			if rate.CacheWrite != nil {
				fields["cache_write"] = *rate.CacheWrite
			}
			if model.Provider == "anthropic" {
				fields["cache_write_1h"] = 2 * rate.Input
			}
			if long := rate.LongContext; long != nil {
				fields["long_input"] = long.Input
				fields["long_output"] = long.Output
				if long.CacheRead != nil {
					fields["long_cache_read"] = *long.CacheRead
				}
				if long.CacheWrite != nil {
					fields["long_cache_write"] = *long.CacheWrite
				}
			}
			model.Prices = fields
			model.ContextThreshold = rate.ContextOver
			model.ContextInclusive = rate.ContextInclusive
			model.CheckedAt = p.catalog.AsOf
			model.Sources = []string{"https://models.dev/api.json"}
		}
		p.mu.RUnlock()
	}
	model.ID = id
	model.Provider = provider
	if rates, found := settings.Prices[id]; found && model.Kind == "tokens" {
		model.Prices = map[string]float64{"input": rates.Input, "output": rates.Output}
		if rates.CacheRead != nil {
			model.Prices["cache_read"] = *rates.CacheRead
		}
		if rates.CacheWrite != nil {
			model.Prices["cache_write"] = *rates.CacheWrite
		}
		model.ContextThreshold = rates.ContextOver
		model.ContextInclusive = rates.ContextInclusive
		if rates.LongContext != nil {
			model.Prices["long_input"] = rates.LongContext.Input
			model.Prices["long_output"] = rates.LongContext.Output
			if rates.LongContext.CacheRead != nil {
				model.Prices["long_cache_read"] = *rates.LongContext.CacheRead
			}
			if rates.LongContext.CacheWrite != nil {
				model.Prices["long_cache_write"] = *rates.LongContext.CacheWrite
			}
		}
		model.Overridden = true
	}
	if fields, found := settings.UnitPrices[id]; found {
		model.Prices = map[string]float64{}
		for key, value := range fields {
			model.Prices[key] = value
		}
		model.Overridden = true
	}
	return model
}

func researchedTokenRate(model string) *Rates {
	info, ok := referenceModels()[model]
	if !ok || info.Kind != "tokens" || info.Status != "published" {
		return nil
	}
	input, okInput := info.Prices["input"]
	output, okOutput := info.Prices["output"]
	if !okInput || !okOutput {
		return nil
	}
	rate := &Rates{Input: input, Output: output, Source: strings.Join(info.Sources, ","), AsOf: info.CheckedAt, ContextOver: info.ContextThreshold, ContextInclusive: info.ContextInclusive}
	if value, ok := info.Prices["cache_read"]; ok {
		rate.CacheRead = &value
	}
	if value, ok := info.Prices["cache_write"]; ok {
		rate.CacheWrite = &value
	}
	if longInput, ok := info.Prices["long_input"]; ok {
		if longOutput, found := info.Prices["long_output"]; found {
			long := &Rates{Input: longInput, Output: longOutput, Source: rate.Source, AsOf: rate.AsOf}
			if v, f := info.Prices["long_cache_read"]; f {
				long.CacheRead = &v
			}
			if v, f := info.Prices["long_cache_write"]; f {
				long.CacheWrite = &v
			}
			rate.LongContext = long
		}
	}
	return rate
}
