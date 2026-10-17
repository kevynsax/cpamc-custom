import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { ClientUsageRow } from '@/services/api/clientUsage';
import { donutPath, donutSegments, PERSON_COLORS } from './pie';
import { valueByModel } from './reporting';
import { formatReais, type CrossQuote } from './currency';
import { friendlyModelName } from './modelNames';
import { formatUsagePercent } from './utils';

export interface ValueSlice {
  id: string;
  value: number;
  label: string;
  wastedUsd?: number | null;
}
export function ValuePie({
  title,
  parts,
  quote,
  classes: styles,
}: {
  title: string;
  parts: ValueSlice[];
  quote: CrossQuote | null;
  classes: Record<string, string>;
}) {
  const { t, i18n } = useTranslation();
  const [hovered, setHovered] = useState<string | null>(null);
  const [focused, setFocused] = useState<string | null>(null);
  const [pinned, setPinned] = useState<string | null>(null);
  const selected = hovered ?? focused ?? pinned;
  const toggle = (id: string) => setPinned((value) => (value === id ? null : id));
  const dismiss = () => {
    setHovered(null);
    setFocused(null);
    setPinned(null);
  };
  const slices = parts
    .filter((part) => part.value > 0)
    .sort((a, b) => b.value - a.value || a.label.localeCompare(b.label));
  const total = slices.reduce((sum, part) => sum + part.value, 0);
  const segments = donutSegments(
    slices.map((part) => ({ ...part, value: (100 * part.value) / total, costUsd: part.value }))
  );
  const active = segments.find((part) => part.id === selected);
  const color = (id: string) =>
    PERSON_COLORS[
      slices
        .map((part) => part.id)
        .sort()
        .indexOf(id) % PERSON_COLORS.length
    ];
  const price = (usd: number) => formatReais(usd, quote);
  const describe = (part: (typeof segments)[number]) =>
    `${part.label} · ${price(part.costUsd)} · ${formatUsagePercent(part.value, i18n.language)}`;
  if (total <= 0 || price(total) === '—') return null;
  return (
    <section className={styles.tokenBreakdown} data-value-pie={title}>
      <h3>{title}</h3>
      <div className={styles.pieContent}>
        <div className={styles.pieStage}>
          <svg viewBox="0 0 100 100" role="group" aria-label={title}>
            {segments.map((part, index) => (
              <path
                key={part.id}
                d={donutPath(part.start + part.gap / 2, part.value - part.gap)}
                fill={color(part.id)}
                fillRule="evenodd"
                className={styles.donutSlice}
                role="button"
                tabIndex={0}
                aria-label={describe(part)}
                aria-pressed={pinned === part.id}
                data-muted={!!active && active.id !== part.id}
                onPointerEnter={() => setHovered(part.id)}
                onPointerLeave={() => setHovered(null)}
                onFocus={() => setFocused(part.id)}
                onBlur={() => setFocused(null)}
                onClick={() => toggle(part.id)}
                onKeyDown={(event) => {
                  if (event.key === 'Escape') dismiss();
                  if (event.key === 'Enter' || event.key === ' ') {
                    event.preventDefault();
                    toggle(part.id);
                  }
                  if (event.key === 'ArrowRight' || event.key === 'ArrowLeft') {
                    event.preventDefault();
                    const paths =
                      event.currentTarget.parentElement?.querySelectorAll<SVGPathElement>('path');
                    paths?.[
                      (index + (event.key === 'ArrowRight' ? 1 : -1) + segments.length) %
                        segments.length
                    ]?.focus();
                  }
                }}
              />
            ))}
          </svg>
          <div className={`${styles.pieCenter} ${styles.priceCenter}`} aria-hidden="true">
            <strong>{price(active?.costUsd ?? total)}</strong>
          </div>
        </div>
        <ul className={`${styles.pieLegend} ${styles.modelLegend}`}>
          {segments.map((part) => (
            <li key={part.id} data-muted={!!active && active.id !== part.id}>
              <button
                type="button"
                aria-label={describe(part)}
                aria-pressed={pinned === part.id}
                onPointerEnter={() => setHovered(part.id)}
                onPointerLeave={() => setHovered(null)}
                onFocus={() => setFocused(part.id)}
                onBlur={() => setFocused(null)}
                onClick={() => toggle(part.id)}
                onKeyDown={(event) => {
                  if (event.key === 'Escape') dismiss();
                }}
              >
                <span className={styles.legendDot} style={{ backgroundColor: color(part.id) }} />
                <span>{part.label}</span>
                <strong>{formatUsagePercent(part.value, i18n.language)}</strong>
                <small>
                  {price(part.costUsd)}
                  {part.wastedUsd != null && part.wastedUsd > 0 && (
                    <span className={styles.wastedTag}>
                      {' '}
                      · {t('client_usage.wasted_money_tag', { amount: price(part.wastedUsd) })}
                    </span>
                  )}
                </small>
              </button>
            </li>
          ))}
        </ul>
      </div>
    </section>
  );
}

export function ModelValuePie({
  rows,
  company,
  quote,
  classes,
}: {
  rows: ClientUsageRow[];
  company: string;
  quote: CrossQuote | null;
  classes: Record<string, string>;
}) {
  const { t } = useTranslation();
  return (
    <div data-model-value-chart={company}>
      <ValuePie
        title={t('client_usage.value_by_model')}
        parts={valueByModel(rows, company).map((part) => ({
          ...part,
          label: friendlyModelName(part.id),
        }))}
        quote={quote}
        classes={classes}
      />
    </div>
  );
}
