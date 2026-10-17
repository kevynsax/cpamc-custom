package usagehistory

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

func TestStoreAttributionPersistenceAndPrivacy(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	store, err := Open(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	record := usage.Record{APIKey: "client-secret-one", AuthID: "upstream-secret-account", Provider: "codex", AuthType: "oauth", RequestedAt: now, Detail: usage.Detail{InputTokens: 10, OutputTokens: 4, CachedTokens: 3, TotalTokens: 14}}
	for i := 0; i < 2; i++ {
		if errRecord := store.Record(record); errRecord != nil {
			t.Fatal(errRecord)
		}
	}
	record.APIKey = "client-secret-two"
	record.Failed = true
	record.Detail = usage.Detail{}
	if errRecord := store.Record(record); errRecord != nil {
		t.Fatal(errRecord)
	}
	record.AuthID = "second-account"
	record.Failed = false
	record.AuthType = "api_key"
	if errRecord := store.Record(record); errRecord != nil {
		t.Fatal(errRecord)
	}
	reopened, errOpen := Open(dir, func() time.Time { return now })
	if errOpen != nil {
		t.Fatal(errOpen)
	}
	rows, errSnapshot := reopened.Snapshot("2026-06-10", "2026-06-10")
	if errSnapshot != nil || len(rows) != 3 {
		t.Fatalf("rows=%+v err=%v", rows, errSnapshot)
	}
	var found bool
	for _, row := range rows {
		if row.KeyID == Fingerprint("client-secret-one") {
			found = true
			if row.Requests != 2 || row.InputTokens != 20 || row.OutputTokens != 8 || row.CachedTokens != 6 || row.TotalTokens != 28 || row.Failed != 0 {
				t.Fatalf("wrong totals: %+v", row)
			}
		}
	}
	if !found {
		t.Fatal("first key missing")
	}
	data, errRead := os.ReadFile(filepath.Join(dir, "2026-06-10.json"))
	if errRead != nil {
		t.Fatal(errRead)
	}
	for _, secret := range []string{"client-secret-one", "client-secret-two", "upstream-secret-account"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("credential persisted")
		}
	}
	info, errStat := os.Stat(filepath.Join(dir, "2026-06-10.json"))
	if errStat != nil {
		t.Fatal(errStat)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Fatalf("unsafe permissions: %v", info.Mode())
	}
}

func TestStoreUTCRetentionAndConcurrency(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	store, err := Open(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	oldest := now.AddDate(0, 0, -364)
	for _, date := range []time.Time{oldest, oldest.AddDate(0, 0, -1), now} {
		if errRecord := store.Record(usage.Record{APIKey: "key", AuthID: "account", RequestedAt: date}); errRecord != nil {
			t.Fatal(errRecord)
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if errRecord := store.Record(usage.Record{APIKey: "key", AuthID: "account", RequestedAt: now}); errRecord != nil {
				t.Error(errRecord)
			}
		}()
	}
	wg.Wait()
	rows, errSnapshot := store.Snapshot(oldest.Format(DateLayout), now.Format(DateLayout))
	if errSnapshot != nil || len(rows) != 2 || rows[1].Requests != 21 {
		t.Fatalf("rows=%+v err=%v", rows, errSnapshot)
	}
	now = now.AddDate(0, 0, 1)
	rows, errSnapshot = store.Snapshot("2000-01-01", now.Format(DateLayout))
	if errSnapshot != nil || len(rows) != 1 {
		t.Fatalf("retention rows=%+v err=%v", rows, errSnapshot)
	}
	if _, errStat := os.Stat(filepath.Join(dir, oldest.Format(DateLayout)+".json")); !os.IsNotExist(errStat) {
		t.Fatalf("expired file remains: %v", errStat)
	}
	at := time.Date(2026, 6, 11, 0, 30, 0, 0, time.FixedZone("UTC+2", 2*60*60))
	if errRecord := store.Record(usage.Record{APIKey: "utc", RequestedAt: at}); errRecord != nil {
		t.Fatal(errRecord)
	}
	rows, _ = store.Snapshot("2026-06-10", "2026-06-10")
	if len(rows) != 2 {
		t.Fatalf("UTC conversion failed: %+v", rows)
	}
}

func TestStoreCorruptionIsNotOverwritten(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-06-10.json")
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, func() time.Time { return now }); err == nil {
		t.Fatal("corrupt history accepted")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "broken" {
		t.Fatal("corrupt history overwritten")
	}
}

func TestStoreDiskFailureRetainsPendingCounters(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	store, err := Open(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	// A directory at the target filename deterministically makes atomic replacement fail.
	blocked := filepath.Join(dir, "2026-06-10.json")
	if errMkdir := os.Mkdir(blocked, 0700); errMkdir != nil {
		t.Fatal(errMkdir)
	}
	record := usage.Record{APIKey: "key", AuthID: "account", RequestedAt: now}
	if errRecord := store.Record(record); errRecord == nil {
		t.Fatal("write failure not surfaced")
	}
	if _, errSnapshot := store.Snapshot("2026-06-10", "2026-06-10"); errSnapshot == nil {
		t.Fatal("unpersisted data reported as healthy")
	}
	if errRemove := os.Remove(blocked); errRemove != nil {
		t.Fatal(errRemove)
	}
	if errRecord := store.Record(record); errRecord != nil {
		t.Fatal(errRecord)
	}
	reopened, errOpen := Open(dir, func() time.Time { return now })
	if errOpen != nil {
		t.Fatal(errOpen)
	}
	rows, _ := reopened.Snapshot("2026-06-10", "2026-06-10")
	if len(rows) != 1 || rows[0].Requests != 2 {
		t.Fatalf("pending usage lost: %+v", rows)
	}
}
