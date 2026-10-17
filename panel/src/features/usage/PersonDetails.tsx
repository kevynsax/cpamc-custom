import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import type { ClientUsageRow } from '@/services/api/clientUsage';
import { COMPANY_NAMES } from './companies';
import { formatReais, type CrossQuote } from './currency';
import { emptyTotal, mergedCompanyModels, percentChange, personDetails } from './reporting';
import { FeeTags } from './FeeTags';
import { sumFeeValues } from './fees';
import type { PersonAllowances } from './personAllocation';
import { ValuePie } from './ModelValuePie';
import { friendlyModelName } from './modelNames';

export function PersonDetails({
  current,
  previous,
  names,
  quote,
  personIds,
  allowances = {},
  currentPeriod = true,
  classes: styles,
}: {
  current: ClientUsageRow[];
  previous: ClientUsageRow[];
  names: Record<string, string>;
  quote: CrossQuote | null;
  personIds?: string[];
  allowances?: PersonAllowances;
  currentPeriod?: boolean;
  classes: Record<string, string>;
}) {
  const { t, i18n } = useTranslation();
  const observed = personDetails(current, previous);
  const ids = personIds ?? observed.map((person) => person.id);
  const people = ids
    .map(
      (id) =>
        observed.find((person) => person.id === id) ?? {
          id,
          current: emptyTotal(),
          previous: emptyTotal(),
          companies: [],
          models: [],
        }
    )
    .sort((a, b) => b.current.costUsd - a.current.costUsd || a.id.localeCompare(b.id));
  if (!people.length) return null;
  return (
    <Card title={<h2>{t('client_usage.person_details')}</h2>}>
      <div className={styles.personDetailGrid}>
        {people.map((person) => {
          const merged = mergedCompanyModels(current.filter((row) => row.keyId === person.id));
          const change =
            person.current.unpriced + person.previous.unpriced === 0
              ? percentChange(person.current.costUsd, person.previous.costUsd)
              : null;
          return (
            <article key={person.id} className={styles.personDetail} data-person-detail={person.id}>
              <header>
                <h3>{names[person.id] || person.id.slice(0, 8)}</h3>
                <strong>
                  {person.current.unpriced && !person.current.costUsd
                    ? '—'
                    : formatReais(person.current.costUsd, quote)}
                </strong>
              </header>
              <FeeTags
                value={sumFeeValues(Object.values(allowances[person.id] ?? {}))}
                current={currentPeriod}
                quote={quote}
                classes={styles}
              />
              {change !== null && (
                <div className={styles.detailChange}>
                  <b>
                    {change > 0 ? '+' : ''}
                    {change.toLocaleString(i18n.language, { maximumFractionDigits: 1 })}%
                  </b>
                </div>
              )}
              {merged ? (
                <ValuePie
                  title={t('client_usage.value_by_model')}
                  parts={merged.map((entry) => ({
                    id: `${entry.company}:${entry.model}`,
                    label: `${COMPANY_NAMES[entry.company] ?? entry.company} · ${friendlyModelName(entry.model)}`,
                    value: entry.value,
                    wastedUsd: allowances[person.id]?.[entry.company]?.wasted.usd,
                  }))}
                  quote={quote}
                  classes={styles}
                />
              ) : (
                <>
                  <ValuePie
                    title={t('client_usage.by_company')}
                    parts={person.companies.map((entry) => ({
                      id: entry.id,
                      label: COMPANY_NAMES[entry.id] ?? entry.id,
                      value: entry.current.costUsd,
                      wastedUsd: allowances[person.id]?.[entry.id]?.wasted.usd,
                    }))}
                    quote={quote}
                    classes={styles}
                  />
                  <ValuePie
                    title={t('client_usage.value_by_model')}
                    parts={person.models.map((entry) => ({
                      id: entry.id,
                      label: friendlyModelName(entry.id),
                      value: entry.current.costUsd,
                    }))}
                    quote={quote}
                    classes={styles}
                  />
                </>
              )}
            </article>
          );
        })}
      </div>
    </Card>
  );
}
