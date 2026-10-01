package management

// CUSTOM (cpamc-custom): quota cards for API-key providers configured under
// api-keys.openai-compatibility (MiniMax, Z.ai). See CUSTOM_FEATURES.md.
//
// GET /v8/management/custom/quota/credentials lists one entry per config key whose
// compat name or base URL is a known quota-capable provider. Entries carry only the
// auth_index (the panel's quota calls use $TOKEN$, resolved server-side), never the key.

import (
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

// customQuotaAPIKeyProvider maps a config compat entry to a quota provider type.
func customQuotaAPIKeyProvider(auth *coreauth.Auth) string {
	if auth == nil || auth.Attributes == nil {
		return ""
	}
	name := strings.ToLower(strings.TrimSpace(auth.Attributes["compat_name"]))
	base := strings.ToLower(strings.TrimSpace(auth.Attributes["base_url"]))
	switch {
	case strings.Contains(base, "minimax.io") || strings.Contains(base, "minimaxi.com") || name == "minimax":
		return "minimax"
	case strings.Contains(base, "z.ai") || strings.Contains(base, "bigmodel.cn") || name == "zai" || name == "z.ai" || name == "zhipu":
		return "zai"
	}
	return ""
}

// CustomQuotaCredentials lists API-key credentials that support quota cards.
func (h *Handler) CustomQuotaCredentials(c *gin.Context) {
	files := make([]gin.H, 0)
	if h != nil && h.authManager != nil {
		for _, auth := range h.authManager.List() {
			provider := customQuotaAPIKeyProvider(auth)
			if provider == "" || auth.Disabled || auth.Status == coreauth.StatusDisabled {
				continue
			}
			auth.EnsureIndex()
			label := strings.TrimSpace(auth.Attributes["compat_name"])
			if label == "" {
				label = provider
			}
			base := strings.TrimSpace(auth.Attributes["base_url"])
			files = append(files, gin.H{
				"id":           auth.ID,
				"auth_index":   auth.Index,
				"name":         label + " (api key)",
				"type":         provider,
				"provider":     provider,
				"label":        label,
				"base_url":     base,
				"runtime_only": true,
				"quota_source": "api-key",
				"disabled":     false,
			})
		}
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i]["name"].(string) < files[j]["name"].(string)
	})
	c.JSON(http.StatusOK, gin.H{"files": files})
}
