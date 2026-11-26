# Image model routes on cpamc

Use `scripts/generate-image.mjs` with Node.js. These requests are independent of chat reasoning:
never add `complexity`, `reasoning`, `reasoning_effort`, or `thinking`. Rendering `quality` is supported
only for GPT Image and is omitted by default. Use `--dry-run` to inspect the sanitized payload without
spending on a generation. Send reference image files to an edit endpoint rather than reading image pixels
into a chat model that does not support image input.

Gateway: `https://ai-models.kevyn.com.br/v1` (from `~/.pi/agent/models.json` → `providers.cpamc`).

## Working backends

### `minimax/MiniMax-H3-Image` (default; aliases `h3`, `minimax`)

- Endpoint: `POST /v1/images/generations`; cpamc-custom forwards it to MiniMax's OpenAI-compatible
  `/v1/images/generations` with the bare model id (CUSTOM_FEATURES.md §14). The `minimax/` prefix is required.
- Body: `{ "model", "prompt", "n"?, "size"?, "aspect_ratio"?, "area"?, "output_format"?, "disable_prompt_enhanced"? }`
  - `size` `WxH`: 655,360–20,000,000 px, ratio 1:3–3:1, never rounded. Or `size: "auto"` with `aspect_ratio` (`16:9`) and
    `area` (`1K`/`2K`/`4K`, default 4K). Do not combine `WxH` with `aspect_ratio`/`area`.
  - Prompt max 32,000 chars; `n` max 10. Prompt enhancement is on by default (`data[].revised_prompt`).
- Response: `{ "data": [ { "b64_json", "size", "revised_prompt" } ] }` (JPEG by default). Failed images carry `error` instead.
- Up to 600 s. Not idempotent: MiniMax charges for finished images even if the client gave up. Never retry automatically.
- Reference edits: `POST /v1/images/edits` multipart (OpenAI `images.edit`), 1–16 `image` files, no `mask`. Call edits with `curl` like GPT Image edits.
- `MiniMax-H3-Image-Highspeed` is not available on the current MiniMax Go plan (error 2068); cpamc does not route it.

### `gpt-image-2.6` (alias `gpt`)

- Endpoint: `POST /v1/images/generations`
- Body: `{ "model", "prompt", "n": 1, "size", "quality"? }`
- Response: `{ "data": [ { "b64_json": "..." } ] }`
- Best for: readable text-in-image, product shots, general quality
- Also listed: `gpt-image-2.6-flare`, `gpt-image-2.6-sunburst`

### `grok-imagine-image-2.0` (alias `grok`)

- Same `/v1/images/generations` path as GPT Image
- Related ids: `grok-imagine-image`, `grok-imagine-image-quality`
- Often returns JPEG bytes inside `b64_json`
- Best for: stylized / alternate aesthetic

## Muse Image (`muse-image-1.0`)

Muse is available through the Meta OpenAI-compatible upstream, but cpamc must
be configured to classify it as an OpenAI-compatible image model. Add
`image: true` to the `muse-image-1.0` model entry under the `meta`
`openai-compatibility` provider:

```yaml
- name: "muse-image-1.0"
  image: true
```

With that configuration, use:

- Endpoint: `POST /v1/images/generations`
- Body: `{ "model": "meta/muse-image-1.0", "prompt": "...", "n": 1, "size": "1024x1024" }`
- Response: `{ "data": [ { "b64_json": "..." } ] }`

The direct Meta endpoint was verified to return a WebP image for this route.
The cpamc `/v1/responses` route currently rejects Muse before forwarding it,
despite Meta's upstream Responses endpoint working, so the image-generation
route is the reliable integration for the skill. The helper's `muse` alias
uses `/v1/images/generations`.

## MiniMax image-01 (`minimax/image-01`, alias `minimax-image-01`)

MiniMax has no OpenAI-compatible images route, so cpamc-custom adds one
(`internal/api/custom_images.go`, CUSTOM_FEATURES.md):

- Endpoint: `POST /v1/images/generations` with `model: "minimax/image-01"` (or `minimax/image-01-live`).
  The `minimax/` prefix is required; bare `image-01` is not intercepted.
- Body: `{ "model", "prompt", "n"?, "size"?, "response_format"?, "aspect_ratio"?, "seed"?, "prompt_optimizer"? }`
  - `size` `WxH` → MiniMax `width`/`height`, clamped to 512–2048, multiple of 8; `aspect_ratio` wins if set.
  - Prompt max 1500 chars (400 otherwise); `n` max 9.
- Response: `{ "data": [ { "b64_json": "..." } ] }` (JPEG). `response_format: "url"` returns MiniMax's 24h URL.
- Key: the `minimax` entry in cpamc `api-keys.openai-compatibility` (same as TTS); never leaves the server.
- Errors: content filter / bad input → 400, rate limit → 429, balance → 402, others → 502.

## Agent rule of thumb

1. Default → `h3` (MiniMax H3-Image)
2. Readable text in the image or GPT Image look → `gpt`
3. Want Grok aesthetic → `grok`
4. Want Meta Muse → `muse` (requires the cpamc `image: true` config above)
5. Older MiniMax image-01 → `minimax-image-01`

## Auth

Helper reads `providers.cpamc.apiKey` + `baseUrl` from `~/.pi/agent/models.json`.
Overrides: `CPAMC_API_KEY`, `CPAMC_BASE_URL`.
