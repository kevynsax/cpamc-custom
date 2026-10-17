import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { useThemeStore } from '@/stores';
import { UsagePage } from '@/features/usage/UsagePage';
import styles from './preview.module.scss';

export function Preview() {
  const { t } = useTranslation();
  const initializeTheme = useThemeStore((state) => state.initializeTheme);
  useEffect(() => initializeTheme(), [initializeTheme]);
  return (
    <main className={styles.preview} data-local-usage-demo>
      <header className={styles.environment}>
        <strong>{t('client_usage.fake_data_title')}</strong>
      </header>
      <UsagePage />
    </main>
  );
}
