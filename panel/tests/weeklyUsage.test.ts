import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { normalizeClientUsage } from '../src/services/api/clientUsage';
import { fallbackPeople, formatUsageMoney, formatUsagePercent } from '../src/features/usage/utils';

const row = {
  date: '2026-06-10',
  at: '2026-06-10T12:00:00Z',
  model: 'model',
  key_id: 'key',
  account_id: 'account',
  provider: 'codex',
  auth_type: 'oauth',
  requests: 1,
  failed: 0,
  input_tokens: 100,
  output_tokens: 20,
  cached_tokens: 10,
  total_tokens: 120,
  cost_usd: 0.01,
  unpriced_requests: 0,
};
const week = {
  id: 'week',
  account_id: 'account',
  provider: 'codex',
  scope: 'account',
  plan: 'Pro',
  start: '2026-06-08T09:00:00Z',
  reset: '2026-06-15T09:00:00Z',
  people: [{ key_id: 'key', cost_usd: 0.01, unpriced: 0, requests: 1, percent: 10 }],
  used: 30,
  remaining: 70,
  unattributed: 20,
  unused_upper_bound: null,
  last_observed: '2026-06-10T12:00:00Z',
  incomplete: true,
  closed: false,
  allocation_available: true,
};
const fixture = () => ({
  collection_enabled: true,
  account_labels: { account: 'Work' },
  key_labels: { key: '••••abcd' },
  rows: [row],
  weeks: [week],
  accounts: { account: { provider: 'codex', plan: 'Pro' } },
  settings: { names: { key: 'Ada' }, plans: {}, prices: { model: { input: 2, output: 10 } } },
  prices_as_of: '2026-06-10',
});

describe('weekly value dashboard', () => {
  test('tiny positive costs and percentages never look like zero usage', () => {
    expect(formatUsageMoney(0.00000001, 'en-US')).toBe('< $0.000001');
    expect(formatUsageMoney(0, 'en-US')).toBe('$0.00');
    expect(formatUsagePercent(0.001, 'en-US')).toBe('< 0.1%');
    expect(formatUsagePercent(0, 'en-US')).toBe('0%');
  });
  test('preserves reset cycles, partial coverage and unknown closing estimates', () => {
    const data = normalizeClientUsage(fixture());
    expect(data.weeks[0].start).toBe('2026-06-08T09:00:00Z');
    expect(data.weeks[0].unusedUpperBound).toBeNull();
    expect(data.weeks[0].incomplete).toBe(true);
    expect(data.weeks[0].people[0].percent).toBe(10);
    expect(data.weeks[0].unattributed).toBe(20);
    expect(data.settings.names.key).toBe('Ada');
    expect(data.settings.prices.model.input).toBe(2);
    expect(data.rows[0].costUsd).toBe(0.01);
  });

  test('rejects malformed money and percentages rather than substituting zero', () => {
    expect(() =>
      normalizeClientUsage({ ...fixture(), rows: [{ ...row, cost_usd: -1 }] })
    ).toThrow();
    expect(() =>
      normalizeClientUsage({ ...fixture(), weeks: [{ ...week, remaining: NaN }] })
    ).toThrow();
    expect(() =>
      normalizeClientUsage({ ...fixture(), weeks: [{ ...week, unused_upper_bound: -1 }] })
    ).toThrow();
  });

  test('fallback cost covers the last seven days without inventing a subscription cycle', () => {
    const data = normalizeClientUsage({
      ...fixture(),
      rows: [
        row,
        { ...row, key_id: 'other', account_id: 'other-account' },
        { ...row, at: '2026-06-01T12:00:00Z', key_id: 'expired' },
        { ...row, at: '', key_id: 'legacy' },
        { ...row, at: '2026-06-11T12:00:00Z', key_id: 'future' },
        { ...row, cost_usd: null, unpriced_requests: 1 },
      ],
    });
    const people = fallbackPeople(data.rows, 'account', new Date('2026-06-10T12:00:00.000Z'));
    expect(people).toHaveLength(1);
    expect(people[0]).toEqual({
      keyId: 'key',
      requests: 2,
      costUsd: 0.01,
      unpriced: 1,
      percent: 0,
    });
  });

  test('all six locales include every usage label and matching interpolation', () => {
    const read = (locale: string) =>
      JSON.parse(
        readFileSync(new URL(`../src/i18n/locales/${locale}.json`, import.meta.url), 'utf8')
      ).client_usage as Record<string, string>;
    const en = read('en');
    for (const locale of ['zh-CN', 'zh-TW', 'ru', 'vi', 'ko']) {
      const translated = read(locale);
      expect(Object.keys(translated).sort()).toEqual(Object.keys(en).sort());
      for (const [key, value] of Object.entries(en)) {
        expect(translated[key].length).toBeGreaterThan(0);
        expect(translated[key].match(/{{.*?}}/g) ?? []).toEqual(value.match(/{{.*?}}/g) ?? []);
      }
    }
  });
});
