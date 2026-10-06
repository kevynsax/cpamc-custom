# cpamc-custom: fork maintenance

This file belongs to the repository. It describes what the fork changes, why, and what every
upstream merge must keep. The step-by-step upgrade procedure (commands, TeamCity, Kubernetes,
verification) lives in the `cpamc-upgrade` skill, outside this repository. See
[What belongs where](#what-belongs-where).

Feature list: [`CUSTOM_FEATURES.md`](../CUSTOM_FEATURES.md). Every custom line is marked
`CUSTOM (cpamc-custom)`; list them with `git grep -n "CUSTOM (cpamc-custom)"`.

## Branches and remotes

| Name | Meaning |
|---|---|
| `origin` | upstream `router-for-me/CLIProxyAPI`. Never push to it. |
| `fork` | `kevynsax/cpamc-custom`. Push `custom` and `*-custom.N` tags only here. |
| `panel-upstream` | `router-for-me/Cli-Proxy-API-Management-Center`, vendored as a subtree in `panel/`. |
| `main` | Tracks `origin/main`. Never commit to it. |
| `custom` | Our branch. Upstream is merged into it. |

## Rules for every upstream merge

1. **Delete GitHub Actions workflows.** Upstream and the panel subtree ship workflows under
   `.github/workflows/` and `panel/.github/workflows/`. We do not use GitHub Actions: builds run
   in TeamCity and deploys in Kubernetes. After each merge:

   ```bash
   git rm -r --ignore-unmatch .github/workflows panel/.github/workflows
   git commit -m "chore: remove upstream GitHub Actions workflows"
   ```

   - If upstream changed a workflow file we deleted, the merge reports a modify/delete conflict.
     Resolve it by deleting the file (`git rm <file>`).
   - Repository Actions must stay disabled on `kevynsax/cpamc-custom`. Check with
     `gh api repos/kevynsax/cpamc-custom/actions/permissions` (`"enabled": false`). Never
     re-enable Actions or dispatch a workflow.
   - Do not add Actions-based automation of any kind. Other `.github/` content (issue templates,
     `FUNDING.yml`, `scripts/`) is harmless and stays.
2. **Keep every `CUSTOM (cpamc-custom)` hook.** After a merge, `git grep` must still find the
   hooks listed in `CUSTOM_FEATURES.md`. If one is missing, reapply it from that file.
3. **Keep the transient-failure fixes** (next section). They decide whether pi survives
   ChatGPT edge outages. If upstream rewrites retry or cooldown code, port the behaviour, not
   the lines, and re-run the tests listed below.
4. **Run the tests:**

   ```bash
   go build ./... && go test ./sdk/cliproxy/auth/ ./internal/api/... ./internal/managementasset/ ./internal/config/...
   (cd panel && bun install --frozen-lockfile && bun run verify)
   ```

## Transient-failure fixes (must survive merges)

### Problem they solve

pi (through cpamc) returned 503 while Codex CLI on the same machine, network and ChatGPT
account kept working.

- **Not throttling.** Stress tests on one account and IP (HTTP SSE as cpamc sends it,
  WebSocket as Codex sends it, pi through cpamc; 8–24 parallel requests, plus a 90-minute soak)
  showed no rate-limiting difference between cpamc and Codex.
- **Retry behaviour was the difference.** The ChatGPT edge (Envoy) sometimes returns 503
  `upstream connect error or disconnect/reset before headers ...` for seconds at a time:

  | | Codex CLI 0.160.1 | cpamc before the fix |
  |---|---|---|
  | Edge 503 | 5 retries, waits 200 ms then doubling, then switches WebSocket → HTTPS | 3 attempts within ~100 ms, then the 503 goes to the client |
  | Envoy "retried and the latest reset reason" 503 | retried | cooled the only credential for 5 s, so every client got errors |
  | In-stream `server_is_overloaded` | not retried | passed straight to the client |

### What the fork changes

| Change | File | Behaviour |
|---|---|---|
| Backoff between retry rounds | `sdk/cliproxy/auth/custom_transient_backoff.go`, hook in `conductor_selection.go` (`shouldRetryAfterErrorWithAttempted`) | Transient 408/500/502/503/504 and transport errors wait 1 s, 2 s, 4 s, 8 s, 8 s between rounds (per-round cap 8 s, never above `max-retry-interval`). Quota (429) and auth (401/403) errors are unchanged. |
| Second Envoy body is transport-only | `sdk/cliproxy/auth/conductor_cooldown.go` (`isProxyConnectionFailure`) | `... retried and the latest reset reason: ...` no longer cools the credential. |
| Config defaults | `config.example.yaml` | `routing.retry.request-retry: 5`, `upstream.codex.stream-bootstrap-buffering: true`, `upstream.codex.stream-bootstrap-timeout: "20s"`. |

The live config on the cluster volume is not overwritten by image upgrades. It must have:

```yaml
routing:
  cooldown:
    transient-error-cooldown-seconds: 5
  retry:
    request-retry: 5
    max-retry-interval: 30
upstream:
  codex:
    stream-bootstrap-buffering: true
    stream-bootstrap-timeout: "20s"
```

### Tests that guard them

| Test | Guards |
|---|---|
| `TestCustomTransientRetryWait` | backoff schedule, caps, which errors back off |
| `TestShouldRetryAfterError_BacksOffEnvoyPreHeaderFailure` | edge 503 waits 1 s / 2 s / 4 s and stops at `request-retry` |
| `TestExecute_RecoversAcrossShortEdgeOutageWithBackoff` | a single credential recovers after 3 failures, with no cooldown |
| `TestProxyConnectionFailureCooldown/envoy_retried` | the "retried" Envoy body does not cool the credential |
| `TestV8ExampleLoadsAndRoundTrips` | template keeps `request-retry: 5` |

All of these fail on the code before the fix. If a merge makes them fail, the fix was lost.

### Reproducing the outage end to end

Unit tests are not enough after a large upstream rewrite. Reproduce the outage end to end:

1. Put mitmproxy in front of chatgpt.com with a fault addon. During a time window it answers
   `POST /backend-api/codex/responses` (and the Codex WebSocket upgrade) with the Envoy 503
   body, or with a 200 SSE stream ending in `response.failed` / `server_is_overloaded`.
2. Run the cpamc image in Docker:
   - `requests.proxy-url` points at the mitm proxy.
   - The mitm CA is installed in the container trust store (on macOS, Go ignores `SSL_CERT_FILE`).
   - The Codex auth is a copy **without `refresh_token`**, so the test instance can never rotate
     the production token.
3. During a 20–30 s outage window, start one `codex exec` (with `HTTPS_PROXY` and
   `CODEX_CA_CERTIFICATE`) and one `pi --print` pointed at the test cpamc.

Expected result: Codex and pi both answer. Before the fix, pi failed at ~15 s in every 20–30 s
window. Reference results from 2026-10-06: before the fix 0/6 at 20–30 s; after the fix 8/8;
with the config change alone 0/4.

## Request-body compression (must survive merges)

### Problem it solves

A long Codex CLI session through cpamc (`codex --profile cpamc`, ~230k tokens, 188 input
items) kept showing "Reconnecting..." forever. Every attempt got the Envoy 503
`upstream connect error ... before headers`, while small requests worked.

- **The ChatGPT edge rejects uncompressed HTTP request bodies above 1 MiB** with that 503.
  Replaying padded bodies straight to chatgpt.com: 1024 KB → 200, 1026 KB → 503.
- **Native Codex never hits the limit.** It sends Responses HTTP bodies with
  `Content-Encoding: zstd` (level 3; feature `enable_request_compression`, default on, ChatGPT
  auth only) and uses a WebSocket otherwise. The failing 1044 KB body compresses to 327 KB.
- **cpamc sent the body raw**, so once a session crossed 1 MiB, every request failed until the
  context shrank. The retry backoff can't help: the request itself is rejected.

### What the fork changes

| Change | File | Behaviour |
|---|---|---|
| zstd request bodies | `internal/runtime/executor/helps/custom_codex_request_compression.go`, hooks in `codex_executor_stream.go` and `codex_executor_execute.go` (`/responses` only) | Bodies ≥ 128 KiB on ChatGPT OAuth credentials are sent zstd-compressed (level 3) with `Content-Encoding: zstd`. Smaller bodies, API-key credentials and `/responses/compact` are unchanged. Compression is transport framing applied after payload rules and request logging. |

### Compression is not enough: very large requests go over WebSocket

The next day, a T3 Code session on jorginho reached 2,037 KB (400 items). Half of that was
encrypted reasoning, which does not compress: zstd level 3 gave 1,022–1,030 KB, still at the
limit, and even the best level gave 991 KB. The 503s came back.

The edge has no such limit for WebSocket frames: the same 2 MB request and a 6 MB one were
accepted over `wss://chatgpt.com/backend-api/codex/responses`, which native Codex uses by
default. So `CodexAutoExecutor` (`codex_websockets_executor.go`) now sends any HTTP-routed
request with a client payload ≥ 1.5 MiB (`helps.CodexHTTPWebsocketThreshold`) over an
ephemeral upstream WebSocket. This applies to ChatGPT OAuth only, never to compact or image
calls; smaller requests keep the zstd HTTP path. The WebSocket executor already falls back to
HTTP when the upgrade is refused (426).

Long turns on such sessions are slow because of the model, not cpamc: the 2 MB request at
`reasoning: high` took 348 s direct to chatgpt.com and 7–10 min through cpamc (each run
reasons ~8k tokens).

### Tests that guard it

| Test | Guards |
|---|---|
| `TestCustomCodexLargeRequestUsesWebsocket` | the ≥ 1.5 MiB WebSocket rule: OAuth only, not API key, compact or nil auth |
| `TestCodexAutoExecutorSendsLargeRequestOverWebsocket` | a large HTTP-routed request reaches the edge as a WebSocket `response.create` with no HTTP POST (fails without the routing) |
| `TestCodexExecutorCompressesLargeResponsesBody` | stream and non-stream: a >1 MiB body reaches a mock edge (enforcing the real 1 MiB limit) as zstd and decodes to the same payload |
| `TestCodexExecutorSmallAndAPIKeyBodiesStayUncompressed` | small bodies and API-key credentials are sent as before |
| `TestCompressCodexRequestBodyRoundTrips` | lossless, replayable `GetBody`, never compressed twice |

Evidence from 2026-10-06, using the exact 1044 KB request captured from the stuck session:
production `v8.0.16-custom.2` returned 503 after ~40 s of retries; the fixed build returned
`response.completed` in 7 s. A realistic 3 MB session body also completed (17 s).

## How Codex and cpamc reach ChatGPT (reference)

| | Codex CLI 0.160.1 | cpamc |
|---|---|---|
| Request body | HTTP: zstd-compressed (`Content-Encoding: zstd`); WebSocket: permessage-deflate | zstd for bodies ≥ 128 KiB (fork change); raw before |
| Transport | WebSocket `wss://chatgpt.com/backend-api/codex/responses`, `OpenAI-Beta: responses_websockets=2026-02-06`, `response.create` frames, `previous_response_id` between turns; falls back to HTTPS after 5 failed retries | HTTP POST SSE to `https://chatgpt.com/backend-api/codex/responses` (WebSocket only when the credential sets `websockets: true` and the client connects by WebSocket) |
| Identity headers | `originator: codex_exec`/`codex-tui`, `User-Agent: codex_exec/0.160.1 ...`, `version`, `session-id`, `thread-id`, `x-client-request-id`, `x-codex-window-id`, `x-codex-turn-metadata`, `x-codex-routing-hint`, `x-codex-beta-features` | `Originator: codex-tui`, fixed `User-Agent: codex-tui/0.154.0 ...` (cloaking), `Session-Id`, `X-Codex-Routing-Hint`, `Chatgpt-Account-Id` |
| Body extras | `client_metadata` (installation id, timing), `text.verbosity`, `generate:false` prewarm | adds the `image_generation` tool, `parallel_tool_calls: true` |
| Other calls | `/codex/models`, plugins, `analytics-events`, `wham/*` | none |
| Sub-agents | at most 6 threads, depth 1 (`agent_max_threads`, `agent_max_depth`) | n/a |

The ChatGPT backend accepted both at the same rate. The cloaking `User-Agent` version (0.154.0)
did not matter in tests. Codex CLI does not send `x-oai-attestation`; only the desktop app does.

## What belongs where

| Topic | Location | Owner |
|---|---|---|
| What the fork changes and why; merge rules (delete workflows, keep hooks, keep transient-failure fixes); tests that guard them | this file, `CUSTOM_FEATURES.md`, `AGENTS.md` | repository |
| Default config values | `config.example.yaml` | repository |
| Upgrade procedure: `check` → `merge` → `tag-push` → `wait-image` → `deploy` → `verify`, TeamCity token, image tags, k8s manifest, outage protocol | `~/.pi/agent/skills/cpamc-upgrade/` (`SKILL.md`, `scripts/cpamc_upgrade.py`) | upgrade skill (local, not in git) |
| Live config values on the cluster | PVC `cpamc-data` → `config.yaml` (namespace `ai-features`) | edited by hand; image upgrades do not change it |
| Build and deploy | TeamCity `CustomCpamc_Build` (`Dockerfile.custom`), `~/src/k8s/ai-features/cpamc.yaml` | TeamCity and the k8s repo, never GitHub Actions |

The skill runs the workflow-removal step automatically after `merge` and refuses `tag-push`
while any workflow file exists. This file explains why.
