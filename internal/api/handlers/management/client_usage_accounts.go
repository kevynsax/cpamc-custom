package management

import (
	"encoding/json"
	"math"
	"net/mail"
	"strconv"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/usagehistory"
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

// usageAccount is one upstream account as the usage reports show it.
type usageAccount struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Plan     string `json:"plan,omitempty"`
	Email    string `json:"email,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
	// MonthlyFeeUSD is the credential's explicit fee, else the public price of its plan.
	MonthlyFeeUSD *float64            `json:"monthly_fee_usd,omitempty"`
	CachedQuotas  []clientCachedQuota `json:"cached_quotas,omitempty"`
}

// clientUsageAccounts lists the live credentials by account fingerprint, with the
// plan and quota last cached for each and the fee set on the credential, if any.
func (h *Handler) clientUsageAccounts() map[string]usageAccount {
	accounts := map[string]usageAccount{}
	h.mu.Lock()
	manager := h.authManager
	h.mu.Unlock()
	if manager == nil {
		return accounts
	}
	cachedQuotas := h.clientCachedQuotas()
	for _, credential := range manager.List() {
		if credential == nil {
			continue
		}
		id := usagehistory.Fingerprint(credential.ID)
		email, fee := h.clientUsageAccountInfo(credential)
		cached := cachedQuotas[id]
		plan := credential.Attributes["plan_type"]
		if plan == "" {
			plan = cached.Plan
		}
		disabled := credential.Disabled || credential.Status == coreauth.StatusDisabled
		provider := quotaViewProvider(credential)
		accounts[id] = usageAccount{ID: id, Provider: provider, Plan: plan, Email: email, Disabled: disabled, MonthlyFeeUSD: fee, CachedQuotas: cached.Quotas}
	}
	return accounts
}

// usageSubscriptions is the metadata the usage store prices each account with.
func usageSubscriptions(accounts map[string]usageAccount) map[string]usagehistory.AccountSubscription {
	subscriptions := map[string]usagehistory.AccountSubscription{}
	for id, account := range accounts {
		subscriptions[id] = usagehistory.AccountSubscription{Provider: account.Provider, Plan: account.Plan, MonthlyFeeUSD: account.MonthlyFeeUSD}
	}
	return subscriptions
}
