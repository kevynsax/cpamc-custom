/**
 * CUSTOM (cpamc-custom): quota cards for API-key providers (MiniMax, Z.ai).
 * See CUSTOM_FEATURES.md at the repo root.
 *
 * These credentials live in the backend config (api-keys.openai-compatibility),
 * not in auth files, so the backend lists them at GET /custom/quota/credentials.
 * Their usage maps onto the Kimi row shape (used/limit/reset), so the existing
 * Kimi state and card body are reused instead of adding new store slices.
 */

import type { TFunction } from 'i18next';
import type { AuthFileItem, KimiQuotaRow, KimiQuotaState } from '@/types';
import { apiCallApi, getApiCallErrorMessage } from '@/services/api';
import { apiClient } from '@/services/api/client';
import { createStatusError, isDisabledAuthFile, resolveAuthProvider } from '@/utils/quota';
import { normalizeAuthIndex } from '@/utils/authIndex';
import type { QuotaProviderData } from './types';

export const MINIMAX_QUOTA_URL = 'https://api.minimax.io/v1/api/openplatform/coding_plan/remains';
export const ZAI_QUOTA_URL = 'https://api.z.ai/api/monitor/usage/quota/limit';

/** Backend-listed API-key credentials; empty on a stock backend (404). */
export async function listApiKeyQuotaCredentials(): Promise<AuthFileItem[]> {
  try {
    const data = await apiClient.get<{ files?: AuthFileItem[] }>('/custom/quota/credentials');
    return data?.files ?? [];
  } catch {
    return [];
  }
}

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v);

const asNumber = (v: unknown): number | null => {
  const n = typeof v === 'string' ? Number(v) : v;
  return typeof n === 'number' && Number.isFinite(n) ? n : null;
};

const parseBody = (body: unknown): unknown => {
  if (typeof body !== 'string') return body;
  try {
    return JSON.parse(body);
  } catch {
    return null;
  }
};

/** MiniMax coding plan: one interval window + one weekly window per model. */
export function buildMinimaxQuotaRows(payload: unknown): KimiQuotaRow[] {
  const data = parseBody(payload);
  const models = isRecord(data) && Array.isArray(data.model_remains) ? data.model_remains : [];
  const rows: KimiQuotaRow[] = [];
  models.forEach((raw, index) => {
    if (!isRecord(raw)) return;
    const model = typeof raw.model_name === 'string' && raw.model_name ? raw.model_name : `#${index + 1}`;
    const windows: [string, string, unknown, unknown, unknown, unknown][] = [
      ['interval', 'window', raw.current_interval_remaining_percent, raw.end_time, raw.start_time, raw.current_interval_usage_count],
      ['weekly', 'weekly', raw.current_weekly_remaining_percent, raw.weekly_end_time, raw.weekly_start_time, raw.current_weekly_usage_count],
    ];
    for (const [id, labelId, remainingPct, end, start, usedCount] of windows) {
      const remaining = asNumber(remainingPct);
      if (remaining === null) continue;
      const endMs = asNumber(end);
      const startMs = asNumber(start);
      rows.push({
        id: `${model}:${id}`,
        label: `${model} · ${labelId === 'weekly' ? 'weekly' : 'current window'}`,
        used: Math.max(0, Math.min(100, 100 - remaining)),
        limit: 100,
        resetAtMs: endMs,
        periodHours: endMs !== null && startMs !== null && endMs > startMs ? (endMs - startMs) / 3_600_000 : null,
        resetHint: asNumber(usedCount) !== null ? `${asNumber(usedCount)} requests used` : undefined,
      });
    }
  });
  return rows;
}

/** Z.ai unit codes: 3 = hours, 5 = minutes, 6 = weeks (number = window length). */
const zaiWindowHours = (unit: number | null, number: number | null): number | null => {
  if (unit === null || number === null) return null;
  if (unit === 3) return number;
  if (unit === 5) return number / 60;
  if (unit === 6) return number * 24 * 7;
  return null;
};

const zaiWindowLabel = (hours: number | null, type: string): string => {
  const kind = type === 'TIME_LIMIT' ? 'tool calls' : 'credits';
  if (hours === null) return kind;
  if (hours >= 24 * 7 && hours % (24 * 7) === 0) return `${hours / (24 * 7)}-week ${kind}`;
  if (hours >= 1) return `${hours}-hour ${kind}`;
  return `${Math.round(hours * 60)}-minute ${kind}`;
};

/** Z.ai coding plan: data.limits[] with usage (cap), currentValue (used), nextResetTime. */
export function buildZaiQuotaRows(payload: unknown): KimiQuotaRow[] {
  const data = parseBody(payload);
  const inner = isRecord(data) && isRecord(data.data) ? data.data : null;
  const limits = inner && Array.isArray(inner.limits) ? inner.limits : [];
  return limits
    .map((raw, index): KimiQuotaRow | null => {
      if (!isRecord(raw)) return null;
      const cap = asNumber(raw.usage);
      const used = asNumber(raw.currentValue);
      if (cap === null || used === null) return null;
      const hours = zaiWindowHours(asNumber(raw.unit), asNumber(raw.number));
      return {
        id: `zai:${index}`,
        label: zaiWindowLabel(hours, String(raw.type ?? '')),
        used,
        limit: cap,
        resetAtMs: asNumber(raw.nextResetTime),
        periodHours: hours,
      };
    })
    .filter((row): row is KimiQuotaRow => row !== null);
}

const makeConfig = (
  type: 'minimax' | 'zai',
  url: string,
  header: Record<string, string>,
  build: (payload: unknown) => KimiQuotaRow[]
): QuotaProviderData<KimiQuotaState, KimiQuotaRow[]> => ({
  type: type as never,
  i18nPrefix: 'kimi_quota',
  filterFn: (file) => resolveAuthProvider(file) === type && !isDisabledAuthFile(file),
  fetchQuota: async (file: AuthFileItem, t: TFunction) => {
    const authIndex = normalizeAuthIndex(file['auth_index'] ?? file.authIndex);
    if (!authIndex) throw new Error(t('kimi_quota.missing_auth_index'));
    const result = await apiCallApi.request({ authIndex, method: 'GET', url, header });
    if (result.statusCode < 200 || result.statusCode >= 300) {
      throw createStatusError(getApiCallErrorMessage(result), result.statusCode);
    }
    const rows = build(result.body ?? result.bodyText);
    if (rows.length === 0) throw new Error(t('kimi_quota.empty_data'));
    return rows;
  },
  storeSelector: (state) => state.kimiQuota,
  storeSetter: 'setKimiQuota',
  buildLoadingState: () => ({ status: 'loading', rows: [] }),
  buildSuccessState: (rows) => ({ status: 'success', rows }),
  buildErrorState: (message, status) => ({ status: 'error', rows: [], error: message, errorStatus: status }),
});

export const MINIMAX_CONFIG = makeConfig(
  'minimax',
  MINIMAX_QUOTA_URL,
  { Authorization: 'Bearer $TOKEN$', 'Content-Type': 'application/json' },
  buildMinimaxQuotaRows
);

export const ZAI_CONFIG = makeConfig(
  'zai',
  ZAI_QUOTA_URL,
  { Authorization: '$TOKEN$', 'Accept-Language': 'en-US,en', 'Content-Type': 'application/json' },
  buildZaiQuotaRows
);
