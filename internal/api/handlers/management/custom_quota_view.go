package management

// CUSTOM (cpamc-custom): read-only quota view for non-management callers.
// See CUSTOM_FEATURES.md at the repo root before merging upstream.
//
// GET /v1/quota/view (client access key, not the management key) returns the
// last cached provider quota responses grouped per credential. It never fetches
// upstream: the scheduled cache refresher owns updates, so this endpoint only
// shows the last observation.
//
// Safety: recipes hold the call shape (including $TOKEN$ placeholders and
// request headers), and provider bodies can carry account identity. The view
// returns neither. Each entry is a stable opaque id, the provider, the
// quota/profile URL, the status code, the cache time, and the body with
// identity and credential fields removed.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

var quotaViewEmailPattern = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)

// Keys whose values identify an account or carry credentials. Matched on the
// normalized key (lowercased, separators removed).
var quotaViewSensitiveKeys = map[string]struct{}{
	"email": {}, "accountemail": {}, "useremail": {}, "mail": {},
	"name": {}, "displayname": {}, "fullname": {}, "username": {}, "givenname": {}, "familyname": {},
	"accountid": {}, "userid": {}, "uuid": {},
	"accesstoken": {}, "refreshtoken": {}, "idtoken": {}, "token": {}, "apikey": {}, "secret": {},
	"authorization": {}, "cookie": {}, "password": {},
	"chatgptaccountid": {}, "projectid": {}, "project": {},
}

// quotaViewEntry is one cached provider response, stripped for non-management callers.
type quotaViewEntry struct {
	URL        string `json:"url"`
	StatusCode int    `json:"status_code"`
	FetchedAt  string `json:"fetched_at"`
	Body       any    `json:"body,omitempty"`
	LastError  string `json:"last_error,omitempty"`
}

// quotaViewCredential groups a credential's cached quota responses.
type quotaViewCredential struct {
	ID       string           `json:"id"`
	Provider string           `json:"provider"`
	Disabled bool             `json:"disabled,omitempty"`
	Entries  []quotaViewEntry `json:"entries"`
}

// CustomQuotaView serves the cached quota snapshot without refreshing it.
func (h *Handler) CustomQuotaView(c *gin.Context) {
	customQuota.mu.Lock()
	h.customQuotaEnsureLoaded()
	type cached struct {
		authID     string
		provider   string
		url        string
		statusCode int
		fetchedAt  time.Time
		body       string
		lastError  string
	}
	rows := make([]cached, 0, len(customQuota.entries))
	for _, e := range customQuota.entries {
		if e == nil || customQuotaUnsafeURL.MatchString(e.Recipe.URL) {
			continue
		}
		rows = append(rows, cached{
			authID: e.Recipe.AuthID, provider: e.Recipe.Provider, url: e.Recipe.URL,
			statusCode: e.StatusCode, fetchedAt: e.FetchedAt,
			body: e.Body, lastError: e.LastError,
		})
	}
	lastRun := customQuota.lastRun
	customQuota.mu.Unlock()

	authsByID := map[string]*coreauth.Auth{}
	if h != nil && h.authManager != nil {
		for _, a := range h.authManager.List() {
			if a != nil {
				authsByID[a.ID] = a
			}
		}
	}

	grouped := map[string]*quotaViewCredential{}
	for _, row := range rows {
		view := grouped[row.authID]
		if view == nil {
			view = &quotaViewCredential{ID: quotaViewID(row.authID), Provider: row.provider}
			if auth := authsByID[row.authID]; auth != nil {
				view.Provider = strings.ToLower(strings.TrimSpace(auth.Provider))
				view.Disabled = auth.Disabled || auth.Status == coreauth.StatusDisabled
			}
			grouped[row.authID] = view
		}
		fetchedAt := ""
		if !row.fetchedAt.IsZero() {
			fetchedAt = row.fetchedAt.UTC().Format(time.RFC3339)
		}
		entry := quotaViewEntry{URL: quotaViewURL(row.url), StatusCode: row.statusCode, FetchedAt: fetchedAt}
		if row.statusCode >= 200 && row.statusCode < 300 && row.body != "" {
			if body := quotaViewBody(row.body); body != nil {
				entry.Body = body
			}
		} else if row.lastError != "" {
			entry.LastError = "upstream request failed"
		}
		view.Entries = append(view.Entries, entry)
	}

	credentials := make([]quotaViewCredential, 0, len(grouped))
	for _, view := range grouped {
		sort.Slice(view.Entries, func(i, j int) bool { return view.Entries[i].URL < view.Entries[j].URL })
		credentials = append(credentials, *view)
	}
	sort.Slice(credentials, func(i, j int) bool {
		if credentials[i].Provider != credentials[j].Provider {
			return credentials[i].Provider < credentials[j].Provider
		}
		return credentials[i].ID < credentials[j].ID
	})

	last := ""
	if !lastRun.IsZero() {
		last = lastRun.UTC().Format(time.RFC3339)
	}
	c.JSON(http.StatusOK, gin.H{
		"credentials":   credentials,
		"last_run":      last,
		"in_window":     customQuotaInWindow(time.Now()),
		"refresh_every": customQuotaRefreshEvery.String(),
		"window":        "09:30-21:30 UTC-3",
	})
}

// quotaViewID is a stable opaque handle for a credential. Auth ids can be file
// paths, so the raw id never leaves the server.
func quotaViewID(authID string) string {
	sum := sha256.Sum256([]byte(authID))
	return hex.EncodeToString(sum[:])[:16]
}

// quotaViewURL keeps only scheme, host, and path: query strings can carry
// account identifiers.
func quotaViewURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return ""
	}
	parsed.RawQuery, parsed.Fragment, parsed.User = "", "", nil
	return parsed.Scheme + "://" + parsed.Host + parsed.EscapedPath()
}

// quotaViewBody returns the cached provider body with identity and credential
// fields removed. Non-JSON bodies are dropped.
func quotaViewBody(raw string) any {
	var payload any
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return nil
	}
	return quotaViewScrub(payload)
}

func quotaViewScrub(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			if quotaViewSensitiveKey(key) {
				continue
			}
			out[key] = quotaViewScrub(child)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			out[i] = quotaViewScrub(child)
		}
		return out
	case string:
		if quotaViewEmailPattern.MatchString(typed) {
			return ""
		}
		return typed
	default:
		return v
	}
}

func quotaViewSensitiveKey(key string) bool {
	norm := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
	_, sensitive := quotaViewSensitiveKeys[norm]
	return sensitive
}
