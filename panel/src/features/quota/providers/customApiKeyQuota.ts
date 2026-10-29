/**
 * CUSTOM (cpamc-custom): quota cards for API-key providers (MiniMax, Z.ai).
 * See CUSTOM_FEATURES.md at the repo root.
 *
 * These credentials live in the backend config (api-keys.openai-compatibility),
 * not in auth files, so the backend lists them at GET /custom/quota/credentials.
 * Their usage maps onto the Kimi row shape (used/limit/reset), so the existing
 * Kimi card body is reused, with independent provider cache slices.
 */

import type { TFunction } from 'i18next';
import type { AuthFileItem, KimiQuotaRow, KimiQuotaState } from '@/types';
import { apiCallApi, getApiCallErrorMessage } from '@/services/api';
import { apiClient } from '@/services/api/client';
import { normalizeAuthFilesResponse } from '@/services/api/authFiles';
import { createStatusError, isDisabledAuthFile, resolveAuthProvider } from '@/utils/quota';
import { normalizeAuthIndex } from '@/utils/authIndex';
import type { QuotaProviderData } from './types';

export const MINIMAX_QUOTA_URL = 'https://api.minimax.io/v1/api/openplatform/coding_plan/remains';
export const MINIMAX_CN_QUOTA_URL =
  'https://api.minimaxi.com/v1/api/openplatform/coding_plan/remains';
export const ZAI_QUOTA_URL = 'https://api.z.ai/api/monitor/usage/quota/limit';
export const ZAI_CN_QUOTA_URL = 'https://open.bigmodel.cn/api/monitor/usage/quota/limit';

/** Backend-listed API-key credentials; empty on a stock backend (404). */
export async function listApiKeyQuotaCredentials(): Promise<AuthFileItem[]> {
  try {
    const data = await apiClient.get<{ files?: AuthFileItem[] }>('/custom/quota/credentials');
    return normalizeAuthFilesResponse({ files: data?.files ?? [] }).files;
  } catch (error: unknown) {
    if ((error as { status?: number })?.status === 404) return [];
    throw error;
  }
}

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v);

const asNumber = (v: unknown): number | null => {
  if (typeof v === 'string' && !v.trim()) return null;
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
    const model =
      typeof raw.model_name === 'string' && raw.model_name ? raw.model_name : `#${index + 1}`;
    const windows: [string, string, unknown, unknown, unknown][] = [
      ['interval', 'window', raw.current_interval_remaining_percent, raw.end_time, raw.start_time],
      [
        'weekly',
        'weekly',
        raw.current_weekly_remaining_percent,
        raw.weekly_end_time,
        raw.weekly_start_time,
      ],
    ];
    for (const [id, labelId, remainingPct, end, start] of windows) {
      const remaining = asNumber(remainingPct);
      if (remaining === null) continue;
      const endMs = asNumber(end);
      const startMs = asNumber(start);
      rows.push({
        id: `${model}:${id}`,
        label: `${model} · ${labelId === 'weekly' ? 'weekly' : 'current window'}`,
        labelKey: `minimax_quota.${labelId}`,
        labelParams: { model },
        used: Math.max(0, Math.min(100, 100 - remaining)),
        limit: 100,
        resetAtMs: endMs,
        periodHours:
          endMs !== null && startMs !== null && endMs > startMs
            ? (endMs - startMs) / 3_600_000
            : null,
      });
    }
  });
  return rows;
}

type ZaiPeriod = 'limit' | 'hour' | 'week' | 'month';

interface ZaiWindow {
  period: ZaiPeriod;
  count: number;
  hours: number | null;
}

/** Z.ai window units (number = window length): 3 = hours, 5 = months, 6 = weeks. */
const ZAI_UNITS: Record<number, { period: ZaiPeriod; hours: number | null }> = {
  3: { period: 'hour', hours: 1 },
  5: { period: 'month', hours: null }, // calendar months have no fixed length
  6: { period: 'week', hours: 168 },
};

const zaiWindow = (unit: number | null, length: number | null): ZaiWindow => {
  const known = unit === null ? undefined : ZAI_UNITS[unit];
  if (!known || length === null || length <= 0) return { period: 'limit', count: 0, hours: null };
  return {
    period: known.period,
    count: length,
    hours: known.hours === null ? null : known.hours * length,
  };
};

/**
 * Credit plans and tool calls report exact counters; legacy token windows
 * report only a rounded used percentage.
 */
const zaiUsage = (raw: Record<string, unknown>): { used: number; limit: number } | null => {
  if (raw.usage === undefined && raw.currentValue === undefined) {
    const percent = asNumber(raw.percentage);
    if (percent === null || percent < 0 || percent > 100) return null;
    return { used: percent, limit: 100 };
  }
  const limit = asNumber(raw.usage);
  const used = asNumber(raw.currentValue);
  if (limit === null || used === null || limit < 0 || used < 0) return null;
  return { used, limit };
};

/**
 * Z.ai answers HTTP 200 with success:false when it refuses the request, most
 * often because the key has no active Coding Plan.
 */
function zaiRefusal(payload: unknown, t: TFunction): string | null {
  const data = parseBody(payload);
  if (!isRecord(data) || data.success !== false) return null;
  const message = typeof data.msg === 'string' ? data.msg.trim() : '';
  if (/coding ?plan|没有资格/i.test(message)) return t('zai_quota.no_plan');
  return message || t('zai_quota.empty_data');
}

/** Z.ai coding plan: data.limits[] with usage (cap), currentValue (used), nextResetTime. */
export function buildZaiQuotaRows(payload: unknown): KimiQuotaRow[] {
  const data = parseBody(payload);
  const inner = isRecord(data) && isRecord(data.data) ? data.data : null;
  const limits = inner && Array.isArray(inner.limits) ? inner.limits : [];
  return limits
    .map((raw, index): KimiQuotaRow | null => {
      if (!isRecord(raw)) return null;
      const usage = zaiUsage(raw);
      if (usage === null) return null;
      const span = zaiWindow(asNumber(raw.unit), asNumber(raw.number));
      const tools = raw.type === 'TIME_LIMIT';
      const kind = tools ? 'tool calls' : 'credits';
      return {
        id: `zai:${index}`,
        label: span.period === 'limit' ? kind : `${span.count}-${span.period} ${kind}`,
        labelKey: `zai_quota.${span.period}_${tools ? 'tools' : 'credits'}`,
        labelParams: { count: span.count },
        used: usage.used,
        limit: usage.limit,
        resetAtMs: asNumber(raw.nextResetTime),
        periodHours: span.hours,
      };
    })
    .filter((row): row is KimiQuotaRow => row !== null);
}

const makeConfig = (
  type: 'minimax' | 'zai',
  url: string,
  header: Record<string, string>,
  build: (payload: unknown) => KimiQuotaRow[],
  refusal?: (payload: unknown, t: TFunction) => string | null
): QuotaProviderData<KimiQuotaState, KimiQuotaRow[]> => ({
  type,
  i18nPrefix: `${type}_quota`,
  filterFn: (file) => resolveAuthProvider(file) === type && !isDisabledAuthFile(file),
  fetchQuota: async (file: AuthFileItem, t: TFunction) => {
    const authIndex = normalizeAuthIndex(file['auth_index'] ?? file.authIndex);
    const prefix = `${type}_quota`;
    if (!authIndex) throw new Error(t(`${prefix}.missing_auth_index`));
    let quotaUrl = url;
    if (typeof file.base_url === 'string') {
      try {
        const host = new URL(file.base_url).hostname.toLowerCase();
        if (type === 'zai' && (host === 'bigmodel.cn' || host.endsWith('.bigmodel.cn')))
          quotaUrl = ZAI_CN_QUOTA_URL;
        if (type === 'minimax' && (host === 'minimaxi.com' || host.endsWith('.minimaxi.com')))
          quotaUrl = MINIMAX_CN_QUOTA_URL;
      } catch {
        // Missing or invalid region hints keep the international endpoint.
      }
    }
    const result = await apiCallApi.request({ authIndex, method: 'GET', url: quotaUrl, header });
    if (result.statusCode < 200 || result.statusCode >= 300) {
      throw createStatusError(getApiCallErrorMessage(result), result.statusCode);
    }
    const payload = result.body ?? result.bodyText;
    const refused = refusal?.(payload, t);
    if (refused) throw new Error(refused);
    const rows = build(payload);
    if (rows.length === 0) throw new Error(t(`${prefix}.empty_data`));
    return rows.map((row) => ({
      ...row,
      label: row.labelKey ? t(row.labelKey, row.labelParams ?? {}) : row.label,
    }));
  },
  storeSelector: (state) => (type === 'zai' ? state.zaiQuota : state.minimaxQuota),
  storeSetter: type === 'zai' ? 'setZaiQuota' : 'setMinimaxQuota',
  buildLoadingState: () => ({ status: 'loading', rows: [] }),
  buildSuccessState: (rows) => ({ status: 'success', rows }),
  buildErrorState: (message, status) => ({
    status: 'error',
    rows: [],
    error: message,
    errorStatus: status,
  }),
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
  buildZaiQuotaRows,
  zaiRefusal
);
