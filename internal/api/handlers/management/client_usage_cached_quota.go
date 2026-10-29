package management

// CUSTOM (cpamc-custom): read cached quota without polling or recording history.
import (
	"fmt"
	"sort"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/usagehistory"
)

type clientCachedQuota struct {
	Scope         string    `json:"scope"`
	Used          float64   `json:"used"`
	Reset         time.Time `json:"reset"`
	ObservedAt    time.Time `json:"observed_at"`
	PeriodSeconds int64     `json:"period_seconds"`
}

type clientCachedAccount struct {
	Plan   string
	Quotas []clientCachedQuota
}

func (h *Handler) clientCachedQuotas() map[string]clientCachedAccount {
	customQuota.mu.Lock()
	h.customQuotaEnsureLoaded()
	entries := make([]customQuotaEntry, 0, len(customQuota.entries))
	for _, entry := range customQuota.entries {
		if entry != nil && entry.StatusCode >= 200 && entry.StatusCode < 300 && !customQuotaUnsafeURL.MatchString(entry.Recipe.URL) {
			entries = append(entries, *entry)
		}
	}
	customQuota.mu.Unlock()
	// Recipes keep the internal auth provider. API-key providers (MiniMax, Z.ai)
	// register as openai-compatibility, so parse with the name quota clients know.
	providers := map[string]string{}
	if h != nil && h.authManager != nil {
		for _, auth := range h.authManager.List() {
			if auth != nil {
				providers[auth.ID] = quotaViewProvider(auth)
			}
		}
	}
	// Newer observations replace older duplicates, but never erase another scope.
	sort.Slice(entries, func(i, j int) bool { return entries[i].FetchedAt.Before(entries[j].FetchedAt) })
	result := map[string]clientCachedAccount{}
	windows := map[string]map[string]clientCachedQuota{}
	for _, entry := range entries {
		id := usagehistory.Fingerprint(entry.Recipe.AuthID)
		account := result[id]
		provider := providers[entry.Recipe.AuthID]
		if provider == "" {
			provider = entry.Recipe.Provider
		}
		quotas, plan := usagehistory.ParseQuotaWindows(provider, []byte(entry.Body))
		if plan != "" {
			account.Plan = plan
		}
		if windows[id] == nil {
			windows[id] = map[string]clientCachedQuota{}
		}
		for _, quota := range quotas {
			key := fmt.Sprintf("%s/%d", quota.Scope, quota.Period)
			windows[id][key] = clientCachedQuota{Scope: quota.Scope, Used: quota.Used, Reset: quota.Reset, ObservedAt: entry.FetchedAt, PeriodSeconds: int64(quota.Period / time.Second)}
		}
		result[id] = account
	}
	for id, account := range result {
		account.Quotas = []clientCachedQuota{}
		for _, window := range windows[id] {
			account.Quotas = append(account.Quotas, window)
		}
		sort.Slice(account.Quotas, func(i, j int) bool {
			if account.Quotas[i].Scope != account.Quotas[j].Scope {
				return account.Quotas[i].Scope < account.Quotas[j].Scope
			}
			return account.Quotas[i].PeriodSeconds < account.Quotas[j].PeriodSeconds
		})
		result[id] = account
	}
	return result
}
