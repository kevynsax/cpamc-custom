import type { CachedUsageQuota } from '@/services/api/clientUsage';

/** Reset timestamps can differ by a second between observations of one pool. */
export function latestQuotaSnapshots(quotas: CachedUsageQuota[]): CachedUsageQuota[] {
  const latest = new Map<string, CachedUsageQuota>();
  for (const quota of quotas) {
    const key = JSON.stringify([quota.scope, quota.periodSeconds]);
    const previous = latest.get(key);
    if (!previous || Date.parse(quota.observedAt) > Date.parse(previous.observedAt))
      latest.set(key, quota);
  }
  return [...latest.values()].sort(
    (a, b) => a.scope.localeCompare(b.scope) || b.periodSeconds - a.periodSeconds
  );
}
