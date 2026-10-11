/**
 * Quota adapters combine React-free data layers with provider body components.
 *
 * Pages consume a type-erased QuotaAdapter interface; provider data modules
 * retain strongly typed states, matching AuthFileQuotaSection's narrow casts.
 */

import type { ComponentType } from 'react';
import type { TFunction } from 'i18next';
import { useQuotaStore } from '@/stores';
import type { AuthFileItem } from '@/types';
import type { QuotaBodyProps } from '../types';
import type { QuotaProviderType, QuotaStore } from './types';
import { withBackendQuotaCache } from './customQuotaCache'; // CUSTOM (cpamc-custom)
import { MINIMAX_CONFIG, ZAI_CONFIG } from './customApiKeyQuota'; // CUSTOM (cpamc-custom)
import { ANTIGRAVITY_CONFIG } from './antigravity/data';
import { AntigravityQuotaBody } from './antigravity/AntigravityQuotaBody';
import { CLAUDE_CONFIG } from './claude/data';
import { ClaudeQuotaBody } from './claude/ClaudeQuotaBody';
import { DEVIN_CONFIG } from './devin/data';
import { DevinQuotaBody } from './devin/DevinQuotaBody';
import { CODEX_CONFIG } from './codex/data';
import { CodexQuotaBody } from './codex/CodexQuotaBody';
import { META_CONFIG } from './meta/data';
import { MetaQuotaBody } from './meta/MetaQuotaBody';
import { KIMI_CONFIG } from './kimi/data';
import { KimiQuotaBody } from './kimi/KimiQuotaBody';
import { PLUGIN_CONFIG } from './plugin/data';
import { PluginQuotaBody } from './plugin/PluginQuotaBody';
import { XAI_CONFIG } from './xai/data';
import { XaiQuotaBody } from './xai/XaiQuotaBody';

/** Common structural subset of every provider quota state. */
export interface QuotaCardState {
  status: 'idle' | 'loading' | 'success' | 'error';
  error?: string;
  errorStatus?: number;
}

export interface QuotaAdapter {
  type: QuotaProviderType;
  i18nPrefix: string;
  filterFn: (file: AuthFileItem) => boolean;
  fetchQuota: (file: AuthFileItem, t: TFunction) => Promise<unknown>;
  enrichQuota?: (file: AuthFileItem, data: unknown, t: TFunction) => Promise<unknown>;
  resetQuota?: (file: AuthFileItem, t: TFunction) => Promise<unknown>;
  canResetQuota?: (quota: QuotaCardState) => boolean;
  storeSelector: (state: QuotaStore) => Record<string, QuotaCardState>;
  storeSetter: keyof QuotaStore;
  buildLoadingState: () => QuotaCardState;
  buildSuccessState: (data: unknown) => QuotaCardState;
  buildErrorState: (message: string, status?: number) => QuotaCardState;
  Body: ComponentType<QuotaBodyProps<QuotaCardState>>;
}

const UPSTREAM_QUOTA_ADAPTERS: Record<QuotaProviderType, QuotaAdapter> = {
  antigravity: {
    ...ANTIGRAVITY_CONFIG,
    Body: AntigravityQuotaBody,
  } as unknown as QuotaAdapter,
  claude: { ...CLAUDE_CONFIG, Body: ClaudeQuotaBody } as unknown as QuotaAdapter,
  codex: { ...CODEX_CONFIG, Body: CodexQuotaBody } as unknown as QuotaAdapter,
  devin: { ...DEVIN_CONFIG, Body: DevinQuotaBody } as unknown as QuotaAdapter,
  kimi: { ...KIMI_CONFIG, Body: KimiQuotaBody } as unknown as QuotaAdapter,
  meta: { ...META_CONFIG, Body: MetaQuotaBody } as unknown as QuotaAdapter,
  plugin: { ...PLUGIN_CONFIG, Body: PluginQuotaBody } as unknown as QuotaAdapter,
  xai: { ...XAI_CONFIG, Body: XaiQuotaBody } as unknown as QuotaAdapter,
  // CUSTOM (cpamc-custom): API-key providers share Kimi's row-based card body.
  minimax: { ...MINIMAX_CONFIG, Body: KimiQuotaBody } as unknown as QuotaAdapter,
  zai: { ...ZAI_CONFIG, Body: KimiQuotaBody } as unknown as QuotaAdapter,
};

// CUSTOM (cpamc-custom): every provider's quota reads go through the backend cache.
// Generic: wraps whatever adapters upstream defines, so new providers are covered.
export const QUOTA_ADAPTERS = withBackendQuotaCache(UPSTREAM_QUOTA_ADAPTERS);

export type QuotaMapUpdater = (
  updater: (prev: Record<string, QuotaCardState>) => Record<string, QuotaCardState>
) => void;

/** Read the adapter's store setter without subscribing. */
export const getQuotaSetter = (adapter: QuotaAdapter): QuotaMapUpdater =>
  useQuotaStore.getState()[adapter.storeSetter] as unknown as QuotaMapUpdater;

/** Read the adapter's quota cache snapshot without subscribing. */
export const getQuotaMap = (adapter: QuotaAdapter): Record<string, QuotaCardState> =>
  adapter.storeSelector(useQuotaStore.getState() as unknown as QuotaStore);
