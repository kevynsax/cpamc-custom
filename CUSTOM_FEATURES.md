# cpamc-custom: features on top of upstream

Fork of `router-for-me/CLIProxyAPI` (remote `origin`, branch `main`), with the
Management Center panel vendored as a git subtree in `panel/` (remote
`panel-upstream` = `router-for-me/Cli-Proxy-API-Management-Center`, branch `main`).
Our branch is `custom` (remote `fork` = `kevynsax/cpamc-custom`).

Every custom line is marked `CUSTOM (cpamc-custom)`. List them all with:

```bash
git grep -n "CUSTOM (cpamc-custom)"
```

Merge rules, the transient-failure fixes in detail, and the split between this repository
and the `cpamc-upgrade` skill: [`docs/custom-fork-maintenance.md`](docs/custom-fork-maintenance.md).

## Features

### API value in reais and company capacity pies

The **Usage** page (`#/usage`) restores a per-person API-equivalent spending card
above the company charts, with stacked company contributions and BRL totals. Company
capacity uses segmented donut charts with remaining capacity in the center. Hovering
a segment or legend item reveals its person/category, percentage and known amount;
focus provides the same detail, arrow keys move between slices, Enter/Space or tapping
pins a slice, and Escape dismisses its tooltip. Legends and chart segments highlight
together, and reduced-motion preferences disable transitions. Existing API-key names
from the configuration key editor identify people; there is no separate naming or
subscription-label editor. Names and Person/System classifications are shared server
metadata in `<auth-dir>/.cpamc-custom/usage-history/key-names.json`, keyed by SHA-256 of the full
client key (never the raw key). The key editor saves them immediately through the
v8 management API, independently of the collection flag. Browser-only names from
older panels are imported only for missing server entries. Explicit clears retain
an empty tombstone so an old browser cannot resurrect a deleted name. Per-key
atomic patches preserve concurrent edits to other names, and model-price saves
cannot replace names or types. The version-2 metadata envelope saves names and
classifications together atomically and reads the original names-only file without
losing labels. Existing configured keys default to Person until explicitly changed;
new keys require choosing one of the icon cards, Person or System, in the key editor.
The distinction is accounting metadata only, not a different access permission.
Keep the auth volume mounted across deployments; this does not synchronize
independent gateway data directories.

`macbook`, `home-gpu`, and `spark` are one **In house** company, retaining
separate underlying accounts. Each company has a second donut showing known
API-equivalent **price by model in reais**, weighted by recorded cost rather than
token count. Its center and legend show reais, not token totals; unknown costs are
not assigned invented slices. Estimate bounds (`≥`) are omitted from the UI. Empty/unavailable
charts are omitted without an explanatory placeholder. Cached/input/output counters
are not added together. Person-detail cards show only money and percentage changes, with company/model
pie sections instead of token/request counters. When the two sections are equivalent
because every priced company has exactly one uniquely owned model, they merge into
one chart with combined company/model legend names; coincidentally equal totals do
not trigger a merge. Friendly model names replace raw slugs and server file paths,
and model cards/legends sort by the selected period's spending, highest first.
Company, person and model-price card collections use responsive CSS-column masonry.
Usage uses the standard Lucide Wallet icon in both navigation and its page heading.
Informational company/person cards and account rows are omitted when they have no
usable money, allowance, comparison, or availability percentage. Names and activity
counters alone do not keep a card visible. Hidden idle Person keys still count in
the allowance denominator; hiding a card never changes classifications or allocation. Current periods
compare with the same elapsed duration in the previous period (calendar UTC
months; each account's reset-aligned subscription week); completed periods compare
full periods. Short prior months are clamped. Comparisons describe observed records,
not complete billing history: paused/missing history is never backfilled, missing
prices suppress value-change percentages, and a zero prior baseline is not an
infinite percentage.

Known monthly subscription fees and their selected-period prorations remain
visible without request history or quota attribution. Current availability reads
sanitized numeric quota windows from the existing quota cache even when tracking
is paused, without polling providers or persisting a new history observation.
Independent scopes/durations remain separate; only the newest observation for a
pool is shown. Observations older than 30 minutes, with a passed reset, or with
invalid timestamps stay unknown except for the explicit Grok presentation policy below.
Unreported prices remain unknown, with explicit `monthly_fee_usd` metadata taking
precedence over known published plan fees. Monetary UI values are reais only;
missing conversion quotes never fall back to dollar amounts. Unknown/zero waste
and available-fee tags are omitted. Waste and availability slices show only their
own tag, and the tags use larger type. Each subscription shows email, plan and
monthly price on one line; quota windows have slim 3px tracks and percentages.
Chart and progress-bar tooltips omit the
client token/key name. Repeated status labels, empty-state prose, observation rows
and the period-comparison paragraph are removed; freshness and reset metadata
remain accessible without those visible rows.

Grok weekly availability uses one shared display policy on Usage and Quota
Management: keep actual percentages (including 0% remaining when exhausted); if no
usable weekly reading exists, show 100% remaining for a seven-day account window.
Same-plan peers provide the window structure only, never their usage, balances or
account-specific reset date. Recent own cached/history observations take precedence
over the assumption. Quota Management asynchronously reads the same existing
read-only account metadata endpoint as Usage to match credential fingerprints and
plans; the credential list and primary refresh are not blocked by that extra read.
Errors remain errors and visible even alongside the assumed percentage, and disabled
subscriptions receive no full-quota fallback. The assumed flag is presentation-only:
it is never persisted as a quota observation or used for routing, recovery sorting,
or per-person waste accounting.

Waste is attributed only to active Person keys, including people without requests.
For each company, subtract System consumption from plan-weighted capacity, then
split the remaining allowance equally among Person keys. Four identical Max plans,
a System consuming 0.2 of one plan and three Person keys give `(4 - 0.2) / 3` plans
per person. Each person's deficit is their allowance minus attributed consumption,
clamped at zero. Actual expired unused fees and currently available fees are split
in proportion to positive deficits, so overuse cannot inflate aggregate waste or
create negative waste. System keys never receive a person's waste allocation.
The person cards show the resulting money tags, including company-level waste on
pie legends. Missing reliable quota/fee data remains unset; it is never invented.

Account progress uses the authenticated account email,
resolved at read time from auth/profile metadata and never persisted in usage files.
Available and already-wasted capacity are shown separately on each account's bar.
A company pie is omitted entirely if any included account/window has incomplete
quota observations or unidentified consumption; no unattributed/unknown slice is
rendered or silently called available. The percentage matrix and old unused-capacity
card stay removed. Only chart titles, figures and controls remain.

API-dollar values are converted to BRL using the live **USDT/BRL buy** ticker from
`wss://cross-otc.com/api/v1/websocket`, exactly the public quote used by cross-otc.com.
This is an approved stablecoin reference, not a claim of a direct USD/BRL fixing.
Only anonymous ticker subscribe/ping messages are sent; no keys or usage go to Cross.
Quotes older than 45 seconds become unavailable, not silently reused. Canonical
model rates remain in USD; BRL edits pin the quote when editing starts and convert
back to USD on save so a moving quote cannot change an in-progress edit.

Capacity is explicitly an **estimate**, not a physical shared-token balance. Per-plan
quotas are normalized separately and averaged equally, so short-window reset counts
do not overwhelm weekly limits. Plans within each company are pooled by published
capacity multipliers where known (Claude Pro/Max 5x/Max 20x), with flagged
price-proportional proxies for ambiguous OpenAI Pro tiers. Unknown tiers use an
explicitly estimated unit weight, not a claimed published ratio. Current month
includes all observed reset cycles, with crossing cycles prorated by time overlap;
request spending uses actual UTC request dates without proration. Pies distinguish
people, currently available and already-wasted capacity. Unknown history is not
invented: incomplete pies are hidden.

Small money tags use the **unused share of the subscription fee**, not a hypothetical
API-value extrapolation. Public known monthly web fees or an explicit authenticated
`monthly_fee_usd` account attribute are prorated by actual UTC calendar-month lengths
to the selected period. Expired unused quota is red `(... wasted)`; unexpired quota
in the current period is separately shown as `(... still for grabs)`. Each person's
share follows their equal-allowance shortfall after System usage, as described above. Unknown
plan fees/quotas stay unset; known subtotals remain estimates without bound prefixes,
and unknown per-person allocations are never guessed. Unit-price configuration
fields and the exchange-rate quote are not usage spending and do not get waste tags.

Cost uses the canonical non-overlapping v2 token buckets (uncached input, cache reads,
cache writes, and output including reasoning). A bundled native-vendor models.dev
snapshot is available offline, refreshed every 24 hours while running; its date is
visible. The public catalog can also be refreshed manually. Each minute/model/rate
bucket records the rate and estimated cost at execution time, so later price changes
never overwrite recorded history. Custom model rates override the catalog. Unknown/ambiguous
models, incomplete accounting, missing cache rates (except the in-house read policy below)
and unsupported service tiers stay unpriced. OpenAI speed modes are stored per row
(`mode`: `fast` for `priority`/`fast`, `ultrafast`) and priced at OpenAI's published
2x / 6x of the Standard rate, cache and long-context rates included. The requested
tier is trusted over the response because OpenAI reports `default` while ultrafast runs.
The panel shows them as separate model slices, e.g. "GPT 6.1 Sol (Ultrafast)". This estimates standard token API value, not subscription billing, guaranteed
savings, image/audio/tool fees or an API-dollar-to-subscription conversion.

Successful quota cache reads/replays preserve each supported quota's own scope and
duration: Claude five-hour, weekly and model-family windows; Codex reported windows;
Meta session/weekly; Devin daily/weekly; MiniMax model intervals/weekly; Z.ai token
windows; and existing xAI/Kimi weekly contracts. Independent observations remain
separate on disk even though the UI averages them into a company-level index. Other
provider/plugin shapes remain explicitly unavailable rather than being guessed. Quota recipes must first be
registered by opening Quota and refreshing the account; subsequently safe GET recipes
are replayed around the clock every 10 minutes when usage collection is enabled.

Between observations, measured quota increases are allocated by each key's share of
priced API value in that interval. Usage is bucketed to UTC minutes, so attribution is
an estimate, especially at observation/reset boundaries. Pre-tracking consumption,
unpriced intervals, gaps over 30 minutes, and consumption with no gateway activity
stay unattributed. Counter decreases invalidate that week's allocation rather than
producing impossible percentages. Activity outside the gateway can still bias the
shares and is disclosed. Missing observations and unsupported quotas are not zero.

Closed cycles have an unused upper-bound estimate only when their final observation
was within 15 minutes of reset; otherwise their leftover capacity stays unattributed.
Open cycles contribute remaining capacity, not expired waste. Monthly budget slices
are time-prorated estimates and must not be treated as an exact monthly token grant.

Model-price cards cover the configured registry inventory. The researched snapshot
`internal/usagehistory/model_reference.json` contains all 68 model IDs advertised by
the live gateway on 2026-10-17: 34 published native API tariffs, four explicitly labeled
family references, 16 OpenRouter reference entries (eight in-house models plus their
`spark/` aliases), and 14 remaining unpriced entries. The OpenRouter snapshot comes
from `https://openrouter.ai/api/v1/models`, using non-free, non-batch catalog entries
and their published input/output/cache-read rates, in canonical USD per million
before the normal Cross conversion to reais. The serving configuration was checked:
`qwen-coder` is Qwen3.8-27B, not Coder Next; `qwen-vl` is Qwen3-VL-30B-A3B-Instruct;
`nemotron-120b` is Nemotron-3-Super-120B-A12B. Missing exact OpenRouter models stay
unset, never replaced with a neighboring model's price. For in-house OpenRouter
comparisons, a missing cached-input read rate uses the regular input rate, not an
invented free cache or a guessed discount; explicit published read discounts remain
unchanged. Native providers, manual overrides and unknown cache-write rates keep
their existing rules. These are counterfactual hosted prices, not electricity costs
or a local subscription bill.

Future executions snapshot that effective rate. Previously unpriced in-house rows
also receive a separate read-time comparison when the recorded input/output/cache
counters form a consistent OpenAI-compatible breakdown. Known GGUF response filenames
resolve to their verified model, and `spark/` aliases normalize to one public model
ID. The API adds `model_alias`, `reference_cost_usd` and `reference_price` while
preserving the original `model`, `cost_usd`, `price` and `unpriced_requests` values.
The panel uses the reference amount only when the recorded amount is absent, so the
money-only charts no longer hide those calls. The projection does not write to disk,
reprice non-null historical costs, backfill requests, or invent a different model's
rate. A retained OpenRouter rate is used before the current bundled reference.
The local panel can preview those sourced model-price cards against an older v8
backend without changing its recorded usage or saved overrides.
Each card links its public evidence. Image, video and voice rates retain their native
units; they are never passed to token accounting without the corresponding metered
quantities. Unknown prices do not mean free. Card edits preserve cache/context tiers;
an explicit reset restores researched defaults. Sources include the Anthropic,
OpenAI, xAI, Meta, MiniMax and Z.ai pricing/model documentation and models.dev. In
particular, Muse Wonder Full and MiniMax M3.1 Flash Preview have no published matching
pay-as-you-go API rate; Z.ai's promotional free cache storage is not a per-token write
price. Local aliases are not assigned invented hosted tariffs.

All endpoints use existing v8 management authentication:
- `GET /v8/management/observability/usage/clients?from=YYYY-MM-DD&to=YYYY-MM-DD`
  returns minute/model rows for inclusive UTC dates (default last 30 days), summarized
  reset-cycle reports under the legacy `weeks` field, account/key labels, accounts,
  configured model-price references, settings, collection status and catalog date.
  `key_names` carries shared names (including clear tombstones), and `key_kinds`
  carries Person/System classifications. Models may include `display_name`. Accounts
  expose the current `disabled` credential flag. Each account may
  include `cached_quotas` with numeric used percentage, scope, duration, reset and
  observation timestamps; cache recipes, bodies and credentials are never returned.
  Cycle reports retain their actual `period_seconds`, not an assumed seven days.
- `GET /v8/management/access/key-names` returns `{version: 2,
  names: {fingerprint: name}, kinds: {fingerprint: "person" | "system"}}`.
- `PATCH /v8/management/access/key-names` accepts optional `names` and `kinds` maps
  (at least one is required), plus `import_only?: boolean`. Ordinary patches update
  only supplied names/types and preserve the other metadata;
  import-only patches never overwrite an existing server entry. Both routes use
  the same management authentication and remain usable with collection disabled.
- `PUT /v8/management/observability/usage/settings` preserves legacy `names`/`plans`
  compatibility and replaces `prices` and `unit_prices` maps. Token overrides use USD
  per million tokens, including optional context thresholds and long-context rates;
  native image/second/minute/character reference overrides retain their units. Legacy
  names are no longer a UI naming source; the key editor's existing names are reused.
- `POST /v8/management/observability/usage/prices/refresh` fetches public prices only;
  no usage or credentials are sent to models.dev.

Data lives under `<auth-dir>/.cpamc-custom/usage-history/`: dated JSON usage buckets,
one snapshot per account/reset pool under `cycles/`, `settings.json`, and a refreshed
`prices.json`, plus independent `key-names.json` metadata. Each quota update rewrites only its own week, not the full year.
Retention is 365 days. Usage rows are per-minute for the last 7 days and are folded into
hourly rows (same totals) at startup and once a day after that. Only the last 31 days stay
in memory; older days are read from disk for each request and not cached. Requests update
memory only; a background writer persists changed days every 10 seconds and on shutdown,
so a hard kill (OOM, SIGKILL) can lose up to 10 seconds of counts. Usage
files are atomically replaced with owner-only permissions and no raw API keys or
upstream credentials. Keep the auth directory on persistent storage. This local store
supports **one gateway process per data directory**, not shared replica aggregation.
Use a `Recreate` deployment strategy so releases never overlap history writers.
Restart after changing `auth-dir`. Corrupt retained data fails initialization instead
of being overwritten. Disk failures are reported and pending counters retry on the next
flush. Collection follows `observability.usage.usage-statistics-enabled` and defaults
to **enabled** when that setting is omitted. The YAML loader, in-memory parser,
configuration template and visual editor share this default. An explicit `false`
remains an opt-out and survives saving/migration; changing the default does not
rewrite an existing live configuration. Disabling collection preserves history. There is no historical backfill or retrospective repricing of old
daily-only rows. The existing asynchronous usage dispatcher can briefly lag inference;
a hard kill can lose events not yet dispatched.


For local UI iteration, `cd panel && bun run dev:usage` starts a loopback-only Vite
preview at `http://127.0.0.1:5190/#/usage` with HMR and a fake API. The eight-account
provider/plan lineup matches the server quota view captured on 2026-10-17, but all
identities, people and usage are synthetic. Model prices are researched public
references, and BRL conversion reads the real public Cross ticker. No gateway or
provider credentials or inference calls are needed. The preview entry and API are serve-only and absent from normal
production builds. See `panel/dev/usage/README.md`.

### Native Jev decisions gateway

`POST /v1/decisions` accepts the native Jev `{model, state, questions}` contract (1–48 questions, up to 1 MiB, no streaming). It uses the normal **client access key**, never a management key. Native payloads do not pass through chat translation: the usual auth manager still owns model routing, credential selection/rotation, proxy transport, payload overrides/filters and usage accounting.

Two upstreams are supported, chosen by the provider base URL:

- **TypeSafe direct**: base URL `https://api.typesafe.ai/v1`; the executor posts to `/v1/systemone`. TypeSafe model ids are `jev-latest`, `jev-1.14.0`, `jev-1.13.0`. Live config: openai-compatibility entry `typesafe`, prefix `typesafe`, model `jev-1.14.0` with alias `jev-1.14`, so clients call `typesafe/jev-1.14`.
- **OpenRouter**: base URL `https://openrouter.ai/api/v1`; the executor posts to the sibling `/api/alpha/decisions`. Upstream ids are `typesafe/jev-*`. Live config: entry `openrouter`, prefix `openrouter`, models `typesafe/jev-1.14` / `typesafe/jev-1.13` aliased to `jev-1.14` / `jev-1.13`, so clients call `openrouter/jev-1.14`.

The route accepts any model name containing `jev-`. The auth manager picks the provider, but the model it hands the executor is still the client alias, so the executor rewrites it per host: a TypeSafe host gets `typesafe/jev-1.14` turned into `jev-1.14.0` (a trailing `.0` or `-latest` is left as-is), and an OpenRouter host keeps the `typesafe/jev-*` slug. Clients use their same gateway URL/key for chat, images and Jev; no OpenRouter secret leaves cpamc. Missing provider/key fails explicitly, malformed native responses return 502, and upstream error bodies are never exposed. Native response usage (`input_tokens`, `output_tokens`) is counted normally. This endpoint introduces no new credential store or management settings.

Implementation: `internal/api/custom_decisions.go`, `internal/runtime/executor/openai_compat_decisions.go`. Tests cover client authentication, request limits, end-to-end auth-manager execution, native contract preservation, server-side credential substitution and upstream failures. No production rollout is implicit in adding the route.

### 1. Backend quota cache (all providers)

- The panel no longer calls providers through `/requests/api-call` for quota.
  Every quota adapter is wrapped (`panel/src/features/quota/providers/customQuotaCache.ts`),
  so its requests go to `POST /v8/management/custom/quota/api-call`. This is generic:
  any provider upstream adds is covered automatically.
- The backend stores each distinct request (credential + method + URL + body) and its
  last response. Cache file: `<auth-dir>/.cpamc-custom/quota-cache.json` (mode 0600).
  Tokens are never written; `$TOKEN$` stays a placeholder and is resolved per call.
- File, git, and object credential stores skip `.cpamc-custom/` directories during
  auth loading, so quota state never appears as a credential in Auth Files.
- Page load reads from the cache. "Refresh all credentials" and per-card refresh send
  `force: true`, which does a live call and updates the cache.
- Never cached or replayed: URLs matching `consume|claim|redeem|/chat/completions`
  (they change state or spend tokens). Codex "reset quota" is never wrapped.
- If the backend has no custom endpoint (404), the panel falls back to the direct call.

### 2. Scheduled refresh

- Every 10 minutes between 09:30 and 21:30 UTC-3, the backend replays cached
  requests with no panel open. With weekly usage collection enabled, safe GET quota
  recipes also replay overnight so reset-week history does not depend on the UI.
- Constants are at the top of `internal/api/handlers/management/custom_quota_cache.go`.
- Status endpoint: `GET /v8/management/custom/quota/status` (no bodies, no tokens).

### 3. Earliest-reset-first routing (all providers)

- After each cache update, the backend reads reset times from the cached bodies
  (`reset_at`, `resets_at`, `resetTime`, `*ResetAtUnix`, `reset_after_seconds`,
  `currentPeriod.end`, `billingPeriodEnd`, ...).
- A credential's reset time is its latest reset within 8 days (the binding window).
- For each provider with 2+ credentials and at least one known reset, the one that
  resets soonest gets the highest priority boost. Credentials with no known reset rank
  after them with no boost.
- **Used-up credentials go last.** A credential whose cached quota says a credential-wide
  window is used up (xAI `creditUsagePercent`, Codex `rate_limit.limit_reached`,
  `used_percent` of a primary/secondary/weekly window, Claude `five_hour`/`seven_day`
  `utilization`, all at 100%) gets a negative boost. Traffic goes to a credential with
  quota left without first hitting the used-up one (no 402/429, no cooldown). Per-model
  and per-product buckets are ignored. A credential that is used up is still served when
  nothing else is ready, and a provider with one credential is never ranked.
- A used-up body whose resets all lie in the past no longer counts. The ranking is
  re-checked every minute, also outside the 09:30–21:30 refresh window.
- The boost is added in `authPriority()` (`sdk/cliproxy/auth/selector.go`), so the
  scheduler sends traffic to the top-ranked credential. When it hits a cooldown, normal
  failover moves to the next one.
- Each change is logged as
  `custom reset-priority: xai: b(10/15 20:55) > a(10/15 13:48, used up)`.
  `GET /v8/management/custom/quota/status` shows `exhausted_by_auth` and `priority_boosts`.

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
- MiniMax and Z.ai keep independent quota caches, so list refreshes cannot clear
  another provider's cards. Keys with the same provider label are distinguished
  by their public `auth_index`, never by exposing the secret key.
- Z.ai selects the international or BigModel quota host from the configured base
  URL. Both providers participate in reset sorting and the quota timeline.
- Reset fields (`end_time`, `weekly_end_time`, `nextResetTime`) feed the earliest-reset routing.

### 7. Text-to-speech endpoint (MiniMax)

- `POST /v1/audio/speech` (client API key) takes OpenAI-style `{input, voice, speed}`
  and returns mp3 from MiniMax T2A (`speech-2.8-hd`, max 10,000 chars). The MiniMax
  key is read from the openai-compatibility entry and never leaves the server.
  Requests with a Grok TTS model (`grok-tts`, `grok-voice-tts-1.0`) dispatch to
  the upstream xAI speech handler instead (`customSpeechDispatch`).
- Used by the pi `speak` extension (`~/.pi/agent/extensions/speak`).

### 8. Quota cards removed from the Auth Files page

- Upstream renders an `AuthFileQuotaSection` inside every `AuthFileCard`. We unmount it:
  quota usage/reset info lives on the Quota page only, where it is served from the
  backend cache. The Auth Files page makes zero quota calls on open.
- The component file and `resolveAuthFileQuotaType` stay on disk untouched
  (upstream subtree files) to avoid delete/modify conflicts on merges; they are
  simply not imported by `AuthFileCard` anymore.

### 9. Transient failure recovery configuration

- The custom config template uses `routing.retry.request-retry: 2` and
  `routing.cooldown.transient-error-cooldown-seconds: 5` to recover from temporary
  connection/server errors without a 60-second credential blackout.
- Existing installations must apply these values to their persisted config; an
  image upgrade does not overwrite that file. Cooldown waiting remains enabled
  through `routing.retry.max-retry-interval: 30`.
- Quota/rate-limit cooldowns, credential overrides, and WebSocket selection are
  unchanged. These settings apply globally, not only to Codex.

### 10. Codex-parity transient retry (edge outages)

- Root cause of "pi gets 503 while Codex CLI keeps working" on the same account: when the
  ChatGPT edge returns Envoy's `upstream connect error ... before headers` 503, the credential
  is deliberately not cooled, so all `request-retry` rounds fired within ~100 ms and the client
  got the 503. Native Codex backs off (200 ms doubling, 5 retries, then WS→HTTPS fallback).
- `sdk/cliproxy/auth/custom_transient_backoff.go`: transient 408/500/502/503/504 and transport
  failures wait 1s, 2s, 4s, 8s, 8s between rounds (capped by `max-retry-interval`). Hooked in
  `shouldRetryAfterErrorWithAttempted` (`conductor_selection.go`). Quota/auth errors unchanged.
- `isProxyConnectionFailure` (`conductor_cooldown.go`) also accepts Envoy's
  `... retried and the latest reset reason: ...` body (seen live), so it no longer cools the
  credential and blacks out every other client for the cooldown window.
- Config: `routing.retry.request-retry: 5`, `upstream.codex.stream-bootstrap-buffering: true`,
  `stream-bootstrap-timeout: "20s"` (in-stream `server_is_overloaded` before the first token
  is retried instead of delivered). Persisted configs must be updated by hand.
- Evidence, guarding tests and the end-to-end reproduction: `docs/custom-fork-maintenance.md`.

### 12. zstd request bodies for Codex (1 MiB edge limit)

- The ChatGPT edge returns Envoy 503 for uncompressed request bodies above 1 MiB, so long
  Codex sessions through cpamc reconnected forever. Native Codex sends zstd bodies.
- `helps/custom_codex_request_compression.go` compresses `/responses` HTTP bodies ≥ 128 KiB
  for ChatGPT OAuth credentials.
- Requests ≥ 1.5 MiB (mostly incompressible encrypted reasoning) go over the Responses
  WebSocket instead, which has no body limit: `customCodexLargeRequestUsesWebsocket` in
  `codex_websockets_executor.go`. Details and tests: `docs/custom-fork-maintenance.md`.

### 13. Early model registry entries (upstream not yet released)

- `internal/registry/custom_claude_builtins.go`: `claude-haiku-5-5` upserted ahead of
  upstream (Anthropic API id `claude-haiku-5-5`, 1M context, 5.5-family thinking
  levels). A hardcoded builtin, not `models.json`: the registry refreshes from the
  remote catalog at runtime, which would wipe an embedded-only entry.
- Same file: `claude-mythos-5-1`, the verification-gated id of Claude Fable 5.1
  (1M context, 128K output, adaptive thinking). Upstream will not list it
  (CLIProxyAPI #6428), so this builtin stays even after catalog refreshes. The
  Claude executor already treats the id like Fable 5.1; only a credential whose
  account has Mythos access can actually serve it.
- Drop the haiku builtin once upstream's catalog includes the same id; until then
  keep both (and the `WithClaudeBuiltins` hook in `GetClaudeModels`) on every merge.

### 14. Image generation endpoint (MiniMax image-01)

- `POST /v1/images/generations` with `model: "minimax/image-01"` (or `minimax/image-01-live`)
  is translated to MiniMax's native `/v1/image_generation` and returned in OpenAI shape
  (`data[].b64_json`, or `data[].url` with `response_format: "url"`). `size` `WxH` maps to
  `width`/`height` (clamped 512–2048, multiple of 8); prompt max 1500 chars; `n` max 9.
- Every other model falls through to the upstream images handler (`customImagesDispatch`).
  The `minimax/` prefix is required so a future upstream `image-01` is never hijacked.
- Key: same MiniMax openai-compatibility entry as TTS (`customMinimaxKey`); never leaves the server.
- Used by the `image-gen` skill (`--model minimax`).

### 15. Video generation endpoint (MiniMax Hailuo V1)

- `POST /v1/videos` and `POST /openai/v1/videos` with `model: "minimax/<hailuo-or-t2v-or-i2v>"`
  create an async MiniMax task via native `POST /v1/video_generation` and return OpenAI video
  shape (`object: video`, `id` = MiniMax `task_id`, `status: queued`).
- `GET /v1/videos/:id` / `GET /openai/v1/videos/:id` poll MiniMax
  `GET /v1/query/video_generation` and map statuses to `queued|in_progress|completed|failed`.
- `GET /v1/videos/:id/content` / `GET /openai/v1/videos/:id/content` download mp4 via
  `retrieve_content` (fallback: `files/retrieve` → `download_url`).
- T2V models: `MiniMax-Hailuo-2.3`, `MiniMax-Hailuo-02`, `T2V-01-Director`, `T2V-01`.
  I2V models: those plus `MiniMax-Hailuo-2.3-Fast`, `I2V-01-Director`, `I2V-01-live`, `I2V-01`
  (first frame from `first_frame_image` / `input_reference.image_url` / `image_url`).
- `seconds` must be `6` or `10`; `size`/`resolution` map to MiniMax `512P|720P|768P|1080P`.
  Prompt max 2000 chars. The `minimax/` prefix is required.
- Key: same MiniMax openai-compatibility entry as TTS/images (`customMinimaxKey`).
- Used by the `video-gen` skill (`--model hailuo`).

### 11. No GitHub Actions

- Upstream and the panel subtree ship `.github/workflows/` and `panel/.github/workflows/`.
  We delete them after every merge; builds run in TeamCity, deploys in Kubernetes.
  Repository Actions stay disabled on `kevynsax/cpamc-custom`.

### GPT Live over an OpenAI API key (`POST /v1/live`, model `gpt-live-1.1`)

The Codex Live route (chatgpt.com, OAuth) answers the SDP and then refuses the voice session
(`Voice session access denied`) when the ChatGPT plan has no Voice entitlement. A `/v1/live` request whose model is
`gpt-live-1.1` (not `gpt-live-1-codex`) is sent instead to the first Codex **API-key** credential:
`POST <base-url>/live/sessions` (default `https://api.openai.com/v1`) with
`{session, transport:{type:"webrtc", sdp}}`. The answer is returned as `application/sdp` with
`Location: /v1/live/<session id>`. The media relay and the sideband are not used (the peer talks to OpenAI directly).
Live config (PVC, not in the image): an `api-keys.codex` group with `prefix: openai-live`, `base-url`
`https://api.openai.com/v1`, `models: [gpt-live-1.1]` and the platform key, so the key is not picked for ordinary
chat traffic. Code: `internal/client/codex/live/custom_apikey_live.go` (+ test); hook in `live.go` `Handle`.
While the platform API still answers `gpt-live-1.1` with `400 invalid_model` ("not supported in realtime mode";
1.1 shipped to ChatGPT first, 2026-10-15), the same request is retried once with `gpt-live-1`. When the API accepts
1.1 the first attempt succeeds and the fallback is never taken.

## Files

| File | Kind |
|---|---|
| `CUSTOM_FEATURES.md`, `Dockerfile.custom`, `docs/custom-fork-maintenance.md` | new |
| `.github/workflows/`, `panel/.github/workflows/` | deleted (re-delete after every merge) |
| `internal/config/config_v8_test.go` | expects `request-retry: 5` |
| `config.example.yaml` | custom transient cooldown + retry configuration |
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
| `sdk/cliproxy/auth/custom_transient_backoff.go` (+ `_test.go`) | new (transient retry backoff) |
| `sdk/cliproxy/auth/conductor_selection.go` | +1 line (backoff hook) |
| `sdk/cliproxy/auth/conductor_cooldown.go` | Envoy "retried" body is transport-only |
| `internal/runtime/executor/helps/custom_codex_request_compression.go`, `custom_codex_request_compression_test.go` | new (zstd request bodies) |
| `internal/runtime/executor/codex_executor_stream.go`, `codex_executor_execute.go` | +4 lines each (compression hook) |
| `internal/runtime/executor/codex_websockets_executor.go` | large requests → WebSocket (`customCodexLargeRequestUsesWebsocket`) |
| `.dockerignore` | +2 lines |
| `internal/api/custom_speech.go` | new (TTS endpoint) |
| `internal/api/custom_images.go` (+ `_test.go`) | new (MiniMax image endpoint) |
| `internal/api/custom_videos.go` (+ `_test.go`) | new (MiniMax Hailuo V1 video endpoint) |
| `internal/api/server_routes.go` | +1 route line (speech), +1 route line + comment (images), video dispatch wrappers + `/v1/videos/:id/content` |
| `internal/api/handlers/management/custom_quota_providers.go` | new (MiniMax/Z.ai credential list) |
| `panel/src/features/quota/providers/customApiKeyQuota.ts` (+ `tests/customApiKeyQuota.test.ts`) | new |
| `panel/src/features/quota/{constants,logic}.ts`, `providers/{index,types}.ts`, `QuotaPage.tsx` | minimax/zai entries |
| `panel/src/features/authFiles/constants.ts`, `i18n/locales/*.json` | icons + tab labels |
| `panel/tests/quotaPageLogic.test.ts` | +2 expected zero counts |
| `internal/client/codex/live/custom_apikey_live.go` (+ `_test.go`) | new (GPT Live via API key) |
| `internal/client/codex/live/live.go` | +4 lines (`isAPIKeyLiveModel` branch in `Handle`) |
| `internal/translator/claude/openai/chat-completions/custom_fast_mode.go` (+ `_test.go`) | new |
| `internal/translator/claude/openai/chat-completions/claude_openai_request.go` | +4 lines (fast mode) |
| `panel/src/services/api/apiCall.ts` | `postApiCall` routes quota-scoped calls |
| `panel/src/features/quota/providers/index.ts` | wraps `QUOTA_ADAPTERS` |
| `panel/src/features/quota/QuotaPage.tsx` | force on refresh-all + load cached on open |
| `panel/src/features/quota/hooks/useQuotaActions.ts` | force on card refresh |
| `panel/src/features/authFiles/components/AuthFileCard.tsx` | quota section unmounted |
| `panel/src/features/authFiles/AuthFilesPage.tsx` | quota filter wiring + `gridQuota` class removed |
| `panel/src/features/authFiles/components/AuthFileQuotaSection.tsx` | force on card refresh |

## Merging a new upstream version

Work directly in the main checkout on `custom`; no task branches or worktrees.
GitHub Actions must remain disabled. Releases are built and deployed with TeamCity.

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
