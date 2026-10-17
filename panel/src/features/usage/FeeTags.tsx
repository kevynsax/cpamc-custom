import { useTranslation } from 'react-i18next';
import { formatReais, type CrossQuote } from './currency';
import type { FeeAmount, UnusedFeeValue } from './fees';

export function FeeTags({
  value,
  current,
  kind,
  quote,
  classes: styles,
}: {
  value: UnusedFeeValue;
  current: boolean;
  kind?: keyof UnusedFeeValue;
  quote: CrossQuote | null;
  classes: Record<string, string>;
}) {
  const { t } = useTranslation();
  const amount = (part: FeeAmount) => {
    if (part.usd === null || part.usd <= 0) return null;
    const formatted = formatReais(part.usd, quote);
    return formatted === '—' ? null : formatted;
  };
  const wasted = kind !== 'available' ? amount(value.wasted) : null;
  const available = current && kind !== 'wasted' ? amount(value.available) : null;
  if (!wasted && !available) return null;
  return (
    <span className={styles.feeTags} data-fee-tags>
      {wasted && (
        <span className={styles.wastedTag}>
          {t('client_usage.wasted_money_tag', { amount: wasted })}
        </span>
      )}
      {available && (
        <span className={styles.availableTag}>
          {t('client_usage.available_money_tag', { amount: available })}
        </span>
      )}
    </span>
  );
}
