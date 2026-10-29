package management

// CUSTOM (cpamc-custom): spend Codex manual resets before they expire.
// See CUSTOM_FEATURES.md at the repo root before merging upstream.
//
// Codex grants manual rate-limit resets that expire unused. A reset is worth nothing once
// it lapses, so every enabled Codex OAuth account is checked periodically and any reset
// within customCodexAutoResetLead of its expiry is spent on that account, then the
// account's gateway cooldown is cleared (the same two steps the panel's "Reset quota" does).
//
// Safety: each credit id is attempted at most once per process, and a consume call is
// never retried, so one expiring credit can spend at most one reset.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

const (
	customCodexAutoResetLead       = 10 * time.Minute
	customCodexAutoResetCheckEvery = 30 * time.Minute
	customCodexAutoResetTick       = time.Minute
	customCodexResetCreditsURL     = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"
)

type customCodexResetCredit struct {
	ID        string
	ExpiresAt time.Time
}

type customCodexAutoResetState struct {
	mu        sync.Mutex
	credits   map[string][]customCodexResetCredit // by auth ID
	checkedAt map[string]time.Time                // by auth ID
	attempted map[string]bool                     // credit IDs already spent or tried
}

var customCodexAutoReset = &customCodexAutoResetState{
	credits:   map[string][]customCodexResetCredit{},
	checkedAt: map[string]time.Time{},
	attempted: map[string]bool{},
}

func (h *Handler) customCodexAutoResetLoop(ctx context.Context) {
	ticker := time.NewTicker(customCodexAutoResetTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			h.customCodexAutoResetRun(ctx, now)
		}
	}
}

func (h *Handler) customCodexAutoResetRun(ctx context.Context, now time.Time) {
	if h == nil || h.authManager == nil {
		return
	}
	for _, auth := range h.authManager.List() {
		if !customCodexAutoResetEligible(auth) {
			continue
		}
		auth.EnsureIndex()
		customCodexAutoReset.mu.Lock()
		stale := now.Sub(customCodexAutoReset.checkedAt[auth.ID]) >= customCodexAutoResetCheckEvery
		customCodexAutoReset.mu.Unlock()
		if stale {
			h.customCodexAutoResetRefresh(ctx, auth, now)
		}
		if credit, ok := customCodexAutoResetDue(auth.ID, now); ok {
			h.customCodexAutoResetSpend(ctx, auth, credit, now)
		}
	}
}

func customCodexAutoResetEligible(auth *coreauth.Auth) bool {
	if auth == nil || auth.Disabled || auth.Status == coreauth.StatusDisabled {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(auth.Provider), "codex") {
		return false
	}
	return auth.AuthKind() != coreauth.AuthKindAPIKey
}

// customCodexAutoResetDue returns the soonest unattempted credit inside the lead window.
func customCodexAutoResetDue(authID string, now time.Time) (customCodexResetCredit, bool) {
	customCodexAutoReset.mu.Lock()
	defer customCodexAutoReset.mu.Unlock()
	for _, credit := range customCodexAutoReset.credits[authID] {
		if customCodexAutoReset.attempted[credit.ID] || !credit.ExpiresAt.After(now) {
			continue
		}
		if credit.ExpiresAt.Sub(now) <= customCodexAutoResetLead {
			return credit, true
		}
	}
	return customCodexResetCredit{}, false
}

func (h *Handler) customCodexAutoResetRefresh(ctx context.Context, auth *coreauth.Auth, now time.Time) bool {
	header := customCodexHeaders(auth)
	header["Accept"] = "application/json"
	header["OpenAI-Beta"] = "codex-1"
	header["Originator"] = "Codex Desktop"
	idx := auth.Index
	resp, err := h.customPerformAPICall(ctx, apiCallRequest{AuthIndexSnake: &idx, Method: http.MethodGet, URL: customCodexResetCreditsURL, Header: header})
	customCodexAutoReset.mu.Lock()
	defer customCodexAutoReset.mu.Unlock()
	customCodexAutoReset.checkedAt[auth.ID] = now
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false
	}
	credits, ok := parseCustomCodexResetCredits(resp.Body)
	if !ok {
		return false
	}
	customCodexAutoReset.credits[auth.ID] = credits
	return true
}

// parseCustomCodexResetCredits keeps available codex_rate_limits credits with an expiry.
func parseCustomCodexResetCredits(body string) ([]customCodexResetCredit, bool) {
	var payload struct {
		Credits []map[string]any `json:"credits"`
	}
	if json.Unmarshal([]byte(body), &payload) != nil || payload.Credits == nil {
		return nil, false
	}
	out := make([]customCodexResetCredit, 0, len(payload.Credits))
	for _, item := range payload.Credits {
		kind, _ := firstPresent(item, "reset_type", "resetType").(string)
		status, _ := item["status"].(string)
		if kind != "codex_rate_limits" || status != "available" {
			continue
		}
		expiresAt, ok := customQuotaInstant(firstPresent(item, "expires_at", "expiresAt"))
		if !ok {
			continue
		}
		id, _ := item["id"].(string)
		if id == "" {
			id = fmt.Sprintf("expiry-%d", expiresAt.UnixNano())
		}
		out = append(out, customCodexResetCredit{ID: id, ExpiresAt: expiresAt})
	}
	return out, true
}

func firstPresent(item map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := item[key]; ok && value != nil {
			return value
		}
	}
	return nil
}

// customCodexAutoResetSpend re-checks the credit live, then spends one reset exactly once.
func (h *Handler) customCodexAutoResetSpend(ctx context.Context, auth *coreauth.Auth, credit customCodexResetCredit, now time.Time) {
	label := auth.Label
	if label == "" {
		label = auth.ID
	}
	// The credit may have been used manually since the last check.
	if !h.customCodexAutoResetRefresh(ctx, auth, now) {
		log.Warnf("codex auto-reset: could not confirm expiring reset for %s, skipping this minute", label)
		return
	}
	if _, stillDue := customCodexAutoResetDue(auth.ID, now); !stillDue {
		return
	}
	customCodexAutoReset.mu.Lock()
	customCodexAutoReset.attempted[credit.ID] = true
	customCodexAutoReset.mu.Unlock()

	if err := h.customCodexConsumeReset(ctx, auth); err != nil {
		log.Warnf("codex auto-reset: reset expiring %s for %s failed: %v", credit.ExpiresAt.Format(time.RFC3339), label, err)
		return
	}
	if _, _, err := h.authManager.ResetQuota(ctx, auth.ID); err != nil {
		log.Warnf("codex auto-reset: reset spent for %s but cooldown was not cleared: %v", label, err)
	}
	customCodexAutoReset.mu.Lock()
	customCodexAutoReset.checkedAt[auth.ID] = time.Time{} // re-read remaining credits next tick
	customCodexAutoReset.mu.Unlock()
	log.Infof("codex auto-reset: spent reset expiring %s on %s", credit.ExpiresAt.Format(time.RFC3339), label)
}

func (h *Handler) customCodexConsumeReset(ctx context.Context, auth *coreauth.Auth) error {
	idx := auth.Index
	data := fmt.Sprintf(`{"redeem_request_id":%q}`, uuid.NewString())
	resp, err := h.customPerformAPICall(ctx, apiCallRequest{
		AuthIndexSnake: &idx, Method: http.MethodPost, URL: customCodexResetCreditsURL + "/consume",
		Header: customCodexHeaders(auth), Data: data,
	})
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal([]byte(resp.Body), &body)
	if body.Code != "reset" && body.Code != "already_redeemed" {
		return fmt.Errorf("reset not confirmed (code %q)", body.Code)
	}
	return nil
}

func customCodexHeaders(auth *coreauth.Auth) map[string]string {
	header := map[string]string{
		"Authorization": "Bearer $TOKEN$",
		"Content-Type":  "application/json",
		"User-Agent":    "codex-tui/0.149.1 (Mac OS; arm64)",
	}
	if auth.Metadata == nil {
		return header
	}
	if accountID, ok := auth.Metadata["account_id"].(string); ok && strings.TrimSpace(accountID) != "" {
		header["Chatgpt-Account-Id"] = strings.TrimSpace(accountID)
	}
	return header
}
