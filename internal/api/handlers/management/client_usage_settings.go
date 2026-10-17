package management

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/usagehistory"
	log "github.com/sirupsen/logrus"
)

func (h *Handler) PutClientUsageSettings(c *gin.Context) {
	if h.clientUsage == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "usage history unavailable"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	var settings usagehistory.Settings
	if err := c.ShouldBindJSON(&settings); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid usage settings"})
		return
	}
	if err := h.clientUsage.UpdateSettings(settings); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot save usage settings; check names, model rates and storage"})
		return
	}
	c.JSON(http.StatusOK, h.clientUsage.Settings())
}

func (h *Handler) RefreshClientUsagePrices(c *gin.Context) {
	if h.clientPricing == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "price catalog unavailable"})
		return
	}
	if err := h.clientPricing.Refresh(c.Request.Context()); err != nil {
		log.WithError(err).Warn("Refresh API-equivalent prices")
		c.JSON(http.StatusBadGateway, gin.H{"error": "public price catalog unavailable; existing rates retained"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"prices_as_of": h.clientPricing.AsOf()})
}

func (h *Handler) clientQuotaRefreshAllowed(now time.Time) bool {
	return customQuotaInWindow(now) || h.clientUsageEnabled()
}

func (h *Handler) clientQuotaWindowLabel() string {
	if h.clientUsageEnabled() {
		return "24h (weekly usage history)"
	}
	return "09:30-21:30 UTC-3"
}

func (h *Handler) clientUsageEnabled() bool {
	if h == nil || h.clientUsage == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg != nil && h.cfg.UsageStatisticsEnabled
}

// observeClientQuota receives successful cached/replayed responses, never recipes
// or response bodies on disk. Only safe numeric weekly observations are persisted.
func (h *Handler) observeClientQuota(recipe customQuotaRecipe, response apiCallResponse, at time.Time) {
	if !h.clientUsageEnabled() || response.StatusCode < 200 || response.StatusCode >= 300 {
		return
	}
	provider := recipe.Provider
	plan := ""
	if h.authManager != nil {
		for _, auth := range h.authManager.List() {
			if auth != nil && auth.ID == recipe.AuthID {
				provider = quotaViewProvider(auth)
				plan = auth.Attributes["plan_type"]
				break
			}
		}
	}
	windows, detected := usagehistory.ParseWeeklyQuota(provider, []byte(response.Body))
	if len(windows) == 0 {
		return
	}
	if detected != "" {
		plan = detected
	}
	if plan == "" {
		customQuota.mu.Lock()
		for _, entry := range customQuota.entries {
			if entry != nil && entry.Recipe.AuthID == recipe.AuthID && entry.StatusCode >= 200 && entry.StatusCode < 300 {
				_, candidate := usagehistory.ParseWeeklyQuota(provider, []byte(entry.Body))
				if candidate != "" {
					plan = candidate
					break
				}
			}
		}
		customQuota.mu.Unlock()
	}
	for _, window := range windows {
		if err := h.clientUsage.ObserveQuota(recipe.AuthID, provider, window.Scope, plan, window.Reset, at, window.Used); err != nil {
			log.WithError(err).Error("Persist subscription reset history")
		}
	}
}

func (h *Handler) seedClientQuotaHistory() {
	if !h.clientUsageEnabled() {
		return
	}
	customQuota.mu.Lock()
	entries := make([]customQuotaEntry, 0, len(customQuota.entries))
	for _, entry := range customQuota.entries {
		if entry != nil {
			entries = append(entries, *entry)
		}
	}
	customQuota.mu.Unlock()
	for _, entry := range entries {
		h.observeClientQuota(entry.Recipe, apiCallResponse{StatusCode: entry.StatusCode, Body: entry.Body}, entry.FetchedAt)
	}
}
