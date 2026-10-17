package usagehistory

// CUSTOM (cpamc-custom): key names and Person/System distinctions are durable metadata.
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
func (s *Store) loadKeyNames() error {
	s.keyNames, s.keyKinds = map[string]string{}, map[string]string{}
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
		s.keyNames, s.keyKinds = metadata.Names, metadata.Kinds
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
	return KeyMetadata{Version: 2, Names: maps.Clone(s.keyNames), Kinds: maps.Clone(s.keyKinds)}
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

// Name/type edits commit together. Imports never overwrite existing names or types.
// Empty names remain as tombstones so old browsers cannot resurrect them.
func (s *Store) PatchKeyMetadata(names, kinds map[string]string, importOnly bool) error {
	if names == nil && kinds == nil || names != nil && !validKeyNames(names) || kinds != nil && !validKeyKinds(kinds) {
		return fmt.Errorf("invalid key metadata")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	nextNames, nextKinds := maps.Clone(s.keyNames), maps.Clone(s.keyKinds)
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
	if !validKeyNames(nextNames) || !validKeyKinds(nextKinds) {
		return fmt.Errorf("too many key metadata entries")
	}
	data, err := json.Marshal(KeyMetadata{Version: 2, Names: nextNames, Kinds: nextKinds})
	if err != nil {
		return err
	}
	if err := atomicJSON(filepath.Join(s.dir, "key-names.json"), data); err != nil {
		return err
	}
	s.keyNames, s.keyKinds = nextNames, nextKinds
	return nil
}
