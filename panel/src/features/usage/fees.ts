import type { AccountCapacity, CompanyUsage } from './companies';
import { CAPACITY_PARTS } from './spending';

export interface FeeAmount {
  usd: number | null;
  partial: boolean;
}
export interface UnusedFeeValue {
  wasted: FeeAmount;
  available: FeeAmount;
}

export function proratedPlanFee(
  monthlyUsd: number | null,
  start: number,
  end: number
): number | null {
  if (
    monthlyUsd === null ||
    !Number.isFinite(monthlyUsd) ||
    monthlyUsd < 0 ||
    !Number.isFinite(start) ||
    !Number.isFinite(end) ||
    end <= start
  )
    return null;
  let result = 0,
    cursor = start;
  while (cursor < end) {
    const date = new Date(cursor);
    const monthStart = Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), 1);
    const next = Date.UTC(date.getUTCFullYear(), date.getUTCMonth() + 1, 1);
    const until = Math.min(next, end);
    result += (monthlyUsd * (until - cursor)) / (next - monthStart);
    cursor = until;
  }
  return result;
}

export function accountUnusedValue(account: AccountCapacity): UnusedFeeValue {
  const valid = account.quotaMeasured && account.periodFeeUsd !== null;
  return {
    wasted: { usd: valid ? (account.periodFeeUsd! * account.expired) / 100 : null, partial: false },
    available: {
      usd: valid ? (account.periodFeeUsd! * account.remaining) / 100 : null,
      partial: false,
    },
  };
}
export function sumFeeValues(values: UnusedFeeValue[]): UnusedFeeValue {
  const sum = (kind: keyof UnusedFeeValue): FeeAmount => {
    const known = values.filter((value) => value[kind].usd !== null);
    return {
      usd: known.length ? known.reduce((total, value) => total + value[kind].usd!, 0) : null,
      partial: known.length < values.length || values.some((value) => value[kind].partial),
    };
  };
  return { wasted: sum('wasted'), available: sum('available') };
}
export function companyUnusedValue(company: CompanyUsage): UnusedFeeValue {
  return sumFeeValues(company.accounts.map(accountUnusedValue));
}
export function personUnusedValue(company: CompanyUsage, personId: string): UnusedFeeValue {
  const unknown: FeeAmount = { usd: null, partial: false };
  if (!company.chartAvailable) return { wasted: unknown, available: unknown };
  const total = company.parts
    .filter((part) => !CAPACITY_PARTS.has(part.id))
    .reduce((sum, part) => sum + part.value, 0);
  const person = company.parts.find((part) => part.id === personId);
  if (!person || total <= 0) return { wasted: unknown, available: unknown };
  const value = companyUnusedValue(company);
  const allocate = (amount: FeeAmount): FeeAmount => ({
    ...amount,
    usd: amount.usd === null ? null : (amount.usd * person.value) / total,
  });
  return { wasted: allocate(value.wasted), available: allocate(value.available) };
}
