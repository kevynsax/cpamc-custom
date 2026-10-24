import { apiClient } from './client';

export type ApiKeyKind = 'person' | 'system';
/** Fastest OpenAI service tier a key may request (CUSTOM cpamc-custom). */
export type ApiKeySpeed = 'standard' | 'fast' | 'ultrafast';
export const API_KEY_SPEEDS: readonly ApiKeySpeed[] = ['standard', 'fast', 'ultrafast'];
export interface KeyMetadata {
  names: Record<string, string>;
  kinds: Record<string, ApiKeyKind>;
  speeds: Record<string, ApiKeySpeed>;
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
export function normalizeKeySpeeds(value: unknown): Record<string, ApiKeySpeed> {
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new Error('Invalid key speeds');
  const entries = Object.entries(value);
  if (
    !entries.every(
      ([key, speed]) => /^[a-f0-9]{64}$/.test(key) && API_KEY_SPEEDS.includes(speed as ApiKeySpeed)
    )
  )
    throw new Error('Invalid key speed');
  return Object.fromEntries(entries) as Record<string, ApiKeySpeed>;
}
export function normalizeKeyMetadata(value: unknown): KeyMetadata {
  const names = normalizeKeyNames(value);
  const record = value as Record<string, unknown>;
  const kinds = normalizeKeyKinds(record.kinds ?? {});
  const speeds = normalizeKeySpeeds(record.speeds ?? {});
  return { names, kinds, speeds };
}
export const keyNamesApi = {
  async get() {
    return normalizeKeyMetadata(await apiClient.get('/access/key-names'));
  },
  async patch(
    names: Record<string, string>,
    importOnly = false,
    kinds: Record<string, ApiKeyKind> = {},
    speeds: Record<string, ApiKeySpeed> = {}
  ) {
    return normalizeKeyMetadata(
      await apiClient.patch('/access/key-names', {
        names,
        kinds,
        speeds,
        import_only: importOnly,
      })
    );
  },
};
