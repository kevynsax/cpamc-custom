package usagehistory

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

type QuotaWindow struct {
	Scope  string
	Reset  time.Time
	Used   float64
	Period time.Duration
}

func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case string:
		f, e := strconv.ParseFloat(n, 64)
		return f, e == nil
	}
	return 0, false
}
func text(v any) string { s, _ := v.(string); return s }
func instant(v any) time.Time {
	if value, ok := v.(string); ok {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			return parsed.UTC()
		}
	}
	n, ok := number(v)
	if !ok || n <= 0 {
		return time.Time{}
	}
	if n > 1e12 {
		return time.UnixMilli(int64(n)).UTC()
	}
	return time.Unix(int64(n), 0).UTC()
}

// ParseWeeklyQuota only accepts explicit seven-day contracts. Short/session,
// monthly billing, model-specific Claude limits and dollar balances are not
// interchangeable with an account's weekly subscription capacity.
func ParseWeeklyQuota(provider string, body []byte) ([]QuotaWindow, string) {
	var root map[string]any
	if json.Unmarshal(body, &root) != nil {
		return nil, ""
	}
	plan := text(root["plan_type"])
	if plan == "" {
		plan = text(root["subscriptionTier"])
	}
	if plan == "" {
		plan = text(object(root["organization"])["rate_limit_tier"])
	}
	if plan == "" {
		account := object(root["account"])
		if account["has_claude_max"] == true {
			plan = "Claude Max"
		} else if account["has_claude_pro"] == true {
			plan = "Claude Pro"
		}
	}
	if len(plan) > 80 {
		plan = ""
	}
	result := []QuotaWindow{}
	add := func(scope string, reset any, used any) {
		p, ok := number(used)
		at := instant(reset)
		if ok && p >= 0 && p <= 100 && !at.IsZero() {
			result = append(result, QuotaWindow{Scope: scope, Reset: at, Used: p, Period: Week})
		}
	}
	switch strings.ToLower(provider) {
	case "claude":
		weekly := object(root["seven_day"])
		add("account", weekly["resets_at"], weekly["utilization"])
	case "codex":
		rate := object(root["rate_limit"])
		for _, name := range []string{"primary_window", "secondary_window"} {
			w := object(rate[name])
			seconds, _ := number(w["limit_window_seconds"])
			if seconds == 604800 {
				add("account", w["reset_at"], w["used_percent"])
			}
		}
	case "meta":
		weekly := object(object(root["subs_usage"])["weekly"])
		add("account", weekly["resets_at"], weekly["used_percent"])
	case "xai":
		config := object(root["config"])
		period := object(config["currentPeriod"])
		if period == nil {
			period = object(config["current_period"])
		}
		if strings.Contains(strings.ToLower(text(period["type"])), "weekly") {
			used := config["creditUsagePercent"]
			if used == nil {
				used = config["credit_usage_percent"]
			}
			add("account", period["end"], used)
		}
	case "kimi":
		summary := object(root["usage"])
		cap, okCap := number(summary["limit"])
		used, okUsed := number(summary["used"])
		if !okUsed {
			if remaining, ok := number(summary["remaining"]); ok {
				used = cap - remaining
				okUsed = true
			}
		}
		reset := summary["reset_at"]
		if reset == nil {
			reset = summary["reset_time"]
		}
		if okCap && okUsed && cap > 0 {
			add("account", reset, 100*used/cap)
		}
	case "devin":
		status := object(object(root["userStatus"])["planStatus"])
		remaining, ok := number(status["weeklyQuotaRemainingPercent"])
		if ok {
			add("account", status["weeklyQuotaResetAtUnix"], 100-remaining)
		}
		if plan == "" {
			plan = text(object(status["planInfo"])["planName"])
		}
	case "minimax":
		models, _ := root["model_remains"].([]any)
		for _, value := range models {
			m := object(value)
			remaining, ok := number(m["current_weekly_remaining_percent"])
			scope := text(m["model_name"])
			if ok && scope != "" {
				add(scope, m["weekly_end_time"], 100-remaining)
			}
		}
	case "zai":
		limits, _ := object(root["data"])["limits"].([]any)
		for _, value := range limits {
			m := object(value)
			unit, _ := number(m["unit"])
			length, _ := number(m["number"])
			cap, okCap := number(m["usage"])
			used, okUsed := number(m["currentValue"])
			if unit == 6 && length == 1 && text(m["type"]) != "TIME_LIMIT" && okCap && okUsed && cap > 0 {
				add("account", m["nextResetTime"], 100*used/cap)
			}
		}
	}
	return result, plan
}
