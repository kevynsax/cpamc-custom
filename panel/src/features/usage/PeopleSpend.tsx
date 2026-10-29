import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import type { ClientUsageResponse } from '@/services/api/clientUsage';
import { COMPANY_NAMES, type CompanyUsage } from './companies';
import { formatReais, type CrossQuote } from './currency';
import { PERSON_COLORS } from './pie';
import { spendingByPerson } from './spending';
import { FeeTags } from './FeeTags';
import { companyUnusedValue, personUnusedValue, sumFeeValues } from './fees';
import type { PersonAllowances } from './personAllocation';
import type { ApiKeyKind } from '@/services/api/keyNames';
import { hasFeeValue, hasMoney } from './visibility';

export function PeopleSpend({
  data,
  companies,
  names,
  quote,
  appearance,
  kinds = {},
  allowances,
  classes: styles,
}: {
  data: ClientUsageResponse;
  companies: CompanyUsage[];
  names: Record<string, string>;
  quote: CrossQuote | null;
  appearance: Record<string, { icon: string | null; color: string }>;
  kinds?: Record<string, ApiKeyKind>;
  allowances?: PersonAllowances;
  classes: Record<string, string>;
}) {
  const { t } = useTranslation();
  const id = useId();
  const actors = spendingByPerson(companies);
  // System keys show their usage but never receive a share of the wasted or available fee.
  const isSystem = (personId: string) => kinds[personId] === 'system';
  const people = actors.filter(
    (person) =>
      hasMoney(person.costUsd, quote) ||
      (!isSystem(person.id) &&
        allowances &&
        hasFeeValue(
          sumFeeValues(Object.values(allowances[person.id] ?? {})),
          companies.some((company) => company.current),
          quote
        ))
  );
  const [hovered, setHovered] = useState<string | null>(null);
  const [focused, setFocused] = useState<string | null>(null);
  const selectedKey = hovered ?? focused;
  const active = people.some((person) =>
    person.companies.some((company) => selectedKey === `${person.id}:${company.id}`)
  )
    ? selectedKey
    : null;
  const actorIds = actors.map((person) => person.id).sort();
  const name = (key: string) => names[key] || data.keyLabels[key] || key.slice(0, 8);
  const price = (usd: number, missing: number) => {
    const value = formatReais(usd, quote);
    return value === '—' ? value : missing > 0 && usd === 0 ? '—' : value;
  };
  const total = people.reduce((sum, person) => sum + person.costUsd, 0);
  const missing = people.reduce((sum, person) => sum + person.unpriced, 0);
  const maximum = Math.max(...people.map((person) => person.costUsd), Number.MIN_VALUE);
  const current = companies.some((company) => company.current);
  const unusedValue = sumFeeValues(companies.map(companyUnusedValue));
  if (!people.length) return null;
  return (
    <Card title={<h2>{t('client_usage.spend_title')}</h2>} className={styles.peopleCard}>
      <div className={styles.headlineGroup}>
        <strong className={styles.headline}>{people.length ? price(total, missing) : '—'}</strong>
        <FeeTags value={unusedValue} current={current} quote={quote} classes={styles} />
      </div>
      <ul className={styles.peopleSpend}>
        {people.map((person) => {
          const selected = person.companies.find(
            (company) => active === `${person.id}:${company.id}`
          );
          const tooltipId = `${id}-${person.id}`;
          const feeValue = sumFeeValues(
            allowances
              ? Object.values(allowances[person.id] ?? {})
              : companies
                  .filter((company) => company.parts.some((part) => part.id === person.id))
                  .map((company) => personUnusedValue(company, person.id))
          );
          const selectedCompany = selected
            ? companies.find((company) => company.id === selected.id)
            : undefined;
          return (
            <li key={person.id} className={styles.personSpend}>
              <div className={styles.personHeading}>
                <span
                  className={styles.personAvatar}
                  style={{
                    backgroundColor:
                      PERSON_COLORS[actorIds.indexOf(person.id) % PERSON_COLORS.length],
                  }}
                  aria-hidden="true"
                >
                  {Array.from(name(person.id))[0]?.toUpperCase()}
                </span>
                <strong>{name(person.id)}</strong>
                <div className={styles.personPrice}>
                  <span>{price(person.costUsd, person.unpriced)}</span>
                  {!isSystem(person.id) && (
                    <FeeTags value={feeValue} current={current} quote={quote} classes={styles} />
                  )}
                </div>
              </div>
              <div className={styles.spendChart}>
                <div className={styles.spendTrack}>
                  {person.companies
                    .filter((company) => company.costUsd > 0)
                    .map((company) => {
                      const key = `${person.id}:${company.id}`;
                      const label = `${COMPANY_NAMES[company.id] ?? company.id} · ${price(company.costUsd, company.unpriced)}`;
                      return (
                        <span
                          key={company.id}
                          className={styles.spendSegment}
                          tabIndex={0}
                          role="img"
                          data-spend-segment={key}
                          aria-label={label}
                          aria-describedby={active === key ? tooltipId : undefined}
                          data-muted={active !== null && active !== key}
                          style={{
                            width: `${(100 * company.costUsd) / maximum}%`,
                            backgroundColor: appearance[company.id]?.color ?? 'var(--text-primary)',
                          }}
                          onPointerEnter={() => setHovered(key)}
                          onPointerLeave={() => setHovered(null)}
                          onFocus={() => setFocused(key)}
                          onBlur={() => setFocused(null)}
                          onKeyDown={(event) => {
                            if (event.key === 'Escape') {
                              setHovered(null);
                              setFocused(null);
                            }
                          }}
                        />
                      );
                    })}
                </div>
                {selected && (
                  <div id={tooltipId} className={styles.spendTooltip} role="tooltip">
                    <span>{COMPANY_NAMES[selected.id] ?? selected.id}</span>
                    <b>{price(selected.costUsd, selected.unpriced)}</b>
                    {selectedCompany && !isSystem(person.id) && (
                      <FeeTags
                        value={
                          allowances?.[person.id]?.[selectedCompany.id] ??
                          personUnusedValue(selectedCompany, person.id)
                        }
                        current={current}
                        quote={quote}
                        classes={styles}
                      />
                    )}
                  </div>
                )}
              </div>
            </li>
          );
        })}
      </ul>
      <div className={styles.companyLegend}>
        {companies
          .filter((company) =>
            people.some((person) =>
              person.companies.some((entry) => entry.id === company.id && entry.costUsd > 0)
            )
          )
          .map((company) => (
            <span key={company.id}>
              <i
                style={{ backgroundColor: appearance[company.id]?.color ?? 'var(--text-primary)' }}
              />
              {appearance[company.id]?.icon && <img src={appearance[company.id].icon!} alt="" />}
              {COMPANY_NAMES[company.id] ?? company.id}
            </span>
          ))}
      </div>
    </Card>
  );
}
