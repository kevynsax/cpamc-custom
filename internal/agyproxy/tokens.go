package agyproxy

// CUSTOM (cpamc-custom)

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"gopkg.in/yaml.v3"
)

type grant struct {
	AccountID string `yaml:"account-id"`
	Client    string `yaml:"client"`
	KeyHash   string `yaml:"key-hash"`
}

func hash(value string) string {
	data := []byte(value)
	sum := sha256.Sum256(data)
	encoded := hex.EncodeToString(sum[:])
	return encoded
}

func (p *Proxy) Bootstrap(clientName, accountID string) ([]byte, error) {
	var selected config.GoogleAccountClient
	cfg := p.config()
	if cfg.AgyProxy.Listen == "" {
		return nil, fmt.Errorf("agy proxy is disabled")
	}
	for _, client := range cfg.GoogleAccountClients {
		if client.Name == clientName {
			selected = client
			break
		}
	}
	_, allowed := p.client(selected.APIKey)
	if !allowed {
		return nil, fmt.Errorf("unknown Google-account client")
	}
	account, ok := p.manager.GetByID(accountID)
	if !ok || !account.AgyProxyOnly() {
		return nil, fmt.Errorf("account must be an agy-only antigravity credential")
	}
	doc, status := accountToken(account)
	if status != http.StatusOK {
		return nil, fmt.Errorf("account disabled or awaiting token refresh")
	}
	random := make([]byte, 32)
	if _, errRand := rand.Read(random); errRand != nil {
		return nil, errRand
	}
	encoded := hex.EncodeToString(random)
	refresh := "cpamc-agy-" + encoded
	keyHash := hash(selected.APIKey)
	value := grant{AccountID: accountID, Client: selected.Name, KeyHash: keyHash}
	refreshHash := hash(refresh)
	p.mu.Lock()
	defer p.mu.Unlock()
	updated := make(map[string]grant, len(p.grants)+1)
	for id, existing := range p.grants {
		if existing.Client != selected.Name {
			updated[id] = existing
		}
	}
	updated[refreshHash] = value
	data, errEncode := yaml.Marshal(updated)
	if errEncode != nil {
		return nil, errEncode
	}
	path := filepath.Join(p.dir, "grants.yaml")
	if errWrite := writePrivate(path, data); errWrite != nil {
		return nil, errWrite
	}
	p.grants = updated
	if errSet := doc.Set("refresh_token", refresh); errSet != nil {
		return nil, errSet
	}
	email, _ := account.Metadata["email"].(string)
	if errSet := doc.Set("id_token_email", email); errSet != nil {
		return nil, errSet
	}
	expiry, _ := account.AccessTokenExpirationTime()
	expiryText := expiry.Format(time.RFC3339)
	if errSet := doc.Set("expiry", expiryText); errSet != nil {
		return nil, errSet
	}
	return doc.Bytes(), nil
}

func (p *Proxy) token(body []byte, client config.GoogleAccountClient) (*ojson.Doc, int) {
	bodyText := string(body)
	form, errParse := url.ParseQuery(bodyText)
	if errParse != nil || form.Get("grant_type") != "refresh_token" || len(form["refresh_token"]) != 1 {
		return oauthError("invalid_grant", http.StatusBadRequest)
	}
	refresh := form.Get("refresh_token")
	id := hash(refresh)
	p.mu.Lock()
	value, known := p.grants[id]
	p.mu.Unlock()
	keyHash := hash(client.APIKey)
	if !known || value.Client != client.Name || value.KeyHash != keyHash {
		return oauthError("invalid_grant", http.StatusBadRequest)
	}
	account, ok := p.manager.GetByID(value.AccountID)
	if !ok || !account.AgyProxyOnly() {
		return oauthError("invalid_grant", http.StatusBadRequest)
	}
	return accountToken(account)
}

func accountToken(account *auth.Auth) (*ojson.Doc, int) {
	if account.Disabled || account.Status == auth.StatusDisabled {
		return oauthError("invalid_grant", http.StatusBadRequest)
	}
	access, _ := account.Metadata["access_token"].(string)
	expires, ok := account.AccessTokenExpirationTime()
	remaining := time.Until(expires)
	if access == "" || !ok || remaining < 2*time.Minute {
		return oauthError("temporarily_unavailable", http.StatusServiceUnavailable)
	}
	doc := ojson.NewDocString("{}")
	seconds := int64(remaining.Seconds())
	if errSet := doc.Set("access_token", access); errSet != nil {
		return oauthError("server_error", http.StatusInternalServerError)
	}
	if errSet := doc.Set("token_type", "Bearer"); errSet != nil {
		return oauthError("server_error", http.StatusInternalServerError)
	}
	if errSet := doc.Set("expires_in", seconds); errSet != nil {
		return oauthError("server_error", http.StatusInternalServerError)
	}
	return doc, http.StatusOK
}

func oauthError(code string, status int) (*ojson.Doc, int) {
	doc := ojson.NewDocString(`{"error":"server_error"}`)
	if errSet := doc.Set("error", code); errSet != nil {
		return doc, http.StatusInternalServerError
	}
	return doc, status
}
