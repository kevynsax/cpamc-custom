import { createHash } from 'node:crypto';
import reference from '../../../internal/usagehistory/model_reference.json';
import { demoPeople } from './people';

// Identities and usage are fake; provider/plan types and model references mirror
// the server's 2026-10-17 inventory. Prices come from the cited public sources.
export const subscriptionLineup = [
  { provider: 'antigravity', label: 'Antigravity', plan: '', model: 'gemini-3.1-flash-image' },
  { provider: 'claude', label: 'Claude Max 20×', plan: 'Claude Max 20×', model: 'claude-opus-4-8' },
  { provider: 'codex', label: 'Codex Pro 100', plan: 'Pro 100', model: 'gpt-6.1-sol' },
  { provider: 'meta', label: 'Meta', plan: '', model: 'muse-spark-1.3-contributor' },
  { provider: 'minimax', label: 'MiniMax', plan: '', model: 'MiniMax-M3' },
  { provider: 'xai', label: 'Grok Pro 1', plan: 'Grok Pro', model: 'grok-4.7' },
  { provider: 'xai', label: 'Grok Pro 2', plan: 'Grok Pro', model: 'grok-4.7' },
  { provider: 'zai', label: 'Z.ai', plan: '', model: 'glm-5.3' },
] as const;
const DAY = 86_400_000,
  WEEK = 7 * DAY;
const hash = (value: string) =>
  createHash('sha256').update(`local-usage-demo:${value}`).digest('hex');
const iso = (value: number) => new Date(value).toISOString();
interface DemoRates {
  input: number;
  output: number;
  cache_read?: number;
  cache_write?: number;
  context_over?: number;
  context_inclusive?: boolean;
  long_context?: DemoRates;
}
interface RawRow {
  date: string;
  at: string;
  model: string;
  key_id: string;
  account_id: string;
  provider: string;
  auth_type: string;
  requests: number;
  failed: number;
  input_tokens: number;
  output_tokens: number;
  cached_tokens: number;
  total_tokens: number;
  cost_usd: number | null;
  unpriced_requests: number;
}
interface RawPerson {
  key_id: string;
  cost_usd: number;
  unpriced: number;
  requests: number;
  percent: number;
}
interface RawWeek {
  id: string;
  account_id: string;
  provider: string;
  scope: string;
  period_seconds: number;
  plan: string;
  start: string;
  reset: string;
  people: RawPerson[];
  used: number;
  unattributed: number;
  remaining: number;
  unused_upper_bound: number | null;
  last_observed: string;
  incomplete: boolean;
  closed: boolean;
  allocation_available: boolean;
}
export type DemoSettings = ReturnType<typeof createUsageFixture>['settings'];

export function createUsageFixture(now = new Date()) {
  const time = now.getTime(),
    today = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate());
  const settings = {
    names: {} as Record<string, string>,
    plans: {} as Record<string, string>,
    prices: {} as Record<string, DemoRates>,
    unit_prices: {} as Record<string, Record<string, number>>,
  };
  const payload = {
    from: iso(today - 364 * DAY).slice(0, 10),
    to: iso(today).slice(0, 10),
    timezone: 'UTC',
    retention_days: 365,
    collection_enabled: true,
    prices_as_of: reference.checked_at,
    accounts: {} as Record<string, { id: string; provider: string; plan: string; email: string }>,

    account_labels: {} as Record<string, string>,
    key_labels: {} as Record<string, string>,
    models: reference.models.map((model) => ({ ...model, overridden: false })),
    settings,
    rows: [] as RawRow[],
    weeks: [] as RawWeek[],
  };
  demoPeople.forEach((person, index) => {
    payload.key_labels[hash(person.name)] = `••••${1001 + index}`;
  });
  subscriptionLineup.forEach((subscription, accountIndex) => {
    const accountId = hash(`account-${accountIndex}`);
    payload.accounts[accountId] = {
      id: accountId,
      provider: subscription.provider,
      plan: subscription.plan,
      email: `${subscription.provider}-${accountIndex + 1}@example.test`,
    };
    payload.account_labels[accountId] = subscription.label;
    const scopes =
      subscription.provider === 'minimax'
        ? ['MiniMax-M3', 'MiniMax-M3.1-Flash-Preview']
        : ['account'];
    scopes.forEach((scope, scopeIndex) => {
      const model = scope === 'account' ? subscription.model : scope;
      const entry = reference.models.find((item) => item.id === model);
      const prices = (entry?.prices ?? {}) as Record<string, number>;
      const priced = prices.input !== undefined && prices.output !== undefined;
      for (let weekIndex = 0; weekIndex < 12; weekIndex++) {
        const reset = today + (1 + (accountIndex % 5)) * DAY + 18 * 3_600_000 - weekIndex * WEEK,
          start = reset - WEEK;
        const used = 32 + ((accountIndex * 13 + scopeIndex * 7 + weekIndex * 17) % 62);
        const baseline = 0;
        const people: RawPerson[] = demoPeople.map(({ name }, personIndex) => {
          const weight = [1.5, 1.05, 0.75, 0.5][(personIndex + accountIndex) % 4];
          const uncached = Math.round(
            (2_800_000 + accountIndex * 340_000) *
              weight *
              (0.75 + (weekIndex % 4) * 0.13) *
              (scopeIndex ? 0.55 : 1)
          );
          const cached = prices.cache_read === undefined ? 0 : Math.round(uncached * 0.6),
            output = Math.round(uncached * 0.18);
          const unpriced = !priced || (accountIndex === 7 && personIndex === 3 && weekIndex === 0);
          const cost = priced
            ? (uncached * prices.input +
                cached * (prices.cache_read ?? 0) +
                output * prices.output) /
              1e6
            : 0;
          const at =
            weekIndex === 0
              ? Math.min(time - 2 * 3_600_000, start + (personIndex + 1) * DAY)
              : start + (personIndex + 1) * DAY;
          const requests = Math.round(180 * weight + accountIndex * 17);
          payload.rows.push({
            date: iso(at).slice(0, 10),
            at: iso(at),
            model,
            key_id: hash(name),
            account_id: accountId,
            provider: subscription.provider,
            auth_type: ['zai', 'minimax'].includes(subscription.provider) ? 'apikey' : 'oauth',
            requests,
            failed: personIndex,
            input_tokens: subscription.provider === 'claude' ? uncached : uncached + cached,
            output_tokens: output,
            cached_tokens: cached,
            total_tokens: uncached + cached + output,
            cost_usd: unpriced ? null : cost,
            unpriced_requests: unpriced ? requests : 0,
          });
          return {
            key_id: hash(name),
            cost_usd: unpriced ? 0 : cost,
            unpriced: unpriced ? requests : 0,
            requests,
            percent: 0,
          };
        });
        const total = people.reduce((sum, person) => sum + person.cost_usd, 0);
        people.forEach((person) => {
          person.percent = total > 0 ? ((used - baseline) * person.cost_usd) / total : 0;
        });
        if (subscription.provider === 'antigravity') continue;
        payload.weeks.push({
          id: `${accountId}|${scope}|${iso(reset)}`,
          account_id: accountId,
          provider: subscription.provider,
          scope,
          period_seconds: WEEK / 1000,
          plan: subscription.plan,
          start: iso(start),
          reset: iso(reset),
          people,
          used,
          unattributed: total > 0 ? baseline : used,
          remaining: 100 - used,
          unused_upper_bound:
            weekIndex === 0 || (accountIndex === 6 && weekIndex === 2) ? null : 100 - used,
          last_observed: iso(weekIndex === 0 ? time - 3 * 60_000 : reset - 5 * 60_000),
          incomplete: accountIndex >= 6 || !priced,
          closed: weekIndex > 0,
          allocation_available: total > 0,
        });
      }
    });
  });
  const templates = payload.weeks.filter(
    (cycle) => !cycle.closed && ['claude', 'meta', 'minimax', 'zai'].includes(cycle.provider)
  );
  for (const template of templates) {
    const period = 5 * 3_600_000;
    for (let index = 0; index < (45 * 24) / 5; index++) {
      const reset = time + 2 * 3_600_000 - index * period,
        used = 20 + ((index * 11) % 72);
      const people = template.people.map((person) => ({
        ...person,
        cost_usd: (person.cost_usd * period) / WEEK,
        percent: template.allocation_available
          ? (person.percent * used) / Math.max(1, template.used - template.unattributed)
          : 0,
      }));
      payload.weeks.push({
        ...template,
        id: `${template.account_id}|${template.scope}|5h|${iso(reset)}`,
        period_seconds: period / 1000,
        start: iso(reset - period),
        reset: iso(reset),
        people,
        used,
        remaining: 100 - used,
        unattributed: template.allocation_available ? 0 : used,
        closed: index > 0,
        unused_upper_bound: index > 0 ? 100 - used : null,
        last_observed: iso(index === 0 ? time - 60_000 : reset - 2 * 60_000),
      });
    }
  }
  payload.rows = payload.rows.filter((row) => Date.parse(row.at) <= time);
  return payload;
}
