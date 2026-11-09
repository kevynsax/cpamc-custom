package handlers

// CUSTOM (cpamc-custom): per-key permission for OpenAI fast/ultrafast service tiers.
// See CUSTOM_FEATURES.md ("OpenAI speed permission per API key").
//
// Every client API key starts at standard speed. A key may request fast only when
// its level is fast or ultrafast, and ultrafast only when its level is ultrafast.
// Requests above the key's level that would run on an OpenAI provider are rejected
// with 403 before any credential is selected. Other providers are not affected.
// The tier is read after the configured payload rules, so a model alias whose
// override rule forces service_tier (e.g. gpt-6.1-sol-ultrafast) is checked too.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/usagehistory"
	coreusage "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	"golang.org/x/net/context"
)

var keySpeedLookup atomic.Pointer[func(apiKey string) string]

var keySpeedPayload atomic.Pointer[config.PayloadConfig]

// SetKeySpeedPayloadConfig installs the payload rules the speed check applies
// before reading the requested tier. Called on startup and every config reload.
func SetKeySpeedPayloadConfig(payload config.PayloadConfig) {
	keySpeedPayload.Store(&payload)
}

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
func keySpeedError(ctx context.Context, providers []string, model, requestedModel string, rawJSON []byte, headers http.Header, internal bool) *interfaces.ErrorMessage {
	lookup := keySpeedLookup.Load()
	if lookup == nil || internal || !usesOpenAIProvider(providers) {
		return nil
	}
	finalBody := payloadWithConfiguredTier(model, requestedModel, rawJSON, headers)
	mode := requestedSpeedMode(finalBody)
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

// payloadWithConfiguredTier applies the configured codex payload rules to a copy
// of the client body, so the speed check sees a service_tier forced by config.
func payloadWithConfiguredTier(model, requestedModel string, rawJSON []byte, headers http.Header) []byte {
	payload := keySpeedPayload.Load()
	if payload == nil || len(rawJSON) == 0 {
		return rawJSON
	}
	cfg := &config.Config{Payload: *payload}
	body := append([]byte(nil), rawJSON...)
	return helps.ApplyPayloadConfigWithRequest(cfg, model, "codex", "", "", body, rawJSON, requestedModel, "", headers)
}

// requestedSpeedMode mirrors the Codex translators: fast/priority and Claude's
// speed "fast" become fast, ultrafast stays ultrafast; anything else is standard.
func requestedSpeedMode(rawJSON []byte) string {
	if speed := ojson.GetBytes(rawJSON, "speed"); speed.Type == ojson.String && strings.EqualFold(strings.TrimSpace(speed.String()), "fast") {
		return usagehistory.KeySpeedFast
	}
	tier := ojson.GetBytes(rawJSON, "service_tier")
	if tier.Type != ojson.String {
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
