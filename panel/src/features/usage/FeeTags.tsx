import { useTranslation } from 'react-i18next';
import { formatReais, type CrossQuote } from './currency';
import type { FeeAmount, UnusedFeeValue } from './fees';

export function FeeTags({
  value,
  current,
  quote,
  classes: styles,
}: {
  value: UnusedFeeValue;
  current: boolean;
  quote: CrossQuote | null;
  classes: Record<string, string>;
}) {
  const { t } = useTranslation();
  const amount = (part: FeeAmount) => {
    if (part.usd === null) return 'R$ —';
    const formatted = formatReais(part.usd, quote);
    return formatted === '—' ? 'R$ —' : `${part.partial ? '≥ ' : ''}${formatted}`;
  };
  return (
    <span className={styles.feeTags} data-fee-tags>
      <span
        className={styles.wastedTag}
        title={t(
          value.wasted.usd === null ? 'client_usage.fee_unknown' : 'client_usage.fee_estimate'
        )}
      >
        {t('client_usage.wasted_money_tag', { amount: amount(value.wasted) })}
      </span>
      {current && (
        <span
          className={styles.availableTag}
          title={t(
            value.available.usd === null ? 'client_usage.fee_unknown' : 'client_usage.fee_estimate'
          )}
        >
          {t('client_usage.available_money_tag', { amount: amount(value.available) })}
        </span>
      )}
    </span>
  );
}
