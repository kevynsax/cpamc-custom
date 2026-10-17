package usagehistory

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

type Settings struct {
	Names      map[string]string             `json:"names"`
	Plans      map[string]string             `json:"plans"`
	Prices     map[string]Rates              `json:"prices"`
	UnitPrices map[string]map[string]float64 `json:"unit_prices"`
}

func emptySettings() Settings {
	return Settings{Names: map[string]string{}, Plans: map[string]string{}, Prices: map[string]Rates{}, UnitPrices: map[string]map[string]float64{}}
}

func validRates(rate Rates) bool {
	valid := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1e6 }
	return valid(rate.Input) && valid(rate.Output) && (rate.CacheRead == nil || valid(*rate.CacheRead)) && (rate.CacheWrite == nil || valid(*rate.CacheWrite))
}

func validFingerprint(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (s *Store) PriceFor(pricing *Pricing, record usage.Record) *Rates {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range []string{record.Alias, record.ResponseModel, record.Model} {
		if _, explicit := s.settings.UnitPrices[key]; explicit {
			if _, tokenRate := s.settings.Prices[key]; !tokenRate {
				return nil
			}
			break
		}
	}
	return pricing.Resolve(record, s.settings.Prices)
}

func (s *Store) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, _ := json.Marshal(s.settings)
	result := emptySettings()
	_ = json.Unmarshal(data, &result)
	return result
}

func (s *Store) UpdateSettings(settings Settings) error {
	if len(settings.Names) > 10000 || len(settings.Plans) > 10000 || len(settings.Prices) > 10000 || len(settings.UnitPrices) > 10000 {
		return fmt.Errorf("too many usage settings")
	}
	for _, values := range []map[string]string{settings.Names, settings.Plans} {
		for key, value := range values {
			if !validFingerprint(key) || utf8.RuneCountInString(value) > 80 || strings.ContainsAny(value, "\r\n\x00") {
				return fmt.Errorf("invalid usage name or plan")
			}
		}
	}
	for model, rate := range settings.Prices {
		if strings.TrimSpace(model) == "" || len(model) > 200 || strings.ContainsAny(model, "\r\n\x00") || !validRates(rate) {
			return fmt.Errorf("invalid model price")
		}
		rate.Source = "manual"
		rate.AsOf = s.now().UTC().Format(DateLayout)
		if rate.ContextOver < 0 || rate.ContextOver > 100_000_000 || rate.LongContext != nil && (rate.ContextOver == 0 || !validRates(*rate.LongContext)) {
			return fmt.Errorf("invalid context model price")
		}
		if rate.LongContext != nil {
			long := *rate.LongContext
			long.Source = rate.Source
			long.AsOf = rate.AsOf
			long.LongContext = nil
			long.ContextOver = 0
			rate.LongContext = &long
		}
		settings.Prices[model] = rate
	}
	allowed := map[string]bool{}
	for _, key := range []string{"input", "output", "cache_read", "cache_write", "cache_write_1h", "long_input", "long_output", "long_cache_read", "long_cache_write", "image_input", "image_cache_read", "text_output", "per_image", "per_second", "per_minute", "per_million_characters"} {
		allowed[key] = true
	}
	for model, fields := range settings.UnitPrices {
		if strings.TrimSpace(model) == "" || len(model) > 200 || strings.ContainsAny(model, "\r\n\x00") || len(fields) > len(allowed) {
			return fmt.Errorf("invalid unit model price")
		}
		for key, value := range fields {
			if !allowed[key] || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1e6 {
				return fmt.Errorf("invalid unit model rate")
			}
		}
	}
	if settings.UnitPrices == nil {
		settings.UnitPrices = map[string]map[string]float64{}
	}
	if settings.Names == nil {
		settings.Names = map[string]string{}
	}
	if settings.Plans == nil {
		settings.Plans = map[string]string{}
	}
	if settings.Prices == nil {
		settings.Prices = map[string]Rates{}
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if errWrite := atomicJSON(filepath.Join(s.dir, "settings.json"), data); errWrite != nil {
		return errWrite
	}
	// Clone maps so callers cannot mutate active settings after this method returns.
	s.settings = emptySettings()
	return json.Unmarshal(data, &s.settings)
}

func atomicJSON(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".usage-*")
	if err != nil {
		return fmt.Errorf("create usage snapshot: %w", err)
	}
	name := file.Name()
	defer func() { _ = os.Remove(name) }()
	if _, errWrite := file.Write(data); errWrite != nil {
		_ = file.Close()
		return fmt.Errorf("write usage snapshot: %w", errWrite)
	}
	if errSync := file.Sync(); errSync != nil {
		_ = file.Close()
		return fmt.Errorf("sync usage snapshot: %w", errSync)
	}
	if errClose := file.Close(); errClose != nil {
		return fmt.Errorf("close usage snapshot: %w", errClose)
	}
	if errRename := os.Rename(name, path); errRename != nil {
		return fmt.Errorf("replace usage snapshot: %w", errRename)
	}
	return nil
}
