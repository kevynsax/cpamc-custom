// CUSTOM (cpamc-custom): MiniMax / Z.ai quota parsing (real response shapes, values anonymized).
import { describe, expect, test } from 'bun:test';
import { buildMinimaxQuotaRows, buildZaiQuotaRows } from '@/features/quota/providers/customApiKeyQuota';
import { classifyQuotaFiles } from '@/features/quota/logic';

describe('MiniMax coding plan quota', () => {
  test('maps interval + weekly windows per model', () => {
    const rows = buildMinimaxQuotaRows({
      model_remains: [
        {
          model_name: 'MiniMax-M3',
          start_time: 1790812800000,
          end_time: 1790899200000,
          current_interval_usage_count: 3,
          current_interval_remaining_percent: 100,
          weekly_start_time: 1790553600000,
          weekly_end_time: 1791158400000,
          current_weekly_remaining_percent: 99,
        },
      ],
      base_resp: { status_code: 0 },
    });
    expect(rows.map((r) => [r.id, r.used, r.limit, r.resetAtMs, r.periodHours])).toEqual([
      ['MiniMax-M3:interval', 0, 100, 1790899200000, 24],
      ['MiniMax-M3:weekly', 1, 100, 1791158400000, 168],
    ]);
  });

  test('empty or invalid payloads give no rows', () => {
    expect(buildMinimaxQuotaRows('not json')).toEqual([]);
    expect(buildMinimaxQuotaRows({ base_resp: { status_code: 1004 } })).toEqual([]);
  });
});

describe('Z.ai coding plan quota', () => {
  test('maps credit limits with window length and reset', () => {
    const rows = buildZaiQuotaRows({
      code: 200,
      data: {
        limits: [
          { type: 'CREDIT_LIMIT', unit: 3, number: 5, usage: 2000, currentValue: 0 },
          { type: 'CREDIT_LIMIT', unit: 6, number: 1, usage: 10000, currentValue: 69, nextResetTime: 1791410021984 },
        ],
        level: 'lite',
      },
    });
    expect(rows.map((r) => [r.label, r.used, r.limit, r.resetAtMs ?? null, r.periodHours])).toEqual([
      ['5-hour credits', 0, 2000, null, 5],
      ['1-week credits', 69, 10000, 1791410021984, 168],
    ]);
  });
});

describe('quota page classification', () => {
  test('backend-listed API-key credentials become MiniMax / Z.ai cards', () => {
    const entries = classifyQuotaFiles([
      { name: 'minimax (api key)', type: 'minimax', provider: 'minimax', auth_index: 'a1', runtime_only: true },
      { name: 'zai (api key)', type: 'zai', provider: 'zai', auth_index: 'a2', runtime_only: true },
    ] as never);
    expect(entries.map((e) => e.type)).toEqual(['minimax', 'zai']);
  });
});
