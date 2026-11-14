package management

import (
	"context"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/usagehistory"
	sdkhandlers "github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

// StartClientUsageHistory attaches local, persistent accounting to the usage dispatcher.
// The auth directory must live on a persistent volume; history is not an auth file.
func (h *Handler) StartClientUsageHistory() {
	if h.cfg == nil || h.cfg.AuthDir == "" {
		return
	}
	store, err := usagehistory.Open(filepath.Join(h.cfg.AuthDir, customQuotaCacheDirName, "usage-history"), nil)
	if err != nil {
		// CUSTOM (cpamc-custom): without key metadata every key stays at standard speed.
		sdkhandlers.SetKeySpeedLookup(func(string) string { return usagehistory.KeySpeedStandard })
		log.WithError(err).Error("Initialize client usage history")
		return
	}
	h.clientUsage = store
	sdkhandlers.SetKeySpeedLookup(func(apiKey string) string { return store.KeySpeed(usagehistory.Fingerprint(apiKey)) })
	pricing, errPricing := usagehistory.OpenPricing(filepath.Join(h.cfg.AuthDir, customQuotaCacheDirName, "usage-history"))
	if errPricing != nil {
		log.WithError(errPricing).Warn("Initialize public API price catalog")
	} else {
		h.clientPricing = pricing
	}
	usage.RegisterNamedPlugin("cpamc-client-usage-history", clientUsagePlugin{handler: h, store: store})
}

// StopClientUsageHistory writes usage still waiting for the batched flush.
func (h *Handler) StopClientUsageHistory() {
	if h == nil || h.clientUsage == nil {
		return
	}
	if err := h.clientUsage.Close(); err != nil {
		log.WithError(err).Error("Flush client usage history")
	}
}

type clientUsagePlugin struct {
	handler *Handler
	store   *usagehistory.Store
}

func (p clientUsagePlugin) HandleUsage(ctx context.Context, record usage.Record) {
	p.handler.mu.Lock()
	enabled := p.handler.cfg != nil && p.handler.cfg.UsageStatisticsEnabled
	p.handler.mu.Unlock()
	if enabled {
		rate := p.store.PriceFor(p.handler.clientPricing, record)
		if err := p.store.RecordPriced(record, rate); err != nil {
			log.WithError(err).Error("Persist weekly client usage")
		}
	}
}

// GetClientUsage reports inclusive UTC daily records without exposing credentials.
func (h *Handler) GetClientUsage(c *gin.Context) {
	if h.clientUsage == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "client usage history unavailable"})
		return
	}
	today := time.Now().UTC()
	from, to := c.DefaultQuery("from", today.AddDate(0, 0, -29).Format(usagehistory.DateLayout)), c.DefaultQuery("to", today.Format(usagehistory.DateLayout))
	start, errStart := time.Parse(usagehistory.DateLayout, from)
	end, errEnd := time.Parse(usagehistory.DateLayout, to)
	earliest := today.AddDate(0, 0, -(usagehistory.RetentionDays - 1)).Format(usagehistory.DateLayout)
	if errStart != nil || errEnd != nil || start.After(end) || from < earliest || to > today.Format(usagehistory.DateLayout) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "from and to must be inclusive UTC dates within the last 365 days"})
		return
	}
	accounts := h.clientUsageAccounts()
	subscriptions := usageSubscriptions(accounts)
	valued, err := h.clientUsage.ValuedUsage(from, to, subscriptions)
	if err != nil {
		log.WithError(err).Error("Read client usage history")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "client usage history persistence unavailable"})
		return
	}
	// Accounts whose credential is gone keep the provider and plan their history recorded.
	for _, window := range valued.Windows {
		account, known := accounts[window.AccountID]
		if !known {
			account = usageAccount{ID: window.AccountID, Provider: window.Provider}
		}
		if account.Plan == "" {
			account.Plan = window.Plan
		}
		accounts[window.AccountID] = account
	}
	for _, row := range valued.Rows {
		if _, known := accounts[row.AccountID]; !known {
			accounts[row.AccountID] = usageAccount{ID: row.AccountID, Provider: row.Provider}
		}
	}
	// An account without a fee on its credential shows the public price of its plan.
	for id, account := range accounts {
		if account.MonthlyFeeUSD != nil {
			continue
		}
		fee, known := usagehistory.MonthlyPlanFee(account.Provider, account.Plan)
		if !known {
			continue
		}
		account.MonthlyFeeUSD = &fee
		accounts[id] = account
	}
	settings := h.clientUsage.Settings()
	labels := make(map[string]string)
	keyLabels := make(map[string]string)
	h.mu.Lock()
	manager := h.authManager
	enabled := h.cfg != nil && h.cfg.UsageStatisticsEnabled
	if h.cfg != nil {
		for _, key := range h.cfg.APIKeys {
			masked := "••••"
			if len(key) > 8 {
				masked += key[len(key)-4:]
			}
			keyLabels[usagehistory.Fingerprint(key)] = masked
		}
	}
	h.mu.Unlock()
	if manager != nil {
		for _, account := range manager.List() {
			if account == nil {
				continue
			}
			label := account.Label
			if label == "" {
				label = account.FileName
			}
			// Account names come from current auth metadata, never from persisted secrets.
			if label == "" {
				continue
			}
			labels[usagehistory.Fingerprint(account.ID)] = label
			if account.Index != "" {
				labels[usagehistory.Fingerprint(account.Index)] = label
			}
		}
	}
	catalogDate := ""
	if h.clientPricing != nil {
		catalogDate = h.clientPricing.AsOf()
	}
	models := make([]usagehistory.ModelReference, 0)
	for _, model := range registry.GetGlobalRegistry().GetAvailableModelInfos() {
		reference := h.clientPricing.ModelReference(model.ID, model.OwnedBy, model.MetadataModelID, settings)
		reference.DisplayName = model.DisplayName
		models = append(models, reference)
	}
	metadata := h.clientKeyMetadata()
	report := gin.H{"models": models, "from": from, "to": to, "timezone": "UTC", "retention_days": usagehistory.RetentionDays, "collection_enabled": enabled, "rows": valued.Rows, "account_labels": labels, "key_labels": keyLabels, "key_names": metadata.Names, "key_kinds": metadata.Kinds, "weeks": valued.Windows, "accounts": accounts, "settings": settings, "prices_as_of": catalogDate}
	writeCompressedJSON(c, http.StatusOK, report)
}
