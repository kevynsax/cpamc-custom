package handlers

// CUSTOM (cpamc-custom): Google-account models are for the configured clients only.
// See CUSTOM_FEATURES.md ("Google-account clients").
//
// Models served through a Google account (providers antigravity, gemini-cli and
// aistudio) are rejected with 403 for every API key that is not a configured
// client's, before any credential is selected. Once a client is configured, a
// request without an API key is rejected too; a gateway without API keys and
// without clients is not restricted.

import (
	"errors"
	"net/http"
	"sync/atomic"

	"github.com/MoeForge/moego/json"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	coreusage "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	"golang.org/x/net/context"
)

var googleAccountClients atomic.Pointer[[]config.GoogleAccountClient]

// SetGoogleAccountClients installs the clients allowed to use Google-account models.
// Called on startup and every config reload.
func SetGoogleAccountClients(clients []config.GoogleAccountClient) {
	googleAccountClients.Store(&clients)
}

// googleAccountKeyError rejects a Google-account model request that is not made
// with a configured client's API key.
func googleAccountKeyError(ctx context.Context, providers []string) *interfaces.ErrorMessage {
	if !usesGoogleAccountProvider(providers) {
		return nil
	}
	apiKey := coreusage.APIKeyFromContext(ctx)
	clients := googleAccountClients.Load()
	if apiKey == "" && !hasClients(clients) {
		return nil
	}
	if !clientHasKey(clients, apiKey) {
		message := "This API key is not allowed to use Google-account models. Ask the gateway administrator to enable it for this key."
		rejected := ErrorResponse{Error: ErrorDetail{Message: message, Type: "permission_error", Code: "google_account_model_not_allowed"}}
		body, _ := json.Marshal(rejected)
		return &interfaces.ErrorMessage{
			StatusCode:     http.StatusForbidden,
			Error:          errors.New(message),
			DirectResponse: true,
			Body:           body,
			Headers:        http.Header{"Content-Type": []string{"application/json"}},
		}
	}
	return nil
}

func usesGoogleAccountProvider(providers []string) bool {
	for _, provider := range providers {
		if config.IsGoogleAccountProvider(provider) {
			return true
		}
	}
	return false
}

func clientHasKey(clients *[]config.GoogleAccountClient, apiKey string) bool {
	if clients == nil || apiKey == "" {
		return false
	}
	for _, client := range *clients {
		if client.APIKey == apiKey {
			return true
		}
	}
	return false
}

func hasClients(clients *[]config.GoogleAccountClient) bool {
	return clients != nil && len(*clients) > 0
}
