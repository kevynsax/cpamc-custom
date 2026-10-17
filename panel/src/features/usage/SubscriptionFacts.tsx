import { useTranslation } from 'react-i18next';
import type { AccountCapacity } from './companies';
import { formatReais, type CrossQuote } from './currency';
import { quotaIsFresh } from './reporting';
import { latestQuotaSnapshots } from './quotaSnapshots';

function friendlyPlanName(plan: string): string {
  const key = plan.toLowerCase().replace(/[^a-z0-9]/g, '');
  if (key.includes('max20') || key.includes('max200')) return 'Max 20×';
  if (key.includes('max5') || key.includes('max100')) return 'Max 5×';
  if (key === 'prolite' || key.includes('pro100')) return 'Pro 100';
  return plan
    .replace(/^default_/, '')
    .replace(/^(?:claude|chatgpt|codex)[ _-]*/i, '')
    .replace(/[_-]+/g, ' ')
    .trim();
}
export function SubscriptionFacts({
  account,
  quote,
  current,
  classes: styles,
}: {
  account: AccountCapacity;
  quote: CrossQuote | null;
  current: boolean;
  classes: Record<string, string>;
}) {
  const { t, i18n } = useTranslation();
  const quotas = current ? latestQuotaSnapshots(account.cachedQuotas ?? []) : [];
  const identity = [account.displayName || account.email, friendlyPlanName(account.plan ?? '')]
    .filter(Boolean)
    .join(' — ');
  return (
    <div className={styles.subscriptionFacts}>
      {(identity || account.monthlyFeeUsd != null) && (
        <header className={styles.subscriptionSummary}>
          <strong title={identity}>{identity}</strong>
          {account.monthlyFeeUsd != null && <b>{formatReais(account.monthlyFeeUsd, quote)}</b>}
        </header>
      )}
      {quotas.length > 0 && (
        <section className={styles.cachedQuotas} aria-label={t('client_usage.cached_availability')}>
          {quotas.map((quota) => {
            const fresh = quota.assumed === true || quotaIsFresh(quota),
              duration = quota.periodSeconds / 3600;
            const scope = quota.scope === 'account' ? '' : quota.scope;
            const label = `${scope ? `${scope} · ` : ''}${t(duration >= 24 ? 'client_usage.pool_days' : 'client_usage.pool_hours', { count: duration >= 24 ? duration / 24 : duration })}`;
            const remaining = 100 - quota.used;
            return (
              <div
                key={`${quota.scope}:${quota.periodSeconds}`}
                className={styles.slimQuotaRow}
                data-quota-fresh={fresh}
              >
                <span>{label}</span>
                <div
                  className={styles.slimQuotaTrack}
                  role="meter"
                  aria-label={`${label} · ${t('client_usage.available_title')}`}
                  aria-valuemin={0}
                  aria-valuemax={100}
                  aria-valuenow={fresh ? remaining : undefined}
                  aria-valuetext={fresh ? `${remaining}%` : t('client_usage.quota_stale')}
                  title={quota.assumed ? t('xai_quota.assumed_weekly') : undefined}
                >
                  {fresh && <i style={{ width: `${remaining}%` }} />}
                </div>
                <b>
                  {fresh
                    ? `${remaining.toLocaleString(i18n.language, { maximumFractionDigits: 1 })}%`
                    : '—'}
                </b>
              </div>
            );
          })}
        </section>
      )}
      {!current && account.quotaMeasured && (
        <div
          className={styles.accountTrack}
          role="img"
          aria-label={`${t('client_usage.used_title')} ${account.used}% · ${t('client_usage.wasted_title')} ${account.expired}%`}
        >
          <span className={styles.accountUsed} style={{ width: `${account.used}%` }} />
          <span className={styles.accountWasted} style={{ width: `${account.expired}%` }} />
        </div>
      )}
    </div>
  );
}
