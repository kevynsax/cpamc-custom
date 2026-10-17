import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { IconSettings, IconTrash2 } from '@/components/ui/icons';
import type { ClientUsageResponse, UsageSettings as Settings } from '@/services/api/clientUsage';
import styles from './UsagePage.module.scss';

export function UsageSettings({
  data,
  accountId,
  busy,
  onSave,
  onRefreshPrices,
}: {
  data: ClientUsageResponse;
  accountId: string;
  busy: boolean;
  onSave: (settings: Settings) => Promise<void>;
  onRefreshPrices: () => Promise<void>;
}) {
  const { t } = useTranslation();
  const serialized = JSON.stringify(data.settings);
  const [draft, setDraft] = useState(data.settings);
  useEffect(() => {
    setDraft(JSON.parse(serialized) as Settings);
  }, [serialized]);
  const [model, setModel] = useState('');
  const [rates, setRates] = useState({ input: '', output: '', cache_read: '', cache_write: '' });
  const keys = [
    ...new Set([
      ...Object.keys(data.keyLabels),
      ...data.rows.map((row) => row.keyId),
      ...data.weeks.flatMap((week) => week.people.map((person) => person.keyId)),
    ]),
  ].filter((id) => id !== 'unknown');
  const addPrice = () => {
    const rate = {
      input: Number(rates.input),
      output: Number(rates.output),
      ...(rates.cache_read !== '' ? { cache_read: Number(rates.cache_read) } : {}),
      ...(rates.cache_write !== '' ? { cache_write: Number(rates.cache_write) } : {}),
    };
    const updated = { ...draft, prices: { ...draft.prices, [model.trim()]: rate } };
    setDraft(updated);
    void onSave(updated);
  };
  return (
    <details className={styles.settings}>
      <summary>
        <IconSettings size={18} /> {t('client_usage.settings')}
      </summary>
      <div className={styles.settingsBody}>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void onSave(draft);
          }}
        >
          <h3>{t('client_usage.people_settings')}</h3>
          <div className={styles.formGrid}>
            {keys.map((id) => (
              <Input
                key={id}
                disabled={busy}
                maxLength={80}
                label={`${data.keyLabels[id] ?? t('client_usage.historical_key')} · ${id.slice(0, 8)}`}
                value={draft.names[id] ?? ''}
                placeholder={t('client_usage.person_name')}
                onChange={(event) =>
                  setDraft({ ...draft, names: { ...draft.names, [id]: event.target.value } })
                }
              />
            ))}
          </div>
          {accountId && accountId !== 'unknown' && (
            <Input
              label={t('client_usage.plan_override')}
              disabled={busy}
              maxLength={80}
              value={draft.plans[accountId] ?? ''}
              hint={t('client_usage.plan_hint')}
              onChange={(event) =>
                setDraft({ ...draft, plans: { ...draft.plans, [accountId]: event.target.value } })
              }
            />
          )}
          <Button type="submit" disabled={busy}>
            {t('client_usage.save')}
          </Button>
        </form>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            addPrice();
          }}
        >
          <h3>{t('client_usage.price_overrides')}</h3>
          <p className={styles.note}>{t('client_usage.price_hint')}</p>
          <p className={styles.note}>
            {t('client_usage.unpriced')}:{' '}
            {[
              ...new Set(
                data.rows
                  .filter((row) => row.unpriced > 0)
                  .map((row) => row.model)
                  .filter(Boolean)
              ),
            ].join(', ') || '—'}
          </p>
          <Input
            disabled={busy}
            required
            maxLength={200}
            label={t('client_usage.model')}
            value={model}
            onChange={(event) => setModel(event.target.value)}
          />
          <div className={styles.formGrid}>
            {(['input', 'output', 'cache_read', 'cache_write'] as const).map((field) => (
              <Input
                key={field}
                disabled={busy}
                type="number"
                min={0}
                max={1_000_000}
                step="any"
                required={field === 'input' || field === 'output'}
                label={t(`client_usage.rate_${field}`)}
                value={rates[field]}
                onChange={(event) => setRates({ ...rates, [field]: event.target.value })}
              />
            ))}
          </div>
          <Button type="submit" disabled={busy || !model.trim()}>
            {t('client_usage.save_price')}
          </Button>
          <ul className={styles.overrideList}>
            {Object.keys(draft.prices).map((name) => (
              <li key={name}>
                <code>{name}</code>
                <Button
                  type="button"
                  variant="ghost"
                  disabled={busy}
                  aria-label={t('client_usage.remove_price', { model: name })}
                  onClick={() => {
                    const prices = { ...draft.prices };
                    delete prices[name];
                    const updated = { ...draft, prices };
                    setDraft(updated);
                    void onSave(updated);
                  }}
                >
                  <IconTrash2 size={16} />
                </Button>
              </li>
            ))}
          </ul>
        </form>
        <div>
          <p className={styles.note}>
            {t('client_usage.catalog', { date: data.pricesAsOf || '—' })}
          </p>
          <Button type="button" disabled={busy} onClick={() => void onRefreshPrices()}>
            {t('client_usage.refresh_prices')}
          </Button>
        </div>
      </div>
    </details>
  );
}
