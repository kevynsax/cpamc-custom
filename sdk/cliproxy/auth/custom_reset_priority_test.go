package auth

// CUSTOM (cpamc-custom): earliest-reset boost makes fill-first/round-robin pick the
// boosted credential, and falls back to the next one when it is unavailable.

import (
	"context"
	"testing"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestCustomResetPriorityBoostWinsAndFailsOver(t *testing.T) {
	t.Cleanup(func() { SetCustomResetPriorityBoosts(nil) })

	early := &Auth{ID: "resets-tonight", Provider: "xai"}
	late := &Auth{ID: "resets-next-week", Provider: "xai"}

	SetCustomResetPriorityBoosts(map[string]int{"resets-tonight": 20, "resets-next-week": 10})
	scheduler := newSchedulerForTest(&RoundRobinSelector{}, late, early)
	for i := 0; i < 3; i++ {
		got, err := scheduler.pickSingle(context.Background(), "xai", "", cliproxyexecutor.Options{}, nil)
		if err != nil || got == nil || got.ID != "resets-tonight" {
			t.Fatalf("pick #%d = %v, %v; want resets-tonight", i, got, err)
		}
	}

	exhausted := early.Clone()
	exhausted.Quota = QuotaState{Exceeded: true, Reason: "credential_quota", NextRecoverAt: time.Now().Add(time.Hour)}
	exhausted.Unavailable = true
	exhausted.NextRetryAfter = time.Now().Add(time.Hour)
	scheduler = newSchedulerForTest(&FillFirstSelector{}, late, exhausted)
	got, err := scheduler.pickSingle(context.Background(), "xai", "", cliproxyexecutor.Options{}, nil)
	if err != nil || got == nil || got.ID != "resets-next-week" {
		t.Fatalf("failover pick = %v, %v; want resets-next-week", got, err)
	}
}

func TestCustomResetPriorityBoostAddsToFilePriority(t *testing.T) {
	t.Cleanup(func() { SetCustomResetPriorityBoosts(nil) })
	auth := &Auth{ID: "a", Attributes: map[string]string{"priority": "5"}}
	SetCustomResetPriorityBoosts(map[string]int{"a": 10})
	if got := authPriority(auth); got != 15 {
		t.Fatalf("authPriority = %d, want 15", got)
	}
	SetCustomResetPriorityBoosts(nil)
	if got := authPriority(auth); got != 5 {
		t.Fatalf("authPriority after clear = %d, want 5", got)
	}
}
