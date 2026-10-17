package usagehistory

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

func TestStoreRejectsInvalidSnapshots(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	for _, data := range []string{"null", `{}`, `[{"date":"2026-06-10"}]`, `[{"date":"2026-06-09","key_id":"key","account_id":"account"}]`} {
		t.Run(data, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "2026-06-10.json"), []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(dir, func() time.Time { return now }); err == nil {
				t.Fatal("invalid snapshot accepted")
			}
		})
	}
}

func TestStoreFallbackIdentityAndProviderAccounting(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	store, err := Open(t.TempDir(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range []usage.Record{
		{APIKey: "client", AuthIndex: "index-only", Provider: "openai", Detail: usage.Detail{InputTokens: 10, OutputTokens: 5, CacheReadTokens: 3}},
		{Provider: "openai", Detail: usage.Detail{InputTokens: -1, OutputTokens: -2, TotalTokens: -3}},
	} {
		if errRecord := store.Record(record); errRecord != nil {
			t.Fatal(errRecord)
		}
	}
	rows, errSnapshot := store.Snapshot("2026-06-10", "2026-06-10")
	if errSnapshot != nil {
		t.Fatal(errSnapshot)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%+v", rows)
	}
	for _, row := range rows {
		if row.KeyID == "unknown" {
			if row.AccountID != "unknown" || row.InputTokens != 0 || row.OutputTokens != 0 || row.TotalTokens != 0 {
				t.Fatalf("invalid unknown row: %+v", row)
			}
		} else if row.AccountID != Fingerprint("index-only") || row.TotalTokens != 15 || row.CachedTokens != 3 {
			t.Fatalf("fallback/accounting: %+v", row)
		}
	}
}
