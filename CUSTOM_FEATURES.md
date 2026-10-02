# cpamc-custom: features on top of upstream

Fork of `router-for-me/CLIProxyAPI` (remote `origin`, branch `main`), with the
Management Center panel vendored as a git subtree in `panel/` (remote
`panel-upstream` = `router-for-me/Cli-Proxy-API-Management-Center`, branch `main`).
Our branch is `custom` (remote `fork` = `kevynsax/cpamc-custom`).

Every custom line is marked `CUSTOM (cpamc-custom)`. List them all with:

```bash
git grep -n "CUSTOM (cpamc-custom)"
```

## Features

### 1. Backend quota cache (all providers)

- The panel no longer calls providers through `/requests/api-call` for quota.
  Every quota adapter is wrapped (`panel/src/features/quota/providers/customQuotaCache.ts`),
  so its requests go to `POST /v8/management/custom/quota/api-call`. This is generic:
  any provider upstream adds is covered automatically.
- The backend stores each distinct request (credential + method + URL + body) and its
  last response. Cache file: `<auth-dir>/.cpamc-custom/quota-cache.json` (mode 0600).
  Tokens are never written; `$TOKEN$` stays a placeholder and is resolved per call.
- Page load reads from the cache. "Refresh all credentials" and per-card refresh send
  `force: true`, which does a live call and updates the cache.
- Never cached or replayed: URLs matching `consume|claim|redeem|/chat/completions`
  (they change state or spend tokens). Codex "reset quota" is never wrapped.
- If the backend has no custom endpoint (404), the panel falls back to the direct call.

### 2. Scheduled refresh

- Every 10 minutes, only between 09:30 and 21:30 UTC-3, the backend replays every
  cached request, for every provider, with no panel open.
- Constants are at the top of `internal/api/handlers/management/custom_quota_cache.go`.
- Status endpoint: `GET /v8/management/custom/quota/status` (no bodies, no tokens).

### 3. Earliest-reset-first routing (all providers)

- After each cache update, the backend reads reset times from the cached bodies
  (`reset_at`, `resets_at`, `resetTime`, `*ResetAtUnix`, `reset_after_seconds`,
  `currentPeriod.end`, `billingPeriodEnd`, ...).
- A credential's reset time is its latest reset within 8 days (the binding window).
- For each provider with 2+ credentials and at least one known reset, the one that
  resets soonest gets the highest priority boost. Credentials with no known reset are ranked last.
- The boost is added in `authPriority()` (`sdk/cliproxy/auth/selector.go`), so the
  scheduler sends traffic to the soonest-reset credential. When that credential is
  exhausted (quota/cooldown), normal failover moves to the next one.
- Each change is logged as `custom reset-priority: xai: a(10/01 20:55) > b(10/08 13:48)`.

### 4. Bundled panel

- `Dockerfile.custom` builds `panel/` and bakes it into the image; it sets
  `CPAMC_BUNDLED_PANEL`. When that variable is set, the GitHub auto-download is
  disabled and the bundled file is installed into the static dir.
- Upstream's `Dockerfile` is untouched. TeamCity builds `Dockerfile.custom`.

### 5. Claude fast mode for chat-completions clients

- Upstream maps fast mode only on the Responses translator, so chat-completions
  clients such as pi silently lost it. `custom_fast_mode.go` maps
  `"speed": "fast"` or `"service_tier": "priority"` to Claude's `speed: "fast"`.
  The executor then adds the `fast-mode-2026-02-01` beta.
- Anthropic still requires usage credits (extra usage) on the account for fast mode.
  Without them it returns 429 "Usage credits are required for fast mode", which
  upstream already treats as request-scoped (no credential cooldown).

### 6. MiniMax and Z.ai quota cards

- These are API-key providers in `api-keys.openai-compatibility`, so the auth-file list
  never shows them. `GET /v8/management/custom/quota/credentials` lists them
  (detected by compat name or base URL; the key itself is never returned).
- The panel merges that list into the quota page and fetches usage through the same
  backend cache: MiniMax `coding_plan/remains` (5h window + weekly per model) and
  Z.ai `monitor/usage/quota/limit` (5-hour + weekly credits). Cards reuse the Kimi row body.
- Reset fields (`end_time`, `weekly_end_time`, `nextResetTime`) feed the earliest-reset routing.

### 7. Text-to-speech endpoint (MiniMax)

- `POST /v1/audio/speech` (client API key) takes OpenAI-style `{input, voice, speed}`
  and returns mp3 from MiniMax T2A (`speech-2.8-hd`, max 10,000 chars). The MiniMax
  key is read from the openai-compatibility entry and never leaves the server.
- Used by the pi `speak` extension (`~/.pi/agent/extensions/speak`).

## Files

| File | Kind |
|---|---|
| `CUSTOM_FEATURES.md`, `Dockerfile.custom` | new |
| `internal/api/handlers/management/custom_quota_cache.go` (+ `_test.go`) | new |
| `internal/managementasset/custom_bundled.go` | new |
| `sdk/cliproxy/auth/custom_reset_priority.go` (+ `_test.go`) | new |
| `panel/src/services/api/customQuotaApiCall.ts` | new |
| `panel/src/features/quota/providers/customQuotaCache.ts` | new |
| `panel/tests/customQuotaCache.test.ts` | new |
| `internal/api/server_management_v8.go` | +2 route lines |
| `internal/api/server.go` | +1 line (start refresher) |
| `internal/managementasset/updater.go` | +2 small guards |
| `sdk/cliproxy/auth/selector.go` | `authPriority` adds the boost |
| `.dockerignore` | +2 lines |
| `internal/api/custom_speech.go` | new (TTS endpoint) |
| `internal/api/server_routes.go` | +1 route line |
| `internal/api/handlers/management/custom_quota_providers.go` | new (MiniMax/Z.ai credential list) |
| `panel/src/features/quota/providers/customApiKeyQuota.ts` (+ `tests/customApiKeyQuota.test.ts`) | new |
| `panel/src/features/quota/{constants,logic}.ts`, `providers/{index,types}.ts`, `QuotaPage.tsx` | minimax/zai entries |
| `panel/src/features/authFiles/constants.ts`, `i18n/locales/*.json` | icons + tab labels |
| `panel/tests/quotaPageLogic.test.ts` | +2 expected zero counts |
| `internal/translator/claude/openai/chat-completions/custom_fast_mode.go` (+ `_test.go`) | new |
| `internal/translator/claude/openai/chat-completions/claude_openai_request.go` | +4 lines (fast mode) |
| `panel/src/services/api/apiCall.ts` | `postApiCall` routes quota-scoped calls |
| `panel/src/features/quota/providers/index.ts` | wraps `QUOTA_ADAPTERS` |
| `panel/src/features/quota/QuotaPage.tsx` | force on refresh-all + load cached on open |
| `panel/src/features/quota/hooks/useQuotaActions.ts` | force on card refresh |
| `panel/src/features/authFiles/components/AuthFileQuotaSection.tsx` | force on card refresh |

## Merging a new upstream version

Handled by the `cpamc-upgrade` skill (`merge` step). Manually:

```bash
git fetch origin --tags && git merge --no-ff origin/main             # backend
git fetch panel-upstream && git subtree pull --prefix=panel panel-upstream main --squash   # panel
go build ./... && go test ./sdk/cliproxy/auth/ ./internal/api/... ./internal/managementasset/
(cd panel && bun install --frozen-lockfile && bun run verify)
git grep -n "CUSTOM (cpamc-custom)"   # every hook must still be present
```

Conflicts most likely in `selector.go` (`authPriority`), `apiCall.ts`
(`apiCallApi.request`), `providers/index.ts` (`QUOTA_ADAPTERS`), and `QuotaPage.tsx`
(refresh-all effect). Re-apply the marked lines; keep upstream's surrounding changes.
