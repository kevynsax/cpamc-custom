import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { IconNetwork } from '@/components/ui/icons';
import type { ClientUsageResponse, ClientUsageRow } from '@/services/api/clientUsage';
import { COMPANY_NAMES, type CompanyUsage, type PiePart } from './companies';
import { formatReais, type CrossQuote } from './currency';
import { formatUsagePercent } from './utils';
import { PERSON_COLORS, personColor, donutPath, donutSegments } from './pie';
import { CAPACITY_PARTS } from './spending';
import { companyUnusedValue, personUnusedValue } from './fees';
import { FeeTags } from './FeeTags';
import { ModelValuePie } from './ModelValuePie';
import { SubscriptionFacts } from './SubscriptionFacts';
import type { PersonAllowances } from './personAllocation';
import type { ApiKeyKind } from '@/services/api/keyNames';
import { hasAccountInformation, hasCompanyInformation } from './visibility';

interface CompanyChartProps {
  kinds: Record<string, ApiKeyKind>;
  allowances?: PersonAllowances;
  rows: ClientUsageRow[];
  company: CompanyUsage;
  icon: string | null;
  label: (id: string) => string;
  color: (id: string) => string;
  price: (usd: number, unpriced: number) => string;
  quote: CrossQuote | null;
  classes: Record<string, string>;
}
function CompanyChart({
  rows,
  company,
  icon,
  label,
  color,
  price,
  quote,
  kinds,
  allowances,
  classes: styles,
}: CompanyChartProps) {
  const { t, i18n } = useTranslation();
  const tooltipId = useId();
  const [hovered, setHovered] = useState<string | null>(null),
    [focused, setFocused] = useState<string | null>(null),
    [pinned, setPinned] = useState<string | null>(null);
  const selectedId = hovered ?? focused ?? pinned;
  const active = company.parts.find((part) => part.id === selectedId),
    activeId = active?.id ?? null;
  const centerId = company.current ? 'available' : 'wasted';
  const center = company.parts.find((part) => part.id === centerId);
  const percentage = (part: PiePart) => formatUsagePercent(part.value, i18n.language);
  const description = (part: PiePart) =>
    `${label(part.id)} · ${percentage(part)}${part.costUsd > 0 || part.unpriced > 0 ? ` · ${price(part.costUsd, part.unpriced)}` : ''}`;
  const toggle = (id: string) => setPinned((current) => (current === id ? null : id));
  const dismiss = () => {
    setHovered(null);
    setFocused(null);
    setPinned(null);
  };
  const segments = donutSegments(company.parts);
  const fees = (part: PiePart) =>
    kinds[part.id] === 'system' ? null : (
      <FeeTags
        value={
          CAPACITY_PARTS.has(part.id)
            ? companyUnusedValue(company)
            : (allowances?.[part.id]?.[company.id] ?? personUnusedValue(company, part.id))
        }
        kind={part.id === 'wasted' ? 'wasted' : part.id === 'available' ? 'available' : undefined}
        current={company.current}
        quote={quote}
        classes={styles}
      />
    );
  return (
    <article className={styles.companyCard} data-company={company.id}>
      <header>
        <div>
          {icon ? <img src={icon} alt="" /> : <IconNetwork size={22} />}
          <h3>{COMPANY_NAMES[company.id] ?? company.id}</h3>
        </div>
        {company.chartAvailable && (
          <span aria-label={t('client_usage.capacity_assumption')}>≈</span>
        )}
      </header>
      <div className={styles.companyPriceRow}>
        <strong className={styles.companyPrice}>{price(company.costUsd, company.unpriced)}</strong>
        <FeeTags
          value={companyUnusedValue(company)}
          current={company.current}
          quote={quote}
          classes={styles}
        />
      </div>
      {company.chartAvailable && (
        <div className={styles.pieContent}>
          <div className={styles.pieStage} onPointerLeave={() => setHovered(null)}>
            <svg
              viewBox="0 0 100 100"
              role="group"
              aria-label={COMPANY_NAMES[company.id] ?? company.id}
            >
              <circle
                cx="50"
                cy="50"
                r="37"
                fill="none"
                stroke="var(--chart-track)"
                strokeWidth="14"
              />
              {segments.map((part, index) => {
                const activeSlice = activeId === part.id,
                  radians = (part.middle * Math.PI) / 180;
                return (
                  <path
                    key={part.id}
                    className={styles.donutSlice}
                    d={donutPath(part.start + part.gap / 2, part.value - part.gap)}
                    fill={color(part.id)}
                    fillRule="evenodd"
                    role="button"
                    tabIndex={0}
                    aria-label={description(part)}
                    aria-pressed={pinned === part.id}
                    aria-describedby={activeSlice ? tooltipId : undefined}
                    data-quota-segment={part.id}
                    data-mid-angle={part.middle}
                    data-active={activeSlice}
                    data-muted={activeId !== null && !activeSlice}
                    style={{
                      transform: activeSlice
                        ? `translate(${Math.cos(radians) * 1.8}px, ${Math.sin(radians) * 1.8}px)`
                        : undefined,
                    }}
                    onPointerEnter={() => setHovered(part.id)}
                    onFocus={() => setFocused(part.id)}
                    onBlur={() => setFocused(null)}
                    onClick={() => toggle(part.id)}
                    onKeyDown={(event) => {
                      if (event.key === 'Escape') {
                        event.preventDefault();
                        dismiss();
                      }
                      if (event.key === 'Enter' || event.key === ' ') {
                        event.preventDefault();
                        toggle(part.id);
                      }
                      if (event.key === 'ArrowRight' || event.key === 'ArrowLeft') {
                        event.preventDefault();
                        const paths =
                          event.currentTarget.parentElement?.querySelectorAll<SVGPathElement>(
                            '[data-quota-segment]'
                          );
                        paths?.[
                          (index + (event.key === 'ArrowRight' ? 1 : -1) + segments.length) %
                            segments.length
                        ]?.focus();
                      }
                    }}
                  />
                );
              })}
            </svg>
            <div className={styles.pieCenter} aria-hidden="true">
              <strong>
                {active
                  ? percentage(active)
                  : formatUsagePercent(center?.value ?? 0, i18n.language)}
              </strong>
              {(!active || CAPACITY_PARTS.has(active.id)) && (
                <span>{label(active?.id ?? centerId)}</span>
              )}
            </div>
            {active && (
              <div id={tooltipId} className={styles.chartTooltip} role="tooltip">
                <span className={styles.tooltipDot} style={{ backgroundColor: color(active.id) }} />
                {CAPACITY_PARTS.has(active.id) && <strong>{label(active.id)}</strong>}
                <b>{percentage(active)}</b>
                <small>
                  {(active.costUsd > 0 || active.unpriced > 0) &&
                    price(active.costUsd, active.unpriced)}
                  {fees(active)}
                </small>
              </div>
            )}
          </div>
          <ul className={styles.pieLegend}>
            {company.parts.map((part) => (
              <li
                key={part.id}
                data-highlighted={activeId === part.id}
                data-muted={activeId !== null && activeId !== part.id}
              >
                <button
                  type="button"
                  aria-label={description(part)}
                  aria-pressed={pinned === part.id}
                  aria-describedby={activeId === part.id ? tooltipId : undefined}
                  onPointerEnter={() => setHovered(part.id)}
                  onPointerLeave={() => setHovered(null)}
                  onFocus={() => setFocused(part.id)}
                  onBlur={() => setFocused(null)}
                  onClick={() => toggle(part.id)}
                  onKeyDown={(event) => {
                    if (event.key === 'Escape') {
                      event.preventDefault();
                      dismiss();
                    }
                  }}
                >
                  <span className={styles.legendDot} style={{ backgroundColor: color(part.id) }} />
                  <span>{label(part.id)}</span>
                  <strong>{percentage(part)}</strong>
                  <small>
                    {(part.costUsd > 0 || part.unpriced > 0) && price(part.costUsd, part.unpriced)}
                    {fees(part)}
                  </small>
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
      <ModelValuePie rows={rows} company={company.id} quote={quote} classes={styles} />
      {company.id !== 'in_house' && (
        <div className={styles.accountBreakdowns}>
          {company.accounts
            .filter((account) => hasAccountInformation(account, company.current, quote))
            .map((account) => (
              <section
                key={account.id}
                className={styles.accountBreakdown}
                data-account-id={account.id}
                aria-label={
                  account.displayName || account.email || t('client_usage.email_unavailable')
                }
              >
                <SubscriptionFacts
                  account={account}
                  quote={quote}
                  current={company.current}
                  classes={styles}
                />
              </section>
            ))}
        </div>
      )}
    </article>
  );
}

export function CompanyPies({
  data,
  companies,
  names,
  quote,
  icons,
  rows = [],
  kinds = {},
  allowances,
  classes: styles,
}: {
  rows?: ClientUsageRow[];
  kinds?: Record<string, ApiKeyKind>;
  allowances?: PersonAllowances;
  data: ClientUsageResponse;
  companies: CompanyUsage[];
  names: Record<string, string>;
  quote: CrossQuote | null;
  icons: Record<string, string | null>;
  classes: Record<string, string>;
}) {
  const { t } = useTranslation();
  // Most used (by priced value) first; the alphabetical input order breaks ties.
  const visibleCompanies = companies
    .filter((company) => hasCompanyInformation(company, quote))
    .sort((a, b) => b.costUsd - a.costUsd);
  const label = (id: string) =>
    (
      ({
        available: t('client_usage.available_title'),
        wasted: t('client_usage.wasted_title'),
      }) as Record<string, string>
    )[id] ||
    names[id] ||
    data.keyLabels[id] ||
    id.slice(0, 8);
  const actorIds = [
    ...new Set(
      companies
        .flatMap((company) => company.parts.map((part) => part.id))
        .filter((id) => !CAPACITY_PARTS.has(id))
    ),
  ].sort();
  const color = (id: string) =>
    (
      ({ available: 'var(--quota-remaining)', wasted: 'var(--quota-expired)' }) as Record<
        string,
        string
      >
    )[id] ||
    PERSON_COLORS[actorIds.indexOf(id) % PERSON_COLORS.length] ||
    personColor(id);
  const price = (usd: number, missing: number) => {
    const value = formatReais(usd, quote);
    return value === '—' ? value : missing > 0 && usd === 0 ? '—' : value;
  };
  if (!visibleCompanies.length) return null;
  return (
    <Card title={<h2>{t('client_usage.subscription_title')}</h2>}>
      <div className={styles.companyGrid}>
        {visibleCompanies.map((company) => (
          <CompanyChart
            key={company.id}
            rows={rows}
            kinds={kinds}
            allowances={allowances}
            company={company}
            icon={icons[company.id]}
            label={label}
            color={color}
            price={price}
            quote={quote}
            classes={styles}
          />
        ))}
      </div>
    </Card>
  );
}
