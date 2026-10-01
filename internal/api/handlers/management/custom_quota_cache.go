package management

// CUSTOM (cpamc-custom): backend quota cache + scheduled refresh + earliest-reset routing.
// See CUSTOM_FEATURES.md at the repo root before merging upstream.
//
// How it works (provider-agnostic):
//   - The panel's quota fetchers call POST /v8/management/custom/quota/api-call with the
//     exact same payload they used to send to /requests/api-call (+ "force").
//   - Each distinct request (credential + method + url + headers + body) is a "recipe".
//     The backend answers from cache unless force=true or nothing is cached yet.
//   - Recipes and responses are persisted to <auth-dir>/.cpamc-custom/quota-cache.json,
//     so the scheduler can replay them for every provider without the panel being open.
//   - Every 10 minutes, only between 09:30 and 21:30 (UTC-3), all recipes are replayed.
//   - After every update the reset instants found in the cached bodies are used to boost
//     the credential that resets soonest (per provider) to the highest priority tier.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

const (
	customQuotaRefreshEvery   = 10 * time.Minute
	customQuotaTick           = time.Minute
	customQuotaWindowStartMin = 9*60 + 30  // 09:30
	customQuotaWindowEndMin   = 21*60 + 30 // 21:30
	customQuotaUTCOffset      = -3 * 3600  // UTC-3 (America/Sao_Paulo, no DST)
	customQuotaMaxReset       = 8 * 24 * time.Hour
	customQuotaBoostStep      = 10
	customQuotaReplayParallel = 4
	customQuotaCacheDirName   = ".cpamc-custom"
	customQuotaCacheFileName  = "quota-cache.json"
)

// Recipes that mutate upstream state must never be cached or replayed.
// chat/completions is a paid health probe (spends tokens), so it is never replayed.
var customQuotaUnsafeURL = regexp.MustCompile(`(?i)(consume|claim|redeem|/chat/completions)`)

var customQuotaZone = time.FixedZone("UTC-3", customQuotaUTCOffset)

type customQuotaRecipe struct {
	AuthID   string            `json:"auth_id"`
	Provider string            `json:"provider"`
	Method   string            `json:"method"`
	URL      string            `json:"url"`
	ProxyURL string            `json:"proxy_url,omitempty"`
	Header   map[string]string `json:"header,omitempty"`
	Data     string            `json:"data,omitempty"`
}

type customQuotaEntry struct {
	Recipe     customQuotaRecipe   `json:"recipe"`
	StatusCode int                 `json:"status_code"`
	Header     map[string][]string `json:"header,omitempty"`
	Body       string              `json:"body"`
	FetchedAt  time.Time           `json:"fetched_at"`
	LastError  string              `json:"last_error,omitempty"`
}

type customQuotaState struct {
	mu        sync.Mutex
	loaded    bool
	path      string
	entries   map[string]*customQuotaEntry
	lastRun   time.Time
	running   bool
	startOnce sync.Once
}

var customQuota = &customQuotaState{entries: map[string]*customQuotaEntry{}}

type customQuotaAPICallRequest struct {
	apiCallRequest
	Force bool `json:"force"`
}

type customQuotaAPICallResponse struct {
	apiCallResponse
	Cached    bool      `json:"cached"`
	FetchedAt time.Time `json:"fetched_at"`
}

func customQuotaKey(r customQuotaRecipe) string {
	// Header values are excluded on purpose: tokens embedded by the panel rotate,
	// and the latest live call always overwrites the stored headers.
	var b strings.Builder
	b.WriteString(r.AuthID + "\n" + strings.ToUpper(r.Method) + "\n" + r.URL + "\n" + r.ProxyURL + "\n" + r.Data)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func customQuotaInWindow(now time.Time) bool {
	local := now.In(customQuotaZone)
	minutes := local.Hour()*60 + local.Minute()
	return minutes >= customQuotaWindowStartMin && minutes < customQuotaWindowEndMin
}

// ---------- persistence ----------

func (h *Handler) customQuotaCachePath() string {
	if h == nil {
		return ""
	}
	h.mu.Lock()
	cfg := h.cfg
	h.mu.Unlock()
	if cfg == nil {
		return ""
	}
	dir, err := util.ResolveAuthDir(cfg.AuthDir)
	if err != nil || strings.TrimSpace(dir) == "" {
		return ""
	}
	return filepath.Join(dir, customQuotaCacheDirName, customQuotaCacheFileName)
}

// customQuotaEnsureLoaded must be called with customQuota.mu held.
func (h *Handler) customQuotaEnsureLoaded() {
	if customQuota.loaded {
		return
	}
	customQuota.loaded = true
	customQuota.path = h.customQuotaCachePath()
	if customQuota.path == "" {
		return
	}
	raw, err := os.ReadFile(customQuota.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.WithError(err).Warn("custom quota cache: read failed")
		}
		return
	}
	var stored struct {
		Entries map[string]*customQuotaEntry `json:"entries"`
		LastRun time.Time                    `json:"last_run"`
	}
	if err = json.Unmarshal(raw, &stored); err != nil {
		log.WithError(err).Warn("custom quota cache: parse failed, starting empty")
		return
	}
	if stored.Entries != nil {
		customQuota.entries = stored.Entries
	}
	customQuota.lastRun = stored.LastRun
}

// customQuotaSaveLocked must be called with customQuota.mu held.
func customQuotaSaveLocked() {
	if customQuota.path == "" {
		return
	}
	raw, err := json.Marshal(struct {
		Entries map[string]*customQuotaEntry `json:"entries"`
		LastRun time.Time                    `json:"last_run"`
	}{customQuota.entries, customQuota.lastRun})
	if err != nil {
		return
	}
	if err = os.MkdirAll(filepath.Dir(customQuota.path), 0o700); err != nil {
		log.WithError(err).Warn("custom quota cache: mkdir failed")
		return
	}
	tmp := customQuota.path + ".tmp"
	if err = os.WriteFile(tmp, raw, 0o600); err != nil {
		log.WithError(err).Warn("custom quota cache: write failed")
		return
	}
	if err = os.Rename(tmp, customQuota.path); err != nil {
		log.WithError(err).Warn("custom quota cache: rename failed")
	}
}

// ---------- live call (reuses the upstream APICall handler untouched) ----------

func (h *Handler) customPerformAPICall(ctx context.Context, payload apiCallRequest) (apiCallResponse, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return apiCallResponse{}, err
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v8/management/requests/api-call", bytes.NewReader(raw))
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	h.APICall(c)
	if rec.Code != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &e)
		return apiCallResponse{}, fmt.Errorf("api-call %d: %s", rec.Code, e.Error)
	}
	var out apiCallResponse
	if err = json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		return apiCallResponse{}, err
	}
	return out, nil
}

func customQuotaPayload(r customQuotaRecipe, authIndex string) apiCallRequest {
	idx := authIndex
	return apiCallRequest{AuthIndexSnake: &idx, Method: r.Method, URL: r.URL, ProxyURL: r.ProxyURL, Header: cloneStringMap(r.Header), Data: r.Data}
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// ---------- HTTP endpoints ----------

// CustomQuotaAPICall serves quota requests from the backend cache (POST /custom/quota/api-call).
func (h *Handler) CustomQuotaAPICall(c *gin.Context) {
	var body customQuotaAPICallRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	authIndex := firstNonEmptyString(body.AuthIndexSnake, body.AuthIndexCamel, body.AuthIndexPascal)
	auth := h.authByIndex(authIndex)
	recipe := customQuotaRecipe{
		Method: strings.ToUpper(strings.TrimSpace(body.Method)), URL: strings.TrimSpace(body.URL),
		ProxyURL: strings.TrimSpace(body.ProxyURL), Header: cloneStringMap(body.Header), Data: body.Data,
	}
	cacheable := auth != nil && recipe.URL != "" && !customQuotaUnsafeURL.MatchString(recipe.URL)
	if !cacheable {
		resp, err := h.customPerformAPICall(c.Request.Context(), body.apiCallRequest)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, customQuotaAPICallResponse{apiCallResponse: resp, FetchedAt: time.Now().UTC()})
		return
	}
	recipe.AuthID = auth.ID
	recipe.Provider = strings.ToLower(strings.TrimSpace(auth.Provider))
	key := customQuotaKey(recipe)

	customQuota.mu.Lock()
	h.customQuotaEnsureLoaded()
	entry := customQuota.entries[key]
	if entry != nil && !body.Force && entry.StatusCode != 0 {
		resp := customQuotaAPICallResponse{
			apiCallResponse: apiCallResponse{StatusCode: entry.StatusCode, Header: entry.Header, Body: entry.Body},
			Cached:          true, FetchedAt: entry.FetchedAt,
		}
		customQuota.mu.Unlock()
		c.Header("X-CPAMC-Quota-Cache", "hit")
		c.JSON(http.StatusOK, resp)
		return
	}
	customQuota.mu.Unlock()

	resp, err := h.customPerformAPICall(c.Request.Context(), customQuotaPayload(recipe, auth.Index))
	now := time.Now().UTC()
	customQuota.mu.Lock()
	if err != nil {
		if entry != nil {
			entry.LastError = err.Error()
		}
		customQuota.mu.Unlock()
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	customQuota.entries[key] = &customQuotaEntry{Recipe: recipe, StatusCode: resp.StatusCode, Header: resp.Header, Body: resp.Body, FetchedAt: now}
	customQuotaSaveLocked()
	customQuota.mu.Unlock()
	h.customQuotaApplyResetPriority()
	c.Header("X-CPAMC-Quota-Cache", "miss")
	c.JSON(http.StatusOK, customQuotaAPICallResponse{apiCallResponse: resp, FetchedAt: now})
}

// CustomQuotaStatus reports cache + routing state (GET /custom/quota/status). Bodies are omitted.
func (h *Handler) CustomQuotaStatus(c *gin.Context) {
	customQuota.mu.Lock()
	h.customQuotaEnsureLoaded()
	type row struct {
		AuthID     string    `json:"auth_id"`
		Provider   string    `json:"provider"`
		Method     string    `json:"method"`
		URL        string    `json:"url"`
		StatusCode int       `json:"status_code"`
		FetchedAt  time.Time `json:"fetched_at"`
		LastError  string    `json:"last_error,omitempty"`
	}
	rows := make([]row, 0, len(customQuota.entries))
	for _, e := range customQuota.entries {
		rows = append(rows, row{e.Recipe.AuthID, e.Recipe.Provider, e.Recipe.Method, e.Recipe.URL, e.StatusCode, e.FetchedAt, e.LastError})
	}
	lastRun := customQuota.lastRun
	customQuota.mu.Unlock()
	sort.Slice(rows, func(i, j int) bool { return rows[i].AuthID+rows[i].URL < rows[j].AuthID+rows[j].URL })
	resets := h.customQuotaResetByAuth(time.Now())
	resetOut := map[string]time.Time{}
	for id, t := range resets {
		resetOut[id] = t.UTC()
	}
	c.JSON(http.StatusOK, gin.H{
		"entries": rows, "last_run": lastRun, "in_window": customQuotaInWindow(time.Now()),
		"refresh_every": customQuotaRefreshEvery.String(), "window": "09:30-21:30 UTC-3",
		"reset_by_auth": resetOut, "priority_boosts": coreauth.CustomResetPriorityBoosts(),
	})
}

// ---------- scheduler ----------

// StartCustomQuotaCache launches the background refresher once per process.
func (h *Handler) StartCustomQuotaCache(ctx context.Context) {
	if h == nil {
		return
	}
	customQuota.startOnce.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}
		customQuota.mu.Lock()
		h.customQuotaEnsureLoaded()
		customQuota.mu.Unlock()
		h.customQuotaApplyResetPriority()
		go h.customQuotaLoop(ctx)
	})
}

func (h *Handler) customQuotaLoop(ctx context.Context) {
	ticker := time.NewTicker(customQuotaTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			customQuota.mu.Lock()
			due := !customQuota.running && now.Sub(customQuota.lastRun) >= customQuotaRefreshEvery
			customQuota.mu.Unlock()
			if due && customQuotaInWindow(now) {
				h.customQuotaRefreshAll(ctx)
			}
		}
	}
}

func (h *Handler) customQuotaRefreshAll(ctx context.Context) {
	customQuota.mu.Lock()
	if customQuota.running {
		customQuota.mu.Unlock()
		return
	}
	customQuota.running = true
	h.customQuotaEnsureLoaded()
	type job struct {
		key    string
		recipe customQuotaRecipe
	}
	jobs := make([]job, 0, len(customQuota.entries))
	for key, e := range customQuota.entries {
		jobs = append(jobs, job{key, e.Recipe})
	}
	customQuota.mu.Unlock()

	authsByID := map[string]*coreauth.Auth{}
	if h.authManager != nil {
		for _, a := range h.authManager.List() {
			if a != nil {
				a.EnsureIndex()
				authsByID[a.ID] = a
			}
		}
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, customQuotaReplayParallel)
	ok, failed, dropped := 0, 0, 0
	var countMu sync.Mutex
	for _, j := range jobs {
		auth := authsByID[j.recipe.AuthID]
		if auth == nil {
			customQuota.mu.Lock()
			delete(customQuota.entries, j.key)
			customQuota.mu.Unlock()
			dropped++
			continue
		}
		if auth.Disabled || auth.Status == coreauth.StatusDisabled {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(j job, index string) {
			defer wg.Done()
			defer func() { <-sem }()
			resp, err := h.customPerformAPICall(ctx, customQuotaPayload(j.recipe, index))
			customQuota.mu.Lock()
			defer customQuota.mu.Unlock()
			entry := customQuota.entries[j.key]
			if entry == nil {
				return
			}
			countMu.Lock()
			defer countMu.Unlock()
			if err != nil {
				entry.LastError = err.Error()
				failed++
				return
			}
			entry.StatusCode, entry.Header, entry.Body = resp.StatusCode, resp.Header, resp.Body
			entry.FetchedAt, entry.LastError = time.Now().UTC(), ""
			ok++
		}(j, auth.Index)
	}
	wg.Wait()

	customQuota.mu.Lock()
	customQuota.lastRun = time.Now()
	customQuota.running = false
	customQuotaSaveLocked()
	customQuota.mu.Unlock()
	log.Infof("custom quota cache: refreshed %d recipe(s), %d failed, %d dropped", ok, failed, dropped)
	h.customQuotaApplyResetPriority()
}

// ---------- earliest-reset routing ----------

// customQuotaResetByAuth returns, per credential, its binding reset instant: the latest
// future reset within customQuotaMaxReset (i.e. the weekly-or-shorter longest window).
func (h *Handler) customQuotaResetByAuth(now time.Time) map[string]time.Time {
	customQuota.mu.Lock()
	h.customQuotaEnsureLoaded()
	out := map[string]time.Time{}
	for _, e := range customQuota.entries {
		if e.StatusCode < 200 || e.StatusCode >= 300 || e.Body == "" {
			continue
		}
		var payload any
		if json.Unmarshal([]byte(e.Body), &payload) != nil {
			continue
		}
		for _, t := range customQuotaCollectResets(payload, "", e.FetchedAt) {
			if !t.After(now) || t.Sub(now) > customQuotaMaxReset {
				continue
			}
			if t.After(out[e.Recipe.AuthID]) {
				out[e.Recipe.AuthID] = t
			}
		}
	}
	customQuota.mu.Unlock()
	return out
}

func (h *Handler) customQuotaApplyResetPriority() {
	if h == nil || h.authManager == nil {
		return
	}
	resets := h.customQuotaResetByAuth(time.Now())
	type cand struct {
		id    string
		label string
		reset time.Time
	}
	groups := map[string][]cand{}
	for _, a := range h.authManager.List() {
		if a == nil || a.Disabled || a.Status == coreauth.StatusDisabled {
			continue
		}
		t := resets[a.ID] // zero = unknown (e.g. just reset): ranked last
		label := a.Label
		if label == "" {
			label = a.ID
		}
		p := strings.ToLower(strings.TrimSpace(a.Provider))
		groups[p] = append(groups[p], cand{a.ID, label, t})
	}
	boosts := map[string]int{}
	var summary []string
	providers := make([]string, 0, len(groups))
	for p := range groups {
		providers = append(providers, p)
	}
	sort.Strings(providers)
	for _, p := range providers {
		list := groups[p]
		known := 0
		for _, c := range list {
			if !c.reset.IsZero() {
				known++
			}
		}
		if len(list) < 2 || known == 0 {
			continue
		}
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].reset.IsZero() != list[j].reset.IsZero() {
				return !list[i].reset.IsZero()
			}
			return list[i].reset.Before(list[j].reset)
		})
		parts := make([]string, 0, len(list))
		for rank, c := range list {
			when := "unknown"
			if !c.reset.IsZero() {
				boosts[c.id] = customQuotaBoostStep * (known - rank)
				when = c.reset.In(customQuotaZone).Format("01/02 15:04")
			}
			parts = append(parts, fmt.Sprintf("%s(%s)", c.label, when))
		}
		summary = append(summary, p+": "+strings.Join(parts, " > "))
	}
	if customQuotaBoostsEqual(boosts, coreauth.CustomResetPriorityBoosts()) {
		return
	}
	coreauth.SetCustomResetPriorityBoosts(boosts)
	h.authManager.RefreshSchedulerAll()
	if len(summary) == 0 {
		log.Info("custom reset-priority: cleared (no provider with 2+ credentials and known reset)")
		return
	}
	log.Infof("custom reset-priority: %s", strings.Join(summary, " | "))
}

func customQuotaBoostsEqual(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// customQuotaCollectResets walks any JSON payload and extracts reset instants using
// provider-agnostic key names (reset_at, resets_at, resetTime, *ResetAtUnix,
// reset_after_seconds, reset_in, currentPeriod.end, billingPeriodEnd, ...).
func customQuotaCollectResets(v any, parentKey string, fetchedAt time.Time) []time.Time {
	var out []time.Time
	switch typed := v.(type) {
	case map[string]any:
		for k, child := range typed {
			norm := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(k))
			switch child.(type) {
			case map[string]any, []any:
				out = append(out, customQuotaCollectResets(child, norm, fetchedAt)...)
				continue
			}
			if t, ok := customQuotaResetValue(norm, parentKey, child, fetchedAt); ok {
				out = append(out, t)
			}
		}
	case []any:
		for _, child := range typed {
			out = append(out, customQuotaCollectResets(child, parentKey, fetchedAt)...)
		}
	}
	return out
}

func customQuotaResetValue(key, parentKey string, v any, fetchedAt time.Time) (time.Time, bool) {
	switch {
	case key == "resetaftersecond" || key == "resetafterseconds" || key == "resetin" || key == "resetinseconds":
		if secs, ok := customQuotaNumber(v); ok && secs > 0 {
			return fetchedAt.Add(time.Duration(secs * float64(time.Second))), true
		}
		return time.Time{}, false
	case key == "resetat" || key == "resetsat" || key == "resettime" || key == "nextresetat" ||
		key == "nextreset" || key == "billingperiodend" || strings.HasSuffix(key, "resetatunix") ||
		(key == "end" && parentKey == "currentperiod"):
		return customQuotaInstant(v)
	}
	return time.Time{}, false
}

func customQuotaNumber(v any) (float64, bool) {
	switch typed := v.(type) {
	case float64:
		return typed, true
	case string:
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%g", &f); err == nil {
			return f, true
		}
	}
	return 0, false
}

func customQuotaInstant(v any) (time.Time, bool) {
	if s, ok := v.(string); ok {
		s = strings.TrimSpace(s)
		if s == "" {
			return time.Time{}, false
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
			if t, err := time.Parse(layout, s); err == nil {
				return t, true
			}
		}
	}
	n, ok := customQuotaNumber(v)
	if !ok || n <= 0 {
		return time.Time{}, false
	}
	if n < 1e11 {
		return time.Unix(0, int64(n*float64(time.Second))), true
	}
	return time.UnixMilli(int64(n)), true
}
