package handlers

// CUSTOM (cpamc-custom): per-key permission for OpenAI fast/ultrafast service tiers.
// See CUSTOM_FEATURES.md ("OpenAI speed permission per API key").
//
// Every client API key starts at standard speed. A key may request fast only when
// its level is fast or ultrafast, and ultrafast only when its level is ultrafast.
// Requests above the key's level that would run on an OpenAI provider are rejected
// with 403 before any credential is selected. Other providers are not affected.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/usagehistory"
	coreusage "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	"github.com/tidwall/gjson"
	"golang.org/x/net/context"
)

var keySpeedLookup atomic.Pointer[func(apiKey string) string]

// SetKeySpeedLookup installs the function returning a client API key's speed level.
// Without a lookup no speed restriction is enforced (embedded SDK use).
func SetKeySpeedLookup(lookup func(apiKey string) string) {
	if lookup == nil {
		keySpeedLookup.Store(nil)
		return
	}
	keySpeedLookup.Store(&lookup)
}

// keySpeedError rejects a request whose OpenAI service tier exceeds the key's level.
func keySpeedError(ctx context.Context, providers []string, rawJSON []byte, internal bool) *interfaces.ErrorMessage {
	lookup := keySpeedLookup.Load()
	if lookup == nil || internal || !usesOpenAIProvider(providers) {
		return nil
	}
	mode := requestedSpeedMode(rawJSON)
	if mode == "" {
		return nil
	}
	apiKey := coreusage.APIKeyFromContext(ctx)
	if apiKey == "" {
		return nil
	}
	if usagehistory.KeySpeedAllows((*lookup)(apiKey), mode) {
		return nil
	}
	message := fmt.Sprintf("This API key is not allowed to use the %s service tier. Ask the gateway administrator to enable it for this key.", mode)
	body, _ := json.Marshal(ErrorResponse{Error: ErrorDetail{Message: message, Type: "permission_error", Code: "service_tier_not_allowed"}})
	return &interfaces.ErrorMessage{
		StatusCode:     http.StatusForbidden,
		Error:          fmt.Errorf("%s", message),
		DirectResponse: true,
		Body:           body,
		Headers:        http.Header{"Content-Type": []string{"application/json"}},
	}
}

// requestedSpeedMode mirrors the Codex translators: fast/priority and Claude's
// speed "fast" become fast, ultrafast stays ultrafast; anything else is standard.
func requestedSpeedMode(rawJSON []byte) string {
	if speed := gjson.GetBytes(rawJSON, "speed"); speed.Type == gjson.String && strings.EqualFold(strings.TrimSpace(speed.String()), "fast") {
		return usagehistory.KeySpeedFast
	}
	tier := gjson.GetBytes(rawJSON, "service_tier")
	if tier.Type != gjson.String {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(tier.String())) {
	case "fast", "priority":
		return usagehistory.KeySpeedFast
	case "ultrafast":
		return usagehistory.KeySpeedUltrafast
	default:
		return ""
	}
}

func usesOpenAIProvider(providers []string) bool {
	for _, provider := range providers {
		switch strings.ToLower(strings.TrimSpace(provider)) {
		case "codex", "openai":
			return true
		}
	}
	return false
}
