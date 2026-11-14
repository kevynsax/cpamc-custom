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
	DisplayName      string             `json:"display_name,omitempty"`
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

type referenceFile struct {
	Models []ModelReference `json:"models"`
}

var referencesOnce sync.Once
var references map[string]ModelReference

func referenceModels() map[string]ModelReference {
	referencesOnce.Do(func() {
		var data referenceFile
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

// ModelReference is the price card of a registry model. A token model shows the same
// standard tariff usage is priced with, so a model missing from the researched
// snapshot still gets its public catalog price. Manual settings win.
func (p *Pricing) ModelReference(id, provider, canonical string, settings Settings) ModelReference {
	// An alias without its own entry is priced as the model it routes to.
	name := id
	model, ok := referenceModels()[name]
	if !ok && canonical != "" {
		name = canonical
		model, ok = referenceModels()[name]
	}
	if !ok {
		model = ModelReference{Kind: "tokens", Status: "unpublished", Prices: map[string]float64{}, Sources: []string{}}
	}
	model = cloneReference(model)
	model.ID = id
	model.Provider = provider
	if model.Kind == "tokens" {
		if rate := p.standardRate(provider, name); rate != nil {
			model.Status = "published"
			model.Prices = rateFields(*rate)
			if priceVendor(provider) == "anthropic" {
				model.Prices["cache_write_1h"] = 2 * rate.Input
			}
			model.ContextThreshold = rate.ContextOver
			model.ContextInclusive = rate.ContextInclusive
			model.CheckedAt = rate.AsOf
			if strings.HasPrefix(rate.Source, "models.dev/") {
				model.Sources = []string{"https://models.dev/api.json"}
			}
		}
	}
	if rates, found := settings.Prices[id]; found && model.Kind == "tokens" {
		model.Prices = rateFields(rates)
		model.ContextThreshold = rates.ContextOver
		model.ContextInclusive = rates.ContextInclusive
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

// rateFields flattens a token tariff into price-card fields.
func rateFields(rate Rates) map[string]float64 {
	fields := map[string]float64{"input": rate.Input, "output": rate.Output}
	if rate.CacheRead != nil {
		fields["cache_read"] = *rate.CacheRead
	}
	if rate.CacheWrite != nil {
		fields["cache_write"] = *rate.CacheWrite
	}
	long := rate.LongContext
	if long == nil {
		return fields
	}
	fields["long_input"] = long.Input
	fields["long_output"] = long.Output
	if long.CacheRead != nil {
		fields["long_cache_read"] = *long.CacheRead
	}
	if long.CacheWrite != nil {
		fields["long_cache_write"] = *long.CacheWrite
	}
	return fields
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
