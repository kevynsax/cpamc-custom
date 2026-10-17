import type { ApiKeyKind } from '@/services/api/keyNames';
import type { CompanyUsage } from './companies';
import { companyUnusedValue, type UnusedFeeValue } from './fees';
import { CAPACITY_PARTS } from './spending';

export interface CapacityShare {
  id: string;
  entitlement: number;
  used: number;
  deficit: number;
  unusedShare: number;
}

/** System use is removed before equal human shares. Overuse cannot create extra waste. */
export function splitPersonCapacity(
  capacity: number,
  systemUsed: number,
  people: { id: string; used: number }[]
): CapacityShare[] {
  if (!people.length || capacity <= 0) return [];
  const entitlement = Math.max(0, capacity - systemUsed) / people.length;
  const parts = people.map((person) => ({
    ...person,
    entitlement,
    deficit: Math.max(0, entitlement - person.used),
  }));
  const missing = parts.reduce((sum, person) => sum + person.deficit, 0);
  return parts.map((person) => ({
    ...person,
    unusedShare: missing > 0 ? person.deficit / missing : 0,
  }));
}
export type PersonAllowances = Record<string, Record<string, UnusedFeeValue>>;
export function allocatePersonFees(
  companies: CompanyUsage[],
  personIds: string[],
  kinds: Record<string, ApiKeyKind>
): PersonAllowances {
  const ids = [...new Set(personIds)].filter((id) => kinds[id] !== 'system');
  const result: PersonAllowances = Object.fromEntries(ids.map((id) => [id, {}]));
  for (const company of companies) {
    if (!company.chartAvailable || !ids.length) continue;
    const capacity = company.accounts.reduce(
      (sum, account) => sum + (account.capacityWeight ?? 1),
      0
    );
    const usage = (id: string) =>
      (capacity * (company.parts.find((part) => part.id === id)?.value ?? 0)) / 100;
    const systemUsed = company.parts
      .filter((part) => !CAPACITY_PARTS.has(part.id) && kinds[part.id] === 'system')
      .reduce((sum, part) => sum + (capacity * part.value) / 100, 0);
    const parts = splitPersonCapacity(
      capacity,
      systemUsed,
      ids.map((id) => ({ id, used: usage(id) }))
    );
    const unused = companyUnusedValue(company);
    for (const person of parts)
      result[person.id][company.id] = {
        wasted: {
          ...unused.wasted,
          usd: unused.wasted.usd === null ? null : unused.wasted.usd * person.unusedShare,
        },
        available: {
          ...unused.available,
          usd: unused.available.usd === null ? null : unused.available.usd * person.unusedShare,
        },
      };
  }
  return result;
}
