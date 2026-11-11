import type { ClientUsageResponse, ClientUsageRow, UsageWeek } from '@/services/api/clientUsage';
import { companyId, reportingRange, type UsagePeriod } from './companies';
import { monthlyPlanFee, proratedPlanFee } from './fees';

export type PriceBasis = 'api' | 'subscription';

/** Starts this close together are one quota window recorded twice. */
const DUPLICATE_WINDOW_GAP = 15 * 60_000;

/** Repeated recordings of one window collapse into the one observed last. */
function distinctWindows(cycles: UsageWeek[]): UsageWeek[] {
  const sorted = [...cycles].sort((a, b) => Date.parse(a.start) - Date.parse(b.start));
  const kept: UsageWeek[] = [];
  let previous = -Infinity;
  for (const cycle of sorted) {
    const start = Date.parse(cycle.start);
    const duplicate = kept.length > 0 && start - previous <= DUPLICATE_WINDOW_GAP;
    previous = start;
    if (!duplicate) {
      kept.push(cycle);
      continue;
    }
    const last = kept[kept.length - 1];
    if (Date.parse(cycle.lastObserved) > Date.parse(last.lastObserved))
      kept[kept.length - 1] = cycle;
  }
  return kept;
}

/**
 * Each key's subscription value per account. Every quota window, including one
 * ended early by a reset, is worth the plan fee for its full length; a key gets
 * its attributed quota percentage of it, prorated by the window's overlap with
 * the period. Independent scope/duration families measure the same plan and are averaged.
 */
function subscriptionValues(
  data: ClientUsageResponse,
  period: UsagePeriod,
  offset: number,
  now: Date
): Map<string, Map<string, number>> {
  const range = reportingRange(period, offset, now);
  const limit = Math.min(range.end, now.getTime());
  const values = new Map<string, Map<string, number>>();
  for (const account of data.accounts) {
    const cycles = data.weeks.filter(
      (cycle) =>
        cycle.accountId === account.id &&
        Date.parse(cycle.start) < limit &&
        Date.parse(cycle.reset) > range.start
    );
    const plan = account.plan || cycles.find((cycle) => cycle.plan)?.plan || '';
    const monthly = account.monthlyFeeUsd ?? monthlyPlanFee(companyId(account.provider), plan);
    if (monthly === null || !cycles.length) continue;
    const families = new Map<string, UsageWeek[]>();
    for (const cycle of cycles) {
      const family = JSON.stringify([
        cycle.scope,
        Date.parse(cycle.reset) - Date.parse(cycle.start),
      ]);
      families.set(family, [...(families.get(family) ?? []), cycle]);
    }
    const keys = new Map<string, number>();
    for (const familyCycles of families.values()) {
      for (const cycle of distinctWindows(familyCycles)) {
        const start = Date.parse(cycle.start),
          end = Date.parse(cycle.reset);
        const overlap =
          Math.max(0, Math.min(end, range.end) - Math.max(start, range.start)) / (end - start);
        if (!(overlap > 0) || !cycle.allocationAvailable) continue;
        const percents = cycle.people.reduce((sum, person) => sum + person.percent, 0);
        const scale = percents > cycle.used && percents > 0 ? cycle.used / percents : 1;
        const windowFee = proratedPlanFee(monthly, start, end) ?? 0;
        for (const person of cycle.people) {
          const value = (windowFee * overlap * person.percent * scale) / 100 / families.size;
          keys.set(person.keyId, (keys.get(person.keyId) ?? 0) + value);
        }
      }
    }
    values.set(account.id, keys);
  }
  return values;
}

/**
 * Re-prices one reporting period's rows at subscription value, split across each
 * key's rows by API value. Rows whose fee, quota or API value is unknown become
 * unpriced. Rows outside the period are returned unchanged.
 */
function periodSubscriptionRows(
  data: ClientUsageResponse,
  rows: ClientUsageRow[],
  period: UsagePeriod,
  offset: number,
  now: Date
): ClientUsageRow[] {
  const range = reportingRange(period, offset, now);
  const values = subscriptionValues(data, period, offset, now);
  const inPeriod = (row: ClientUsageRow) => {
    const at = Date.parse(row.at || row.date);
    return at >= range.start && at < range.end;
  };
  const costs = new Map<string, number>();
  for (const row of rows.filter(inPeriod)) {
    const key = `${row.keyId}\u0000${row.accountId}`;
    costs.set(key, (costs.get(key) ?? 0) + (row.costUsd ?? 0));
  }
  return rows.map((row) => {
    if (!inPeriod(row)) return row;
    const accountValues = values.get(row.accountId);
    const cost = costs.get(`${row.keyId}\u0000${row.accountId}`) ?? 0;
    if (!accountValues || cost <= 0 || row.costUsd === null)
      return { ...row, costUsd: null, unpriced: Math.max(row.unpriced, row.requests, 1) };
    const value = accountValues.get(row.keyId) ?? 0;
    return { ...row, costUsd: (value * row.costUsd) / cost, unpriced: 0 };
  });
}

/** The selected and previous periods re-priced at subscription value. */
export function withSubscriptionValue(
  data: ClientUsageResponse,
  period: UsagePeriod,
  offset: number,
  now: Date
): ClientUsageResponse {
  const current = periodSubscriptionRows(data, data.rows, period, offset, now);
  const rows = periodSubscriptionRows(data, current, period, offset + 1, now);
  return { ...data, rows };
}
