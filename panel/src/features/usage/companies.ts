import type { ClientUsageResponse, UsageAccount, UsageWeek } from '@/services/api/clientUsage';
import { monthlyPlanFee, proratedPlanFee } from './fees';

export type UsagePeriod = 'week' | 'month';
export interface PiePart {
  id: string;
  value: number;
  costUsd: number;
  unpriced: number;
}
export interface AccountCapacity {
  id: string;
  email: string;
  periodFeeUsd: number | null;
  quotaMeasured: boolean;
  chartAvailable: boolean;
  used: number;
  unused: number;
  unknown: number;
  unidentified: number;
  remaining: number;
  expired: number;
}
export interface CompanyUsage {
  id: string;
  accounts: AccountCapacity[];
  remaining: number;
  expired: number;
  provider: string;
  parts: PiePart[];
  costUsd: number;
  unpriced: number;
  incomplete: boolean;
  weightEstimated: boolean;
  chartAvailable: boolean;
  current: boolean;
}
const DAY = 86_400_000;
const WEEK = 7 * DAY;
export const COMPANY_NAMES: Record<string, string> = {
  anthropic: 'Anthropic',
  openai: 'OpenAI',
  google: 'Google',
  xai: 'xAI',
  meta: 'Meta',
  minimax: 'MiniMax',
  zai: 'Z.ai',
};
export function companyId(provider: string): string {
  const name = provider.toLowerCase().replace(/^openai-compatib(?:le|ility)-/, '');
  return (
    (
      {
        claude: 'anthropic',
        codex: 'openai',
        antigravity: 'google',
        gemini: 'google',
        grok: 'xai',
      } as Record<string, string>
    )[name] ?? name
  );
}

// Published Claude multipliers outrank price ratios. Unpublished plan sizes use
// the explicitly requested price-proportional proxy, never a claimed token count.
export function planWeight(provider: string, plan: string): { weight: number; estimated: boolean } {
  const key = plan.toLowerCase().replace(/[^a-z0-9]/g, '');
  if (companyId(provider) === 'anthropic') {
    if (key.includes('max20') || key.includes('max200')) return { weight: 20, estimated: false };
    if (key.includes('max5') || key.includes('max100')) return { weight: 5, estimated: false };
    if (key === 'pro' || key.includes('claudepro')) return { weight: 1, estimated: false };
  }
  if (companyId(provider) === 'openai') {
    if (key === 'prolite' || key.includes('pro100')) return { weight: 5, estimated: false };
    if (key.includes('pro500')) return { weight: 25, estimated: true };
    if (key === 'pro' || key.includes('pro200')) return { weight: 10, estimated: true };
    if (key === 'plus' || key === 'team') return { weight: 1, estimated: false };
  }
  return { weight: 1, estimated: true };
}

export function usageRange(period: UsagePeriod, offset: number, now = new Date()) {
  const today = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate());
  const start =
    period === 'month'
      ? Date.UTC(now.getUTCFullYear(), now.getUTCMonth() - offset, 1)
      : today - (14 + offset * 7) * DAY;
  const end =
    period === 'month'
      ? Math.min(now.getTime(), Date.UTC(now.getUTCFullYear(), now.getUTCMonth() - offset + 1, 1))
      : now.getTime() - offset * WEEK;
  return {
    from: new Date(Math.max(start, today - 364 * DAY)).toISOString().slice(0, 10),
    to: new Date(Math.max(start, end)).toISOString().slice(0, 10),
  };
}

function accountRange(
  account: UsageAccount,
  weeks: UsageWeek[],
  period: UsagePeriod,
  offset: number,
  now: Date
) {
  if (period === 'month')
    return {
      start: Date.UTC(now.getUTCFullYear(), now.getUTCMonth() - offset, 1),
      end: Date.UTC(now.getUTCFullYear(), now.getUTCMonth() - offset + 1, 1),
    };
  const primary = weeks
    .filter(
      (week) =>
        week.accountId === account.id &&
        Math.abs(Date.parse(week.reset) - Date.parse(week.start) - WEEK) < 1000
    )
    .sort(
      (a, b) =>
        Number(b.scope === 'account') - Number(a.scope === 'account') ||
        Date.parse(b.reset) - Date.parse(a.reset)
    )[0];
  if (primary) {
    let reset = Date.parse(primary.reset);
    while (reset <= now.getTime()) reset += WEEK;
    return { start: reset - (offset + 1) * WEEK, end: reset - offset * WEEK };
  }
  return { start: now.getTime() - (offset + 1) * WEEK, end: now.getTime() - offset * WEEK };
}

export function buildCompanyUsage(
  data: ClientUsageResponse,
  period: UsagePeriod,
  offset = 0,
  now = new Date()
): CompanyUsage[] {
  const companies = new Map<string, CompanyUsage>();
  for (const account of data.accounts) {
    const id = companyId(account.provider);
    const company = companies.get(id) ?? {
      id,
      provider: account.provider,
      accounts: [],
      remaining: 0,
      expired: 0,
      parts: [],
      costUsd: 0,
      unpriced: 0,
      incomplete: false,
      weightEstimated: false,
      chartAvailable: true,
      current: offset === 0,
    };
    const range = accountRange(account, data.weeks, period, offset, now);
    const accountWeeks = data.weeks.filter(
      (week) =>
        week.accountId === account.id &&
        Date.parse(week.start) < Math.min(range.end, now.getTime()) &&
        Date.parse(week.reset) > range.start
    );
    const plan = account.plan || accountWeeks.find((week) => week.plan)?.plan || '';
    const weight = planWeight(account.provider, plan);
    company.weightEstimated ||= weight.estimated;
    const families = new Map<string, UsageWeek[]>();
    for (const cycle of accountWeeks) {
      const duration = Date.parse(cycle.reset) - Date.parse(cycle.start);
      const family = JSON.stringify([cycle.scope, duration]);
      families.set(family, [...(families.get(family) ?? []), cycle]);
    }
    const normalized = new Map<string, number>();
    for (const cycles of families.values()) {
      let budget = 0;
      const slices = new Map<string, number>();
      const add = (key: string, value: number) => slices.set(key, (slices.get(key) ?? 0) + value);
      for (const cycle of cycles) {
        const start = Date.parse(cycle.start),
          end = Date.parse(cycle.reset);
        const overlap =
          Math.max(0, Math.min(end, range.end) - Math.max(start, range.start)) / (end - start);
        if (!Number.isFinite(overlap) || overlap <= 0) continue;
        budget += overlap;
        const shares = cycle.allocationAvailable
          ? cycle.people.reduce((sum, person) => sum + person.percent, 0)
          : 0;
        const scale = shares > cycle.used && shares > 0 ? cycle.used / shares : 1;
        if (cycle.allocationAvailable)
          for (const person of cycle.people)
            add(person.keyId, (overlap * person.percent * scale) / 100);
        const allocated = Math.min(cycle.used, shares);
        add('unidentified', (overlap * Math.max(0, cycle.used - allocated)) / 100);
        if (cycle.closed) {
          if (cycle.unusedUpperBound === null) {
            add('unknown', (overlap * (100 - cycle.used)) / 100);
            company.incomplete = true;
          } else add('expired', (overlap * cycle.unusedUpperBound) / 100);
        } else add('remaining', (overlap * (100 - cycle.used)) / 100);
        company.incomplete ||= cycle.incomplete;
      }
      if (budget > 0)
        for (const [key, value] of slices)
          normalized.set(key, (normalized.get(key) ?? 0) + value / budget / families.size);
    }
    if (families.size === 0) {
      normalized.set('unknown', 1);
      company.incomplete = true;
    }
    let accountTotal = [...normalized.values()].reduce((sum, value) => sum + value, 0);
    if (!(accountTotal > 0)) {
      normalized.clear();
      normalized.set('unknown', 1);
      accountTotal = 1;
      company.incomplete = true;
    }
    const accountPercent = (key: string) => (100 * (normalized.get(key) ?? 0)) / accountTotal;
    const remaining = accountPercent('remaining'),
      expired = accountPercent('expired'),
      unknown = accountPercent('unknown');
    const quotaMeasured =
      accountWeeks.length > 0 && unknown < 1e-8 && !accountWeeks.some((cycle) => cycle.incomplete);
    const chartAvailable = quotaMeasured && accountPercent('unidentified') < 1e-8;
    company.chartAvailable &&= chartAvailable;
    company.accounts.push({
      id: account.id,
      email: account.email ?? '',
      periodFeeUsd: proratedPlanFee(
        account.monthlyFeeUsd ?? monthlyPlanFee(id, plan),
        range.start,
        range.end
      ),
      quotaMeasured,
      chartAvailable,
      used: Math.max(0, 100 - remaining - expired - unknown),
      unused: remaining + expired,
      unknown,
      unidentified: accountPercent('unidentified'),
      remaining,
      expired,
    });
    const addPart = (key: string, value: number) => {
      let part = company.parts.find((entry) => entry.id === key);
      if (!part) {
        part = { id: key, value: 0, costUsd: 0, unpriced: 0 };
        company.parts.push(part);
      }
      part.value += value;
      return part;
    };
    for (const [key, value] of normalized) addPart(key, (value * weight.weight) / accountTotal);
    for (const row of data.rows) {
      if (row.accountId !== account.id) continue;
      const at = Date.parse(row.at || row.date);
      if (!Number.isFinite(at) || at < range.start || at >= Math.min(range.end, now.getTime() + 1))
        continue;
      company.costUsd += row.costUsd ?? 0;
      company.unpriced += row.unpriced;
      const person = addPart(row.keyId, 0);
      person.costUsd += row.costUsd ?? 0;
      person.unpriced += row.unpriced;
    }
    companies.set(id, company);
  }
  return [...companies.values()]
    .map((company) => {
      const total = company.parts.reduce((sum, part) => sum + part.value, 0);
      const normalized = company.parts.map((part) => ({
        ...part,
        value: total > 0 ? (100 * part.value) / total : 0,
      }));
      company.remaining = normalized.find((part) => part.id === 'remaining')?.value ?? 0;
      company.expired = normalized.find((part) => part.id === 'expired')?.value ?? 0;
      company.parts = normalized.filter((part) => !['remaining', 'expired'].includes(part.id));
      if (company.remaining > 1e-8)
        company.parts.push({ id: 'available', value: company.remaining, costUsd: 0, unpriced: 0 });
      if (company.expired > 1e-8)
        company.parts.push({ id: 'wasted', value: company.expired, costUsd: 0, unpriced: 0 });
      company.parts = company.parts.filter(
        (part) => part.value > 1e-8 || part.costUsd > 0 || part.unpriced > 0
      );
      company.accounts.sort((a, b) => a.id.localeCompare(b.id));
      return company;
    })
    .sort((a, b) => (COMPANY_NAMES[a.id] ?? a.id).localeCompare(COMPANY_NAMES[b.id] ?? b.id));
}
