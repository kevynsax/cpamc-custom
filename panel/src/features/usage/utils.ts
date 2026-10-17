import type { ClientUsageRow, PersonWeek } from '@/services/api/clientUsage';

export function aggregateClientUsage(rows: ClientUsageRow[]): ClientUsageRow[] {
  const groups = new Map<string, ClientUsageRow>();
  for (const row of rows) {
    const key = JSON.stringify([row.keyId, row.accountId, row.provider, row.authType]);
    const previous = groups.get(key);
    if (!previous) {
      groups.set(key, { ...row });
      continue;
    }
    previous.requests += row.requests;
    previous.failed += row.failed;
    previous.inputTokens += row.inputTokens;
    previous.outputTokens += row.outputTokens;
    previous.cachedTokens += row.cachedTokens;
    previous.totalTokens += row.totalTokens;
    previous.costUsd =
      previous.costUsd === null && row.costUsd === null
        ? null
        : (previous.costUsd ?? 0) + (row.costUsd ?? 0);
    previous.unpriced += row.unpriced;
  }
  return [...groups.values()].sort(
    (a, b) =>
      a.keyId.localeCompare(b.keyId) ||
      b.totalTokens - a.totalTokens ||
      a.accountId.localeCompare(b.accountId)
  );
}

export function fallbackPeople(
  rows: ClientUsageRow[],
  accountId: string,
  now = new Date()
): PersonWeek[] {
  const from = now.getTime() - 7 * 86_400_000;
  const people = new Map<string, PersonWeek>();
  for (const row of rows) {
    const at = Date.parse(row.at);
    if (row.accountId !== accountId || !Number.isFinite(at) || at < from || at > now.getTime())
      continue;
    const person = people.get(row.keyId) ?? {
      keyId: row.keyId,
      requests: 0,
      costUsd: 0,
      unpriced: 0,
      percent: 0,
    };
    person.requests += row.requests;
    person.costUsd += row.costUsd ?? 0;
    person.unpriced += row.unpriced;
    people.set(row.keyId, person);
  }
  return [...people.values()].sort((a, b) => b.costUsd - a.costUsd);
}

export function formatUsageMoney(value: number, locale: string): string {
  const formatter = new Intl.NumberFormat(locale, {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: value > 0 && value < 1 ? 6 : 2,
  });
  return value > 0 && value < 0.000001
    ? `< ${formatter.format(0.000001)}`
    : formatter.format(value);
}

export function formatUsagePercent(value: number, locale: string): string {
  return value > 0 && value < 0.1
    ? `< ${(0.1).toLocaleString(locale)}%`
    : `${value.toLocaleString(locale, { maximumFractionDigits: 1 })}%`;
}

export function utcDateOffset(days: number, now = new Date()): string {
  const date = new Date(now);
  date.setUTCDate(date.getUTCDate() + days);
  return date.toISOString().slice(0, 10);
}
