package management

// CUSTOM (cpamc-custom): periodic per-key usage snapshot + read-only endpoint.
// See CUSTOM_FEATURES.md at the repo root before merging upstream.
//
// What it does:
//   - Every 15 minutes the backend snapshots the same per-key aggregates served
//     live by GetAPIKeyUsage (success/failed counters + recent request buckets
//     per API key, grouped by provider) into an in-memory cache.
//   - GET /v0/management/usage-snapshot (v8: /observability/usage/snapshot)
//     returns the last snapshot with its fetched_at timestamp. It never triggers
//     a refresh, so callers only ever see (not refresh) the last update.
//   - The snapshot is in-memory only (no raw keys written to disk). A restart
//     clears it; the first snapshot is taken at startup, so the window with
//     "not ready" is tiny.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const usageSnapshotRefreshEvery = 15 * time.Minute

var usageSnapshot = struct {
	sync.Mutex
	startOnce sync.Once
	fetchedAt time.Time
	payload   map[string]map[string]apiKeyUsageEntry
}{}

// StartUsageSnapshotCache launches the background snapshotting loop once per process.
func (h *Handler) StartUsageSnapshotCache(ctx context.Context) {
	if h == nil {
		return
	}
	usageSnapshot.startOnce.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}
		go h.usageSnapshotLoop(ctx)
	})
}

func (h *Handler) usageSnapshotLoop(ctx context.Context) {
	h.refreshUsageSnapshot()
	ticker := time.NewTicker(usageSnapshotRefreshEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.refreshUsageSnapshot()
		}
	}
}

func (h *Handler) refreshUsageSnapshot() {
	if h == nil {
		return
	}
	h.mu.Lock()
	manager := h.authManager
	h.mu.Unlock()
	if manager == nil {
		return
	}
	payload := buildAPIKeyUsagePayload(manager, time.Now())
	maskUsageSnapshotKeys(payload)
	usageSnapshot.Lock()
	usageSnapshot.payload = payload
	usageSnapshot.fetchedAt = time.Now().UTC()
	usageSnapshot.Unlock()
}

// maskUsageSnapshotKeys replaces raw key material in composite keys
// ("base_url|api_key") with a sha256 fingerprint prefix, so the snapshot
// never carries usable secrets.
func maskUsageSnapshotKeys(payload map[string]map[string]apiKeyUsageEntry) {
	for _, bucket := range payload {
		for key, entry := range bucket {
			base, raw, ok := splitUsageCompositeKey(key)
			if !ok {
				continue
			}
			masked := base + "|sha256:" + usageKeyFingerprint(raw)
			if masked == key {
				continue
			}
			delete(bucket, key)
			if existing, dup := bucket[masked]; dup {
				existing.Success += entry.Success
				existing.Failed += entry.Failed
				existing.RecentRequests = mergeRecentRequestBuckets(existing.RecentRequests, entry.RecentRequests)
				bucket[masked] = existing
				continue
			}
			bucket[masked] = entry
		}
	}
}

func splitUsageCompositeKey(key string) (string, string, bool) {
	idx := -1
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == '|' {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", "", false
	}
	base, raw := key[:idx], key[idx+1:]
	if raw == "" || len(raw) >= 20 && raw[:7] == "sha256:" {
		return "", "", false
	}
	return base, raw, true
}

func usageKeyFingerprint(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])[:16]
}

// GetUsageSnapshot returns the last usage snapshot (read-only, never refreshes).
func (h *Handler) GetUsageSnapshot(c *gin.Context) {
	if h == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler not initialized"})
		return
	}
	usageSnapshot.Lock()
	payload := usageSnapshot.payload
	fetchedAt := usageSnapshot.fetchedAt
	usageSnapshot.Unlock()
	if fetchedAt.IsZero() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "usage snapshot not ready"})
		return
	}
	if payload == nil {
		payload = map[string]map[string]apiKeyUsageEntry{}
	}
	c.JSON(http.StatusOK, gin.H{
		"fetched_at":    fetchedAt,
		"refresh_every": usageSnapshotRefreshEvery.String(),
		"usage":         payload,
	})
}
