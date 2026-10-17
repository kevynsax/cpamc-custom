package usagehistory

import (
	"encoding/json"
	"fmt"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type Settings struct {
	Names  map[string]string `json:"names"`
	Plans  map[string]string `json:"plans"`
	Prices map[string]Rates  `json:"prices"`
}

func emptySettings() Settings {
	return Settings{Names: map[string]string{}, Plans: map[string]string{}, Prices: map[string]Rates{}}
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
	if len(settings.Names) > 10000 || len(settings.Plans) > 10000 || len(settings.Prices) > 10000 {
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
		rate.LongContext = nil
		rate.ContextOver = 0
		settings.Prices[model] = rate
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
