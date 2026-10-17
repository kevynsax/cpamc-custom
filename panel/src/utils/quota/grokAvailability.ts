import type { AuthFileItem, XaiBillingSummary, XaiQuotaState } from '@/types';
import { resolveXaiSubscriptionPlan } from './builders';
import { sha256 } from '@noble/hashes/sha2.js';
import { bytesToHex } from '@noble/hashes/utils.js';
import type { UsageAccount } from '@/services/api/clientUsage';

export const GROK_WEEK_SECONDS = 7 * 24 * 60 * 60;
export interface GrokWeeklyWindow {
  plan: string;
  used: number | null;
  scope: string;
  periodSeconds: number;
  reset?: string;
  usable?: boolean;
}
export interface GrokWeeklyAvailability extends Omit<GrokWeeklyWindow, 'used'> {
  used: number;
  remaining: number;
  assumed: boolean;
}
const planKey = (value: string) => value.toLowerCase().replace(/[^a-z0-9]/g, '');

// Display-only defaults must never enter routing, observations, or waste accounting.
export function resolveGrokWeekly(
  window: GrokWeeklyWindow,
  peers: GrokWeeklyWindow[] = []
): GrokWeeklyAvailability {
  const reported = window.used;
  if (window.usable !== false && typeof reported === 'number' && Number.isFinite(reported)) {
    const used = Math.max(0, Math.min(100, reported));
    return { ...window, used, remaining: 100 - used, assumed: false };
  }
  const peer = peers.find(
    (candidate) =>
      planKey(window.plan) !== '' &&
      planKey(candidate.plan) === planKey(window.plan) &&
      candidate.periodSeconds === GROK_WEEK_SECONDS
  );
  // Reset instants are account-specific, even when two subscriptions share a plan.
  return {
    ...window,
    scope: peer?.scope ?? window.scope,
    periodSeconds: GROK_WEEK_SECONDS,
    reset: window.reset,
    used: 0,
    remaining: 100,
    assumed: true,
  };
}
export function quotaObservationIsFresh(
  quota: { observedAt: string; reset: string },
  now = Date.now()
) {
  const observed = Date.parse(quota.observedAt),
    reset = Date.parse(quota.reset);
  return (
    Number.isFinite(observed) &&
    Number.isFinite(reset) &&
    observed <= now &&
    now - observed <= 30 * 60_000 &&
    reset > now
  );
}
export function grokFilePlan(file: AuthFileItem): string {
  const metadata =
    file.metadata && typeof file.metadata === 'object'
      ? (file.metadata as Record<string, unknown>)
      : {};
  for (const value of [
    file.plan_type,
    file.planLabel,
    file.subscriptionTier,
    metadata.plan_type,
    metadata.subscriptionTier,
  ])
    if (typeof value === 'string' && value.trim()) return value.trim();
  return '';
}
export function findGrokAccount(
  file: AuthFileItem,
  accounts: UsageAccount[]
): UsageAccount | undefined {
  for (const id of [file.id, file.name]) {
    if (typeof id !== 'string' || !id.trim()) continue;
    const fingerprint = bytesToHex(sha256(new TextEncoder().encode(id)));
    const account = accounts.find(
      (entry) => entry.id === fingerprint && ['xai', 'grok'].includes(entry.provider)
    );
    if (account) return account;
  }
  return undefined;
}
export function grokDisplayQuota(
  file: AuthFileItem,
  quota?: XaiQuotaState,
  account?: UsageAccount,
  now = Date.now()
): XaiQuotaState | null {
  if (
    !quota ||
    quota.status === 'idle' ||
    quota.status === 'loading' ||
    file.disabled ||
    account?.disabled
  )
    return null;
  const plan = resolveXaiSubscriptionPlan(
    quota.billing?.planLabel || grokFilePlan(file) || account?.plan,
    null
  );
  const empty: XaiBillingSummary = {
    mode: 'billing',
    periodType: 'unknown',
    usagePercent: null,
    productUsage: [],
    monthlyLimitCents: null,
    usedCents: null,
    includedUsedCents: null,
    onDemandCapCents: null,
    onDemandUsedCents: null,
    onDemandUsedPercent: null,
    usedPercent: null,
  };
  const display: XaiBillingSummary = {
    ...empty,
    ...quota.billing,
    planLabel: plan?.label,
    planTier: quota.billing?.planTier ?? plan?.tier,
  };
  const measured =
    display.periodType === 'weekly' &&
    display.usagePercent !== null &&
    Number.isFinite(display.usagePercent);
  const cached = account?.cachedQuotas
    ?.filter(
      (window) =>
        window.scope === 'account' &&
        window.periodSeconds === GROK_WEEK_SECONDS &&
        !window.assumed &&
        quotaObservationIsFresh(window, now)
    )
    .sort((a, b) => Date.parse(b.observedAt) - Date.parse(a.observedAt))[0];
  if (!measured && cached) {
    display.periodType = 'weekly';
    display.usagePercent = cached.used;
    display.periodEnd = cached.reset;
    display.periodStart = new Date(
      Date.parse(cached.reset) - GROK_WEEK_SECONDS * 1000
    ).toISOString();
    display.resetAtMs = Date.parse(cached.reset);
    display.periodHours = GROK_WEEK_SECONDS / 3600;
  }
  return { ...quota, billing: display };
}
