import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import '@/styles/global.scss';
import i18n from '@/i18n';
import { apiClient } from '@/services/api/client';
import { useAuthStore, useLanguageStore, useConfigStore } from '@/stores';
import { saveApiKeyName } from '@/features/config/apiKeyNames';
import { Preview } from './Preview';

// Injected only by the serve-only plugin; production keeps its normal login entry.
async function bootstrap() {
  const response = await fetch('/v8/management/config');
  const config = (await response.json()) as {
    access: { 'api-keys': string[] };
    demo_key_names: Record<string, string>;
  };
  const apiBase = window.location.origin;
  apiClient.setConfig({ apiBase, managementKey: 'local-demo-only' });
  useAuthStore.setState({
    apiBase,
    managementKey: 'local-demo-only',
    rememberPassword: false,
    isAuthenticated: true,
    connectionStatus: 'connected',
  });
  useConfigStore.setState({ config: { apiKeys: config.access['api-keys'] } });
  for (const key of config.access['api-keys'])
    saveApiKeyName(apiBase, key, config.demo_key_names[key]);
  useLanguageStore.getState().setLanguage('en');
  document.title = `${i18n.t('client_usage.title')} · ${i18n.t('client_usage.fake_data_title')}`;
  document.documentElement.lang = 'en';
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <Preview />
    </StrictMode>
  );
}
void bootstrap();
