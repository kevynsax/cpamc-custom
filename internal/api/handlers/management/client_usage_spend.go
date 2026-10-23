package management

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/usagehistory"
	log "github.com/sirupsen/logrus"
)

// GetClientUsageSpend returns per-key API value totals for one week or month.
// CUSTOM (cpamc-custom): compact companion to /observability/usage/clients for
// clients that only need totals, not minute rows or price tables.
func (h *Handler) GetClientUsageSpend(c *gin.Context) {
	if h.clientUsage == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "client usage history unavailable"})
		return
	}
	period := c.DefaultQuery("period", "week")
	offset, errOffset := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if period != "week" && period != "month" || errOffset != nil || offset < 0 || offset > usagehistory.SpendMaxOffset(period) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "period must be week or month with a supported offset"})
		return
	}
	providers := map[string]string{}
	h.mu.Lock()
	manager := h.authManager
	h.mu.Unlock()
	if manager != nil {
		for _, account := range manager.List() {
			if account != nil {
				providers[usagehistory.Fingerprint(account.ID)] = quotaViewProvider(account)
			}
		}
	}
	items, err := h.clientUsage.Spend(period, offset, providers)
	if err != nil {
		log.WithError(err).Error("Read client usage spend")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "client usage history persistence unavailable"})
		return
	}
	metadata := h.clientKeyMetadata()
	writeCompressedJSON(c, http.StatusOK, gin.H{"period": period, "offset": offset, "items": items, "key_names": metadata.Names, "key_kinds": metadata.Kinds})
}
