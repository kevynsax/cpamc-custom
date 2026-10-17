package management

import (
	"encoding/json"
	"math"
	"net/mail"
	"strconv"
	"strings"

	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func usageEmail(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	parsed, err := mail.ParseAddress(strings.TrimSpace(text))
	if err != nil {
		return ""
	}
	return parsed.Address
}
func usageProfileEmail(value any) string {
	data, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"email", "account_email", "user_email"} {
		if email := usageEmail(data[key]); email != "" {
			return email
		}
	}
	for _, key := range []string{"account", "user", "profile", "data"} {
		if email := usageProfileEmail(data[key]); email != "" {
			return email
		}
	}
	return ""
}

// Identity is resolved at read time for authenticated management clients; it is
// never copied into usage snapshots or the public client-key quota view.
func (h *Handler) clientUsageAccountInfo(account *coreauth.Auth) (string, *float64) {
	if account == nil {
		return "", nil
	}
	email := usageProfileEmail(account.Metadata)
	if email == "" {
		email = usageEmail(account.Attributes["email"])
	}
	if email == "" {
		email = usageEmail(account.Label)
	}
	if email == "" {
		customQuota.mu.Lock()
		for _, entry := range customQuota.entries {
			if entry == nil || entry.Recipe.AuthID != account.ID || entry.StatusCode < 200 || entry.StatusCode >= 300 {
				continue
			}
			var profile map[string]any
			if json.Unmarshal([]byte(entry.Body), &profile) == nil {
				if found := usageProfileEmail(profile); found != "" {
					email = found
					break
				}
			}
		}
		customQuota.mu.Unlock()
	}
	var fee *float64
	raw := account.Attributes["monthly_fee_usd"]
	if raw == "" {
		if value, ok := account.Metadata["monthly_fee_usd"].(float64); ok {
			raw = strconv.FormatFloat(value, 'f', -1, 64)
		} else if value, ok := account.Metadata["monthly_fee_usd"].(string); ok {
			raw = value
		}
	}
	if value, err := strconv.ParseFloat(raw, 64); err == nil && !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1e6 {
		fee = &value
	}
	return email, fee
}
