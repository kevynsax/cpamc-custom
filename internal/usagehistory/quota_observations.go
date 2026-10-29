package usagehistory

import (
	"encoding/json"
	"strings"
	"time"
)

// ParseQuotaWindows preserves each independent limit's scope and duration.
func ParseQuotaWindows(provider string, body []byte) ([]QuotaWindow, string) {
	result, plan := ParseWeeklyQuota(provider, body)
	var root map[string]any
	if json.Unmarshal(body, &root) != nil {
		return result, plan
	}
	add := func(scope string, reset any, used any, period time.Duration) {
		value, ok := number(used)
		at := instant(reset)
		if ok && value >= 0 && value <= 100 && !at.IsZero() && period >= time.Minute && period <= 31*24*time.Hour {
			result = append(result, QuotaWindow{Scope: scope, Reset: at, Used: value, Period: period})
		}
	}
	switch strings.ToLower(provider) {
	case "claude":
		session := object(root["five_hour"])
		add("account", session["resets_at"], session["utilization"], 5*time.Hour)
		for _, family := range []string{"opus", "sonnet", "fable"} {
			window := object(root["seven_day_"+family])
			add("family:"+family, window["resets_at"], window["utilization"], Week)
		}
	case "codex":
		limits := object(root["rate_limit"])
		for _, key := range []string{"primary_window", "secondary_window"} {
			window := object(limits[key])
			seconds, _ := number(window["limit_window_seconds"])
			if seconds > 0 && seconds != 604800 {
				add("account", window["reset_at"], window["used_percent"], time.Duration(seconds)*time.Second)
			}
		}
	case "meta":
		session := object(object(root["subs_usage"])["window"])
		minutes, _ := number(session["window_duration_mins"])
		if minutes > 0 {
			add("account", session["resets_at"], session["used_percent"], time.Duration(minutes)*time.Minute)
		}
	case "devin":
		status := object(object(root["userStatus"])["planStatus"])
		remaining, ok := number(status["dailyQuotaRemainingPercent"])
		if ok {
			add("account", status["dailyQuotaResetAtUnix"], 100-remaining, 24*time.Hour)
		}
	case "minimax":
		models, _ := root["model_remains"].([]any)
		for _, value := range models {
			model := object(value)
			remaining, ok := number(model["current_interval_remaining_percent"])
			start, end := instant(model["start_time"]), instant(model["end_time"])
			if ok && !start.IsZero() {
				add(text(model["model_name"]), model["end_time"], 100-remaining, end.Sub(start))
			}
		}
	case "zai":
		limits, _ := object(root["data"])["limits"].([]any)
		for _, value := range limits {
			window := object(value)
			unit, _ := number(window["unit"])
			length, _ := number(window["number"])
			used, ok := zaiUsedPercent(window)
			// Unit 3 is hours. Weekly windows (unit 6) come from ParseWeeklyQuota, and
			// unit 5 is calendar months, which have no fixed period.
			if unit == 3 && ok && text(window["type"]) != "TIME_LIMIT" {
				add("account", window["nextResetTime"], used, time.Duration(length)*time.Hour)
			}
		}
	}
	return result, plan
}
