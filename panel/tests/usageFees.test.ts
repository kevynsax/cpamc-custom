import { describe, expect, test } from 'bun:test';
import { buildCompanyUsage } from '../src/features/usage/companies';
import {
  monthlyPlanFee,
  proratedPlanFee,
  companyUnusedValue,
  personUnusedValue,
  sumFeeValues,
} from '../src/features/usage/fees';
import { normalizeClientUsage } from '../src/services/api/clientUsage';
import { createUsageFixture } from '../dev/usage/fixtures';

const now = new Date('2026-10-17T12:00:00Z');
describe('unused subscription-fee values', () => {
  test('uses subscription fees, never API token spending, and leaves unknown fees unset', () => {
    expect(monthlyPlanFee('anthropic', 'Max 20x')).toBe(200);
    expect(monthlyPlanFee('openai', 'prolite')).toBe(100);
    expect(monthlyPlanFee('xai', 'GrokPro')).toBeNull();
    expect(
      proratedPlanFee(200, Date.parse('2026-10-12T00:00:00Z'), Date.parse('2026-10-19T00:00:00Z'))
    ).toBeCloseTo((200 * 7) / 31, 10);
    expect(
      proratedPlanFee(200, Date.parse('2026-10-01T00:00:00Z'), Date.parse('2026-11-01T00:00:00Z'))
    ).toBe(200);
    expect(
      proratedPlanFee(300, Date.parse('2026-09-28T00:00:00Z'), Date.parse('2026-10-05T00:00:00Z'))
    ).toBeCloseTo((300 * 3) / 30 + (300 * 4) / 31, 10);
  });
  test('current-month available value excludes expired capacity', () => {
    const data = normalizeClientUsage(createUsageFixture(now));
    const company = buildCompanyUsage(data, 'month', 0, now).find(
      (entry) => entry.id === 'anthropic'
    )!;
    const account = company.accounts[0];
    const values = companyUnusedValue(company);
    expect(account.periodFeeUsd).toBe(200);
    expect(values.available.usd).toBeCloseTo((200 * account.remaining) / 100, 8);
    expect(values.wasted.usd).toBeCloseTo((200 * account.expired) / 100, 8);
    expect(values.available.usd! + values.wasted.usd!).toBeCloseTo((200 * account.unused) / 100, 8);
    expect(values.wasted.usd).not.toBe(company.costUsd);
  });
  test('per-person fee shares conserve the company values', () => {
    const companies = buildCompanyUsage(
      normalizeClientUsage(createUsageFixture(now)),
      'month',
      0,
      now
    );
    const company = companies.find((entry) => entry.id === 'anthropic')!;
    expect(company.chartAvailable).toBe(true);
    const people = company.parts.filter(
      (part) => !['available', 'wasted', 'unknown', 'unidentified'].includes(part.id)
    );
    const allocated = sumFeeValues(people.map((person) => personUnusedValue(company, person.id)));
    expect(allocated.available.usd).toBeCloseTo(companyUnusedValue(company).available.usd!, 8);
    expect(allocated.wasted.usd).toBeCloseTo(companyUnusedValue(company).wasted.usd!, 8);
    const google = companies.find((entry) => entry.id === 'google')!;
    expect(google.chartAvailable).toBe(false);
    expect(companyUnusedValue(google).wasted.usd).toBeNull();
    expect(
      sumFeeValues([companyUnusedValue(company), companyUnusedValue(google)]).wasted.partial
    ).toBe(true);
  });
  test('emails come from account metadata, not generic account numbers', () => {
    const companies = buildCompanyUsage(
      normalizeClientUsage(createUsageFixture(now)),
      'week',
      0,
      now
    );
    expect(
      companies
        .flatMap((company) => company.accounts)
        .every((account) => account.email.endsWith('@example.test'))
    ).toBe(true);
    expect(companies.filter((company) => company.chartAvailable).length).toBeGreaterThan(0);
    expect(companies.filter((company) => !company.chartAvailable).length).toBeGreaterThan(0);
  });
});
