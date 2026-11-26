import type { ComponentType } from 'react';
import {
  IconCode,
  IconKey,
  IconNetwork,
  IconSatellite,
  IconScrollText,
  IconShield,
  IconSlidersHorizontal,
  type IconProps,
} from '@/components/ui/icons';
import type { VisualConfigFieldPath } from '@/types/visualConfig';
import type { VisualSectionId } from './searchIndex';

/** Editor mode: visual form or YAML source. */
export type ConfigEditorMode = 'visual' | 'source';

/** Top-level tabs: common settings and six canonical sections. */
export type ConfigTabId = 'common' | VisualSectionId;

export const CONFIG_SECTION_IDS = [
  'connectivity',
  'network',
  'logging',
  'streaming',
  'advanced',
  'payload',
] as const satisfies readonly VisualSectionId[];

export const CONFIG_TAB_IDS: readonly ConfigTabId[] = ['common', ...CONFIG_SECTION_IDS];

/** Section numbers (01–06); the common tab is an unnumbered alias view. */
export const SECTION_INDEX_LABELS: Record<VisualSectionId, string> = {
  connectivity: '01',
  network: '02',
  logging: '03',
  streaming: '04',
  advanced: '05',
  payload: '06',
};

export const CONFIG_TAB_ICONS: Record<ConfigTabId, ComponentType<IconProps>> = {
  common: IconSlidersHorizontal,
  connectivity: IconKey,
  network: IconNetwork,
  logging: IconScrollText,
  streaming: IconSatellite,
  advanced: IconShield,
  payload: IconCode,
};

/** Six frequent fields share rendering with their canonical sections. */
export const COMMON_FIELD_IDS = [
  'host',
  'port',
  'apiKeys',
  'proxyUrl',
  'debug',
  'loggingToFile',
] as const;

/**
 * Validation field paths grouped by section for tab error badges.
 * Payload validation is tracked separately by hasPayloadValidationErrors.
 */
export const SECTION_VALIDATION_FIELDS: Record<VisualSectionId, readonly VisualConfigFieldPath[]> =
  {
    connectivity: ['port', 'serverId', 'trustedProxies', 'discoveryServiceType'],
    network: [
      'requestRetry',
      'maxRetryCredentials',
      'maxRetryInterval',
      'authAutoRefreshWorkers',
      'transientErrorCooldownSeconds',
      'videoResultAuthCacheTTL',
    ],
    logging: ['errorLogsMaxFiles', 'logsMaxTotalSizeMb', 'redisUsageQueueRetentionSeconds'],
    streaming: [
      'streaming.keepaliveSeconds',
      'streaming.bootstrapRetries',
      'streaming.nonstreamKeepaliveInterval',
    ],
    advanced: [
      'claudeHeaderTimezone',
      'codexStreamBootstrapTimeout',
      'antigravityConnectionPoolIdleTimeout',
      'antigravityConnectionPoolMaxIdleConnsPerHost',
      'codexLiveMediaRelayMaxSessions',
      'codexLiveMediaRelayPublicIP',
      'codexLiveMediaRelayUDPPortMin',
      'codexLiveMediaRelayUDPPortMax',
      'oauthErrorRules',
      'codexLiveMediaRelayICEServers',
    ],
    payload: [],
  };

/**
 * Maps field IDs to useVisualConfig dirty keys, with dotted streaming leaves.
 * tests/configFieldParity.test.ts keeps this map, the search index and sections aligned.
 */
export const FIELD_VALUE_KEYS: Record<string, readonly string[]> = {
  routingSessionAffinitySubagents: ['routingSessionAffinitySubagents'],
  saveCooldownStatus: ['saveCooldownStatus'],
  transientErrorCooldownSeconds: ['transientErrorCooldownSeconds'],
  videoResultAuthCacheTTL: ['videoResultAuthCacheTTL'],
  claudeHeaderTimezone: ['claudeHeaderTimezone'],
  claudeModelLevelCooling: ['claudeModelLevelCooling'],
  claudeDisableCloakMode: ['claudeDisableCloakMode'],
  claudeCodeDisableCloakingModelList: ['claudeCodeDisableCloakingModelList'],
  codexDisableCloaking: ['codexDisableCloaking'],
  codexModelLevelCooling: ['codexModelLevelCooling'],
  codexStreamBootstrapBuffering: ['codexStreamBootstrapBuffering'],
  codexStreamBootstrapTimeout: ['codexStreamBootstrapTimeout'],
  codexOptimizeMultiAgentV2: ['codexOptimizeMultiAgentV2'],
  codexOrphanDelegationCompatibility: ['codexOrphanDelegationCompatibility'],
  codexResponseSteering: ['codexResponseSteering'],
  antigravityConnectionPoolEnabled: ['antigravityConnectionPoolEnabled'],
  antigravityConnectionPoolIdleTimeout: ['antigravityConnectionPoolIdleTimeout'],
  antigravityConnectionPoolMaxIdleConnsPerHost: ['antigravityConnectionPoolMaxIdleConnsPerHost'],
  xaiInjectXSearch: ['xaiInjectXSearch'],
  codexLiveMediaRelayEnabled: ['codexLiveMediaRelayEnabled'],
  codexLiveMediaRelayMaxSessions: ['codexLiveMediaRelayMaxSessions'],
  codexLiveMediaRelayDisablePrivateRemoteIPs: ['codexLiveMediaRelayDisablePrivateRemoteIPs'],
  codexLiveMediaRelayPublicIP: ['codexLiveMediaRelayPublicIP'],
  codexLiveMediaRelayUDPPortMin: ['codexLiveMediaRelayUDPPortMin'],
  codexLiveMediaRelayUDPPortMax: ['codexLiveMediaRelayUDPPortMax'],
  codexLiveMediaRelayICEServers: ['codexLiveMediaRelayICEServers'],

  // ── connectivity ──────────────────────────────────────────────────────────
  githubToken: ['githubToken'],
  serverId: ['serverId'],
  oauthErrorRules: ['oauthErrorRules'],
  trustedProxies: ['trustedProxies'],
  discoveryEnabled: ['discoveryEnabled'],
  discoveryServiceName: ['discoveryServiceName'],
  discoveryServiceType: ['discoveryServiceType'],
  discoverySubtypes: ['discoverySubtypes'],
  discoveryInterfacesInclude: ['discoveryInterfacesInclude'],
  discoveryInterfacesExclude: ['discoveryInterfacesExclude'],
  discoveryAuthRequired: ['discoveryAuthRequired'],
  discoveryAdvertiseManagement: ['discoveryAdvertiseManagement'],
  host: ['host'],
  port: ['port'],
  authDir: ['authDir'],
  apiKeys: ['apiKeysText', 'googleAccountClients'],
  tlsEnable: ['tlsEnable'],
  tlsCert: ['tlsCert'],
  tlsKey: ['tlsKey'],
  rmAllowRemote: ['rmAllowRemote'],
  rmDisableControlPanel: ['rmDisableControlPanel'],
  rmDisableAutoUpdatePanel: ['rmDisableAutoUpdatePanel'],
  rmSecretKey: ['rmSecretKey'],
  rmPanelRepo: ['rmPanelRepo'],
  // ── network ───────────────────────────────────────────────────────────────
  proxyUrl: ['proxyUrl'],
  requestRetry: ['requestRetry'],
  maxRetryCredentials: ['maxRetryCredentials'],
  maxRetryInterval: ['maxRetryInterval'],
  authAutoRefreshWorkers: ['authAutoRefreshWorkers'],
  routingStrategy: ['routingStrategy'],
  disableImageGeneration: ['disableImageGeneration'],
  gptImage2BaseModel: ['gptImage2BaseModel'],
  routingSessionAffinityTTL: ['routingSessionAffinityTTL'],
  forceModelPrefix: ['forceModelPrefix'],
  passthroughHeaders: ['passthroughHeaders'],
  disableCooling: ['disableCooling'],
  routingSessionAffinity: ['routingSessionAffinity'],
  wsAuth: ['wsAuth'],
  // ── logging ───────────────────────────────────────────────────────────────
  debug: ['debug'],
  commercialMode: ['commercialMode'],
  loggingToFile: ['loggingToFile'],
  logsMaxTotalSizeMb: ['logsMaxTotalSizeMb'],
  errorLogsMaxFiles: ['errorLogsMaxFiles'],
  redisUsageQueueRetentionSeconds: ['redisUsageQueueRetentionSeconds'],
  usageStatisticsEnabled: ['usageStatisticsEnabled'],
  // ── streaming ─────────────────────────────────────────────────────────────
  streamingKeepaliveSeconds: ['streaming.keepaliveSeconds'],
  streamingBootstrapRetries: ['streaming.bootstrapRetries'],
  streamingNonstreamKeepalive: ['streaming.nonstreamKeepaliveInterval'],
  // ── advanced ──────────────────────────────────────────────────────────────
  quotaAntigravityCredits: ['quotaAntigravityCredits'],
  pluginsEnabled: ['pluginsEnabled'],
  pluginStoreSources: ['pluginStoreSources'],
  pluginStoreAuth: ['pluginStoreAuth'],
  antigravitySensitiveWordsReplace: ['antigravitySensitiveWordsReplace'],
  antigravitySensitivePatternsReplace: ['antigravitySensitivePatternsReplace'],
  devinSensitiveWordsReplace: ['devinSensitiveWordsReplace'],
  devinSensitivePatternsReplace: ['devinSensitivePatternsReplace'],
  antigravitySensitiveWords: ['antigravitySensitiveWords'],
  devinSensitiveWords: ['devinSensitiveWords'],
  antigravitySignatureCacheEnabled: ['antigravitySignatureCacheEnabled'],
  antigravitySignatureBypassStrict: ['antigravitySignatureBypassStrict'],
  claudeHeaderUserAgent: ['claudeHeaderUserAgent'],
  claudeHeaderPackageVersion: ['claudeHeaderPackageVersion'],
  claudeHeaderRuntimeVersion: ['claudeHeaderRuntimeVersion'],
  claudeHeaderOs: ['claudeHeaderOs'],
  claudeHeaderArch: ['claudeHeaderArch'],
  claudeHeaderTimeout: ['claudeHeaderTimeout'],
  claudeHeaderStabilizeDeviceProfile: ['claudeHeaderStabilizeDeviceProfile'],
  codexHeaderUserAgent: ['codexHeaderUserAgent'],
  codexHeaderBetaFeatures: ['codexHeaderBetaFeatures'],
  // ── payload ───────────────────────────────────────────────────────────────
  payloadDefaultRules: ['payloadDefaultRules'],
  payloadDefaultRawRules: ['payloadDefaultRawRules'],
  payloadOverrideRules: ['payloadOverrideRules'],
  payloadOverrideRawRules: ['payloadOverrideRawRules'],
  payloadFilterRules: ['payloadFilterRules'],
  payloadReplaceRules: ['payloadReplaceRules'],
};

/** Shared tab and panel DOM IDs keep their accessibility associations aligned. */
export const configTabDomId = (id: ConfigTabId) => `config-tab-${id}`;
export const configPanelDomId = (id: ConfigTabId) => `config-panel-${id}`;

/** Browser storage keys for editor mode and the selected section. */
export const CONFIG_MODE_STORAGE_KEY = 'config-management:tab';
export const CONFIG_SECTION_STORAGE_KEY = 'config-management:section';
/** Browser storage entry cleared during mounting. */
export const LEGACY_EDITOR_MODE_STORAGE_KEY = 'config-management:editor-mode';
