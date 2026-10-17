import type { CompanyUsage } from './companies';

export const CAPACITY_PARTS = new Set([
  'available',
  'wasted',
  'unused',
  'unidentified',
  'unknown',
  'remaining',
  'expired',
  'unattributed',
]);
export interface PersonSpending {
  id: string;
  costUsd: number;
  unpriced: number;
  companies: { id: string; costUsd: number; unpriced: number }[];
}

export function spendingByPerson(companies: CompanyUsage[]): PersonSpending[] {
  const people = new Map<string, PersonSpending>();
  for (const company of companies) {
    for (const part of company.parts) {
      if (CAPACITY_PARTS.has(part.id)) continue;
      const person = people.get(part.id) ?? { id: part.id, costUsd: 0, unpriced: 0, companies: [] };
      person.costUsd += part.costUsd;
      person.unpriced += part.unpriced;
      person.companies.push({ id: company.id, costUsd: part.costUsd, unpriced: part.unpriced });
      people.set(person.id, person);
    }
  }
  return [...people.values()].sort((a, b) => b.costUsd - a.costUsd || a.id.localeCompare(b.id));
}
