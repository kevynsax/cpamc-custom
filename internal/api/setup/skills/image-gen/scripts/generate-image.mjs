#!/usr/bin/env node
/** Standalone image requests: no chat-session state, reasoning parameters or Python dependencies. */
import { readFile, writeFile, mkdir, mkdtemp, rm, stat } from 'node:fs/promises';
import { homedir, tmpdir } from 'node:os';
import { dirname, extname, join, resolve } from 'node:path';
import { randomUUID } from 'node:crypto';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';

const run = promisify(execFile);
const aliases = new Map([
  ['h3', 'minimax/MiniMax-H3-Image'], ['minimax', 'minimax/MiniMax-H3-Image'], ['minimax-h3', 'minimax/MiniMax-H3-Image'],
  ['gpt', 'gpt-image-2.6'], ['gpt-image', 'gpt-image-2.6'],
  ['grok', 'grok-imagine-image-2.0'], ['grok-imagine', 'grok-imagine-image-2.0'],
  ['muse', 'meta/muse-image-1.0'], ['muse-image', 'meta/muse-image-1.0'], ['muse-image-1.0', 'meta/muse-image-1.0'],
  ['minimax-image-01', 'minimax/image-01'], ['minimax-image', 'minimax/image-01'], ['image-01', 'minimax/image-01'],
  ['image-01-live', 'minimax/image-01-live'],
]);
const imageModels = new Set([
  'gpt-image-2.6', 'gpt-image-2.6-flare', 'gpt-image-2.6-sunburst',
  'grok-imagine-image', 'grok-imagine-image-2.0', 'grok-imagine-image-quality',
  'meta/muse-image-1.0', 'minimax/image-01', 'minimax/image-01-live',
  'minimax/MiniMax-H3-Image',
]);
const flags = new Set(['help', 'json', 'transparent', 'dry-run']);
const valued = new Set(['prompt', 'prompt-file', 'model', 'out', 'size', 'quality', 'icon-size', 'icon-pad', 'chroma-fuzz', 'keep-raw', 'key-only']);
const outputMimes = { '.png': 'image/png', '.jpg': 'image/jpeg', '.jpeg': 'image/jpeg', '.webp': 'image/webp', '.gif': 'image/gif' };
const chroma = '#FF00FF';
const chromaPrompt = ' The entire background must be solid bright magenta #FF00FF, edge to edge, with no gradient, texture or shadow. Do not use magenta, pink or purple anywhere on the subject. The subject must not touch the image edges.';
const help = `Generate images through cpamc without inheriting chat reasoning settings.

node generate-image.mjs --prompt TEXT [--model h3|gpt|grok|muse|minimax-image-01] [--out PATH]
  --prompt-file PATH      Read the prompt from a UTF-8 file
  --size WxH              Images endpoint size (default 1024x1024)
  --quality LEVEL         GPT Image only: low, medium, high, auto (default omitted)
  --transparent           Chroma-key to transparent PNG using ImageMagick
  --icon-size NUMBER      Transparent output size (default 256)
  --icon-pad NUMBER       Transparent canvas padding (default 6)
  --chroma-fuzz NUMBER    Key tolerance in percent (default 12)
  --keep-raw PATH         Preserve the original render when removing its background
  --key-only PATH         Process an existing render without an API call
  --dry-run               Validate and print the request without calling the API
  --json                  Print result metadata as JSON

Never accepts complexity, reasoning or thinking flags. Credentials come from
CPAMC_BASE_URL/CPAMC_API_KEY, ~/.pi/agent/models.json (providers.cpamc) or
~/.config/cpamc/cli-client-api-key.`;

function parse(args) {
  const options = {};
  for (let index = 0; index < args.length; index += 1) {
    const arg = args[index];
    if (!arg.startsWith('--')) throw new Error(`Unexpected argument: ${arg}`);
    const name = arg.slice(2);
    if (flags.has(name)) {
      options[name] = true;
      continue;
    }
    if (!valued.has(name)) throw new Error(`Unsupported option --${name}. Use --help; image requests do not accept reasoning or complexity.`);
    const value = args[index + 1];
    if (value === undefined || value.startsWith('--')) throw new Error(`Missing value for --${name}`);
    options[name] = value;
    index += 1;
  }
  return options;
}

function integer(value, fallback, name, min, max) {
  if (value === undefined) return fallback;
  const number = Number(value);
  if (!Number.isInteger(number) || number < min || number > max) throw new Error(`--${name} must be an integer from ${min} to ${max}`);
  return number;
}

function imageMime(bytes) {
  if (bytes.length >= 8 && bytes.subarray(0, 8).equals(Buffer.from('89504e470d0a1a0a', 'hex'))) return 'image/png';
  if (bytes[0] === 0xff && bytes[1] === 0xd8 && bytes[2] === 0xff) return 'image/jpeg';
  const riff = bytes.toString('ascii', 0, 4);
  const webp = bytes.toString('ascii', 8, 12);
  if (riff === 'RIFF' && webp === 'WEBP') return 'image/webp';
  const gif = bytes.toString('ascii', 0, 6);
  if (gif === 'GIF87a' || gif === 'GIF89a') return 'image/gif';
  throw new Error('The response was not a supported PNG, JPEG, WebP or GIF image');
}

function suffix(mime) {
  const extensions = { 'image/png': '.png', 'image/jpeg': '.jpg', 'image/webp': '.webp', 'image/gif': '.gif' };
  return extensions[mime];
}

async function requestFor(options) {
  if (options.prompt && options['prompt-file']) throw new Error('Use either --prompt or --prompt-file, not both');
  const text = options['prompt-file'] ? await readFile(options['prompt-file'], 'utf8') : options.prompt ?? '';
  let prompt = text.trim();
  if (!prompt) throw new Error('Provide --prompt or --prompt-file');
  if (options.transparent) {
    prompt = prompt.replace(/\b(?:pure |plain |solid )?white background\b\.?/gi, '').trim();
    prompt += chromaPrompt;
  }
  const selected = options.model ?? 'h3';
  const normalized = selected.toLowerCase();
  const model = aliases.get(normalized) ?? selected;
  if (!imageModels.has(model)) throw new Error(`Unsupported image model: ${model}. Use a documented model or alias.`);
  if (options.quality && !model.startsWith('gpt-image-')) throw new Error('--quality is a GPT Image rendering option; omit it for this model');
  const levels = ['low', 'medium', 'high', 'auto'];
  if (options.quality && !levels.includes(options.quality)) throw new Error('--quality must be low, medium, high or auto');
  let size = options.size ?? '1024x1024';
  const dimensions = /^(\d+)x(\d+)$/.exec(size);
  if (!dimensions) throw new Error('--size must be WxH, for example 1024x1024');
  const h3 = model === 'minimax/MiniMax-H3-Image';
  if (h3 && prompt.length > 32000) throw new Error('MiniMax H3 prompts must not exceed 32000 characters');
  const area = Number(dimensions[1]) * Number(dimensions[2]);
  const ratio = Number(dimensions[1]) / Number(dimensions[2]);
  if (h3 && (area < 655_360 || area > 20_000_000 || ratio < 1 / 3 || ratio > 3)) {
    throw new Error('MiniMax H3 --size must be 655,360 to 20,000,000 pixels with a ratio from 1:3 to 3:1');
  }
  if (model.startsWith('minimax/image-01')) {
    if (prompt.length > 1500) throw new Error('MiniMax prompts must not exceed 1500 characters');
    const width = Math.min(2048, Math.max(512, Math.round(Number(dimensions[1]) / 8) * 8));
    const height = Math.min(2048, Math.max(512, Math.round(Number(dimensions[2]) / 8) * 8));
    size = `${width}x${height}`;
  }
  const payload = { model, prompt, n: 1, size };
  if (options.quality) payload.quality = options.quality;
  return { endpoint: '/images/generations', payload };
}

async function credentials() {
  let base = process.env.CPAMC_BASE_URL?.trim();
  let key = process.env.CPAMC_API_KEY?.trim();
  if (!base || !key) {
    const path = join(homedir(), '.pi', 'agent', 'models.json');
    let provider;
    try {
      const json = await readFile(path, 'utf8');
      const config = JSON.parse(json);
      provider = config.providers?.cpamc;
    } catch (error) {
      if (error.code !== 'ENOENT') throw new Error(`Cannot read cpamc configuration: ${error.message}`);
      const anthropic = process.env.ANTHROPIC_BASE_URL?.replace(/\/$/, '');
      base ||= anthropic ? `${anthropic}/v1` : undefined;
      key ||= process.env.ANTHROPIC_AUTH_TOKEN || process.env.ANTHROPIC_API_KEY;
    }
    base ||= provider?.baseUrl;
    key ||= provider?.apiKey;
  }
  if (!key) {
    // Written by the gateway's /setup installer.
    const keyPath = join(homedir(), '.config', 'cpamc', 'cli-client-api-key');
    try {
      const stored = await readFile(keyPath, 'utf8');
      key = stored.trim();
      base ||= 'https://ai-models.kevyn.com.br/v1';
    } catch (error) {
      if (error.code !== 'ENOENT') throw new Error(`Cannot read ${keyPath}: ${error.message}`);
    }
  }
  if (!base || !key) throw new Error('Set CPAMC_BASE_URL and CPAMC_API_KEY, or configure providers.cpamc in ~/.pi/agent/models.json');
  // Shell environments often export the gateway root; the OpenAI-compatible routes live under /v1.
  const trimmed = base.replace(/\/+$/, '');
  const root = /\/v1$/.test(trimmed) ? trimmed : `${trimmed}/v1`;
  return { base: root, key };
}

async function generate(request, config) {
  const headers = { Authorization: `Bearer ${config.key}`, 'Content-Type': 'application/json', Accept: 'application/json' };
  const body = JSON.stringify(request.payload);
  // MiniMax H3 can take up to 600 s and is not idempotent: wait for it, never retry.
  const h3 = request.payload.model === 'minimax/MiniMax-H3-Image';
  const signal = AbortSignal.timeout(h3 ? 600_000 : 180_000);
  const init = { method: 'POST', headers, body, signal };
  const response = await fetch(`${config.base}${request.endpoint}`, init);
  const text = await response.text();
  let data;
  try { data = JSON.parse(text); } catch { throw new Error(`HTTP ${response.status}: gateway returned invalid JSON`); }
  if (!response.ok) {
    const detail = typeof data.error === 'string' ? data.error : data.error?.message ?? data.message ?? 'Image request failed';
    const safe = String(detail).replaceAll(config.key, '[redacted]').slice(0, 600);
    throw new Error(`HTTP ${response.status}: ${safe}`);
  }
  const first = data.data?.[0];
  if (first?.b64_json) {
    const bytes = Buffer.from(first.b64_json, 'base64');
    const mime = imageMime(bytes);
    return { bytes, mime };
  }
  const encoded = first?.url;
  if (typeof encoded !== 'string') throw new Error('The gateway returned no image. No output file was written.');
  const match = /^data:image\/[a-zA-Z0-9.+-]+;base64,([\s\S]+)$/.exec(encoded);
  if (!match) throw new Error('Expected an inline image data URL; remote URLs are not automatically fetched');
  const bytes = Buffer.from(match[1], 'base64');
  const mime = imageMime(bytes);
  return { bytes, mime };
}

async function magick(args) {
  try {
    const result = await run('magick', args);
    return result.stdout;
  } catch (error) {
    if (error.code === 'ENOENT') throw new Error('This operation needs ImageMagick (brew install imagemagick)');
    const detail = String(error.stderr || error.message).slice(0, 600);
    throw new Error(`ImageMagick failed: ${detail}`);
  }
}

async function keyBackground(source, destination, options) {
  const size = integer(options['icon-size'], 256, 'icon-size', 16, 4096);
  const pad = integer(options['icon-pad'], 6, 'icon-pad', 0, 2047);
  const fuzz = integer(options['chroma-fuzz'], 12, 'chroma-fuzz', 0, 100);
  const inner = size - 2 * pad;
  if (inner < 1) throw new Error('--icon-pad must leave room for the subject');
  const sampleArgs = [source, '-format', '%[pixel:p{3,3}]|%[pixel:p{-4,3}]|%[pixel:p{3,-4}]|%[pixel:p{-4,-4}]', 'info:'];
  const sampled = await magick(sampleArgs);
  const counts = new Map();
  for (const raw of sampled.split('|')) {
    const color = raw.trim();
    if (color) counts.set(color, (counts.get(color) ?? 0) + 1);
  }
  let background = chroma;
  let most = 0;
  for (const [color, count] of counts) {
    if (count <= most) continue;
    most = count;
    background = color;
  }
  const args = [source, '-fuzz', `${fuzz}%`, '-transparent', background, '-trim', '+repage', '-resize', `${inner}x${inner}`, '-background', 'none', '-gravity', 'center', '-extent', `${size}x${size}`, `PNG32:${destination}`];
  await magick(args);
}

function outputPath(options, mime) {
  const extension = options.transparent || options['key-only'] ? '.png' : suffix(mime);
  if (options.out) {
    const path = extname(options.out) ? options.out : `${options.out}${extension}`;
    const extension = extname(path).toLowerCase();
    if (!outputMimes[extension]) throw new Error('Output must use .png, .jpg, .jpeg, .webp or .gif');
    if ((options.transparent || options['key-only']) && extension !== '.png') throw new Error('Transparent output requires a .png path');
    return resolve(path);
  }
  const id = randomUUID();
  return join(tmpdir(), 'pi-image-gen', `${id}${extension}`);
}

function preview(request, options) {
  const payload = structuredClone(request.payload);
  const result = { dryRun: true, endpoint: request.endpoint, payload, transparent: Boolean(options.transparent), output: options.out ?? '(unique file under /tmp/pi-image-gen)' };
  const json = JSON.stringify(result, null, 2);
  console.log(json);
}

async function main() {
  const args = process.argv.slice(2);
  const options = parse(args);
  if (options.help) { console.log(help); return; }
  if (options['key-only']) {
    const source = resolve(options['key-only']);
    const path = outputPath(options, 'image/png');
    if (path === source) throw new Error('--key-only output must differ from the input file');
    if (options['dry-run']) {
      const plan = { dryRun: true, operation: 'key-only', source, output: path };
      console.log(JSON.stringify(plan, null, 2));
      return;
    }
    const directory = dirname(path);
    await mkdir(directory, { recursive: true });
    await keyBackground(source, path, options);
    const info = await stat(path);
    const result = { ok: true, path, bytes: info.size, mime: 'image/png', transparent: true };
    console.log(options.json ? JSON.stringify(result) : `saved ${path} (${info.size} bytes)`);
    return;
  }
  if (options.transparent) {
    const size = integer(options['icon-size'], 256, 'icon-size', 16, 4096);
    const pad = integer(options['icon-pad'], 6, 'icon-pad', 0, 2047);
    integer(options['chroma-fuzz'], 12, 'chroma-fuzz', 0, 100);
    if (size <= 2 * pad) throw new Error('--icon-pad must leave room for the subject');
    if (!options['dry-run']) {
      const args = ['-version'];
      await magick(args);
    }
  }
  const request = await requestFor(options);
  // Validate output options before calling a paid endpoint.
  outputPath(options, 'image/png');
  if (options['dry-run']) { preview(request, options); return; }
  const config = await credentials();
  const image = await generate(request, config);
  const path = outputPath(options, image.mime);
  const directory = dirname(path);
  await mkdir(directory, { recursive: true });
  if (options.transparent) {
    const scratch = await mkdtemp(join(tmpdir(), 'pi-image-chroma-'));
    try {
      const extension = suffix(image.mime);
      const raw = join(scratch, `original${extension}`);
      await writeFile(raw, image.bytes);
      if (options['keep-raw']) {
        const rawPath = resolve(options['keep-raw']);
        const rawDirectory = dirname(rawPath);
        await mkdir(rawDirectory, { recursive: true });
        await writeFile(rawPath, image.bytes);
      }
      await keyBackground(raw, path, options);
    } finally {
      await rm(scratch, { recursive: true, force: true });
    }
  } else {
    const wantedMime = outputMimes[extname(path).toLowerCase()];
    if (wantedMime !== image.mime) {
      const scratch = await mkdtemp(join(tmpdir(), 'pi-image-format-'));
      try {
        const extension = suffix(image.mime);
        const raw = join(scratch, `original${extension}`);
        await writeFile(raw, image.bytes);
        const args = [raw, path];
        await magick(args);
        image.mime = wantedMime;
      } finally {
        await rm(scratch, { recursive: true, force: true });
      }
    } else {
      await writeFile(path, image.bytes);
    }
  }
  const info = await stat(path);
  const result = { ok: true, model: request.payload.model, path, bytes: info.size, mime: options.transparent ? 'image/png' : image.mime, transparent: Boolean(options.transparent) };
  console.log(options.json ? JSON.stringify(result) : `saved ${path} (${info.size} bytes, model=${result.model})`);
}

function fail(error) {
  console.error(`error: ${error.message}`);
  process.exitCode = 1;
}
const pending = main();
pending.catch(fail);
