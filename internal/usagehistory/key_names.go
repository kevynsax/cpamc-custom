package usagehistory

// CUSTOM (cpamc-custom): key names, Person/System distinctions and OpenAI speed
// permissions are durable metadata.
import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type KeyMetadata struct {
	Version int               `json:"version"`
	Names   map[string]string `json:"names"`
	Kinds   map[string]string `json:"kinds"`
	// Speeds is the fastest OpenAI service tier a key may request; absent means standard.
	Speeds map[string]string `json:"speeds,omitempty"`
}

// Key speed permissions, slowest first. A key may use its level and every slower one.
const (
	KeySpeedStandard  = "standard"
	KeySpeedFast      = "fast"
	KeySpeedUltrafast = "ultrafast"
)

var keySpeedRanks = map[string]int{KeySpeedStandard: 0, KeySpeedFast: 1, KeySpeedUltrafast: 2}

// KeySpeedAllows reports whether a key holding the allowed level may request mode
// ("" or standard, fast, ultrafast). Unknown modes are only allowed at ultrafast.
func KeySpeedAllows(allowed, mode string) bool {
	if mode == "" || mode == KeySpeedStandard {
		return true
	}
	requested, known := keySpeedRanks[mode]
	if !known {
		requested = keySpeedRanks[KeySpeedUltrafast]
	}
	return keySpeedRanks[allowed] >= requested
}

func validKeyNames(names map[string]string) bool {
	if names == nil || len(names) > 10000 {
		return false
	}
	for key, name := range names {
		if !validFingerprint(key) || utf8.RuneCountInString(name) > 80 || strings.ContainsAny(name, "\r\n\x00") {
			return false
		}
	}
	return true
}
func validKeyKinds(kinds map[string]string) bool {
	if kinds == nil || len(kinds) > 10000 {
		return false
	}
	for key, kind := range kinds {
		if !validFingerprint(key) || (kind != "person" && kind != "system") {
			return false
		}
	}
	return true
}
func validKeySpeeds(speeds map[string]string) bool {
	if speeds == nil || len(speeds) > 10000 {
		return false
	}
	for key, speed := range speeds {
		if _, known := keySpeedRanks[speed]; !validFingerprint(key) || !known {
			return false
		}
	}
	return true
}
func (s *Store) loadKeyNames() error {
	s.keyNames, s.keyKinds, s.keySpeeds = map[string]string{}, map[string]string{}, map[string]string{}
	data, err := os.ReadFile(filepath.Join(s.dir, "key-names.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read key metadata: %w", err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil || envelope == nil {
		return fmt.Errorf("invalid persisted key metadata")
	}
	if _, modern := envelope["version"]; modern {
		var metadata KeyMetadata
		if err := json.Unmarshal(data, &metadata); err != nil || metadata.Version != 2 || !validKeyNames(metadata.Names) || !validKeyKinds(metadata.Kinds) {
			return fmt.Errorf("invalid persisted key metadata")
		}
		if metadata.Speeds == nil {
			metadata.Speeds = map[string]string{}
		}
		if !validKeySpeeds(metadata.Speeds) {
			return fmt.Errorf("invalid persisted key metadata")
		}
		s.keyNames, s.keyKinds, s.keySpeeds = metadata.Names, metadata.Kinds, metadata.Speeds
	} else if err := json.Unmarshal(data, &s.keyNames); err != nil || !validKeyNames(s.keyNames) {
		return fmt.Errorf("invalid persisted key names")
	}
	return nil
}
func (s *Store) KeyNames() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.keyNames)
}
func (s *Store) KeyMetadata() KeyMetadata {
	s.mu.Lock()
	defer s.mu.Unlock()
	return KeyMetadata{Version: 2, Names: maps.Clone(s.keyNames), Kinds: maps.Clone(s.keyKinds), Speeds: maps.Clone(s.keySpeeds)}
}

// KeySpeed returns the fastest OpenAI service tier the key fingerprint may use.
func (s *Store) KeySpeed(fingerprint string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	speed, ok := s.keySpeeds[fingerprint]
	if !ok {
		return KeySpeedStandard
	}
	return speed
}
func (s *Store) KeyKinds() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.keyKinds)
}
func (s *Store) PatchKeyNames(names map[string]string, importOnly bool) error {
	if names == nil {
		return fmt.Errorf("invalid key names")
	}
	return s.PatchKeyMetadata(names, nil, importOnly)
}
func (s *Store) PatchKeyMetadata(names, kinds map[string]string, importOnly bool) error {
	return s.PatchKeyMetadataWithSpeeds(names, kinds, nil, importOnly)
}

// Name/type/speed edits commit together. Imports never overwrite existing values.
// Empty names remain as tombstones so old browsers cannot resurrect them.
func (s *Store) PatchKeyMetadataWithSpeeds(names, kinds, speeds map[string]string, importOnly bool) error {
	if names == nil && kinds == nil && speeds == nil || names != nil && !validKeyNames(names) || kinds != nil && !validKeyKinds(kinds) || speeds != nil && !validKeySpeeds(speeds) {
		return fmt.Errorf("invalid key metadata")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	nextNames, nextKinds, nextSpeeds := maps.Clone(s.keyNames), maps.Clone(s.keyKinds), maps.Clone(s.keySpeeds)
	if nextSpeeds == nil {
		nextSpeeds = map[string]string{}
	}
	if nextNames == nil {
		nextNames = map[string]string{}
	}
	if nextKinds == nil {
		nextKinds = map[string]string{}
	}
	for key, name := range names {
		if _, exists := nextNames[key]; !importOnly || !exists {
			nextNames[key] = strings.TrimSpace(name)
		}
	}
	for key, kind := range kinds {
		if _, exists := nextKinds[key]; !importOnly || !exists {
			nextKinds[key] = kind
		}
	}
	for key, speed := range speeds {
		if _, exists := nextSpeeds[key]; !importOnly || !exists {
			nextSpeeds[key] = speed
		}
	}
	if !validKeyNames(nextNames) || !validKeyKinds(nextKinds) || !validKeySpeeds(nextSpeeds) {
		return fmt.Errorf("too many key metadata entries")
	}
	data, err := json.Marshal(KeyMetadata{Version: 2, Names: nextNames, Kinds: nextKinds, Speeds: nextSpeeds})
	if err != nil {
		return err
	}
	if err := atomicJSON(filepath.Join(s.dir, "key-names.json"), data); err != nil {
		return err
	}
	s.keyNames, s.keyKinds, s.keySpeeds = nextNames, nextKinds, nextSpeeds
	return nil
}
