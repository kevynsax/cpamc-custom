---
name: image-gen
description: Generate or edit actual image files, icons, mockups and diagrams through Kevyn's cpamc gateway. Use for image requests and image-generation errors, including unsupported complexity. Keep the chat model unchanged; use the Node.js helper with image-only parameters.
---

# Image generation

Generate pictures without changing the conversation's model or reasoning level.

## Avoid the unsupported-complexity failure

Image generation is a separate request, not a reasoning turn. The current chat session may use `high` reasoning; image requests must not inherit it.

- Do not switch the chat session to an image model.
- Do not send `complexity`, `reasoning`, `reasoning_effort`, `thinking` or the session's `PI_REASONING_LEVEL` to an image endpoint.
- `--quality` is an optional **GPT Image rendering parameter**, not reasoning effort. Omit it by default. The helper rejects it for other backends.
- Do not use a reasoning/model-setting command to retry a failed image request.
- If a dedicated image tool is required by the current system instructions, use only its declared parameters. Do not invent a complexity parameter.

**Reference images:** pass local file paths to the helper with `--ref`. Do not read an image into a text-only chat model to inspect it. In the reported failure, image-file reads preceded the failed assistant turn; the generation helper had not run. A claim that the image API was retried would therefore be incorrect.

## Run the Node.js helper

Use the `scripts/generate-image.mjs` beside this skill. Both installed paths work; prefer the path of the skill you loaded. No Python or additional npm dependencies are needed.

```bash
node ~/.claude/skills/image-gen/scripts/generate-image.mjs \
  --prompt "Flat illustrated settings cog, blue and teal, clear at small sizes, no shadows, no text" \
  --transparent --out /tmp/settings-option-1.png
```

The helper reads `providers.cpamc.baseUrl` and `apiKey` from `~/.pi/agent/models.json`. Environment overrides: `CPAMC_BASE_URL`, `CPAMC_API_KEY`. Claude's existing `ANTHROPIC_BASE_URL` and auth variables are a fallback when models.json is absent, then `~/.config/cpamc/cli-client-api-key` (written by the gateway's `/setup` installer). Never print credentials.

### Backends

| Use | Model / alias | Route |
|---|---|---|
| Default: MiniMax H3-Image (photoreal, up to 4K, strong prompt adherence) | `minimax/MiniMax-H3-Image` / `h3` (also `minimax`) | `/images/generations` |
| Readable text in images, GPT Image look | `gpt-image-2.6` / `gpt` | `/images/generations` |
| Reference-based generation or edits | `gemini-3.1-flash-image` / `nano` | `/chat/completions` |
| Alternate stylization | `grok-imagine-image-2.0` / `grok` | `/images/generations` |
| Meta Muse | `meta/muse-image-1.0` / `muse` | `/images/generations` |
| Older MiniMax | `minimax/image-01` / `minimax-image-01` | `/images/generations` |

`--ref` without `--model` selects Nano Banana. An incompatible explicit model fails before any request, rather than silently ignoring the reference. Unknown models also fail before any request; do not guess routes or silently switch to another paid backend.

See `references/models.md` for gateway-specific requirements.

### Options

- `--prompt TEXT` or `--prompt-file PATH`
- `--model ID_OR_ALIAS` (default `h3`; `nano` when `--ref` is given)
- `--out PATH` (default: unique file under `/tmp/pi-image-gen/`)
- `--size 1024x1024` (images endpoint only; H3 accepts 655,360–20,000,000 px, ratio 1:3–3:1, e.g. `2048x2048`)
- `--quality low|medium|high|auto` (GPT Image only; omitted by default)
- `--ref PATH` (Nano Banana only)
- `--transparent` (ImageMagick required): generate against magenta, remove that background, fit to a transparent PNG; preserve white details in the subject
- `--icon-size 256`, `--icon-pad 6`, `--chroma-fuzz 12`
- `--keep-raw PATH` (optional original render when removing its background)
- `--key-only PATH` (remove the background from an existing render; no API call)
- `--dry-run` (validate arguments and show the sanitized request, without credentials or an API call)
- `--json` (machine-readable result; no base64 dump)

```bash
# Use existing artwork as a reference without feeding it to the chat model.
node ~/.claude/skills/image-gen/scripts/generate-image.mjs \
  --ref /absolute/path/to/reference.webp \
  --prompt "Generate a settings icon matching this flat illustrated style. No scene, border or shadow." \
  --transparent --out /tmp/settings-option-2.png

# Check the exact request safely before generating.
node ~/.claude/skills/image-gen/scripts/generate-image.mjs \
  --model gpt --prompt "A flat settings cog" --dry-run
```

## Matching an existing icon set

A text description of a style is not enough: it drifts to a generic look. Send an existing icon as the reference.

- Use GPT Image's edit endpoint (`POST {base}/images/edits`, multipart, `model=gpt-image-2.6`, `image=@ref.png`).
  The helper does not cover edits yet; call it with `curl`, then remove the background with the helper's `--key-only`.
- Send **one** reference icon per request. Several icons combined into one sheet make the model redraw a reference's
  subject instead of the new one.
- Choose the reference to match the target look, and rotate between two set icons for two candidates.
- Prompt: "Image 1 is an icon from an app icon set. Create a DIFFERENT icon in the exact same art style. The new icon
  shows ONLY: <subject>. Nothing from image 1 may appear." Then add a solid magenta `#FF00FF` background, no text.
- Do not describe a style the reference doesn't have (e.g. "thick black outlines" when the set has none).
- Check each result with a vision model on the gateway (`/chat/completions` with the image): does it show the requested
  subject, and nothing from the reference? Regenerate failures before showing the user.
- Show candidates next to the existing set in one comparison image; the user chooses.

## Workflow

1. Read the project's visual constraints and existing image paths. Only visually inspect references if the current model explicitly supports image input; otherwise give their paths to Nano Banana.
2. Write a concrete prompt: subject, composition, palette, flatness/detail level, small-size readability, background and what to avoid.
3. Generate the requested number of distinct options. Keep unselected candidates under `/tmp`; do not replace the current app icon or commit alternatives before the user chooses.
4. For transparent icons, use `--transparent`; do not key out white, which destroys white subject details. This mode needs a PNG output path.
5. Confirm the returned file exists. Use `file` or ImageMagick for dimensions/alpha without feeding pixels to an unsupported chat model.
6. Present the actual outputs. Ask the user to choose through `questionnaire` when selection is needed. Do not claim to have visually judged images the current model cannot see.

Do not print base64 image data or place prompt sidecars in an application's runtime assets. Only selected final assets belong in the application repository.

## Recover from errors

- **Unsupported complexity:** determine which operation failed. If no helper result exists, do not claim the gateway helper failed. Use the helper's explicit image-only payload; leave chat reasoning unchanged. If the helper itself receives that error with no complexity parameter in its dry-run payload, report a gateway-side incompatibility instead of looping or changing chat settings.
- **Text-only model cannot read an image:** stop image reads; use the file path as `--ref` or inspect metadata only.
- **Missing credentials / unavailable route / rejected model:** report that blocker. Do not modify gateway, provider or deployment configuration as an image-generation workaround.
- **No image in the response:** treat it as a failed generation, not a saved image. Do not fall back silently to a different endpoint/model.
