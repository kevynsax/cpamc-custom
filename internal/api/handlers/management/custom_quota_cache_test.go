package management

// CUSTOM (cpamc-custom): tests for the quota cache helpers and reset extraction.

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCustomQuotaInWindow(t *testing.T) {
	cases := map[string]bool{
		"2026-10-01T12:29:00Z": false, // 09:29 UTC-3
		"2026-10-01T12:30:00Z": true,  // 09:30 UTC-3
		"2026-10-02T00:29:00Z": true,  // 21:29 UTC-3
		"2026-10-02T00:30:00Z": false, // 21:30 UTC-3
		"2026-10-02T05:00:00Z": false, // 02:00 UTC-3
	}
	for raw, want := range cases {
		now, _ := time.Parse(time.RFC3339, raw)
		if got := customQuotaInWindow(now); got != want {
			t.Errorf("customQuotaInWindow(%s) = %v, want %v", raw, got, want)
		}
	}
}

func collect(t *testing.T, body string, fetched time.Time) []time.Time {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatal(err)
	}
	return customQuotaCollectResets(v, "", fetched)
}

func TestCustomQuotaCollectResetsAcrossProviders(t *testing.T) {
	fetched := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		body string
		want time.Time
	}{
		{"claude resets_at", `{"five_hour":{"utilization":10,"resets_at":"2026-10-01T17:00:00Z"}}`, time.Date(2026, 10, 1, 17, 0, 0, 0, time.UTC)},
		{"codex reset_after_seconds", `{"rate_limit":{"primary_window":{"reset_after_seconds":3600}}}`, fetched.Add(time.Hour)},
		{"xai currentPeriod.end", `{"config":{"currentPeriod":{"start":"2026-09-24T23:55:00Z","end":"2026-10-01T23:55:00Z"}}}`, time.Date(2026, 10, 1, 23, 55, 0, 0, time.UTC)},
		{"xai billingPeriodEnd", `{"config":{"billingPeriodEnd":"2026-10-08T16:48:00Z"}}`, time.Date(2026, 10, 8, 16, 48, 0, 0, time.UTC)},
		{"devin unix seconds", `{"userStatus":{"planStatus":{"weeklyQuotaResetAtUnix":"1790899200"}}}`, time.Unix(1790899200, 0)},
		{"meta resets_at seconds", `{"subs_usage":{"weekly":{"resets_at":1790899200}}}`, time.Unix(1790899200, 0)},
		{"zai nextResetTime", `{"data":{"limits":[{"usage":10000,"nextResetTime":1791410021984}]}}`, time.UnixMilli(1791410021984)},
		{"minimax weekly_end_time", `{"model_remains":[{"weekly_end_time":1791158400000}]}`, time.UnixMilli(1791158400000)},
		{"antigravity resetTime", `{"groups":[{"buckets":[{"resetTime":"2026-10-08T00:00:00Z"}]}]}`, time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		got := collect(t, tc.body, fetched)
		if len(got) != 1 || !got[0].Equal(tc.want) {
			t.Errorf("%s: got %v, want [%v]", tc.name, got, tc.want)
		}
	}
	if got := collect(t, `{"currentPeriod":{"end":"x"},"end":"2026-10-01T00:00:00Z"}`, fetched); len(got) != 0 {
		t.Errorf("unrelated 'end' must be ignored, got %v", got)
	}
}

func TestCustomQuotaKeyIgnoresRotatingHeaders(t *testing.T) {
	a := customQuotaRecipe{AuthID: "x", Method: "GET", URL: "https://u", Header: map[string]string{"Authorization": "Bearer $TOKEN$"}}
	b := a
	b.Header = map[string]string{"Authorization": "Bearer other"}
	if customQuotaKey(a) != customQuotaKey(b) {
		t.Fatal("header values must not change the recipe key")
	}
	c := a
	c.URL = "https://v"
	if customQuotaKey(a) == customQuotaKey(c) {
		t.Fatal("different URLs must map to different keys")
	}
}

func TestCustomQuotaUnsafeURLs(t *testing.T) {
	for _, u := range []string{
		"https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume",
		"https://api.x.ai/v1/chat/completions",
	} {
		if !customQuotaUnsafeURL.MatchString(u) {
			t.Errorf("%s must not be cached/replayed", u)
		}
	}
	if customQuotaUnsafeURL.MatchString("https://cli-chat-proxy.grok.com/v1/billing") {
		t.Error("billing must be cacheable")
	}
}
