/**
 * CUSTOM (cpamc-custom): quota requests go through the backend cache.
 * See CUSTOM_FEATURES.md at the repo root.
 *
 * Every quota adapter call (fetchQuota / enrichQuota) is wrapped in `withQuotaCache`
 * (providers/index.ts). While inside that scope, `apiCallApi.request` posts to the
 * backend cache endpoint instead of the direct proxy, so upstream provider code and
 * its tests stay unchanged and new upstream providers are covered automatically.
 *
 * The backend answers from its cache (refreshed server-side every 10 min between
 * 09:30 and 21:30 UTC-3). Explicit refresh actions ("Refresh all credentials",
 * per-card refresh) run inside `withQuotaForceRefresh`, which asks for a live call.
 *
 * Scope tracking uses counters: quota calls are only issued from inside these
 * scopes, and non-quota calls made concurrently only lose caching (never correctness)
 * because the backend falls back to a live call for non-cacheable requests.
 */

let cacheDepth = 0;
let forceDepth = 0;

export const QUOTA_CACHE_API_CALL_PATH = '/custom/quota/api-call';

/** Route quota requests made by `fn` through the backend cache. */
export async function withQuotaCache<T>(fn: () => Promise<T>): Promise<T> {
  cacheDepth += 1;
  try {
    return await fn();
  } finally {
    cacheDepth -= 1;
  }
}

/** Run `fn` with live (non-cached) quota requests. */
export async function withQuotaForceRefresh<T>(fn: () => Promise<T>): Promise<T> {
  forceDepth += 1;
  try {
    return await fn();
  } finally {
    forceDepth -= 1;
  }
}

/** Path + extra payload fields for the current scope, or null for the direct call. */
export function quotaCacheRoute(): { path: string; force: boolean } | null {
  if (cacheDepth <= 0) return null;
  return { path: QUOTA_CACHE_API_CALL_PATH, force: forceDepth > 0 };
}
