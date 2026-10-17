package management

import (
	"context"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/usagehistory"
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
		log.WithError(err).Error("Initialize client usage history")
		return
	}
	h.clientUsage = store
	pricing, errPricing := usagehistory.OpenPricing(filepath.Join(h.cfg.AuthDir, customQuotaCacheDirName, "usage-history"))
	if errPricing != nil {
		log.WithError(errPricing).Warn("Initialize public API price catalog")
	} else {
		h.clientPricing = pricing
	}
	usage.RegisterNamedPlugin("cpamc-client-usage-history", clientUsagePlugin{handler: h, store: store})
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
	rows, err := h.clientUsage.Snapshot(from, to)
	if err != nil {
		log.WithError(err).Error("Read client usage history")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "client usage history persistence unavailable"})
		return
	}
	weeks, errWeeks := h.clientUsage.Reports(from, to)
	if errWeeks != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "weekly usage history unavailable"})
		return
	}
	settings := h.clientUsage.Settings()
	labels := make(map[string]string)
	accounts := map[string]gin.H{}
	for _, week := range weeks {
		if accounts[week.AccountID] == nil {
			accounts[week.AccountID] = gin.H{"id": week.AccountID, "provider": week.Provider, "plan": week.Plan}
		}
	}
	for _, row := range rows {
		if accounts[row.AccountID] == nil {
			accounts[row.AccountID] = gin.H{"id": row.AccountID, "provider": row.Provider}
		}
	}
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
			id := usagehistory.Fingerprint(account.ID)
			info := accounts[id]
			if info == nil {
				info = gin.H{"id": id}
				accounts[id] = info
			}
			info["provider"] = quotaViewProvider(account)
			email, fee := h.clientUsageAccountInfo(account)
			info["email"] = email
			if fee != nil {
				info["monthly_fee_usd"] = *fee
			}
			if plan := account.Attributes["plan_type"]; plan != "" {
				info["plan"] = plan
			}
			label := account.Label
			if label == "" {
				label = account.FileName
			}
			// Account names come from current auth metadata, never from persisted secrets.
			if label != "" {
				labels[usagehistory.Fingerprint(account.ID)] = label
				if account.Index != "" {
					labels[usagehistory.Fingerprint(account.Index)] = label
				}
			}
		}
	}
	catalogDate := ""
	if h.clientPricing != nil {
		catalogDate = h.clientPricing.AsOf()
	}
	models := make([]usagehistory.ModelReference, 0)
	for _, model := range registry.GetGlobalRegistry().GetAvailableModelInfos() {
		models = append(models, h.clientPricing.ModelReference(model.ID, model.OwnedBy, model.MetadataModelID, settings))
	}
	c.JSON(http.StatusOK, gin.H{"models": models, "from": from, "to": to, "timezone": "UTC", "retention_days": usagehistory.RetentionDays, "collection_enabled": enabled, "rows": rows, "account_labels": labels, "key_labels": keyLabels, "weeks": weeks, "accounts": accounts, "settings": settings, "prices_as_of": catalogDate})
}
