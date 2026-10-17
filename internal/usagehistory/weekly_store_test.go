package usagehistory

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWeeklySnapshotsRetryDirtyWeeksAcrossRollover(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	store, err := Open(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	reset := now.Add(time.Hour)
	id := Fingerprint("account") + "|account|" + reset.Format(time.RFC3339)
	blocked := store.cyclePath(id)
	if errMkdir := os.MkdirAll(blocked, 0700); errMkdir != nil {
		t.Fatal(errMkdir)
	}
	if errObserve := store.ObserveQuota("account", "claude", "account", "Max", reset, now, 20); errObserve == nil {
		t.Fatal("weekly persistence error hidden")
	}
	if _, errWeeks := store.Weeks(); errWeeks == nil {
		t.Fatal("unpersisted quota reported healthy")
	}
	if errRemove := os.Remove(blocked); errRemove != nil {
		t.Fatal(errRemove)
	}
	now = reset.Add(time.Minute)
	if errObserve := store.ObserveQuota("account", "claude", "account", "Max", reset.Add(Week), now, 10); errObserve != nil {
		t.Fatal(errObserve)
	}
	reopened, errOpen := Open(dir, func() time.Time { return now })
	if errOpen != nil {
		t.Fatal(errOpen)
	}
	reports, errWeeks := reopened.Weeks()
	if errWeeks != nil || len(reports) != 2 {
		t.Fatalf("reports=%+v err=%v", reports, errWeeks)
	}
	files, errRead := os.ReadDir(filepath.Join(dir, "cycles"))
	if errRead != nil || len(files) != 2 {
		t.Fatalf("snapshots=%v err=%v", files, errRead)
	}
}

func TestWeeklySnapshotsExpireWithoutNewTraffic(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	store, err := Open(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if errObserve := store.ObserveQuota("account", "claude", "account", "Max", now.Add(time.Hour), now, 20); errObserve != nil {
		t.Fatal(errObserve)
	}
	now = now.AddDate(0, 0, 367)
	reports, errWeeks := store.Weeks()
	if errWeeks != nil || len(reports) != 0 {
		t.Fatalf("retained expired weeks: %+v err=%v", reports, errWeeks)
	}
	files, errRead := os.ReadDir(filepath.Join(dir, "cycles"))
	if errRead != nil || len(files) != 0 {
		t.Fatalf("expired snapshots remain: %v err=%v", files, errRead)
	}
}
