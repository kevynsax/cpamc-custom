import { beforeAll, describe, expect, test } from 'bun:test';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { createInstance } from 'i18next';
import { I18nextProvider } from 'react-i18next';
import en from '../src/i18n/locales/en.json';
import { splitPersonCapacity, allocatePersonFees } from '../src/features/usage/personAllocation';
import type { CompanyUsage } from '../src/features/usage/companies';
import { friendlyModelName } from '../src/features/usage/modelNames';
import { ApiKeyKindCards } from '../src/features/config/components/blocks/ApiKeyKindCards';
import { normalizeKeyMetadata } from '../src/services/api/keyNames';
import { SubscriptionFacts } from '../src/features/usage/SubscriptionFacts';
import { ModelPriceCards } from '../src/features/usage/ModelPriceCards';
import { PersonDetails } from '../src/features/usage/PersonDetails';
import { normalizeClientUsage } from '../src/services/api/clientUsage';
import { createUsageFixture } from '../dev/usage/fixtures';
import { mergedCompanyModels, periodRows } from '../src/features/usage/reporting';

const i18n = createInstance();
beforeAll(async () => {
  await i18n.init({ lng: 'en', resources: { en: { translation: en } } });
});
const classes = new Proxy({} as Record<string, string>, { get: (_, key) => String(key) });
const html = (element: React.ReactNode) =>
  renderToStaticMarkup(createElement(I18nextProvider, { i18n }, element));
function company(used = [25, 25, 20], expired = 25): CompanyUsage {
  return {
    id: 'anthropic',
    provider: 'claude',
    remaining: 0,
    expired,
    costUsd: 0,
    unpriced: 0,
    incomplete: false,
    weightEstimated: false,
    chartAvailable: true,
    current: false,
    accounts: Array.from({ length: 4 }, (_, index) => ({
      id: String(index),
      email: '',
      capacityWeight: 20,
      periodFeeUsd: 200,
      quotaMeasured: true,
      chartAvailable: true,
      used: 100 - expired,
      unused: expired,
      unknown: 0,
      unidentified: 0,
      remaining: 0,
      expired,
    })),
    parts: [
      ...used.map((value, index) => ({ id: `p${index + 1}`, value, costUsd: 0, unpriced: 0 })),
      { id: 'system', value: 5, costUsd: 0, unpriced: 0 },
      { id: 'wasted', value: expired, costUsd: 0, unpriced: 0 },
    ],
  };
}
describe('person allowances and compact financial UI', () => {
  test('subtracts the system first, then splits four Max plans among three people', () => {
    const shares = splitPersonCapacity(4, 0.2, [
      { id: 'a', used: 1 },
      { id: 'b', used: 1 },
      { id: 'c', used: 0.8 },
    ]);
    expect(shares[0].entitlement).toBeCloseTo(1.2666666667, 8);
    expect(shares[2].deficit).toBeCloseTo(0.4666666667, 8);
    expect(shares.reduce((sum, item) => sum + item.unusedShare, 0)).toBeCloseTo(1, 10);
    expect(splitPersonCapacity(4, 0.2, [])).toEqual([]);
  });
  test('allocates actual expired fee by shortfall, never to the System key', () => {
    const values = allocatePersonFees([company()], ['p1', 'p2', 'p3', 'system'], {
      system: 'system',
    });
    expect(values.system).toBeUndefined();
    expect(values.p1.anthropic.wasted.usd).toBeCloseTo((200 * 4) / 15, 8);
    expect(values.p3.anthropic.wasted.usd).toBeCloseTo((200 * 7) / 15, 8);
    expect(
      Object.values(values).reduce((sum, item) => sum + (item.anthropic.wasted.usd ?? 0), 0)
    ).toBeCloseTo(200, 8);
  });
  test('overuse cannot inflate waste or turn someone’s waste negative', () => {
    const values = allocatePersonFees([company([75, 0, 0], 20)], ['p1', 'p2', 'p3'], {
      system: 'system',
    });
    expect(values.p1.anthropic.wasted.usd).toBe(0);
    expect(values.p2.anthropic.wasted.usd).toBe(80);
    expect(values.p3.anthropic.wasted.usd).toBe(80);
  });
  test('missing quota does not become fabricated waste', () => {
    const c = company();
    c.chartAvailable = false;
    expect(allocatePersonFees([c], ['p1'], {})).toEqual({ p1: {} });
  });
  test('renders Person and System as icon cards, never radio buttons', () => {
    const result = html(createElement(ApiKeyKindCards, { value: 'system', onChange: () => {} }));
    expect((result.match(/<button/g) ?? []).length).toBe(2);
    expect((result.match(/<svg/g) ?? []).length).toBe(2);
    expect(result).toContain('Person');
    expect(result).toContain('System');
    expect(result).not.toContain('type="radio"');
    expect(result).toContain('aria-pressed="true"');
  });
  test('normalizes types while still accepting old names-only responses', () => {
    const id = 'a'.repeat(64);
    expect(
      normalizeKeyMetadata({ names: { [id]: 'Job' }, kinds: { [id]: 'system' } }).kinds[id]
    ).toBe('system');
    expect(normalizeKeyMetadata({ names: {} }).kinds).toEqual({});
    expect(() => normalizeKeyMetadata({ names: {}, kinds: { [id]: 'robot' } })).toThrow();
  });
  test('friendly model names preserve versions and hide server file paths', () => {
    expect(friendlyModelName('gpt-6.1-sol')).toBe('GPT 6.1 Sol');
    expect(friendlyModelName('claude-opus-5-5')).toBe('Claude Opus 5.5');
    expect(friendlyModelName('spark/qwen-coder')).toBe('Qwen 3.8 27B');
    expect(friendlyModelName('/models/qwen/Qwen3-VL-30B-A3B-Instruct-Q4_K_M.gguf')).not.toContain(
      '/models'
    );
  });
  test('subscription identity, plan and monthly price share one line, with slim quota tracks', () => {
    const now = Date.now();
    const result = html(
      createElement(SubscriptionFacts, {
        account: {
          ...company().accounts[0],
          email: 'person@example.test',
          plan: 'Claude Max 20x',
          monthlyFeeUsd: 200,
          cachedQuotas: [
            {
              scope: 'account',
              used: 20,
              reset: new Date(now + 3600000).toISOString(),
              observedAt: new Date(now - 1000).toISOString(),
              periodSeconds: 18000,
            },
          ],
        },
        quote: { rate: 5, observedAt: now },
        current: true,
        classes,
      })
    );
    expect(result).toContain('person@example.test — Max 20×');
    expect(result).toContain('1.000,00');
    expect(result).toContain('role="meter"');
    expect(result).not.toContain('Monthly');
    expect(result).not.toContain('Period');
  });
  test('model cards sort by actual period cost and use friendly financial labels', () => {
    const data = normalizeClientUsage(createUsageFixture(new Date('2026-10-17T12:00:00Z')));
    const result = html(
      createElement(ModelPriceCards, {
        data,
        quote: { rate: 5, observedAt: Date.now() },
        busy: false,
        spending: { 'gpt-6.1-sol': 100, 'claude-mythos-5-1': 10 },
        onSave: async () => true,
      })
    );
    expect(result.indexOf('data-model-id="gpt-6.1-sol"')).toBeLessThan(
      result.indexOf('data-model-id="claude-mythos-5-1"')
    );
    expect(result).toContain('GPT 6.1 Sol');
    expect(result).not.toMatch(/\btokens\b/i);
  });
  test('merges company/model graphs only when each company owns exactly one priced model', () => {
    const source = normalizeClientUsage(createUsageFixture(new Date('2026-10-17T12:00:00Z')));
    const base = source.rows[0];
    const rows = [
      { ...base, provider: 'codex', model: 'gpt-6.1-sol', costUsd: 63.23 },
      { ...base, provider: 'claude', model: 'claude-opus-5-5', costUsd: 2.32 },
    ];
    expect(mergedCompanyModels(rows)?.map((entry) => entry.company)).toEqual([
      'openai',
      'anthropic',
    ]);
    expect(
      mergedCompanyModels([...rows, { ...rows[0], model: 'second-model', costUsd: 2.32 }])
    ).toBeNull();
    expect(mergedCompanyModels([...rows, { ...rows[0], provider: 'claude' }])).toBeNull();
    expect(mergedCompanyModels([])).toBeNull();
  });
  test('person cards exclude systems, retain idle people with actual allowances, and show pies and waste without counts', () => {
    const now = new Date('2026-10-17T12:00:00Z');
    const data = normalizeClientUsage(createUsageFixture(now));
    const rows = periodRows(data, 'month', 0, now);
    const ids = Object.keys(data.keyLabels).slice(0, 3);
    const result = html(
      createElement(PersonDetails, {
        current: rows.current,
        previous: rows.previous,
        personIds: [...ids, 'idle'],
        names: { idle: 'Idle person' },
        allowances: {
          idle: {
            anthropic: {
              wasted: { usd: null, partial: false },
              available: { usd: 20, partial: false },
            },
          },
          [ids[0]]: {
            anthropic: {
              wasted: { usd: 10, partial: true },
              available: { usd: null, partial: false },
            },
          },
        },
        quote: { rate: 5, observedAt: Date.now() },
        classes,
      })
    );
    expect((result.match(/data-person-detail=/g) ?? []).length).toBe(4);
    expect(result).toContain('data-value-pie=');
    expect(result).toContain('wasted');
    expect(result).toContain('Idle person');
    expect(result).not.toContain('Requests');
    expect(result).not.toMatch(/\btokens\b/i);
    expect(result).not.toContain('≥');
  });
});
