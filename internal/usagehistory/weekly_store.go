package usagehistory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (s *Store) cyclePath(id string) string {
	return filepath.Join(s.dir, "cycles", Fingerprint(id)+".json")
}

// Each week has its own atomic snapshot: a new observation must not rewrite a year
// of unrelated accounts' history on the usage dispatcher's persistence mutex.
func (s *Store) persistCycle(cycle Cycle) error {
	if err := os.MkdirAll(filepath.Join(s.dir, "cycles"), 0700); err != nil {
		return fmt.Errorf("create weekly history: %w", err)
	}
	data, err := json.Marshal(cycle)
	if err != nil {
		return err
	}
	return atomicJSON(s.cyclePath(cycle.ID), data)
}

func (s *Store) loadCycles() error {
	files, err := os.ReadDir(filepath.Join(s.dir, "cycles"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list weekly history: %w", err)
	}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.dir, "cycles", file.Name())
		data, errRead := os.ReadFile(path)
		if errRead != nil {
			return fmt.Errorf("read weekly history: %w", errRead)
		}
		var cycle Cycle
		if errDecode := json.Unmarshal(data, &cycle); errDecode != nil {
			return fmt.Errorf("decode weekly history: %w", errDecode)
		}
		if !validCycle(cycle.ID, cycle) || filepath.Base(s.cyclePath(cycle.ID)) != file.Name() {
			return fmt.Errorf("invalid weekly history snapshot")
		}
		s.cycles[cycle.ID] = cycle
	}
	return nil
}

// pruneCycles is called with the store mutex held (or during initialization).
func (s *Store) pruneCycles() error {
	for key, cycle := range s.cycles {
		if !cycle.Reset.Before(s.now().AddDate(0, 0, -RetentionDays)) {
			continue
		}
		if err := os.Remove(s.cyclePath(key)); err != nil && !os.IsNotExist(err) {
			s.quotaErr = err
			return fmt.Errorf("expire weekly history: %w", err)
		}
		delete(s.cycles, key)
		delete(s.quotaDirty, key)
	}
	return nil
}
