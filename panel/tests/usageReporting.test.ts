import { beforeAll, describe, expect, test } from 'bun:test';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { createInstance } from 'i18next';
import { I18nextProvider } from 'react-i18next';
import en from '../src/i18n/locales/en.json';
import type { ClientUsageResponse, ClientUsageRow } from '../src/services/api/clientUsage';
import { normalizeClientUsage } from '../src/services/api/clientUsage';
import { buildCompanyUsage, companyId } from '../src/features/usage/companies';
import {
  periodRows,
  personDetails,
  tokensByModel,
  valueByModel,
  quotaIsFresh,
  percentChange,
} from '../src/features/usage/reporting';
import { latestQuotaSnapshots } from '../src/features/usage/quotaSnapshots';
import { PersonDetails } from '../src/features/usage/PersonDetails';
import { ModelValuePie } from '../src/features/usage/ModelValuePie';
import { CompanyPies } from '../src/features/usage/CompanyPies';
import { FeeTags } from '../src/features/usage/FeeTags';
import { withInHousePrices } from '../src/features/usage/inHousePrices';
import { SubscriptionFacts } from '../src/features/usage/SubscriptionFacts';
import { createUsageFixture } from '../dev/usage/fixtures';
import { normalizeKeyNames } from '../src/services/api/keyNames';

const now = new Date('2026-10-17T12:00:00Z');
const row = (at: string, extra: Partial<ClientUsageRow> = {}): ClientUsageRow => ({
  at,
  date: at.slice(0, 10),
  model: 'model-a',
  keyId: 'person-a',
  accountId: 'one',
  provider: 'codex',
  authType: 'oauth',
  requests: 1,
  failed: 0,
  inputTokens: 80,
  cachedTokens: 40,
  outputTokens: 20,
  totalTokens: 100,
  costUsd: 3,
  unpriced: 0,
  ...extra,
});
const data = (rows: ClientUsageRow[]): ClientUsageResponse => ({
  collectionEnabled: false,
  rows,
  accountLabels: {},
  keyLabels: {},
  accounts: [{ id: 'one', provider: 'codex', plan: 'pro' }],
  weeks: [],
  settings: { names: {}, plans: {}, prices: {} },
  pricesAsOf: '',
});
const i18n = createInstance();
beforeAll(async () => {
  await i18n.init({ lng: 'en', resources: { en: { translation: en } } });
});
const classes = new Proxy({} as Record<string, string>, { get: (_, key) => String(key) });
function html(element: React.ReactNode) {
  return renderToStaticMarkup(createElement(I18nextProvider, { i18n }, element));
}

describe('usage reporting', () => {
  test('compares the same elapsed month without including later prior-period usage', () => {
    const source = data([
      row('2026-10-01T00:00:00Z'),
      row('2026-10-17T11:00:00Z'),
      row('2026-10-17T13:00:00Z'),
      row('2026-09-01T00:00:00Z'),
      row('2026-09-17T11:00:00Z'),
      row('2026-09-17T13:00:00Z'),
    ]);
    const selected = periodRows(source, 'month', 0, now);
    expect(selected.current).toHaveLength(2);
    expect(selected.previous).toHaveLength(2);
    expect(personDetails(selected.current, selected.previous)[0].current.tokens).toBe(200);
    expect(source.rows).toHaveLength(6);
  });
  test('historical periods use the complete previous period; short months are clamped', () => {
    const source = data([row('2026-09-30T23:00:00Z'), row('2026-08-31T23:00:00Z')]);
    const selected = periodRows(source, 'month', 1, now);
    expect(selected.current).toHaveLength(1);
    expect(selected.previous).toHaveLength(1);
    const short = periodRows(
      data([row('2026-02-28T23:59:00Z'), row('2026-03-01T00:00:00Z')]),
      'month',
      0,
      new Date('2026-03-31T12:00:00Z')
    );
    expect(short.previous).toHaveLength(1);
    expect(short.current).toHaveLength(1);
  });
  test('reporting weeks share Thursday 20:00 UTC-3 and compare only elapsed time', () => {
    const source = normalizeClientUsage(createUsageFixture(now));
    const selected = periodRows(source, 'week', 0, now);
    const start = Date.parse('2026-10-15T23:00:00Z');
    const previousStart = start - 7 * 86400000;
    expect(selected.current.length).toBeGreaterThan(0);
    for (const current of selected.current) {
      expect(Date.parse(current.at)).toBeGreaterThanOrEqual(start);
      expect(Date.parse(current.at)).toBeLessThanOrEqual(now.getTime());
    }
    for (const previous of selected.previous) {
      expect(Date.parse(previous.at)).toBeGreaterThanOrEqual(previousStart);
      expect(Date.parse(previous.at)).toBeLessThanOrEqual(previousStart + now.getTime() - start);
    }
    expect(periodRows({ ...source, weeks: [] }, 'week', 0, now)).toEqual(selected);
  });
  test('groups all three in-house providers without merging people or double-counting cache tokens', () => {
    for (const provider of ['macbook', 'home-gpu', 'spark', 'openai-compatibility-spark'])
      expect(companyId(provider)).toBe('in_house');
    const source = data(
      ['macbook', 'home-gpu', 'spark'].map((provider, index) =>
        row('2026-10-15T12:00:00Z', {
          accountId: String(index),
          provider: 'openai-compatibility',
          model: `model-${index}`,
          costUsd: null,
          unpriced: 1,
        })
      )
    );
    source.accounts = ['macbook', 'home-gpu', 'spark'].map((provider, index) => ({
      id: String(index),
      provider,
      plan: '',
    }));
    const companies = buildCompanyUsage(source, 'month', 0, now);
    expect(companies).toHaveLength(1);
    expect(companies[0].accounts).toHaveLength(3);
    const selected = periodRows(source, 'month', 0, now);
    const people = personDetails(selected.current, []);
    expect(people).toHaveLength(1);
    expect(people[0].current.tokens).toBe(300);
    expect(people[0].current.unpriced).toBe(3);
    expect(people[0].companies.map((company) => company.id)).toEqual(['in_house']);
    expect(
      tokensByModel(selected.current, 'in_house').reduce((sum, part) => sum + part.value, 0)
    ).toBe(300);
  });
  test('preserves model/person/company totals and missing prices, including disappeared people', () => {
    const current = [
      row('2026-10-15T12:00:00Z'),
      row('2026-10-15T12:00:00Z', {
        provider: 'claude',
        model: 'model-b',
        costUsd: null,
        unpriced: 1,
      }),
    ];
    const previous = [row('2026-09-15T12:00:00Z', { keyId: 'person-b', costUsd: 4 })];
    const people = personDetails(current, previous);
    expect(people).toHaveLength(2);
    expect(people[0].companies.reduce((sum, item) => sum + item.current.tokens, 0)).toBe(200);
    expect(people[0].models.reduce((sum, item) => sum + item.current.costUsd, 0)).toBe(3);
    expect(people[1].current.tokens).toBe(0);
    expect(people[1].previous.tokens).toBe(100);
    expect(percentChange(150, 100)).toBe(50);
    expect(percentChange(0, 100)).toBe(-100);
    expect(percentChange(100, 0)).toBeNull();
  });
  test('cached availability requires an unexpired observation no older than 30 minutes', () => {
    const quota = {
      scope: 'account',
      used: 40,
      reset: '2026-10-17T13:00:00Z',
      observedAt: '2026-10-17T11:50:00Z',
      periodSeconds: 18000,
    };
    expect(quotaIsFresh(quota, now.getTime())).toBe(true);
    expect(quotaIsFresh({ ...quota, observedAt: '2026-10-17T11:00:00Z' }, now.getTime())).toBe(
      false
    );
    expect(quotaIsFresh({ ...quota, reset: '2026-10-17T11:59:00Z' }, now.getTime())).toBe(false);
    expect(quotaIsFresh({ ...quota, observedAt: 'invalid' }, now.getTime())).toBe(false);
    const latest = latestQuotaSnapshots([
      quota,
      { ...quota, used: 50, observedAt: '2026-10-17T11:55:00Z', reset: '2026-10-17T13:00:01Z' },
    ]);
    expect(latest).toHaveLength(1);
    expect(latest[0].used).toBe(50);
  });
  test('renders concise reais-only fees, price pies, and person details without empty explanations', () => {
    const facts = html(
      createElement(SubscriptionFacts, {
        account: {
          id: 'one',
          email: '',
          monthlyFeeUsd: 200,
          periodFeeUsd: 45,
          quotaMeasured: false,
          chartAvailable: false,
          used: 0,
          unused: 0,
          unknown: 100,
          unidentified: 0,
          remaining: 0,
          expired: 0,
        },
        quote: { rate: 5, observedAt: Date.now() },
        current: true,
        classes,
      })
    );
    expect(facts).toContain('R$');
    expect(facts).toContain('1.000,00');
    expect(facts).not.toContain('$200.00');
    expect(facts).not.toContain('availability unknown');
    expect(facts).not.toContain('100%');
    const pie = html(
      createElement(ModelValuePie, {
        rows: [row('2026-10-15T12:00:00Z')],
        company: 'openai',
        quote: { rate: 5, observedAt: Date.now() },
        classes,
      })
    );
    expect(pie).toContain('15,00');
    expect(pie).not.toContain('100 tokens');
    expect(pie).toContain('tabindex="0"');
    expect(pie).toContain('Models');
    const person = html(
      createElement(PersonDetails, {
        current: [row('2026-10-15T12:00:00Z')],
        previous: [],
        names: { 'person-a': 'Alice' },
        quote: { rate: 5, observedAt: Date.now() },
        classes,
      })
    );
    expect(person).toContain('Alice');
    expect(person).not.toContain('Tracking paused');
    expect(person).not.toContain('No previous usage recorded');
    expect(person).not.toContain('Observed usage only');
    expect(person).toContain('Models');
    expect((person.match(/data-value-pie=/g) ?? []).length).toBe(1);
    expect(person).toContain('Model A');
    expect(person).not.toContain('tokens');
    expect(person).not.toContain('Requests');
    expect(person).toContain('data-value-pie=');
    expect(person).not.toContain('Infinity');
  });
  test('a Spark reference estimate makes the In house money chart visible without quota tracking', () => {
    const fixture = createUsageFixture(now);
    const original = {
      ...fixture.rows[0],
      at: now.toISOString(),
      date: now.toISOString().slice(0, 10),
      provider: 'openai-compatible-spark',
      model: '/models/Qwen3-VL-30B-A3B-Instruct-Q4_K_M.gguf',
      model_alias: 'qwen-vl',
      cost_usd: null,
      reference_cost_usd: 0.00027,
      requests: 1,
      unpriced_requests: 1,
      failed: 0,
      input_tokens: 1000,
      cached_tokens: 800,
      output_tokens: 200,
      total_tokens: 1200,
    };
    const data = normalizeClientUsage({
      ...fixture,
      rows: [original],
      weeks: [],
      accounts: { [original.account_id]: { provider: 'spark', plan: '', email: '' } },
    });
    const companies = buildCompanyUsage(data, 'month', 0, now);
    const selected = periodRows(data, 'month', 0, now);
    expect(companies[0].id).toBe('in_house');
    expect(companies[0].costUsd).toBe(0.00027);
    expect(companies[0].chartAvailable).toBe(false);
    const rendered = html(
      createElement(CompanyPies, {
        data,
        companies,
        rows: selected.current,
        names: {},
        quote: { rate: 5, observedAt: Date.now() },
        icons: {},
        classes,
      })
    );
    expect(rendered).toContain('In house');
    expect(rendered).toContain('data-model-value-chart="in_house"');
    expect(rendered).toContain('Qwen 3 VL 30B A3B');
    expect(rendered).toContain('<svg');
    expect(rendered).toContain('R$');
    expect(original.cost_usd).toBeNull();
    expect(original.unpriced_requests).toBe(1);
  });
  test('price pies are weighted by cost, not token count, and never reprice unknown rows', () => {
    const rows = [
      row('2026-10-15T12:00:00Z', { model: 'expensive', totalTokens: 10, costUsd: 9 }),
      row('2026-10-15T12:00:00Z', { model: 'cheap', totalTokens: 990, costUsd: 1 }),
      row('2026-10-15T12:00:00Z', { model: 'unknown', costUsd: null, unpriced: 1 }),
    ];
    expect(valueByModel(rows, 'openai')).toEqual([
      { id: 'expensive', value: 9, unpriced: 0 },
      { id: 'cheap', value: 1, unpriced: 0 },
    ]);
    const pie = html(
      createElement(ModelValuePie, {
        rows,
        company: 'openai',
        quote: { rate: 5, observedAt: Date.now() },
        classes,
      })
    );
    expect(pie).toContain('90%');
    expect(pie).not.toContain('≥');
    expect(pie).not.toContain('990');
    expect(
      html(createElement(ModelValuePie, { rows: [], company: 'openai', quote: null, classes }))
    ).not.toContain('<svg');
  });
  test('wasted and available slices only show their own fee, without unknown or zero tags', () => {
    const props = {
      value: { wasted: { usd: 10, partial: false }, available: { usd: 20, partial: false } },
      current: true,
      quote: { rate: 5, observedAt: Date.now() },
      classes,
    };
    const wasted = html(createElement(FeeTags, { ...props, kind: 'wasted' }));
    expect(wasted).toContain('wasted');
    expect(wasted).not.toContain('for grabs');
    const available = html(createElement(FeeTags, { ...props, kind: 'available' }));
    expect(available).toContain('for grabs');
    expect(available).not.toContain('wasted');
    expect(
      html(
        createElement(FeeTags, {
          ...props,
          value: { wasted: { usd: null, partial: false }, available: { usd: 0, partial: false } },
        })
      )
    ).toBe('');
  });
  test('local preview adds sourced OpenRouter cards without altering history or overrides', () => {
    const source = normalizeClientUsage(createUsageFixture(now));
    const model = source.models!.find((model) => model.id === 'qwen-coder')!;
    model.status = 'self_hosted';
    model.prices = {};
    const before = JSON.stringify(source.rows);
    const updated = withInHousePrices(source);
    const card = updated.models!.find((model) => model.id === 'qwen-coder')!;
    expect(card.prices.input).toBe(0.24);
    expect(card.sources[0]).toBe('https://openrouter.ai/qwen/qwen3.8-27b');
    expect(JSON.stringify(updated.rows)).toBe(before);
    expect(source.models!.find((model) => model.id === 'qwen-coder')!.prices).toEqual({});
    model.overridden = true;
    model.prices = { input: 0, output: 0 };
    expect(
      withInHousePrices(source).models!.find((model) => model.id === 'qwen-coder')!.prices.input
    ).toBe(0);
  });
  test('normalizes only fingerprinted server names, including clear tombstones', () => {
    const key = 'a'.repeat(64);
    expect(normalizeKeyNames({ names: { [key]: '' } })).toEqual({ [key]: '' });
    for (const value of [
      null,
      {},
      { names: [] },
      { names: { 'raw-key': 'name' } },
      { names: { [key]: 4 } },
    ])
      expect(() => normalizeKeyNames(value)).toThrow();
  });
});
