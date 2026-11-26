// CUSTOM (cpamc-custom)
import { isMap, isSeq, YAMLSeq, type Document, type YAMLMap } from 'yaml';
import type { GoogleAccountClient } from '@/types/visualConfig';
import { clientKeyFingerprint } from './apiKeyNames';

export const GOOGLE_ACCOUNT_CLIENTS_PATH = ['access', 'google-account-clients'];

export function readGoogleAccountClients(raw: unknown): GoogleAccountClient[] {
  if (raw === undefined || raw === null) return [];
  if (!Array.isArray(raw)) throw new Error('Invalid Google-account clients');

  const clients: GoogleAccountClient[] = [];
  const entries: unknown[] = raw;
  for (const entry of entries) {
    if (entry === null || typeof entry !== 'object') {
      throw new Error('Invalid Google-account client');
    }
    const record = entry as Record<string, unknown>;
    const name = record.name;
    const apiKey = record['api-key'];
    if (typeof name !== 'string' || typeof apiKey !== 'string') {
      throw new Error('Invalid Google-account client');
    }
    const client: GoogleAccountClient = { name, apiKey };
    clients.push(client);
  }
  return clients;
}

export function writeGoogleAccountClients(doc: Document, clients: GoogleAccountClient[]): void {
  if (clients.length === 0) {
    doc.deleteIn(GOOGLE_ACCOUNT_CLIENTS_PATH);
    return;
  }

  const sequence = doc.getIn(GOOGLE_ACCOUNT_CLIENTS_PATH, true);
  const available = isSeq(sequence) ? [...sequence.items] : [];
  const nodes = [];
  for (const client of clients) {
    const matchesName = (node: unknown) => {
      if (!isMap(node)) return false;
      return node.get('name') === client.name;
    };
    const existing = available.find(matchesName);
    const record = { name: client.name, 'api-key': client.apiKey };
    const node = isMap(existing) && !existing.anchor ? existing : doc.createNode(record);
    node.set('name', client.name);
    node.set('api-key', client.apiKey);
    nodes.push(node);
  }
  if (isSeq(sequence) && !sequence.anchor) {
    sequence.items = nodes;
    return;
  }
  const replacement = new YAMLSeq<YAMLMap>(doc.schema);
  replacement.items = nodes;
  doc.setIn(GOOGLE_ACCOUNT_CLIENTS_PATH, replacement);
}

export function updateAgyClient(
  clients: GoogleAccountClient[],
  previousKey: string,
  apiKey: string,
  displayName: string,
  agy: boolean
): GoogleAccountClient[] {
  const matchesPreviousKey = (client: GoogleAccountClient) => client.apiKey === previousKey;
  const previous = clients.find(matchesPreviousKey);
  const keepOtherClient = (client: GoogleAccountClient) =>
    client.apiKey !== previousKey && client.apiKey !== apiKey;
  const updated = clients.filter(keepOtherClient);
  if (!agy) return updated;

  let name = previous?.name;
  if (!name) {
    const normalized = displayName.trim().toLowerCase();
    const slug = normalized.replace(/[^a-z0-9._-]+/g, '-');
    const trimmedSlug = slug.replace(/^[^a-z0-9]+|[-.]+$/g, '');
    const fingerprint = clientKeyFingerprint(apiKey);
    const suffix = fingerprint.slice(0, 12);
    const base = trimmedSlug.slice(0, 64) || `agy-${suffix}`;
    name = base;
    let index = 2;
    const nameInUse = (client: GoogleAccountClient) => client.name === name;
    while (updated.some(nameInUse)) {
      const number = String(index);
      const stem = base.slice(0, 63 - number.length);
      name = `${stem}-${number}`;
      index++;
    }
  }
  const client: GoogleAccountClient = { name, apiKey };
  if (previous) {
    const index = clients.indexOf(previous);
    updated.splice(index, 0, client);
  } else {
    updated.push(client);
  }
  return updated;
}
