import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { IconChevronLeft, IconRefreshCw, IconScrollText, IconTimer } from '@/components/ui/icons';
import { getAuthFileIcon, getTypeColor } from '@/features/authFiles/constants';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import { useAuthStore, useConfigStore, useThemeStore } from '@/stores';
import { buildCompanyUsage, companyId, usageRange, type UsagePeriod } from './companies';
import { CompanyPies } from './CompanyPies';
import { PeopleSpend } from './PeopleSpend';
import { ModelPriceCards } from './ModelPriceCards';
import { resolveUsageKeyNames } from './keyNames';
import { useClientUsage } from './useClientUsage';
import { useCrossQuote } from './useCrossQuote';
import styles from './UsagePage.module.scss';

export function UsagePage() {
  const { t } = useTranslation();
  const [period, setPeriod] = useState<UsagePeriod>('week');
  const [offset, setOffset] = useState(0);
  const range = usageRange(period, offset);
  const { data, catalog, connected, loading, error, saving, refresh, saveSettings } =
    useClientUsage(range.from, range.to);
  const quote = useCrossQuote();
  const theme = useThemeStore((state) => state.resolvedTheme);
  const apiBase = useAuthStore((state) => state.apiBase);
  const config = useConfigStore((state) => state.config);
  const fetchConfig = useConfigStore((state) => state.fetchConfig);
  useEffect(() => {
    if (connected && !config) void fetchConfig().catch(() => {});
  }, [connected, config, fetchConfig]);
  useHeaderRefresh(refresh, connected);
  const names = resolveUsageKeyNames(apiBase, config?.apiKeys ?? []);
  const companies = data ? buildCompanyUsage(data, period, offset) : [];
  const appearance = Object.fromEntries(
    (data?.accounts ?? []).map((account) => [
      companyId(account.provider),
      {
        icon: getAuthFileIcon(account.provider, theme),
        color: getTypeColor(account.provider, theme).text,
      },
    ])
  );
  const icons = Object.fromEntries(
    Object.entries(appearance).map(([id, value]) => [id, value.icon])
  );
  const choosePeriod = (value: UsagePeriod) => {
    setPeriod(value);
    setOffset(0);
  };

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <h1>
          <IconScrollText />
          {t('client_usage.title')}
        </h1>
        <div className={styles.controls}>
          <a
            className={styles.fxQuote}
            href="https://cross-otc.com"
            target="_blank"
            rel="noreferrer"
            title="Cross · USDT/BRL · buy"
          >
            {quote
              ? `R$ ${quote.rate.toLocaleString('pt-BR', { maximumFractionDigits: 4 })}`
              : 'R$ —'}{' '}
            · Cross
          </a>
          <Button
            variant="ghost"
            onClick={() => void refresh()}
            disabled={!connected || loading || saving}
            aria-label={t('client_usage.refresh')}
          >
            <IconRefreshCw size={18} />
          </Button>
        </div>
      </header>
      <div className={styles.periodControls}>
        <div className={styles.periodCards}>
          {(['week', 'month'] as const).map((value) => (
            <button
              type="button"
              key={value}
              className={styles.periodCard}
              aria-pressed={period === value}
              disabled={saving}
              onClick={() => choosePeriod(value)}
            >
              <IconTimer size={18} />
              <strong>{t(`client_usage.current_${value}`)}</strong>
            </button>
          ))}
        </div>
        <div className={styles.weekNavigation}>
          <Button
            variant="ghost"
            disabled={offset >= (period === 'month' ? 11 : 50) || saving}
            aria-label={t('client_usage.previous_period')}
            onClick={() => setOffset(offset + 1)}
          >
            <IconChevronLeft size={18} />
          </Button>
          {offset > 0 && <h2>{t('client_usage.period_offset', { offset })}</h2>}
          <Button
            variant="ghost"
            disabled={offset === 0 || saving}
            aria-label={t('client_usage.next_period')}
            onClick={() => setOffset(offset - 1)}
          >
            <IconChevronLeft size={18} className={styles.nextIcon} />
          </Button>
        </div>
      </div>
      {error && (
        <h2 role="alert" className={styles.status}>
          {t('client_usage.unavailable_title')}
        </h2>
      )}
      {loading && !data && (
        <h2 role="status" className={styles.status}>
          {t('common.loading')}
        </h2>
      )}
      {data && !data.collectionEnabled && (
        <h2 role="status" className={styles.status}>
          {t('client_usage.paused_title')}
        </h2>
      )}
      {data && (
        <PeopleSpend
          data={data}
          companies={companies}
          names={names}
          quote={quote}
          appearance={appearance}
          classes={styles}
        />
      )}
      {data && (
        <CompanyPies
          data={data}
          companies={companies}
          names={names}
          quote={quote}
          icons={icons}
          classes={styles}
        />
      )}
      {catalog && (
        <ModelPriceCards
          data={catalog}
          quote={quote}
          busy={saving || loading}
          onSave={saveSettings}
        />
      )}
    </div>
  );
}
