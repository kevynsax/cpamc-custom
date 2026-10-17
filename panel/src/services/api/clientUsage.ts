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
}

export interface UsageSettings {
  names: Record<string, string>;
  plans: Record<string, string>;
  prices: Record<string, UsageRates>;
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
  provider: string;
  plan: string;
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
        Object.entries(object(settings.prices)).map(([model, value]) => {
          const rate = object(value);
          return [
            model,
            {
              input: count(rate, 'input'),
              output: count(rate, 'output'),
              ...(rate.cache_read !== undefined ? { cache_read: count(rate, 'cache_read') } : {}),
              ...(rate.cache_write !== undefined
                ? { cache_write: count(rate, 'cache_write') }
                : {}),
            },
          ];
        })
      ),
    },
    accounts: Object.entries(object(data.accounts))
      .map(([id, value]) => {
        const account = object(value);
        return {
          id,
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
