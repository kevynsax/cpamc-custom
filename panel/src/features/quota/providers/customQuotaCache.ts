/**
 * CUSTOM (cpamc-custom): wrap every quota adapter so its reads go through the
 * backend cache. See CUSTOM_FEATURES.md at the repo root.
 *
 * resetQuota is intentionally NOT wrapped: it mutates upstream state and must
 * always be a live call.
 */

import { withQuotaCache } from '@/services/api/customQuotaApiCall';
import type { QuotaAdapter } from './index';

export function withBackendQuotaCache<K extends string>(
  adapters: Record<K, QuotaAdapter>
): Record<K, QuotaAdapter> {
  const out = {} as Record<K, QuotaAdapter>;
  (Object.keys(adapters) as K[]).forEach((key) => {
    const adapter = adapters[key];
    out[key] = {
      ...adapter,
      fetchQuota: (file, t) => withQuotaCache(() => adapter.fetchQuota(file, t)),
      ...(adapter.enrichQuota
        ? {
            enrichQuota: (file, data, t) =>
              withQuotaCache(() => adapter.enrichQuota!(file, data, t)),
          }
        : {}),
    };
  });
  return out;
}
