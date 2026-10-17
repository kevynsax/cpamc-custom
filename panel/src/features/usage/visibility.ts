import type { CrossQuote } from './currency';
import { formatReais } from './currency';
import type { AccountCapacity, CompanyUsage } from './companies';
import { companyUnusedValue, type UnusedFeeValue } from './fees';
import type { PersonDetail } from './reporting';
import { quotaIsFresh } from './reporting';

export const hasMoney = (value: number | null | undefined, quote: CrossQuote | null) =>
  value != null && value > 0 && formatReais(value, quote) !== '—';
export const hasFeeValue = (value: UnusedFeeValue, current: boolean, quote: CrossQuote | null) =>
  hasMoney(value.wasted.usd, quote) || (current && hasMoney(value.available.usd, quote));
export function hasAccountInformation(
  account: AccountCapacity,
  current: boolean,
  quote: CrossQuote | null,
  now = Date.now()
) {
  return (
    hasMoney(account.monthlyFeeUsd, quote) ||
    (!current && account.quotaMeasured) ||
    (current &&
      (account.cachedQuotas ?? []).some(
        (quota) => quota.assumed === true || quotaIsFresh(quota, now)
      ))
  );
}
export function hasCompanyInformation(
  company: CompanyUsage,
  quote: CrossQuote | null,
  now = Date.now()
) {
  return (
    hasMoney(company.costUsd, quote) ||
    hasFeeValue(companyUnusedValue(company), company.current, quote) ||
    (company.chartAvailable && company.parts.some((part) => part.value > 0)) ||
    company.accounts.some((account) => hasAccountInformation(account, company.current, quote, now))
  );
}
export function hasPersonInformation(
  person: PersonDetail,
  fees: UnusedFeeValue,
  current: boolean,
  quote: CrossQuote | null
) {
  const hasChange =
    person.previous.costUsd > 0 && person.current.unpriced + person.previous.unpriced === 0;
  return hasMoney(person.current.costUsd, quote) || hasChange || hasFeeValue(fees, current, quote);
}
