// CUSTOM (cpamc-custom)
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Modal } from '@/components/ui/Modal';
import { IconDownload, IconSidebarOauth } from '@/components/ui/icons';
import { agyApi, AgySetupSaveRequiredError, type AgyAccount } from '@/services/api/agy';
import { apiClient } from '@/services/api/client';
import { useAuthStore } from '@/stores';
import type { GoogleAccountClient } from '@/types/visualConfig';
import { downloadBlob } from '@/utils/download';
import styles from './AgySetupButton.module.scss';

type AgySetupButtonProps = { client: GoogleAccountClient; disabled: boolean };
type AgySetupDialogProps = { client: GoogleAccountClient; onClose: () => void };
type AgySetupNoticeProps = { downloaded: boolean };
type AgyAccountCardProps = {
  account: AgyAccount;
  selected: boolean;
  disabled: boolean;
  onSelect: (id: string) => void;
};

function AgySetupNotice({ downloaded }: AgySetupNoticeProps) {
  const { t } = useTranslation();
  if (!downloaded) {
    return (
      <p className={styles.warning}>{t('config_management.visual.api_keys.agy_setup_warning')}</p>
    );
  }
  return <p role="status">{t('config_management.visual.api_keys.agy_setup_success')}</p>;
}

function AgyAccountCard({ account, selected, disabled, onSelect }: AgyAccountCardProps) {
  const { t } = useTranslation();
  const select = () => {
    onSelect(account.id);
  };
  return (
    <button
      type="button"
      className={styles.account}
      aria-pressed={selected}
      disabled={disabled || account.status !== 'ready'}
      onClick={select}
    >
      <IconSidebarOauth size={24} />
      <span className={styles.accountCopy}>
        <strong>{account.email}</strong>
        <span>{t(`config_management.visual.api_keys.agy_setup_${account.status}`)}</span>
      </span>
    </button>
  );
}

function AgySetupDialog({ client, onClose }: AgySetupDialogProps) {
  const { t } = useTranslation();
  const [accounts, setAccounts] = useState<AgyAccount[]>([]);
  const [selected, setSelected] = useState('');
  const [loading, setLoading] = useState(true);
  const [downloading, setDownloading] = useState(false);
  const [downloaded, setDownloaded] = useState(false);
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);
  const mounted = useRef(false);
  const currentRevision = apiClient.getConnectionRevision();
  const revision = useRef(currentRevision);
  const selectApiBase = (state: ReturnType<typeof useAuthStore.getState>) => state.apiBase;
  const apiBase = useAuthStore(selectApiBase);
  const isCurrent = () => {
    const activeRevision = apiClient.getConnectionRevision();
    return mounted.current && revision.current === activeRevision;
  };
  const load = () => {
    mounted.current = true;
    let cancelled = false;
    const readAccounts = async () => {
      try {
        const available = await agyApi.accounts(client);
        if (cancelled || !isCurrent()) return;
        setAccounts(available);
      } catch (failure) {
        if (cancelled || !isCurrent()) return;
        const key =
          failure instanceof AgySetupSaveRequiredError
            ? 'agy_setup_save_first'
            : 'agy_setup_load_error';
        setError(key);
      } finally {
        if (!cancelled && isCurrent()) setLoading(false);
      }
    };
    void readAccounts();
    return () => {
      cancelled = true;
      mounted.current = false;
    };
  };
  useEffect(load, [client, attempt]);

  const retry = () => {
    setLoading(true);
    setError('');
    setSelected('');
    setAttempt(attempt + 1);
  };
  const download = async () => {
    if (!selected || downloading || downloaded) return;
    setDownloading(true);
    setError('');
    try {
      const blob = await agyApi.bootstrap(client, selected);
      if (!isCurrent()) return;
      const filename = `agy-bootstrap-${client.name}.json`;
      const options = { filename, blob };
      downloadBlob(options);
      setDownloaded(true);
    } catch (failure) {
      if (!isCurrent()) return;
      const key =
        failure instanceof AgySetupSaveRequiredError
          ? 'agy_setup_save_first'
          : 'agy_setup_download_error';
      setError(key);
    } finally {
      if (isCurrent()) setDownloading(false);
    }
  };
  const renderAccount = (account: AgyAccount) => (
    <AgyAccountCard
      key={account.id}
      account={account}
      selected={selected === account.id}
      disabled={downloading || downloaded}
      onSelect={setSelected}
    />
  );
  const cards = accounts.map(renderAccount);
  const accountReady = (account: AgyAccount) => account.status === 'ready';
  const ready = accounts.some(accountReady);
  const connection = new URL(apiBase);
  const setupUrl = new URL('/setup/agy.md', connection);
  const introOptions = { client: client.name };
  return (
    <Modal
      open
      onClose={onClose}
      closeDisabled={downloading}
      title={t('config_management.visual.api_keys.agy_setup_title')}
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={downloading}>
            {t('common.close')}
          </Button>
          {!downloaded && (
            <Button onClick={download} disabled={loading || !selected} loading={downloading}>
              <IconDownload size={16} />
              {t('config_management.visual.api_keys.agy_setup_download')}
            </Button>
          )}
        </>
      }
    >
      <div className={styles.content}>
        <p>{t('config_management.visual.api_keys.agy_setup_intro', introOptions)}</p>
        {loading && <p role="status">{t('common.loading')}</p>}
        {error && (
          <div className="error-box" role="alert">
            {t(`config_management.visual.api_keys.${error}`)}
          </div>
        )}
        {!loading && !error && accounts.length === 0 && (
          <p>{t('config_management.visual.api_keys.agy_setup_empty')}</p>
        )}
        {!loading && cards.length > 0 && (
          <div
            className={styles.accounts}
            role="group"
            aria-label={t('config_management.visual.api_keys.agy_setup_account')}
          >
            {cards}
          </div>
        )}
        {!loading && (error || !ready) && (
          <Button variant="secondary" onClick={retry} disabled={downloading}>
            {t('config_management.visual.api_keys.agy_setup_retry')}
          </Button>
        )}
        <AgySetupNotice downloaded={downloaded} />
        <a href={setupUrl.href} target="_blank" rel="noreferrer">
          {t('config_management.visual.api_keys.agy_setup_instructions')}
        </a>
      </div>
    </Modal>
  );
}

export function AgySetupButton({ client, disabled }: AgySetupButtonProps) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const show = () => {
    setOpen(true);
  };
  const close = () => {
    setOpen(false);
  };
  return (
    <>
      <Button variant="ghost" size="sm" onClick={show} disabled={disabled}>
        <IconDownload size={16} />
        {t('config_management.visual.api_keys.agy_setup_action')}
      </Button>
      {open && <AgySetupDialog client={client} onClose={close} />}
    </>
  );
}
