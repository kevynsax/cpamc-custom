import reference from '../../../../internal/usagehistory/model_reference.json';
import type { ClientUsageResponse, UsageModelPrice } from '@/services/api/clientUsage';

const sources = new Map(
  reference.models
    .filter((model) => model.status === 'openrouter')
    .map((model) => [model.id, model])
);

/** Let a local panel preview the sourced prices before the backend is upgraded.
 * This only decorates price cards: recorded request costs are never repriced.
 */
export function withInHousePrices(data: ClientUsageResponse): ClientUsageResponse {
  return {
    ...data,
    models: data.models?.map((model): UsageModelPrice => {
      const source = sources.get(model.id);
      if (
        !source ||
        model.overridden ||
        model.status !== 'self_hosted' ||
        Object.keys(model.prices).length
      )
        return model;
      return {
        ...model,
        prices: { ...source.prices } as Record<string, number>,
        status: 'openrouter',
        sources: source.sources,
        checkedAt: source.checked_at,
      };
    }),
  };
}
