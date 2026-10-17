package usagehistory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPersonSystemMetadataSurvivesLegacyNamesAndRenames(t *testing.T) {
	dir := t.TempDir()
	id := Fingerprint("fixture-person-system-key")
	if err := os.WriteFile(filepath.Join(dir, "key-names.json"), []byte(`{"`+id+`":"Worker"}`), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PatchKeyMetadata(map[string]string{id: "Background"}, map[string]string{id: "system"}, false); err != nil {
		t.Fatal(err)
	}
	if err := store.PatchKeyNames(map[string]string{id: "Renamed"}, false); err != nil {
		t.Fatal(err)
	}
	restarted, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.KeyKinds()[id] != "system" || restarted.KeyNames()[id] != "Renamed" {
		t.Fatal("renaming or deployment lost key type")
	}
	if err := restarted.PatchKeyMetadata(map[string]string{id: "Old"}, map[string]string{id: "person"}, true); err != nil {
		t.Fatal(err)
	}
	if restarted.KeyKinds()[id] != "system" || restarted.KeyNames()[id] != "Renamed" {
		t.Fatal("legacy import overwrote metadata")
	}
	copy := restarted.KeyMetadata()
	copy.Kinds[id] = "person"
	if restarted.KeyKinds()[id] != "system" {
		t.Fatal("mutable key type escaped")
	}
}
func TestPersonSystemMetadataValidationAndAtomicFailure(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	id := Fingerprint("fixture-key")
	for _, kinds := range []map[string]string{{id: "robot"}, {"raw-api-key": "system"}} {
		if store.PatchKeyMetadata(map[string]string{id: "Name"}, kinds, false) == nil {
			t.Fatal("invalid kind accepted")
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "key-names.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if store.PatchKeyMetadata(map[string]string{id: "Name"}, map[string]string{id: "system"}, false) == nil {
		t.Fatal("failed atomic write accepted")
	}
	if len(store.KeyKinds()) != 0 || len(store.KeyNames()) != 0 {
		t.Fatal("failed write partially changed metadata")
	}
}
