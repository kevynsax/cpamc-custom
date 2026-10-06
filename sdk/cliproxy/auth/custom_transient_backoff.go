package auth

// CUSTOM (cpamc-custom): exponential backoff between retry rounds for transient upstream failures.
// See CUSTOM_FEATURES.md. Transport-neutral failures (for example the ChatGPT edge's Envoy
// "upstream connect error ... before headers" 503) deliberately do not cool the credential, so
// the retry loop found it immediately available and spent every request-retry round within
// milliseconds. Native Codex instead backs off (200ms doubling, five retries, then an HTTPS
// fallback) and outlives short edge outages that made cpamc clients fail. Waiting between
// rounds happens before the next credential acquisition; it never bounds an established
// upstream connection.

import (
	"net/http"
	"sync/atomic"
	"time"
)

// customTransientRetryBackoffBase is the wait before the first transient retry round. Each
// later round doubles it. Zero or negative disables the backoff.
var customTransientRetryBackoffBase atomic.Int64

// customTransientRetryBackoffCap bounds a single round's wait so recovery is noticed within a
// few seconds once the upstream is healthy again.
const customTransientRetryBackoffCap = 8 * time.Second

func init() {
	customTransientRetryBackoffBase.Store(int64(time.Second))
}

// SetCustomTransientRetryBackoffBase overrides the first-round backoff (tests and tuning).
func SetCustomTransientRetryBackoffBase(base time.Duration) {
	customTransientRetryBackoffBase.Store(int64(base))
}

// customTransientRetryWait returns the wait before retry round attempt+1. A positive wait from
// cooldown or upstream retry advice is kept. Otherwise, transient failures wait base*2^attempt,
// capped at customTransientRetryBackoffCap and maxWait. maxWait <= 0 keeps the documented
// "no waiting" behaviour.
func customTransientRetryWait(err error, attempt int, wait, maxWait time.Duration) time.Duration {
	if wait > 0 || maxWait <= 0 || attempt < 0 || !isCustomTransientRetryError(err) {
		return wait
	}
	base := time.Duration(customTransientRetryBackoffBase.Load())
	if base <= 0 {
		return wait
	}
	limit := min(customTransientRetryBackoffCap, maxWait)
	backoff := base
	for i := 0; i < attempt && backoff < limit; i++ {
		backoff *= 2
	}
	return min(backoff, limit)
}

// isCustomTransientRetryError reports server-side or transport failures that a later attempt
// on the same credential can succeed on. Quota (429) and auth (401/403) failures keep their own
// cooldown handling.
func isCustomTransientRetryError(err error) bool {
	if err == nil {
		return false
	}
	switch statusCodeFromError(err) {
	case http.StatusRequestTimeout, http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	case 0:
		return isTransientTransportError(err)
	default:
		return false
	}
}
