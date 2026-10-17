import { apiClient } from './client';

export interface ClientUsageRow {
  date: string;
  at: string;
  model: string;
  keyId: string;
  accountId: string;
  provider: string;
  authType: string;
  requests: number;
  failed: number;
  inputTokens: number;
  outputTokens: number;
  cachedTokens: number;
  totalTokens: number;
  costUsd: number | null;
  unpriced: number;
}

export interface UsageRates {
  input: number;
  output: number;
  cache_read?: number;
  cache_write?: number;
  context_over?: number;
  context_inclusive?: boolean;
  long_context?: UsageRates;
}

export interface UsageSettings {
  names: Record<string, string>;
  plans: Record<string, string>;
  prices: Record<string, UsageRates>;
  unit_prices?: Record<string, Record<string, number>>;
}

export interface PersonWeek {
  keyId: string;
  costUsd: number;
  unpriced: number;
  requests: number;
  percent: number;
}

export interface UsageWeek {
  id: string;
  accountId: string;
  provider: string;
  scope: string;
  periodSeconds?: number;
  plan: string;
  start: string;
  reset: string;
  people: PersonWeek[];
  used: number;
  remaining: number;
  unattributed: number;
  unusedUpperBound: number | null;
  lastObserved: string;
  incomplete: boolean;
  closed: boolean;
  allocationAvailable: boolean;
}

export interface UsageAccount {
  id: string;
  email?: string;
  monthlyFeeUsd?: number | null;
  provider: string;
  plan: string;
}
export interface UsageModelPrice {
  id: string;
  provider: string;
  kind: string;
  status: string;
  prices: Record<string, number>;
  sources: string[];
  checkedAt: string;
  contextThreshold: number;
  contextInclusive: boolean;
  overridden: boolean;
}

export interface ClientUsageResponse {
  collectionEnabled: boolean;
  rows: ClientUsageRow[];
  accountLabels: Record<string, string>;
  keyLabels: Record<string, string>;
  accounts: UsageAccount[];
  weeks: UsageWeek[];
  settings: UsageSettings;
  pricesAsOf: string;
  models?: UsageModelPrice[];
}

function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new Error('Invalid usage response');
  return value as Record<string, unknown>;
}
function text(row: Record<string, unknown>, key: string): string {
  if (typeof row[key] !== 'string') throw new Error('Invalid usage identity');
  return row[key] as string;
}
function count(row: Record<string, unknown>, key: string): number {
  const value = row[key];
  if (typeof value !== 'number' || !Number.isFinite(value) || value < 0)
    throw new Error('Invalid usage count');
  return value;
}
function nullableCount(row: Record<string, unknown>, key: string): number | null {
  return row[key] === null || row[key] === undefined ? null : count(row, key);
}
function flag(row: Record<string, unknown>, key: string): boolean {
  if (typeof row[key] !== 'boolean') throw new Error('Invalid usage flag');
  return row[key] as boolean;
}
function array(row: Record<string, unknown>, key: string): unknown[] {
  if (!Array.isArray(row[key])) throw new Error('Invalid usage list');
  return row[key] as unknown[];
}
function labels(value: unknown): Record<string, string> {
  return Object.fromEntries(
    Object.entries(object(value)).filter(
      (entry): entry is [string, string] => typeof entry[1] === 'string'
    )
  );
}

function numericMap(value: unknown): Record<string, number> {
  const row = object(value);
  return Object.fromEntries(Object.keys(row).map((key) => [key, count(row, key)]));
}
function normalizeRates(value: unknown): UsageRates {
  const rate = object(value);
  return {
    input: count(rate, 'input'),
    output: count(rate, 'output'),
    ...(rate.cache_read != null ? { cache_read: count(rate, 'cache_read') } : {}),
    ...(rate.cache_write != null ? { cache_write: count(rate, 'cache_write') } : {}),
    ...(rate.context_over != null
      ? {
          context_over: count(rate, 'context_over'),
          context_inclusive: rate.context_inclusive === true,
        }
      : {}),
    ...(rate.long_context ? { long_context: normalizeRates(rate.long_context) } : {}),
  };
}

export function normalizeClientUsage(value: unknown): ClientUsageResponse {
  const data = object(value);
  const settings = object(data.settings);
  return {
    collectionEnabled: flag(data, 'collection_enabled'),
    accountLabels: labels(data.account_labels),
    keyLabels: labels(data.key_labels),
    pricesAsOf: text(data, 'prices_as_of'),
    settings: {
      names: labels(settings.names),
      plans: labels(settings.plans),
      prices: Object.fromEntries(
        Object.entries(object(settings.prices)).map(([model, rate]) => [
          model,
          normalizeRates(rate),
        ])
      ),
      unit_prices: Object.fromEntries(
        Object.entries(object(settings.unit_prices ?? {})).map(([id, fields]) => [
          id,
          numericMap(fields),
        ])
      ),
    },
    models: (data.models === undefined ? [] : array(data, 'models')).map((value) => {
      const model = object(value);
      return {
        id: text(model, 'id'),
        provider: text(model, 'provider'),
        kind: text(model, 'kind'),
        status: text(model, 'status'),
        prices: numericMap(model.prices),
        sources: array(model, 'sources').filter(
          (value): value is string => typeof value === 'string' && value.startsWith('https://')
        ),
        checkedAt: text(model, 'checked_at'),
        contextThreshold: model.context_threshold ? count(model, 'context_threshold') : 0,
        contextInclusive: model.context_inclusive === true,
        overridden: model.overridden === true,
      };
    }),
    accounts: Object.entries(object(data.accounts))
      .map(([id, value]) => {
        const account = object(value);
        return {
          id,
          email: typeof account.email === 'string' ? account.email : '',
          monthlyFeeUsd: nullableCount(account, 'monthly_fee_usd'),
          provider: text(account, 'provider'),
          plan: typeof account.plan === 'string' ? account.plan : '',
        };
      })
      .sort((a, b) => a.provider.localeCompare(b.provider) || a.id.localeCompare(b.id)),
    rows: array(data, 'rows').map((value) => {
      const row = object(value);
      return {
        date: text(row, 'date'),
        at: typeof row.at === 'string' ? row.at : '',
        model: typeof row.model === 'string' ? row.model : '',
        keyId: text(row, 'key_id'),
        accountId: text(row, 'account_id'),
        provider: text(row, 'provider'),
        authType: text(row, 'auth_type'),
        requests: count(row, 'requests'),
        failed: count(row, 'failed'),
        inputTokens: count(row, 'input_tokens'),
        outputTokens: count(row, 'output_tokens'),
        cachedTokens: count(row, 'cached_tokens'),
        totalTokens: count(row, 'total_tokens'),
        costUsd: nullableCount(row, 'cost_usd'),
        unpriced:
          row.cost_usd == null
            ? count(row, 'requests')
            : typeof row.unpriced_requests === 'number'
              ? count(row, 'unpriced_requests')
              : 0,
      };
    }),
    weeks: array(data, 'weeks').map((value) => {
      const week = object(value);
      return {
        id: text(week, 'id'),
        accountId: text(week, 'account_id'),
        provider: text(week, 'provider'),
        scope: text(week, 'scope'),
        periodSeconds: week.period_seconds
          ? count(week, 'period_seconds')
          : (Date.parse(text(week, 'reset')) - Date.parse(text(week, 'start'))) / 1000,
        plan: text(week, 'plan'),
        start: text(week, 'start'),
        reset: text(week, 'reset'),
        used: count(week, 'used'),
        remaining: count(week, 'remaining'),
        unattributed: count(week, 'unattributed'),
        unusedUpperBound: nullableCount(week, 'unused_upper_bound'),
        lastObserved: text(week, 'last_observed'),
        allocationAvailable: flag(week, 'allocation_available'),
        incomplete: flag(week, 'incomplete'),
        closed: flag(week, 'closed'),
        people: array(week, 'people').map((value) => {
          const person = object(value);
          return {
            keyId: text(person, 'key_id'),
            costUsd: count(person, 'cost_usd'),
            unpriced: count(person, 'unpriced'),
            requests: count(person, 'requests'),
            percent: count(person, 'percent'),
          };
        }),
      };
    }),
  };
}

export const clientUsageApi = {
  async get(from?: string, to?: string): Promise<ClientUsageResponse> {
    return normalizeClientUsage(
      await apiClient.get<unknown>('/observability/usage/clients', { params: { from, to } })
    );
  },
  saveSettings: (settings: UsageSettings) =>
    apiClient.put('/observability/usage/settings', settings),
  refreshPrices: () => apiClient.post('/observability/usage/prices/refresh'),
};
