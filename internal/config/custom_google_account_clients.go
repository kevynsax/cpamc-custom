package config

// CUSTOM (cpamc-custom): Google-account clients. See CUSTOM_FEATURES.md
// ("Google-account clients").

import (
	"fmt"
	"strings"

	"github.com/MoeForge/moego/regexp"
)

// GoogleAccountClient is one client API key that may use the models cpamc serves through
// a Google account. The key must also be listed in access.api-keys. The name selects the
// request log directory, <logs>/clients/<name>, that receives every request made with the key.
type GoogleAccountClient struct {
	Name   string `yaml:"name" json:"name"`
	APIKey string `yaml:"api-key" json:"api-key"`
}

var googleAccountClientNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// validateGoogleAccountClients rejects a client without a name or key, an unsafe log
// directory name, and a repeated name or key.
func validateGoogleAccountClients(clients []GoogleAccountClient) error {
	names := make(map[string]bool, len(clients))
	keys := make(map[string]bool, len(clients))
	for i, client := range clients {
		name := client.Name
		key := client.APIKey
		if name == "" || key == "" {
			return fmt.Errorf("invalid google-account-clients entry %d: name and api-key are required", i)
		}
		if !googleAccountClientNamePattern.MatchString(name) {
			return fmt.Errorf("invalid google-account-clients name %q: expected 1-64 lowercase letters, digits, dots, underscores or hyphens, starting with a letter or digit", name)
		}
		if names[name] {
			return fmt.Errorf("invalid google-account-clients name %q: it is used by two clients", name)
		}
		if keys[key] {
			return fmt.Errorf("invalid google-account-clients entry %d: its api-key is used by two clients", i)
		}
		names[name] = true
		keys[key] = true
	}
	return nil
}

// GoogleAccountClientNames lists the configured client names in configuration order.
func GoogleAccountClientNames(clients []GoogleAccountClient) []string {
	names := make([]string, 0, len(clients))
	for _, client := range clients {
		names = append(names, client.Name)
	}
	return names
}

// IsGoogleAccountProvider reports whether provider serves its models through a Google account.
func IsGoogleAccountProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "antigravity", "gemini-cli", "aistudio":
		return true
	}
	return false
}
