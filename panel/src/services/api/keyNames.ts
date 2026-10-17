import { apiClient } from './client';

export type ApiKeyKind = 'person' | 'system';
export interface KeyMetadata {
  names: Record<string, string>;
  kinds: Record<string, ApiKeyKind>;
}
export function normalizeKeyNames(value: unknown): Record<string, string> {
  if (!value || typeof value !== 'object' || !('names' in value))
    throw new Error('Invalid key names');
  const names = value.names;
  if (!names || typeof names !== 'object' || Array.isArray(names))
    throw new Error('Invalid key names');
  const entries = Object.entries(names);
  if (!entries.every(([key, name]) => /^[a-f0-9]{64}$/.test(key) && typeof name === 'string'))
    throw new Error('Invalid key names');
  return Object.fromEntries(entries) as Record<string, string>;
}
export function normalizeKeyKinds(value: unknown): Record<string, ApiKeyKind> {
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new Error('Invalid key kinds');
  const entries = Object.entries(value);
  if (
    !entries.every(
      ([key, kind]) => /^[a-f0-9]{64}$/.test(key) && (kind === 'person' || kind === 'system')
    )
  )
    throw new Error('Invalid key kind');
  return Object.fromEntries(entries) as Record<string, ApiKeyKind>;
}
export function normalizeKeyMetadata(value: unknown): KeyMetadata {
  const names = normalizeKeyNames(value);
  const kinds = normalizeKeyKinds((value as Record<string, unknown>).kinds ?? {});
  return { names, kinds };
}
export const keyNamesApi = {
  async get() {
    return normalizeKeyMetadata(await apiClient.get('/access/key-names'));
  },
  async patch(
    names: Record<string, string>,
    importOnly = false,
    kinds: Record<string, ApiKeyKind> = {}
  ) {
    return normalizeKeyMetadata(
      await apiClient.patch('/access/key-names', { names, kinds, import_only: importOnly })
    );
  },
};
