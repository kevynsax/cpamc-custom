import type { UsageModelPrice, UsageRates, UsageSettings } from '@/services/api/clientUsage';

export function priceFields(model: UsageModelPrice): string[] {
  if (model.kind === 'image') return ['per_image'];
  if (model.kind === 'second') return ['per_second'];
  if (model.kind === 'minute') return ['per_minute'];
  if (model.kind === 'characters') return ['per_million_characters'];
  if (model.kind === 'image_tokens')
    return [...new Set(['input', 'output', ...Object.keys(model.prices)])];
  return [
    ...new Set([
      'input',
      'output',
      'cache_read',
      'cache_write',
      ...Object.keys(model.prices),
      ...(model.contextThreshold > 0
        ? ['long_input', 'long_output', 'long_cache_read', 'long_cache_write']
        : []),
    ]),
  ];
}

export function settingsWithModelPrice(
  settings: UsageSettings,
  model: UsageModelPrice,
  values: Record<string, number> | null
): UsageSettings {
  const prices = { ...settings.prices },
    unitPrices = { ...settings.unit_prices };
  delete prices[model.id];
  delete unitPrices[model.id];
  if (values) {
    unitPrices[model.id] = { ...values };
    if (model.kind === 'tokens' && values.input !== undefined && values.output !== undefined) {
      const rate: UsageRates = { input: values.input, output: values.output };
      if (values.cache_read !== undefined) rate.cache_read = values.cache_read;
      if (values.cache_write !== undefined) rate.cache_write = values.cache_write;
      if (model.contextThreshold > 0) {
        rate.context_over = model.contextThreshold;
        rate.context_inclusive = model.contextInclusive;
        if (values.long_input !== undefined && values.long_output !== undefined) {
          rate.long_context = { input: values.long_input, output: values.long_output };
          if (values.long_cache_read !== undefined)
            rate.long_context.cache_read = values.long_cache_read;
          if (values.long_cache_write !== undefined)
            rate.long_context.cache_write = values.long_cache_write;
        }
      }
      prices[model.id] = rate;
    }
  }
  return { ...settings, prices, unit_prices: unitPrices };
}
