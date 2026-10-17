import { beforeAll, describe, expect, test } from 'bun:test';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { createInstance } from 'i18next';
import { I18nextProvider } from 'react-i18next';
import en from '../src/i18n/locales/en.json';
import type { CompanyUsage } from '../src/features/usage/companies';
import {
  hasAccountInformation,
  hasCompanyInformation,
  hasPersonInformation,
} from '../src/features/usage/visibility';
import { emptyTotal } from '../src/features/usage/reporting';
import { PersonDetails } from '../src/features/usage/PersonDetails';
import { CompanyPies } from '../src/features/usage/CompanyPies';
import { PeopleSpend } from '../src/features/usage/PeopleSpend';
import { normalizeClientUsage } from '../src/services/api/clientUsage';
import { createUsageFixture } from '../dev/usage/fixtures';

const now = Date.parse('2026-10-17T12:00:00Z');
const quote = () => ({ rate: 5, observedAt: Date.now() });
const emptyFees = {
  wasted: { usd: null, partial: false },
  available: { usd: null, partial: false },
};
const i18n = createInstance();
beforeAll(async () => {
  await i18n.init({ lng: 'en', resources: { en: { translation: en } } });
});
const classes = new Proxy({} as Record<string, string>, { get: (_, key) => String(key) });
const html = (element: React.ReactNode) =>
  renderToStaticMarkup(createElement(I18nextProvider, { i18n }, element));
const account = () => ({
  id: 'empty-account',
  email: 'name@example.test',
  plan: 'Some plan',
  periodFeeUsd: null,
  monthlyFeeUsd: null,
  quotaMeasured: false,
  chartAvailable: false,
  used: 0,
  unused: 0,
  unknown: 100,
  unidentified: 0,
  remaining: 0,
  expired: 0,
  cachedQuotas: [],
});
function company(): CompanyUsage {
  return {
    id: 'empty-company',
    provider: 'unknown',
    accounts: [account()],
    remaining: 0,
    expired: 0,
    parts: [],
    costUsd: 0,
    unpriced: 10,
    incomplete: true,
    weightEstimated: true,
    chartAvailable: false,
    current: true,
  };
}

describe('hide informational cards without usable money or percentages', () => {
  test('names, plan labels, request counts and unknown amounts do not make a useful card', () => {
    expect(hasAccountInformation(account(), true, quote(), now)).toBe(false);
    expect(hasCompanyInformation(company(), quote(), now)).toBe(false);
    expect(
      hasPersonInformation(
        {
          id: 'idle',
          current: { ...emptyTotal(), requests: 999, tokens: 100000 },
          previous: emptyTotal(),
          companies: [],
          models: [],
        },
        emptyFees,
        true,
        quote()
      )
    ).toBe(false);
  });
  test('paid fees, priced spending and measured or assumed percentages remain visible', () => {
    expect(hasAccountInformation({ ...account(), monthlyFeeUsd: 200 }, true, quote(), now)).toBe(
      true
    );
    const full = {
      scope: 'account',
      periodSeconds: 604800,
      used: 100,
      reset: '2026-10-22T12:00:00Z',
      observedAt: '2026-10-17T11:59:00Z',
    };
    expect(hasAccountInformation({ ...account(), cachedQuotas: [full] }, true, quote(), now)).toBe(
      true
    );
    expect(
      hasAccountInformation(
        {
          ...account(),
          cachedQuotas: [{ ...full, observedAt: '', reset: '', used: 0, assumed: true }],
        },
        true,
        quote(),
        now
      )
    ).toBe(true);
    expect(hasCompanyInformation({ ...company(), costUsd: 1 }, quote(), now)).toBe(true);
    expect(
      hasAccountInformation(
        { ...account(), cachedQuotas: [{ ...full, observedAt: '2026-10-17T10:00:00Z' }] },
        true,
        quote(),
        now
      )
    ).toBe(false);
  });
  test('no empty company or person wrapper is rendered', () => {
    const data = normalizeClientUsage(createUsageFixture(new Date(now)));
    expect(
      html(
        createElement(CompanyPies, {
          data,
          companies: [company()],
          names: {},
          quote: quote(),
          icons: {},
          classes,
        })
      )
    ).toBe('');
    expect(
      html(
        createElement(PersonDetails, {
          current: [],
          previous: [],
          personIds: ['idle'],
          names: { idle: 'Idle person' },
          quote: quote(),
          classes,
        })
      )
    ).toBe('');
    expect(
      html(
        createElement(PeopleSpend, {
          data,
          companies: [company()],
          names: {},
          quote: quote(),
          appearance: {},
          classes,
        })
      )
    ).toBe('');
  });
  test('idle people with money available or wasted are not hidden and do not change the allocation roster', () => {
    const result = html(
      createElement(PersonDetails, {
        current: [],
        previous: [],
        personIds: ['idle', 'empty'],
        names: { idle: 'Useful budget', empty: 'Empty card' },
        allowances: {
          idle: {
            anthropic: {
              wasted: { usd: 10, partial: false },
              available: { usd: 20, partial: false },
            },
          },
        },
        quote: quote(),
        classes,
      })
    );
    expect(result).toContain('Useful budget');
    expect(result).toContain('wasted');
    expect(result).toContain('for grabs');
    expect(result).not.toContain('Empty card');
    expect((result.match(/data-person-detail=/g) ?? []).length).toBe(1);
  });
  test('a meaningful comparison remains visible even when current spending is zero', () => {
    expect(
      hasPersonInformation(
        {
          id: 'prior',
          current: emptyTotal(),
          previous: { ...emptyTotal(), costUsd: 20 },
          companies: [],
          models: [],
        },
        emptyFees,
        true,
        null
      )
    ).toBe(true);
  });
});
