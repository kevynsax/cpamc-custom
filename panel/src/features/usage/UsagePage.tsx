import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { IconChevronLeft, IconNetwork, IconRefreshCw, IconScrollText } from '@/components/ui/icons';
import { getAuthFileIcon } from '@/features/authFiles/constants';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import { useThemeStore } from '@/stores';
import { fallbackPeople, formatUsageMoney, formatUsagePercent } from './utils';
import { useClientUsage } from './useClientUsage';
import { UsageSettings } from './UsageSettings';
import styles from './UsagePage.module.scss';

export function UsagePage() {
  const { t, i18n } = useTranslation();
  const { data, connected, loading, error, saving, refresh, saveSettings, refreshPrices } =
    useClientUsage();
  const theme = useThemeStore((state) => state.resolvedTheme);
  const [accountChoice, setAccountChoice] = useState('');
  const [scopeChoice, setScopeChoice] = useState('');
  const [weekChoice, setWeekChoice] = useState('');
  useHeaderRefresh(refresh, connected);

  const account =
    data?.accounts.find((entry) => entry.id === accountChoice) ??
    data?.accounts.find((entry) => data.weeks.some((week) => week.accountId === entry.id)) ??
    data?.accounts[0];
  const accountId = account?.id ?? '';
  const allWeeks = data?.weeks.filter((week) => week.accountId === accountId) ?? [];
  const scopes = [...new Set(allWeeks.map((week) => week.scope))].sort((a, b) =>
    a === 'account' ? -1 : b === 'account' ? 1 : a.localeCompare(b)
  );
  const scope = scopes.includes(scopeChoice) ? scopeChoice : scopes[0];
  const weeks = allWeeks
    .filter((week) => week.scope === scope)
    .sort((a, b) => b.reset.localeCompare(a.reset));
  const week = weeks.find((entry) => entry.id === weekChoice) ?? weeks[0];
  const weekIndex = week ? weeks.indexOf(week) : -1;
  const people = week?.people ?? (data ? fallbackPeople(data.rows, accountId) : []);
  const totalCost = people.reduce((sum, person) => sum + person.costUsd, 0);
  const unpriced = people.reduce((sum, person) => sum + person.unpriced, 0);
  const largest = Math.max(...people.map((person) => person.costUsd), Number.MIN_VALUE);
  const personName = (id: string) =>
    data?.settings.names[id] ||
    data?.keyLabels[id] ||
    (id === 'unknown' ? t('client_usage.unknown') : id.slice(0, 12));
  const money = (value: number) => formatUsageMoney(value, i18n.language);
  const price = (value: number, missing: number) =>
    missing > 0
      ? value >= 0.000001
        ? `≥ ${money(value)}`
        : t('client_usage.unpriced')
      : money(value);
  const percent = (value: number) => formatUsagePercent(value, i18n.language);
  const date = (value: string) =>
    new Date(value).toLocaleDateString(i18n.language, { month: 'short', day: 'numeric' });
  const stamp = (value: string) => new Date(value).toLocaleString(i18n.language);
  const plan =
    data?.settings.plans[accountId] ||
    week?.plan ||
    account?.plan ||
    t('client_usage.plan_unknown');
  const historyStart = Math.max(0, weekIndex - 5);
  const history = weeks.slice(historyStart, historyStart + 12).reverse();

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div>
          <h1>
            <IconScrollText /> {t('client_usage.title')}
          </h1>
          <p>{t('client_usage.description')}</p>
        </div>
        <Button onClick={() => void refresh()} disabled={!connected || loading || saving}>
          <IconRefreshCw size={16} /> {t('client_usage.refresh')}
        </Button>
      </header>
      {error && (
        <p role="alert" className="error-box">
          {t('client_usage.error')}
        </p>
      )}
      {loading && <p role="status">{t('client_usage.loading')}</p>}
      {data && !data.collectionEnabled && <p role="status">{t('client_usage.disabled')}</p>}
      {data && (
        <>
          <div className={styles.accountPicker} aria-label={t('client_usage.choose_account')}>
            {data.accounts.map((entry) => {
              const icon = getAuthFileIcon(entry.provider, theme);
              return (
                <button
                  key={entry.id}
                  type="button"
                  className={styles.accountOption}
                  aria-pressed={entry.id === accountId}
                  onClick={() => {
                    setAccountChoice(entry.id);
                    setScopeChoice('');
                    setWeekChoice('');
                  }}
                >
                  {icon ? <img src={icon} alt="" /> : <IconNetwork size={22} />}
                  <span>
                    <strong>{data.accountLabels[entry.id] || entry.provider}</strong>
                    <small>
                      {entry.provider} · {entry.id.slice(0, 8)}
                    </small>
                  </span>
                </button>
              );
            })}
          </div>
          {scopes.length > 1 && (
            <div className={styles.accountPicker} aria-label={t('client_usage.choose_window')}>
              {scopes.map((entry) => (
                <button
                  key={entry}
                  type="button"
                  className={styles.accountOption}
                  aria-pressed={entry === scope}
                  onClick={() => {
                    setScopeChoice(entry);
                    setWeekChoice('');
                  }}
                >
                  <IconNetwork size={18} />
                  <span>{entry === 'account' ? t('client_usage.account_window') : entry}</span>
                </button>
              ))}
            </div>
          )}
          <section className={styles.weekHeader} aria-label={t('client_usage.week')}>
            <div>
              <span className={styles.note}>
                {week
                  ? t(week.closed ? 'client_usage.finished_week' : 'client_usage.current_week')
                  : t('client_usage.last_seven_days')}
              </span>
              <h2>
                {week ? `${date(week.start)} — ${date(week.reset)}` : t('client_usage.no_schedule')}
              </h2>
              {week && (
                <span className={styles.note}>
                  {plan} · {t('client_usage.reset_time', { time: stamp(week.reset) })}
                </span>
              )}
            </div>
            {week && (
              <div className={styles.weekNavigation}>
                <Button
                  variant="secondary"
                  disabled={weekIndex >= weeks.length - 1}
                  onClick={() => setWeekChoice(weeks[weekIndex + 1].id)}
                >
                  <IconChevronLeft size={16} />
                  {t('client_usage.previous_week')}
                </Button>
                <Button
                  variant="secondary"
                  disabled={weekIndex <= 0}
                  onClick={() => setWeekChoice(weeks[weekIndex - 1].id)}
                >
                  {t('client_usage.next_week')}
                  <IconChevronLeft size={16} className={styles.nextIcon} />
                </Button>
              </div>
            )}
          </section>
          {!week && (
            <p className={styles.note}>
              {t('client_usage.no_schedule_hint')}{' '}
              <Link to="/quota">{t('client_usage.open_quota')}</Link>
            </p>
          )}
          <Card className={styles.valueCard}>
            <span>{t('client_usage.api_value')}</span>
            <strong className={styles.headline}>{price(totalCost, unpriced)}</strong>
            <p className={styles.note}>{t('client_usage.value_hint')}</p>
            {unpriced > 0 && (
              <p role="status" className={styles.note}>
                {t('client_usage.unpriced_count', { count: unpriced })}
              </p>
            )}
          </Card>
          <div className={styles.chartGrid}>
            <Card title={t('client_usage.spend_title')}>
              <p className={styles.note}>{t('client_usage.spend_hint')}</p>
              {people.length === 0 ? (
                <p>{t('client_usage.empty')}</p>
              ) : (
                <ul className={styles.barList}>
                  {people.map((person) => (
                    <li key={person.keyId}>
                      <div className={styles.barLabel}>
                        <span title={person.keyId}>{personName(person.keyId)}</span>
                        <strong>{price(person.costUsd, person.unpriced)}</strong>
                      </div>
                      <div
                        className={styles.track}
                        role="img"
                        aria-label={`${personName(person.keyId)}: ${price(person.costUsd, person.unpriced)}`}
                      >
                        <span style={{ width: `${(100 * person.costUsd) / largest}%` }} />
                      </div>
                    </li>
                  ))}
                </ul>
              )}
            </Card>
            <Card title={t('client_usage.subscription_title')}>
              <p className={styles.note}>{t('client_usage.subscription_hint', { plan })}</p>
              {!week ? (
                <p>{t('client_usage.quota_unavailable')}</p>
              ) : (
                <>
                  <div className={styles.capacitySummary}>
                    <strong>{percent(week.used)}</strong>
                    <span>{t('client_usage.used')}</span>
                    <strong>{percent(week.remaining)}</strong>
                    <span>
                      {t(
                        week.closed ? 'client_usage.last_remaining' : 'client_usage.remaining_now'
                      )}
                    </span>
                  </div>
                  <div
                    className={styles.track}
                    role="img"
                    aria-label={t('client_usage.capacity_label', {
                      used: percent(week.used),
                      remaining: percent(week.remaining),
                    })}
                  >
                    <span style={{ width: `${Math.min(100, week.used)}%` }} />
                  </div>
                  <ul className={styles.barList}>
                    {people.map((person) => (
                      <li key={person.keyId}>
                        <div className={styles.barLabel}>
                          <span>{personName(person.keyId)}</span>
                          <strong>
                            {week.allocationAvailable ? percent(person.percent) : '—'}
                          </strong>
                        </div>
                        <div
                          className={styles.track}
                          role="img"
                          aria-label={`${personName(person.keyId)}: ${week.allocationAvailable ? percent(person.percent) : '—'}`}
                        >
                          <span style={{ width: `${Math.min(100, person.percent)}%` }} />
                        </div>
                      </li>
                    ))}
                  </ul>
                  {week.unattributed > 0 && (
                    <p className={styles.note}>
                      {percent(week.unattributed)} · {t('client_usage.unknown')}
                    </p>
                  )}
                  {week.incomplete && <p className={styles.note}>{t('client_usage.partial')}</p>}
                  <details className={styles.explainer}>
                    <summary>{t('client_usage.estimate_details')}</summary>
                    <p className={styles.note}>{t('client_usage.estimate_hint')}</p>
                    <p className={styles.note}>
                      {t('client_usage.unattributed', { percent: percent(week.unattributed) })}
                    </p>
                    {week.incomplete && (
                      <p className={styles.note}>{t('client_usage.incomplete')}</p>
                    )}
                  </details>
                  <p className={styles.note}>
                    {t('client_usage.observed', { time: stamp(week.lastObserved) })}
                  </p>
                </>
              )}
            </Card>
          </div>
          <Card title={t('client_usage.unused_title')}>
            <p className={styles.note}>{t('client_usage.unused_short')}</p>
            {history.length === 0 ? (
              <p>{t('client_usage.quota_unavailable')}</p>
            ) : (
              <div className={styles.history} aria-label={t('client_usage.unused_title')}>
                {history.map((entry) => {
                  const remaining = entry.closed ? entry.unusedUpperBound : entry.remaining;
                  return (
                    <div key={entry.id} className={styles.historyColumn}>
                      <strong>
                        {remaining === null
                          ? '—'
                          : `${entry.closed && !(remaining > 0 && remaining < 0.1) ? '≤ ' : ''}${percent(remaining)}`}
                      </strong>
                      <div
                        className={`${styles.historyTrack} ${remaining === null ? styles.noObservation : ''}`}
                        role="img"
                        aria-label={`${date(entry.reset)}: ${remaining === null ? t('client_usage.no_closing_observation') : `${t(entry.closed ? 'client_usage.unused_estimate' : 'client_usage.still_available')} ${percent(remaining)}`}`}
                      >
                        <span style={{ height: `${remaining ?? 0}%` }} />
                      </div>
                      <span>{date(entry.reset)}</span>
                      <small>
                        {t(
                          !entry.closed
                            ? 'client_usage.still_available'
                            : remaining === null
                              ? 'client_usage.no_closing_observation'
                              : 'client_usage.unused_estimate'
                        )}
                      </small>
                    </div>
                  );
                })}
              </div>
            )}
            <details className={styles.explainer}>
              <summary>{t('client_usage.estimate_details')}</summary>
              <p className={styles.note}>{t('client_usage.unused_hint')}</p>
            </details>
          </Card>
          <UsageSettings
            key={accountId}
            data={data}
            accountId={accountId}
            busy={saving || loading}
            onSave={saveSettings}
            onRefreshPrices={refreshPrices}
          />
        </>
      )}
    </div>
  );
}
