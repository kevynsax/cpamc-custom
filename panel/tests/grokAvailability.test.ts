import { beforeAll, describe, expect, test } from 'bun:test';
import { createHash } from 'node:crypto';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { createInstance } from 'i18next';
import { I18nextProvider } from 'react-i18next';
import en from '../src/i18n/locales/en.json';
import {
  GROK_WEEK_SECONDS,
  grokDisplayQuota,
  findGrokAccount,
  resolveGrokWeekly,
} from '../src/utils/quota/grokAvailability';
import { withGrokAvailability } from '../src/features/usage/grokAvailability';
import { buildCompanyUsage } from '../src/features/usage/companies';
import { SubscriptionFacts } from '../src/features/usage/SubscriptionFacts';
import { XaiQuotaBody } from '../src/features/quota/providers/xai/XaiQuotaBody';
import { QUOTA_CLASS_KEYS, bindQuotaClasses } from '../src/features/quota/types';
import { normalizeClientUsage, type UsageWeek } from '../src/services/api/clientUsage';
import { buildXaiBillingSummary } from '../src/utils/quota';
import { createUsageFixture } from '../dev/usage/fixtures';
import type { XaiQuotaState } from '../src/types';

const now = new Date('2026-10-17T12:00:00Z');
const i18n = createInstance();
beforeAll(async () => {
  await i18n.init({ lng: 'en', resources: { en: { translation: en } } });
});
const classes = new Proxy({} as Record<string, string>, { get: (_, key) => String(key) });
const quotaClasses = bindQuotaClasses(
  Object.fromEntries(QUOTA_CLASS_KEYS.map((key) => [key, key])),
  'test'
);
const html = (element: React.ReactNode) =>
  renderToStaticMarkup(createElement(I18nextProvider, { i18n }, element));
const measured = {
  scope: 'account',
  used: 48,
  reset: '2026-10-22T12:00:00Z',
  observedAt: '2026-10-17T11:59:00Z',
  periodSeconds: GROK_WEEK_SECONDS,
};

function fixture() {
  const data = normalizeClientUsage(createUsageFixture(now));
  data.accounts = [
    {
      id: 'readable',
      provider: 'xai',
      plan: 'GrokPro',
      email: 'one@example.test',
      cachedQuotas: [measured],
    },
    {
      id: 'missing',
      provider: 'xai',
      plan: 'GrokPro',
      email: 'two@example.test',
      cachedQuotas: [],
    },
  ];
  data.rows = [];
  data.weeks = [];
  return data;
}
describe('shared Grok weekly presentation policy', () => {
  test('missing availability uses the same-plan weekly contract and defaults to 100%', () => {
    const result = resolveGrokWeekly(
      { plan: 'Grok Pro', used: null, scope: 'account', periodSeconds: 0 },
      [
        {
          plan: 'GrokPro',
          used: 48,
          scope: 'account',
          periodSeconds: GROK_WEEK_SECONDS,
          reset: measured.reset,
        },
      ]
    );
    expect(result.remaining).toBe(100);
    expect(result.used).toBe(0);
    expect(result.periodSeconds).toBe(GROK_WEEK_SECONDS);
    expect(result.assumed).toBe(true);
    expect(result.reset).toBeUndefined();
  });
  test('never copies usage, reset dates, or the scope of a different plan', () => {
    const result = resolveGrokWeekly(
      { plan: 'GrokPro', used: null, scope: 'account', periodSeconds: GROK_WEEK_SECONDS },
      [
        {
          plan: 'Heavy',
          used: 99,
          scope: 'different',
          periodSeconds: GROK_WEEK_SECONDS,
          reset: measured.reset,
        },
      ]
    );
    expect(result.scope).toBe('account');
    expect(result.reset).toBeUndefined();
    expect(result.remaining).toBe(100);
  });
  test.each([0, 48, 100, 120])('preserves explicit usage %s, including exhausted quota', (used) => {
    const result = resolveGrokWeekly({
      plan: 'GrokPro',
      used,
      scope: 'account',
      periodSeconds: GROK_WEEK_SECONDS,
    });
    expect(result.remaining).toBe(100 - Math.min(100, used));
    expect(result.assumed).toBe(false);
  });
  test('Usage preserves the real sibling and adds the missing sibling without mutating history', () => {
    const data = fixture();
    const before = JSON.stringify(data);
    const result = withGrokAvailability(data, now.getTime());
    expect(result.accounts[0].cachedQuotas?.[0].used).toBe(48);
    expect(result.accounts[1].cachedQuotas?.[0]).toMatchObject({
      used: 0,
      periodSeconds: GROK_WEEK_SECONDS,
      assumed: true,
      observedAt: '',
      reset: '',
    });
    expect(JSON.stringify(data)).toBe(before);
    expect(result.weeks).toBe(data.weeks);
    expect(
      buildCompanyUsage(result, 'week', 0, now).every((company) => !company.chartAvailable)
    ).toBe(true);
  });
  test('recent actual history is preferred over an empty cache; stale data falls back only for Grok', () => {
    const data = fixture();
    data.accounts[1].provider = 'codex';
    data.accounts[0].cachedQuotas = [];
    data.weeks = [
      {
        id: 'real',
        accountId: 'readable',
        provider: 'xai',
        scope: 'account',
        plan: 'GrokPro',
        periodSeconds: GROK_WEEK_SECONDS,
        start: '2026-10-15T12:00:00Z',
        reset: measured.reset,
        lastObserved: measured.observedAt,
        used: 100,
        remaining: 0,
        people: [],
        unattributed: 100,
        unusedUpperBound: null,
        incomplete: true,
        closed: false,
        allocationAvailable: false,
      } as UsageWeek,
    ];
    const result = withGrokAvailability(data, now.getTime());
    expect(result.accounts[0].cachedQuotas?.[0].used).toBe(100);
    expect(result.accounts[0].cachedQuotas?.[0].assumed).not.toBe(true);
    expect(result.accounts[1].cachedQuotas).toEqual([]);
    expect(
      withGrokAvailability(data, now.getTime() + 3600000).accounts[0].cachedQuotas?.[0].assumed
    ).toBe(true);
  });
  test('disabled subscriptions never receive a full-availability assumption', () => {
    const data = fixture();
    data.accounts[1].disabled = true;
    expect(withGrokAvailability(data, now.getTime()).accounts[1].cachedQuotas).toEqual([]);
  });
  test('both Usage and Quota Management render the missing weekly percentage as 100%', () => {
    const data = withGrokAvailability(fixture(), now.getTime());
    const account = buildCompanyUsage(data, 'week', 0, now)[0].accounts.find(
      (account) => account.id === 'missing'
    )!;
    const usage = html(
      createElement(SubscriptionFacts, {
        account,
        quote: { rate: 5, observedAt: Date.now() },
        current: true,
        classes,
      })
    );
    expect(usage).toContain('100%');
    expect(usage).toContain('7 d');
    expect(usage).toContain('role="meter"');
    const quota = grokDisplayQuota(
      { name: 'missing.json', plan_type: 'GrokPro' },
      { status: 'error', billing: null, error: 'Quota request failed', errorStatus: 500 }
    )!;
    const management = html(createElement(XaiQuotaBody, { quota, classes: quotaClasses }));
    expect(management).toContain('100%');
    expect(management).toContain('Weekly limit');
    expect(management).toContain('width:100%');
    expect(management).toContain('GrokPro');
    expect(quota.status).toBe('error');
    expect(quota.billing?.usagePercent).toBeNull();
  });
  test.each([401, 403, 429])(
    'error status %s remains an error while availability is a display-only assumption',
    (errorStatus) => {
      const result = grokDisplayQuota(
        { name: 'blocked.json', plan_type: 'GrokPro' },
        { status: 'error', billing: null, errorStatus, error: 'Original error' }
      )!;
      expect(result.status).toBe('error');
      expect(result.errorStatus).toBe(errorStatus);
      expect(result.error).toBe('Original error');
      expect(result.billing?.usagePercent).toBeNull();
    }
  );
  test('Quota Management matches its own cached account and plan without borrowing a sibling’s usage', () => {
    const id = 'credential-id';
    const fingerprint = createHash('sha256').update(id).digest('hex');
    const own = {
      id: fingerprint,
      provider: 'xai',
      plan: 'GrokPro',
      cachedQuotas: [{ ...measured, used: 100 }],
    };
    const peer = {
      id: 'other',
      provider: 'xai',
      plan: 'GrokPro',
      cachedQuotas: [{ ...measured, used: 48 }],
    };
    const matched = findGrokAccount({ name: 'other-name', id }, [peer, own]);
    expect(matched).toBe(own);
    const raw: XaiQuotaState = {
      status: 'error',
      billing: null,
      error: 'Read unavailable',
      errorStatus: 403,
    };
    const display = grokDisplayQuota({ name: 'other-name', id }, raw, matched, now.getTime())!;
    expect(display.billing?.planLabel).toBe('GrokPro');
    expect(display.billing?.usagePercent).toBe(100);
    expect(display.status).toBe('error');
    expect(display.errorStatus).toBe(403);
    expect(raw.billing).toBeNull();
    const missing = grokDisplayQuota(
      { name: 'other-name', id },
      raw,
      { ...own, cachedQuotas: [] },
      now.getTime()
    )!;
    expect(missing.billing?.usagePercent).toBeNull();
    expect(html(createElement(XaiQuotaBody, { quota: missing, classes: quotaClasses }))).toContain(
      'width:100%'
    );
  });
  test('a direct measured weekly counter outranks older account metadata', () => {
    const billing = buildXaiBillingSummary({
      currentPeriod: { type: 'weekly' },
      creditUsagePercent: 90,
    })!;
    const display = grokDisplayQuota(
      { name: 'direct' },
      { status: 'success', billing },
      { id: 'direct', provider: 'xai', plan: 'GrokPro', cachedQuotas: [measured] },
      now.getTime()
    )!;
    expect(display.billing?.usagePercent).toBe(90);
  });
  test('monthly spending is never used as the weekly counter', () => {
    const billing = buildXaiBillingSummary({ monthlyLimit: { val: 15000 }, used: { val: 6000 } })!;
    const quota: XaiQuotaState = { status: 'success', billing };
    const result = html(createElement(XaiQuotaBody, { quota, classes: quotaClasses }));
    expect(billing.periodType).toBe('monthly');
    expect(billing.usagePercent).toBe(40);
    expect(result).toContain('width:100%');
    expect(result).toContain('width:60%');
  });
});
