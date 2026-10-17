package usagehistory

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestKeyNamesPersistIndependentlyOfUsageAndPrices(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	id := Fingerprint("fixture-client-secret")
	if err := store.PatchKeyNames(map[string]string{id: " Alice "}, false); err != nil {
		t.Fatal(err)
	}
	settings := store.Settings()
	settings.Prices["model"] = Rates{Input: 3, Output: 6}
	if err := store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	restarted, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.KeyNames()[id] != "Alice" {
		t.Fatal("name lost on restart or price update")
	}
	names := restarted.KeyNames()
	names[id] = "Mutated"
	if restarted.KeyNames()[id] != "Alice" {
		t.Fatal("mutable names escaped")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "key-names.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "fixture-client-secret") {
		t.Fatal("raw key persisted")
	}
	info, _ := os.Stat(filepath.Join(dir, "key-names.json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("key name file must be owner-only")
	}
}

func TestKeyNamesImportsNeverOverwriteRenamesOrClears(t *testing.T) {
	store, _ := Open(t.TempDir(), nil)
	one, two := Fingerprint("one"), Fingerprint("two")
	if err := store.PatchKeyNames(map[string]string{one: "Server"}, false); err != nil {
		t.Fatal(err)
	}
	if err := store.PatchKeyNames(map[string]string{one: "Old browser", two: "Imported"}, true); err != nil {
		t.Fatal(err)
	}
	if store.KeyNames()[one] != "Server" || store.KeyNames()[two] != "Imported" {
		t.Fatal("unsafe import merge")
	}
	if err := store.PatchKeyNames(map[string]string{one: ""}, false); err != nil {
		t.Fatal(err)
	}
	if err := store.PatchKeyNames(map[string]string{one: "Resurrected"}, true); err != nil {
		t.Fatal(err)
	}
	if name, exists := store.KeyNames()[one]; !exists || name != "" {
		t.Fatal("cleared name resurrected")
	}
	for _, invalid := range []map[string]string{nil, {"raw-key": "Name"}, {one: strings.Repeat("x", 81)}, {one: "line\nbreak"}} {
		if err := store.PatchKeyNames(invalid, false); err == nil {
			t.Fatal("invalid names accepted")
		}
	}
}

func TestKeyNamesConcurrentPatchesConserveUnrelatedEdits(t *testing.T) {
	store, _ := Open(t.TempDir(), nil)
	var wg sync.WaitGroup
	for _, id := range []string{"one", "two", "three"} {
		wg.Go(func() {
			if err := store.PatchKeyNames(map[string]string{Fingerprint(id): id}, false); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if len(store.KeyNames()) != 3 {
		t.Fatal("concurrent edits lost")
	}
}

func TestKeyNamesCorruptionAndWriteFailuresAreNotSuccess(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "key-names.json"), []byte("null"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, nil); err == nil {
		t.Fatal("corrupt names accepted")
	}
	if err := os.Remove(filepath.Join(dir, "key-names.json")); err != nil {
		t.Fatal(err)
	}
	store, _ := Open(dir, nil)
	if err := os.Mkdir(filepath.Join(dir, "key-names.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := store.PatchKeyNames(map[string]string{Fingerprint("one"): "Name"}, false); err == nil {
		t.Fatal("failed disk write accepted")
	}
	if len(store.KeyNames()) != 0 {
		t.Fatal("failed write changed active names")
	}
}
