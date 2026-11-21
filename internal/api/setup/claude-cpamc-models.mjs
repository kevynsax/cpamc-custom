#!/usr/bin/env node
// Writes the CPAMC model list where Claude Code's gateway model picker reads it
// ($CLAUDE_CONFIG_DIR/cache/gateway-models.json). Installed by the gateway's /setup.
import { mkdirSync, renameSync, writeFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { dirname, join } from 'node:path';

const gatewayBaseUrl = 'https://ai-models.kevyn.com.br';
const configDir = process.env.CLAUDE_CONFIG_DIR || join(homedir(), '.claude');
const cachePath = join(configDir, 'cache', 'gateway-models.json');
const temporaryPath = `${cachePath}.tmp`;

// Non-Claude models travel as claude-fable-5-dd-<id reversed>; the gateway decodes them.
const encodeGatewayModel = (model) => `claude-fable-5-dd-${[...model].reverse().join('')}`;

const models = [
  { id: 'claude-opus-5-5', display_name: 'Claude Opus 5.5' },
  { id: 'claude-fable-5-2', display_name: 'Claude Fable 5.2' },
  { id: encodeGatewayModel('gpt-6.1-sol'), display_name: 'GPT 6.1 Sol' },
  { id: encodeGatewayModel('grok-4.7'), display_name: 'Grok 4.7' },
  { id: encodeGatewayModel('gemini-4-neon-xhigh'), display_name: 'Gemini 4 Neon' },
  { id: encodeGatewayModel('muse-wonder-1.0-contributor'), display_name: 'Muse Wonder 1.0' },
  { id: 'claude-sonnet-5-5', display_name: 'Claude Sonnet 5.5' },
];

const cache = { baseUrl: gatewayBaseUrl, fetchedAt: Date.now(), models };
const content = `${JSON.stringify(cache)}\n`;
mkdirSync(dirname(cachePath), { recursive: true });
writeFileSync(temporaryPath, content, { mode: 0o600 });
renameSync(temporaryPath, cachePath);
