import type { ClientUsageResponse, ClientUsageRow } from '@/services/api/clientUsage';
import { accountRange, companyId, type UsagePeriod } from './companies';

export interface UsageTotal {
  tokens: number;
  requests: number;
  costUsd: number;
  unpriced: number;
}
export interface UsageDetail {
  id: string;
  current: UsageTotal;
  previous: UsageTotal;
}
export interface PersonDetail extends UsageDetail {
  companies: UsageDetail[];
  models: UsageDetail[];
}
export const emptyTotal = (): UsageTotal => ({ tokens: 0, requests: 0, costUsd: 0, unpriced: 0 });
const total = (rows: ClientUsageRow[]): UsageTotal =>
  rows.reduce(
    (sum, row) => ({
      tokens: sum.tokens + row.totalTokens,
      requests: sum.requests + row.requests,
      costUsd: sum.costUsd + (row.costUsd ?? 0),
      unpriced: sum.unpriced + row.unpriced,
    }),
    emptyTotal()
  );

/** Match each account's own reset week; never prorate tokens or costs. */
export function periodRows(
  data: ClientUsageResponse,
  period: UsagePeriod,
  offset: number,
  now = new Date()
) {
  const current: ClientUsageRow[] = [],
    previous: ClientUsageRow[] = [];
  for (const account of data.accounts) {
    const range = accountRange(account, data.weeks, period, offset, now);
    const prior = accountRange(account, data.weeks, period, offset + 1, now);
    const end = Math.min(range.end, now.getTime() + 1);
    const previousEnd =
      offset === 0 ? Math.min(prior.end, prior.start + Math.max(0, end - range.start)) : prior.end;
    for (const row of data.rows) {
      if (row.accountId !== account.id) continue;
      const at = Date.parse(row.at || row.date);
      if (at >= range.start && at < end) current.push({ ...row, provider: account.provider });
      if (at >= prior.start && at < previousEnd)
        previous.push({ ...row, provider: account.provider });
    }
  }
  return { current, previous };
}

function details(
  current: ClientUsageRow[],
  previous: ClientUsageRow[],
  key: (row: ClientUsageRow) => string
): UsageDetail[] {
  const ids = new Set([...current, ...previous].map(key));
  return [...ids]
    .map((id) => ({
      id,
      current: total(current.filter((row) => key(row) === id)),
      previous: total(previous.filter((row) => key(row) === id)),
    }))
    .sort((a, b) => b.current.costUsd - a.current.costUsd || a.id.localeCompare(b.id));
}

export function personDetails(
  current: ClientUsageRow[],
  previous: ClientUsageRow[]
): PersonDetail[] {
  return details(current, previous, (row) => row.keyId).map((person) => {
    const rows = current.filter((row) => row.keyId === person.id);
    const prior = previous.filter((row) => row.keyId === person.id);
    return {
      ...person,
      companies: details(rows, prior, (row) => companyId(row.provider)),
      models: details(rows, prior, (row) => row.model),
    };
  });
}

export function tokensByModel(rows: ClientUsageRow[], company?: string) {
  const totals = new Map<string, number>();
  for (const row of rows) {
    if (company && companyId(row.provider) !== company) continue;
    totals.set(row.model, (totals.get(row.model) ?? 0) + row.totalTokens);
  }
  return [...totals]
    .map(([id, value]) => ({ id, value }))
    .filter((part) => part.value > 0)
    .sort((a, b) => b.value - a.value || a.id.localeCompare(b.id));
}

export function valueByModel(rows: ClientUsageRow[], company?: string) {
  const totals = new Map<string, { id: string; value: number; unpriced: number }>();
  for (const row of rows) {
    if (company && companyId(row.provider) !== company) continue;
    const part = totals.get(row.model) ?? { id: row.model, value: 0, unpriced: 0 };
    part.value += row.costUsd ?? 0;
    part.unpriced += row.unpriced;
    totals.set(row.model, part);
  }
  return [...totals.values()]
    .filter((part) => part.value > 0)
    .sort((a, b) => b.value - a.value || a.id.localeCompare(b.id));
}

/** Merge duplicate views only for a real one-to-one company/model relationship. */
export function mergedCompanyModels(rows: ClientUsageRow[]) {
  const pairs = new Map<string, { company: string; model: string; value: number }>();
  const owners = new Map<string, string>();
  for (const row of rows) {
    if (!(row.costUsd != null && row.costUsd > 0)) continue;
    const company = companyId(row.provider);
    const pair = pairs.get(company);
    if (
      (pair && pair.model !== row.model) ||
      (owners.has(row.model) && owners.get(row.model) !== company)
    )
      return null;
    pairs.set(company, { company, model: row.model, value: (pair?.value ?? 0) + row.costUsd });
    owners.set(row.model, company);
  }
  return pairs.size
    ? [...pairs.values()].sort((a, b) => b.value - a.value || a.company.localeCompare(b.company))
    : null;
}

export function percentChange(current: number, previous: number): number | null {
  return previous > 0 ? (100 * (current - previous)) / previous : null;
}

/** Expired/reset or stale observations must never look like fresh available quota. */
export function quotaIsFresh(quota: { observedAt: string; reset: string }, now = Date.now()) {
  const observed = Date.parse(quota.observedAt),
    reset = Date.parse(quota.reset);
  return (
    Number.isFinite(observed) &&
    Number.isFinite(reset) &&
    observed <= now &&
    now - observed <= 30 * 60_000 &&
    reset > now
  );
}
