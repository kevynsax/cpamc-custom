import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { IconCheck, IconExternalLink, IconRefreshCw } from '@/components/ui/icons';
import type {
  ClientUsageResponse,
  UsageModelPrice,
  UsageSettings,
} from '@/services/api/clientUsage';
import type { CrossQuote } from './currency';
import { priceFields, settingsWithModelPrice } from './modelPrices';
import { friendlyModelName } from './modelNames';
import styles from './UsagePage.module.scss';

function ModelPriceCard({
  model,
  data,
  quote,
  busy,
  onSave,
}: {
  model: UsageModelPrice;
  data: ClientUsageResponse;
  quote: CrossQuote | null;
  busy: boolean;
  onSave: (settings: UsageSettings) => Promise<boolean>;
}) {
  const { t } = useTranslation();
  const [draft, setDraft] = useState<{ rate: number; fields: Record<string, string> } | null>(null);
  const live = quote;
  const fields = priceFields(model);
  const value = (key: string) =>
    draft?.fields[key] ??
    (live && model.prices[key] !== undefined
      ? Number((model.prices[key] * live.rate).toPrecision(10)).toString()
      : '');
  const edit = (key: string, text: string) => {
    if (!live) return;
    const initial = draft ?? {
      rate: live.rate,
      fields: Object.fromEntries(fields.map((field) => [field, value(field)])),
    };
    setDraft({ rate: initial.rate, fields: { ...initial.fields, [key]: text } });
  };
  const save = async () => {
    if (!draft) return;
    const prices = Object.fromEntries(
      Object.entries(draft.fields)
        .filter(([, text]) => text.trim() !== '')
        .map(([key, text]) => [key, Number(text) / draft.rate])
    );
    if (
      Object.values(prices).some(
        (number) => !Number.isFinite(number) || number < 0 || number > 1_000_000
      )
    )
      return;
    if (await onSave(settingsWithModelPrice(data.settings, model, prices))) setDraft(null);
  };
  const label = (key: string) => {
    const base = key.replace(/^long_/, '');
    const suffix =
      model.kind === 'image_tokens' && base === 'output'
        ? 'image_output'
        : model.kind === 'image_tokens' && base === 'input'
          ? 'text_input'
          : base;
    return t(`client_usage.price_field_${suffix}`);
  };
  const renderFields = (keys: string[]) => (
    <div className={styles.priceFields}>
      {keys.map((key) => (
        <Input
          key={key}
          type="number"
          min={0}
          step="any"
          label={label(key)}
          value={value(key)}
          placeholder="—"
          disabled={busy || !live}
          onChange={(event) => edit(key, event.target.value)}
        />
      ))}
    </div>
  );
  return (
    <article className={styles.modelCard} data-model-id={model.id}>
      <header>
        <h3>{friendlyModelName(model.id, model.displayName)}</h3>
        {model.sources[0] && (
          <a
            href={model.sources[0]}
            target="_blank"
            rel="noreferrer"
            aria-label={t('client_usage.price_source')}
            title={t('client_usage.price_source')}
          >
            <IconExternalLink size={15} />
          </a>
        )}
      </header>
      {(model.overridden || model.status === 'openrouter') && (
        <span className={styles.modelStatus}>
          {model.overridden ? t('client_usage.price_status_manual') : 'OpenRouter'}
        </span>
      )}
      <form
        onSubmit={(event) => {
          event.preventDefault();
          void save();
        }}
      >
        {renderFields(fields.filter((key) => !key.startsWith('long_')))}
        {model.contextThreshold > 0 && (
          <details className={styles.contextPrices}>
            <summary>{t('client_usage.long_context')}</summary>
            {renderFields(fields.filter((key) => key.startsWith('long_')))}
          </details>
        )}
        <footer>
          {draft && (
            <Button type="submit" disabled={busy || !live}>
              <IconCheck size={15} />
              {t('common.save')}
            </Button>
          )}
          {model.overridden && (
            <Button
              type="button"
              variant="ghost"
              disabled={busy}
              aria-label={t('client_usage.reset_price')}
              title={t('client_usage.reset_price')}
              onClick={() => {
                setDraft(null);
                void onSave(settingsWithModelPrice(data.settings, model, null));
              }}
            >
              <IconRefreshCw size={15} />
            </Button>
          )}
        </footer>
      </form>
    </article>
  );
}

export function ModelPriceCards({
  data,
  quote,
  busy,
  onSave,
  spending = {},
}: {
  data: ClientUsageResponse;
  quote: CrossQuote | null;
  busy: boolean;
  spending?: Record<string, number>;
  onSave: (settings: UsageSettings) => Promise<boolean>;
}) {
  const { t } = useTranslation();
  const [search, setSearch] = useState('');
  const models = (data.models ?? [])
    .filter((model) =>
      `${model.id} ${model.provider} ${friendlyModelName(model.id, model.displayName)}`
        .toLowerCase()
        .includes(search.toLowerCase())
    )
    .sort(
      (a, b) =>
        (spending[b.id] ?? 0) - (spending[a.id] ?? 0) ||
        friendlyModelName(a.id, a.displayName).localeCompare(friendlyModelName(b.id, b.displayName))
    );
  return (
    <details className={styles.modelSettings}>
      <summary>
        <h2>{t('client_usage.model_prices_title')}</h2>
        <span>{data.models?.length ?? 0}</span>
      </summary>
      <Input
        type="search"
        label={t('client_usage.search_models')}
        value={search}
        onChange={(event) => setSearch(event.target.value)}
      />
      <div className={styles.modelGrid}>
        {models.map((model) => (
          <ModelPriceCard
            key={`${model.id}:${JSON.stringify(model.prices)}`}
            model={model}
            data={data}
            quote={quote}
            busy={busy}
            onSave={onSave}
          />
        ))}
      </div>
    </details>
  );
}
