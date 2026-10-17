import { beforeAll, describe, expect, test } from 'bun:test';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { createInstance } from 'i18next';
import { I18nextProvider } from 'react-i18next';
import en from '../src/i18n/locales/en.json';
import { normalizeClientUsage, type UsageWeek } from '../src/services/api/clientUsage';
import { buildCompanyUsage, planWeight, usageRange } from '../src/features/usage/companies';
import { CompanyPies } from '../src/features/usage/CompanyPies';
import { parseCrossQuote, formatReais } from '../src/features/usage/currency';
import { resolveUsageKeyNames } from '../src/features/usage/keyNames';
import { apiKeyNameFingerprint } from '../src/features/config/apiKeyNames';
import { priceFields, settingsWithModelPrice } from '../src/features/usage/modelPrices';
import { piePath, donutPath, donutSegments } from '../src/features/usage/pie';
import { PeopleSpend } from '../src/features/usage/PeopleSpend';
import { spendingByPerson } from '../src/features/usage/spending';
import { createUsageFixture } from '../dev/usage/fixtures';
import { demoPeople } from '../dev/usage/people';
import { usageDemoPlugin, validDemoSettings } from '../dev/usage/plugin';

const now = new Date('2026-10-17T12:00:00Z');
const fixture = () => normalizeClientUsage(createUsageFixture(now));
const i18n = createInstance();
beforeAll(async () => {
  await i18n.init({ lng: 'en', resources: { en: { translation: en } } });
});

function cycle(
  id: string,
  accountId: string,
  start: string,
  reset: string,
  used: number,
  closed: boolean
): UsageWeek {
  return {
    id,
    accountId,
    provider: 'claude',
    scope: 'account',
    plan: '',
    start,
    reset,
    used,
    remaining: 100 - used,
    people: [{ keyId: 'person', costUsd: 999, unpriced: 0, requests: 1, percent: used }],
    unattributed: 0,
    unusedUpperBound: closed ? 100 - used : null,
    lastObserved: reset,
    incomplete: false,
    closed,
    allocationAvailable: true,
  };
}

describe('company capacity pies', () => {
  test('merges eight subscriptions into seven companies, averaging independent quotas', () => {
    const data = fixture();
    const companies = buildCompanyUsage(data, 'month', 0, now);
    expect(data.accounts).toHaveLength(8);
    expect(companies).toHaveLength(7);
    expect(companies.filter((company) => company.id === 'xai')).toHaveLength(1);
    for (const company of companies)
      expect(company.parts.reduce((sum, part) => sum + part.value, 0)).toBeCloseTo(100, 8);
    expect(companies.some((company) => company.expired > 0)).toBe(true);
    expect(companies.flatMap((company) => company.accounts)).toHaveLength(8);
    for (const company of companies) {
      expect(
        (company.parts.find((part) => part.id === 'available')?.value ?? 0) +
          (company.parts.find((part) => part.id === 'wasted')?.value ?? 0)
      ).toBeCloseTo(company.remaining + company.expired, 8);
      for (const account of company.accounts)
        expect(account.used + account.unused + account.unknown).toBeCloseTo(100, 8);
    }
    expect(
      companies
        .find((company) => company.id === 'google')
        ?.parts.find((part) => part.id === 'unknown')?.value
    ).toBe(100);
  });
  test('uses published Claude multipliers, and marks monetary proxies as assumptions', () => {
    expect(planWeight('claude', 'default_claude_max_20x')).toEqual({
      weight: 20,
      estimated: false,
    });
    expect(planWeight('claude', 'Max 5x').weight).toBe(5);
    expect(planWeight('codex', 'prolite').weight).toBe(5);
    expect(planWeight('codex', 'Pro 500').weight / planWeight('codex', 'Pro 200').weight).toBe(2.5);
    expect(planWeight('codex', 'Pro 500').estimated).toBe(true);
    const data = fixture();
    data.accounts = [
      { id: 'max', provider: 'claude', plan: 'Max 20x' },
      { id: 'pro', provider: 'claude', plan: 'Pro' },
    ];
    data.rows = [];
    data.weeks = [
      cycle('max', 'max', '2026-10-12T00:00:00Z', '2026-10-19T00:00:00Z', 100, false),
      cycle('pro', 'pro', '2026-10-12T00:00:00Z', '2026-10-19T00:00:00Z', 0, false),
    ];
    const company = buildCompanyUsage(data, 'week', 0, now)[0];
    expect(company.parts.find((part) => part.id === 'person')?.value).toBeCloseTo(
      (100 * 20) / 21,
      8
    );
  });
  test('month boundaries prorate quota cycles, not actual request spending', () => {
    const data = fixture();
    data.accounts = [{ id: 'account', provider: 'claude', plan: 'Pro' }];
    data.weeks = [
      cycle('first', 'account', '2026-09-28T00:00:00Z', '2026-10-05T00:00:00Z', 100, true),
      cycle('second', 'account', '2026-10-05T00:00:00Z', '2026-10-12T00:00:00Z', 0, true),
      cycle('third', 'account', '2026-10-12T00:00:00Z', '2026-10-19T00:00:00Z', 50, false),
    ];
    const sample = data.rows[0];
    data.rows = [
      { ...sample, accountId: 'account', at: '2026-09-30T12:00:00Z', costUsd: 900 },
      { ...sample, accountId: 'account', at: '2026-10-01T12:00:00Z', costUsd: 10, unpriced: 0 },
    ];
    const company = buildCompanyUsage(data, 'month', 0, now)[0];
    expect(company.costUsd).toBe(10);
    expect(company.parts.find((part) => part.id === 'person')?.value).toBeCloseTo(
      (100 * (4 / 7 + 0.5)) / (4 / 7 + 2),
      8
    );
    expect(company.expired).toBeCloseTo(100 / (4 / 7 + 2), 8);
    expect(company.remaining).toBeCloseTo(50 / (4 / 7 + 2), 8);
    expect(
      (company.parts.find((part) => part.id === 'available')?.value ?? 0) +
        (company.parts.find((part) => part.id === 'wasted')?.value ?? 0)
    ).toBeCloseTo(150 / (4 / 7 + 2), 8);
    expect(usageRange('month', 0, new Date('2026-10-31T12:00:00Z')).from).toBe('2026-10-01');
  });
  test('unused never includes already consumed or unobserved capacity', () => {
    const data = fixture();
    data.accounts = [{ id: 'account', provider: 'claude', plan: 'Pro' }];
    data.rows = [];
    const observed = cycle(
      'current',
      'account',
      '2026-10-12T00:00:00Z',
      '2026-10-19T00:00:00Z',
      60,
      false
    );
    observed.people[0].percent = 10;
    data.weeks = [observed];
    const company = buildCompanyUsage(data, 'week', 0, now)[0];
    expect(company.parts.find((part) => part.id === 'available')?.value).toBeCloseTo(40, 8);
    expect(company.chartAvailable).toBe(false);
    expect(company.parts.find((part) => part.id === 'unidentified')?.value).toBeCloseTo(50, 8);
    expect(company.accounts[0].used).toBeCloseTo(60, 8);
    expect(company.accounts[0].unused).toBeCloseTo(40, 8);
    data.weeks = [];
    const unknown = buildCompanyUsage(data, 'week', 0, now)[0];
    expect(unknown.accounts[0].unknown).toBe(100);
    expect(unknown.parts.find((part) => part.id === 'available')).toBeUndefined();
    expect(unknown.chartAvailable).toBe(false);
  });
  test('each account retains its own used/unused split after company merging', () => {
    const data = fixture();
    data.accounts = [
      { id: 'one', provider: 'xai', plan: 'Grok Pro' },
      { id: 'two', provider: 'xai', plan: 'Grok Pro' },
    ];
    data.rows = [];
    data.weeks = [
      cycle('one', 'one', '2026-10-12T00:00:00Z', '2026-10-19T00:00:00Z', 20, false),
      cycle('two', 'two', '2026-10-12T00:00:00Z', '2026-10-19T00:00:00Z', 80, false),
    ];
    const company = buildCompanyUsage(data, 'week', 0, now)[0];
    expect(company.accounts.map((account) => [account.used, account.unused])).toEqual([
      [20, 80],
      [80, 20],
    ]);
    expect(company.parts.find((part) => part.id === 'available')?.value).toBeCloseTo(50, 8);
  });
  test('reuses key names from the existing key editor without another name store', () => {
    const base = 'http://127.0.0.1:5190';
    const names = Object.fromEntries(
      demoPeople.map((person) => [apiKeyNameFingerprint(base, person.apiKey), person.name])
    );
    const resolved = resolveUsageKeyNames(
      base,
      demoPeople.map((person) => person.apiKey),
      names
    );
    expect(Object.values(resolved).sort()).toEqual(demoPeople.map((person) => person.name).sort());
    expect(Object.keys(resolved).every((id) => fixture().keyLabels[id])).toBe(true);
    expect(Object.keys(fixture().settings.names)).toHaveLength(0);
  });
  test('renders compact subscription summaries, pies, names and reais without tables or paragraphs', () => {
    const data = fixture();
    const names = Object.fromEntries(
      Object.keys(data.keyLabels).map((id, i) => [id, demoPeople[i].name])
    );
    const classes = new Proxy({} as Record<string, string>, { get: (_, key) => String(key) });
    const html = renderToStaticMarkup(
      createElement(
        I18nextProvider,
        { i18n },
        createElement(CompanyPies, {
          data,
          companies: buildCompanyUsage(data, 'month', 0, now),
          names,
          quote: { rate: 5, observedAt: Date.now() },
          icons: {},
          classes,
        })
      )
    );
    expect((html.match(/data-company=/g) ?? []).length).toBe(7);
    expect(html).toContain('R$');
    expect(html).toContain('Ana');
    expect(html).toContain('Available');
    expect(html).toContain('Wasted');
    expect(html).not.toContain('Unidentified usage');
    expect(html).not.toContain('Unattributed');
    expect(html).not.toContain('Unknown capacity');
    expect((html.match(/data-account-id=/g) ?? []).length).toBe(8);
    expect(html).toContain('@example.test');
    expect(html).not.toContain('Account 1');
    expect(html).not.toContain('Claude Max');
    expect(html).toContain('Grok Pro');
    expect(html).toContain('Max 20×');
    expect(html).not.toMatch(/<p(?:\s|>)|<table|<select/);
    expect(piePath(0, 100)).not.toContain('NaN');
    expect(piePath(0, 25)).toContain('A50 50');
  });
});

describe('interactive charts and restored spending card', () => {
  test('spending stays per person and excludes capacity-only categories', () => {
    const companies = buildCompanyUsage(fixture(), 'month', 0, now);
    const people = spendingByPerson(companies);
    expect(people).toHaveLength(4);
    expect(people.reduce((sum, person) => sum + person.costUsd, 0)).toBeCloseTo(
      companies.reduce((sum, company) => sum + company.costUsd, 0),
      8
    );
    expect(
      people.every(
        (person) =>
          !['unused', 'unidentified', 'unknown', 'remaining', 'expired', 'unattributed'].includes(
            person.id
          )
      )
    ).toBe(true);
    for (const person of people)
      expect(person.companies.reduce((sum, company) => sum + company.costUsd, 0)).toBeCloseTo(
        person.costUsd,
        8
      );
  });
  test('donut gaps preserve tiny slices and full-circle geometry', () => {
    const input = [{ value: 0.01 }, { value: 39.99 }, { value: 60 }, { value: 0 }];
    const segments = donutSegments(input);
    expect(segments).toHaveLength(3);
    expect(segments[0].gap).toBeLessThan(segments[0].value);
    expect(segments[2].start + segments[2].value).toBeCloseTo(100, 8);
    expect(donutPath(0, 100)).toContain('A30 30');
    for (const segment of segments)
      expect(donutPath(segment.start, segment.value)).not.toContain('NaN');
    expect(input[0]).toEqual({ value: 0.01 });
  });
  test('restores the original per-person card and makes quota slices keyboard-focusable', () => {
    const data = fixture(),
      companies = buildCompanyUsage(data, 'month', 0, now);
    const names = Object.fromEntries(
      Object.keys(data.keyLabels).map((id, index) => [id, demoPeople[index].name])
    );
    const classes = new Proxy({} as Record<string, string>, { get: (_, key) => String(key) });
    const quote = { rate: 5, observedAt: Date.now() };
    const spendHtml = renderToStaticMarkup(
      createElement(
        I18nextProvider,
        { i18n },
        createElement(PeopleSpend, { data, companies, names, quote, appearance: {}, classes })
      )
    );
    expect(spendHtml).toContain('API value by person');
    expect((spendHtml.match(/class="personSpend"/g) ?? []).length).toBe(4);
    expect(spendHtml).toContain('data-spend-segment=');
    expect(spendHtml).toContain('R$');
    const pieHtml = renderToStaticMarkup(
      createElement(
        I18nextProvider,
        { i18n },
        createElement(CompanyPies, { data, companies, names, quote, icons: {}, classes })
      )
    );
    expect(pieHtml).toContain('data-quota-segment=');
    expect(pieHtml).toContain('tabindex="0"');
    expect(pieHtml).toContain('role="button"');
    expect(pieHtml).toContain('fill-rule="evenodd"');
  });
});

describe('Cross conversion and model price cards', () => {
  test('uses the Cross USDT buy side and never substitutes a stale or missing quote', () => {
    const quote = parseCrossQuote(
      '{"event":"update_prices","payload":{"symbol":"usdt-brl","prices":{"buy":5.1,"sell":5.0}}}',
      1000
    )!;
    expect(quote.rate).toBe(5.1);
    expect(formatReais(2, quote, 1001)).toContain('10,20');
    expect(formatReais(2, quote, 50_000)).toBe('—');
    expect(
      parseCrossQuote(
        '{"event":"update_prices","payload":{"symbol":"usdc-brl","prices":{"buy":5}}}'
      )
    ).toBeNull();
    expect(parseCrossQuote('bad')).toBeNull();
  });
  test('all 68 configured models have researched provenance; unknown prices stay blank', () => {
    const models = fixture().models!;
    expect(models).toHaveLength(68);
    expect(
      models.every((model) => model.sources.every((source) => source.startsWith('https://')))
    ).toBe(true);
    expect(models.find((model) => model.id === 'claude-mythos-5-1')?.prices.input).toBe(10);
    expect(models.find((model) => model.id === 'grok-imagine-video-1.5')?.prices.per_second).toBe(
      0.08
    );
    for (const model of models.filter((model) =>
      ['self_hosted', 'unpublished', 'subscription_only'].includes(model.status)
    ))
      expect(Object.keys(model.prices)).toHaveLength(0);
    const media = models.find((model) => model.id === 'grok-imagine-image')!;
    expect(priceFields(media)).toEqual(['per_image']);
    const settings = settingsWithModelPrice(fixture().settings, media, { per_image: 0.03 });
    expect(settings.unit_prices?.[media.id].per_image).toBe(0.03);
    expect(settings.prices[media.id]).toBeUndefined();
  });
  test('edits preserve cache/context rates and reset back to published defaults', () => {
    const data = fixture();
    const model = data.models!.find((model) => model.id === 'gpt-6.1-sol')!;
    const edited = settingsWithModelPrice(data.settings, model, { ...model.prices, input: 3 });
    expect(edited.prices[model.id].input).toBe(3);
    expect(edited.prices[model.id].long_context?.output).toBe(15);
    expect(settingsWithModelPrice(edited, model, null).unit_prices?.[model.id]).toBeUndefined();
  });
  test('the preview entry remains serve-only', async () => {
    const plugin = usageDemoPlugin();
    expect(plugin.apply).toBe('serve');
    const transform = plugin.transformIndexHtml;
    if (typeof transform !== 'function') throw Error('Missing transform');
    expect(
      await transform('<script src="/src/main.tsx"></script>', {
        path: '/',
        filename: 'index.html',
      })
    ).toContain('/dev/usage/main.tsx');
    expect(validDemoSettings(createUsageFixture(now).settings)).toBe(true);
    expect(
      validDemoSettings({
        ...createUsageFixture(now).settings,
        unit_prices: { bad: { input: -1 } },
      })
    ).toBe(false);
  });
});
