import { describe, expect, test } from 'bun:test';
import { aggregateClientUsage, utcDateOffset } from '../src/features/usage/utils';
import { normalizeClientUsage } from '../src/services/api/clientUsage';

const rawRow = {
  date: '2026-06-10',
  key_id: 'key-a',
  account_id: 'account-a',
  provider: 'codex',
  auth_type: 'oauth',
  requests: 2,
  failed: 1,
  input_tokens: 10,
  output_tokens: 5,
  cached_tokens: 3,
  total_tokens: 15,
};
const response = (rows = [rawRow]) => ({
  collection_enabled: true,
  account_labels: { 'account-a': 'Work account' },
  key_labels: { 'key-a': '••••abcd' },
  rows,
  prices_as_of: '2026-06-10',
  accounts: { 'account-a': { provider: 'codex', plan: 'pro' } },
  settings: { names: {}, plans: {}, prices: {} },
  weeks: [],
});

describe('client usage', () => {
  test('normalizes the v8 API contract', () => {
    const data = normalizeClientUsage(response());
    expect(data.collectionEnabled).toBe(true);
    expect(data.rows[0]).toEqual({
      date: '2026-06-10',
      at: '',
      model: '',
      costUsd: null,
      unpriced: 2,
      keyId: 'key-a',
      accountId: 'account-a',
      provider: 'codex',
      authType: 'oauth',
      requests: 2,
      failed: 1,
      inputTokens: 10,
      outputTokens: 5,
      cachedTokens: 3,
      totalTokens: 15,
    });
    expect(data.accountLabels['account-a']).toBe('Work account');
    expect(data.keyLabels['key-a']).toBe('••••abcd');
  });

  test('separate in-house reference estimates reach money charts without changing recorded history', () => {
    const projected = {
      ...rawRow,
      provider: 'openai-compatible-spark',
      model: '/models/Qwen3-VL-30B-A3B-Instruct-Q4_K_M.gguf',
      model_alias: 'qwen-vl',
      cost_usd: null,
      unpriced_requests: 2,
      reference_cost_usd: 0.00027,
    };
    const data = normalizeClientUsage(response([projected]));
    expect(data.rows[0].model).toBe('qwen-vl');
    expect(data.rows[0].costUsd).toBe(0.00027);
    expect(data.rows[0].recordedCostUsd).toBeNull();
    expect(data.rows[0].referenceCostUsd).toBe(0.00027);
    expect(data.rows[0].unpriced).toBe(0);
    expect(projected.cost_usd).toBeNull();
    expect(projected.unpriced_requests).toBe(2);
    expect(projected.model).toContain('/models/');
  });
  test('recorded costs win over a separate reference estimate', () => {
    const projected = { ...rawRow, cost_usd: 9, reference_cost_usd: 99, unpriced_requests: 0 };
    const data = normalizeClientUsage(response([projected]));
    expect(data.rows[0].costUsd).toBe(9);
    expect(data.rows[0].referenceCostUsd).toBeUndefined();
  });
  test('rejects malformed rows instead of displaying false zero usage', () => {
    for (const value of [
      null,
      {},
      { ...response(), rows: [{}] },
      response([{ ...rawRow, requests: -1 }]),
      response([{ ...rawRow, input_tokens: NaN }]),
    ]) {
      expect(() => normalizeClientUsage(value)).toThrow();
    }
  });

  test('aggregates across dates without merging clients or accounts', () => {
    const data = normalizeClientUsage(
      response([
        rawRow,
        { ...rawRow, date: '2026-06-11' },
        { ...rawRow, key_id: 'key-b' },
        { ...rawRow, account_id: 'account-b' },
        { ...rawRow, provider: 'claude' },
        { ...rawRow, auth_type: 'api_key' },
      ])
    );
    const rows = aggregateClientUsage(data.rows);
    expect(rows).toHaveLength(5);
    const combined = rows.find((row) => row.requests === 4);
    expect(combined?.totalTokens).toBe(30);
    expect(combined?.inputTokens).toBe(20);
    expect(combined?.failed).toBe(2);
    expect(data.rows[0].requests).toBe(2);
  });

  test('computes date ranges using UTC and handles year boundaries', () => {
    const date = new Date('2026-01-01T00:30:00+02:00');
    expect(utcDateOffset(0, date)).toBe('2025-12-31');
    expect(utcDateOffset(-29, date)).toBe('2025-12-02');
    expect(utcDateOffset(-364, new Date('2026-06-10T12:00:00Z'))).toBe('2025-06-11');
  });
});
