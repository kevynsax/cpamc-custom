// CUSTOM (cpamc-custom): MiniMax / Z.ai discovery, identity, caches and reset wiring.
import { afterEach, describe, expect, spyOn, test } from 'bun:test';
import i18n from '@/i18n';
import type { AuthFileItem, KimiQuotaState } from '@/types';
import { apiCallApi } from '@/services/api';
import { apiClient } from '@/services/api/client';
import { useQuotaStore } from '@/stores/useQuotaStore';
import { getQuotaCacheKey, getQuotaDisplayName } from '@/utils/quota/identity';
import { buildTimelineLane } from '@/features/quota/quotaTimelineModel';
import { nextRecoveryMs } from '@/features/quota/resetSchedule';
import {
  MINIMAX_CONFIG,
  ZAI_CONFIG,
  MINIMAX_QUOTA_URL,
  MINIMAX_CN_QUOTA_URL,
  ZAI_QUOTA_URL,
  ZAI_CN_QUOTA_URL,
  listApiKeyQuotaCredentials,
  buildZaiQuotaRows,
} from '@/features/quota/providers/customApiKeyQuota';

const now = Date.UTC(2026, 9, 3, 12);
const state: KimiQuotaState = {
  status: 'success',
  rows: [
    {
      id: 'weekly',
      label: 'Weekly',
      used: 25,
      limit: 100,
      resetAtMs: now + 3600000,
      periodHours: 168,
    },
  ],
};
const file = (provider: string, authIndex: string): AuthFileItem => ({
  name: `${provider} (api key)`,
  provider,
  auth_index: authIndex,
  quota_source: 'api-key',
});

afterEach(() => {
  useQuotaStore.getState().clearQuotaCache();
});

describe('API-key credential identity and cache', () => {
  test('same-label keys stay distinct and never display account secrets', () => {
    const first = { ...file('zai', 'first'), account: 'never-display-this' };
    const second = file('zai', 'second');
    expect(getQuotaCacheKey(first)).not.toBe(getQuotaCacheKey(second));
    expect(getQuotaDisplayName(first)).toBe('zai (api key) · first');
    expect(getQuotaDisplayName(first)).not.toContain('never-display-this');
  });

  test('each provider has independent state and supports session/file invalidation', () => {
    const store = useQuotaStore.getState();
    const key = getQuotaCacheKey(file('zai', 'first'));
    store.setKimiQuota({ kimi: state });
    store.setMinimaxQuota({ minimax: state });
    store.setZaiQuota({ [key]: state });
    store.setKimiQuota({});
    expect(MINIMAX_CONFIG.storeSelector(useQuotaStore.getState()).minimax).toBe(state);
    expect(ZAI_CONFIG.storeSelector(useQuotaStore.getState())[key]).toBe(state);
    store.clearQuotaCache(['zai (api key)']);
    expect(useQuotaStore.getState().zaiQuota).toEqual({});
    expect(useQuotaStore.getState().minimaxQuota.minimax).toBe(state);
    store.clearQuotaCache();
    expect(useQuotaStore.getState().minimaxQuota).toEqual({});
    expect(useQuotaStore.getState().zaiQuota).toEqual({});
  });

  test.each(['minimax', 'zai'] as const)(
    '%s reset windows feed sorting and the timeline',
    (provider) => {
      expect(nextRecoveryMs(provider, state, now)).toBe(now + 3600000);
      const lane = buildTimelineLane({ name: 'test', provider, quota: state });
      expect(lane.anchorMs).toBe(now + 3600000);
      expect(lane.periodHours).toBe(168);
      expect(lane.remaining).toBe(75);
    }
  );
});

describe('credential API normalization', () => {
  test('normalizes indexes and preserves multiple keys with the same label', async () => {
    const get = spyOn(apiClient, 'get').mockResolvedValue({
      files: [file('minimax', 'a'), file('minimax', 'b')],
    });
    try {
      const files = await listApiKeyQuotaCredentials();
      expect(files).toHaveLength(2);
      expect(files.map((entry) => entry.authIndex)).toEqual(['a', 'b']);
    } finally {
      get.mockRestore();
    }
  });

  test('only a missing custom endpoint is treated as unsupported', async () => {
    const get = spyOn(apiClient, 'get').mockRejectedValue(
      Object.assign(new Error('not found'), { status: 404 })
    );
    try {
      expect(await listApiKeyQuotaCredentials()).toEqual([]);
      get.mockRejectedValue(Object.assign(new Error('backend failed'), { status: 502 }));
      await expect(listApiKeyQuotaCredentials()).rejects.toThrow('backend failed');
    } finally {
      get.mockRestore();
    }
  });
});

describe('provider request wiring', () => {
  const minimax = {
    model_remains: [
      { model_name: 'MiniMax-M3', current_interval_remaining_percent: 75, end_time: now + 3600000 },
    ],
  };
  const zai = {
    data: {
      limits: [
        {
          type: 'CREDIT_LIMIT',
          unit: 3,
          number: 5,
          usage: 100,
          currentValue: 25,
          nextResetTime: now + 3600000,
        },
      ],
    },
  };
  test.each([
    [
      MINIMAX_CONFIG,
      'minimax',
      'https://api.minimax.io/v1',
      MINIMAX_QUOTA_URL,
      'Bearer $TOKEN$',
      minimax,
    ],
    [
      MINIMAX_CONFIG,
      'minimax',
      'https://api.minimaxi.com/v1',
      MINIMAX_CN_QUOTA_URL,
      'Bearer $TOKEN$',
      minimax,
    ],
    [ZAI_CONFIG, 'zai', 'https://api.z.ai/api/coding/paas/v4', ZAI_QUOTA_URL, '$TOKEN$', zai],
    [ZAI_CONFIG, 'zai', 'https://open.bigmodel.cn/api/paas/v4', ZAI_CN_QUOTA_URL, '$TOKEN$', zai],
    [ZAI_CONFIG, 'zai', 'https://open.bigmodel.cn.example.test/v1', ZAI_QUOTA_URL, '$TOKEN$', zai],
  ] as const)(
    'fetches %s for %s at %s',
    async (config, provider, base, url, authorization, body) => {
      const request = spyOn(apiCallApi, 'request').mockResolvedValue({
        statusCode: 200,
        body,
        bodyText: JSON.stringify(body),
        header: {},
      });
      try {
        const rows = await config.fetchQuota(
          { ...file(provider, 'credential'), base_url: base },
          i18n.t
        );
        expect(request.mock.calls[0][0]).toMatchObject({
          authIndex: 'credential',
          method: 'GET',
          url,
          header: { Authorization: authorization },
        });
        expect(rows).toHaveLength(1);
        expect(rows[0].used).toBe(25);
        expect(rows[0].resetAtMs).toBe(now + 3600000);
      } finally {
        request.mockRestore();
      }
    }
  );

  test('invalid Z.ai counters never become healthy zero usage', () => {
    for (const bad of ['', ' ', null, true, -1, Infinity]) {
      expect(buildZaiQuotaRows({ data: { limits: [{ usage: 100, currentValue: bad }] } })).toEqual(
        []
      );
    }
  });

  test.each(['en', 'zh-CN', 'zh-TW', 'ru'])(
    'has localized card states and row labels in %s',
    (language) => {
      for (const provider of ['minimax', 'zai']) {
        for (const key of [
          'empty_title',
          'empty_desc',
          'idle',
          'loading',
          'load_failed',
          'missing_auth_index',
          'empty_data',
        ]) {
          expect(i18n.exists(`${provider}_quota.${key}`, { lng: language })).toBe(true);
        }
      }
      for (const key of ['window', 'weekly'])
        expect(i18n.exists(`minimax_quota.${key}`, { lng: language })).toBe(true);
      for (const period of ['limit', 'hour', 'week', 'minute']) {
        for (const kind of ['credits', 'tools'])
          expect(i18n.exists(`zai_quota.${period}_${kind}`, { lng: language })).toBe(true);
      }
    }
  );
});
