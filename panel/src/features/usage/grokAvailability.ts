import type { CachedUsageQuota, ClientUsageResponse } from '@/services/api/clientUsage';
import {
  GROK_WEEK_SECONDS,
  resolveGrokWeekly,
  type GrokWeeklyWindow,
} from '@/utils/quota/grokAvailability';
import { quotaIsFresh } from './reporting';

const isGrok = (provider: string) => ['xai', 'grok'].includes(provider.toLowerCase());
const isWeekly = (quota: CachedUsageQuota) =>
  quota.periodSeconds === GROK_WEEK_SECONDS && quota.scope === 'account';

/** A current display projection, never a quota-history observation. */
export function withGrokAvailability(
  data: ClientUsageResponse,
  now = Date.now()
): ClientUsageResponse {
  const windowsByAccount = new Map(
    data.accounts.map((account) => [
      account.id,
      [
        ...(account.cachedQuotas ?? []).filter((quota) => !quota.assumed),
        ...data.weeks
          .filter((cycle) => cycle.accountId === account.id && !cycle.closed)
          .map((cycle): CachedUsageQuota => ({
            scope: cycle.scope,
            used: cycle.used,
            reset: cycle.reset,
            observedAt: cycle.lastObserved,
            periodSeconds:
              cycle.periodSeconds ?? (Date.parse(cycle.reset) - Date.parse(cycle.start)) / 1000,
          })),
      ],
    ])
  );
  const peers: GrokWeeklyWindow[] = data.accounts
    .filter((account) => isGrok(account.provider) && !account.disabled)
    .flatMap((account) =>
      (windowsByAccount.get(account.id) ?? [])
        .filter(isWeekly)
        .map((quota) => ({ plan: account.plan, ...quota }))
    );
  return {
    ...data,
    accounts: data.accounts.map((account) => {
      if (!isGrok(account.provider) || account.disabled) return account;
      const windows = windowsByAccount.get(account.id) ?? [];
      const timestamp = (quota: CachedUsageQuota) =>
        Number.isFinite(Date.parse(quota.observedAt)) ? Date.parse(quota.observedAt) : -Infinity;
      const weekly = windows.filter(isWeekly).sort((a, b) => timestamp(b) - timestamp(a))[0];
      const availability = resolveGrokWeekly(
        {
          plan: account.plan,
          scope: weekly?.scope ?? 'account',
          periodSeconds: GROK_WEEK_SECONDS,
          used: weekly?.used ?? null,
          reset: weekly?.reset,
          usable: weekly ? quotaIsFresh(weekly, now) : false,
        },
        peers
      );
      const display: CachedUsageQuota =
        weekly && !availability.assumed
          ? weekly
          : {
              scope: availability.scope,
              used: availability.used,
              reset: availability.reset ?? '',
              observedAt: '',
              periodSeconds: availability.periodSeconds,
              assumed: true,
            };
      return {
        ...account,
        cachedQuotas: [
          ...(account.cachedQuotas ?? []).filter((quota) => !isWeekly(quota)),
          display,
        ],
      };
    }),
  };
}
