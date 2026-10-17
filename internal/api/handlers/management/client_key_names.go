package management

// CUSTOM (cpamc-custom): shared key metadata persists even with history paused.
import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/usagehistory"
)

// Existing keys begin as Person; explicit System classifications survive renames.
func (h *Handler) clientKeyMetadata() usagehistory.KeyMetadata {
	metadata := h.clientUsage.KeyMetadata()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg != nil {
		for _, key := range h.cfg.APIKeys {
			id := usagehistory.Fingerprint(key)
			if _, exists := metadata.Kinds[id]; !exists {
				metadata.Kinds[id] = "person"
			}
		}
	}
	return metadata
}
func (h *Handler) GetClientKeyNames(c *gin.Context) {
	if h.clientUsage == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "key metadata storage unavailable"})
		return
	}
	c.JSON(http.StatusOK, h.clientKeyMetadata())
}
func (h *Handler) PatchClientKeyNames(c *gin.Context) {
	if h.clientUsage == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "key metadata storage unavailable"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	var body struct {
		Names      map[string]string `json:"names"`
		Kinds      map[string]string `json:"kinds"`
		ImportOnly bool              `json:"import_only"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid key metadata"})
		return
	}
	if err := h.clientUsage.PatchKeyMetadata(body.Names, body.Kinds, body.ImportOnly); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot persist key metadata; check values and storage"})
		return
	}
	c.JSON(http.StatusOK, h.clientKeyMetadata())
}
