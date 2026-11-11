import type { ClientUsageResponse, ClientUsageRow } from '@/services/api/clientUsage';
import { buildCompanyUsage, reportingRange, type UsagePeriod } from './companies';

export type PriceBasis = 'api' | 'subscription';

/**
 * Re-prices one reporting period's rows at subscription value: the account's
 * prorated plan fee times the key's quota share, split across the key's rows by
 * API value. Rows whose fee, quota share or API value is unknown become unpriced.
 * Rows outside the period are returned unchanged.
 */
function periodSubscriptionRows(
  data: ClientUsageResponse,
  rows: ClientUsageRow[],
  period: UsagePeriod,
  offset: number,
  now: Date
): ClientUsageRow[] {
  const range = reportingRange(period, offset, now);
  const companies = buildCompanyUsage(data, period, offset, now);
  const accounts = new Map(
    companies.flatMap((company) => company.accounts).map((account) => [account.id, account])
  );
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
    const account = accounts.get(row.accountId);
    const cost = costs.get(`${row.keyId}\u0000${row.accountId}`) ?? 0;
    if (!account?.keyShares || account.periodFeeUsd === null || cost <= 0 || row.costUsd === null)
      return { ...row, costUsd: null, unpriced: Math.max(row.unpriced, row.requests, 1) };
    const share = account.keyShares[row.keyId] ?? 0;
    return { ...row, costUsd: (account.periodFeeUsd * share * row.costUsd) / cost, unpriced: 0 };
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
