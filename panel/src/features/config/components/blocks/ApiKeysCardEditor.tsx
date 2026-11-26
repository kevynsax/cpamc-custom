// CUSTOM (cpamc-custom)
import { memo, useId, useState, type ChangeEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { FieldLabel } from '@/components/ui/FieldLabel';
import { useAuthStore, useNotificationStore } from '@/stores';
import { clientKeyFingerprint } from '../../apiKeyNames';
import { useApiKeyNames } from '../../useApiKeyNames';
import type { ApiKeyKind, ApiKeySpeed, KeyMetadata } from '@/services/api/keyNames';
import type { GoogleAccountClient, VisualConfigValues } from '@/types/visualConfig';
import { updateAgyClient } from '../../apiKeyAgy';
import { ApiKeyKindCards } from './ApiKeyKindCards';
import { ApiKeyClientSettings } from './ApiKeyClientSettings';
import { copyToClipboard } from '@/utils/clipboard';
import { generateSecureApiKey } from '@/utils/apiKey';
import { maskApiKey } from '@/utils/format';
import { isValidApiKeyCharset } from '@/utils/validation';
import { ApiKeyStrengthMeter } from './ApiKeyStrengthMeter';
import styles from './Blocks.module.scss';

type ApiKeysCardEditorProps = {
  value: string;
  googleAccountClients: GoogleAccountClient[];
  disabled?: boolean;
  onChange: (patch: Pick<VisualConfigValues, 'apiKeysText' | 'googleAccountClients'>) => void;
};

type ApiKeyEntryProps = {
  apiKey: string;
  keyId: string;
  index: number;
  name: string;
  kind: ApiKeyKind;
  speed: ApiKeySpeed;
  agy: boolean;
  disabled: boolean;
  onCopy: (key: string) => Promise<void>;
  onEdit: (keyId: string) => void;
  onDelete: (keyId: string) => void;
};

type ApiKeyEntriesProps = KeyMetadata & {
  apiKeys: string[];
  keyIds: string[];
  googleAccountClients: GoogleAccountClient[];
  disabled: boolean;
  onCopy: ApiKeyEntryProps['onCopy'];
  onEdit: ApiKeyEntryProps['onEdit'];
  onDelete: ApiKeyEntryProps['onDelete'];
};

function ApiKeyEntry(props: ApiKeyEntryProps) {
  const { apiKey, keyId, index, name, kind, speed, agy, disabled, onCopy, onEdit, onDelete } =
    props;
  const { t } = useTranslation();
  const copy = async () => {
    await onCopy(apiKey);
  };
  const edit = () => {
    onEdit(keyId);
  };
  const remove = () => {
    onDelete(keyId);
  };
  const usageKey = agy
    ? 'config_management.visual.api_keys.purpose_agy'
    : `config_management.visual.api_keys.speed_${speed}`;
  return (
    <li className={styles.apiKeyRow}>
      <span className={styles.apiKeyIndex}>#{index + 1}</span>
      <span className={styles.apiKeyMeta}>
        <span className={styles.apiKeyName}>
          {name || t('config_management.visual.api_keys.input_label')}
        </span>
        <span className={styles.apiKeyKind}>
          {t(`config_management.visual.api_keys.kind_${kind}`)}
          {' · '}
          {t(usageKey)}
        </span>
        <span className={styles.apiKeyValue}>{maskApiKey(apiKey)}</span>
      </span>
      <span className={styles.apiKeyActions}>
        <Button variant="ghost" size="sm" onClick={copy} disabled={disabled}>
          {t('common.copy')}
        </Button>
        <Button variant="ghost" size="sm" onClick={edit} disabled={disabled}>
          {t('config_management.visual.common.edit')}
        </Button>
        <Button
          variant="ghost"
          size="sm"
          className={styles.apiKeyDelete}
          onClick={remove}
          disabled={disabled}
        >
          {t('config_management.visual.common.delete')}
        </Button>
      </span>
    </li>
  );
}

function ApiKeyEntries(props: ApiKeyEntriesProps) {
  const {
    apiKeys,
    keyIds,
    googleAccountClients,
    names,
    kinds,
    speeds,
    disabled,
    onCopy,
    onEdit,
    onDelete,
  } = props;
  const { t } = useTranslation();
  if (apiKeys.length === 0) {
    return <div className={styles.emptyState}>{t('config_management.visual.api_keys.empty')}</div>;
  }
  const renderEntry = (apiKey: string, index: number) => {
    const fingerprint = keyIds[index];
    const matchesKey = (client: GoogleAccountClient) => client.apiKey === apiKey;
    const agy = googleAccountClients.some(matchesKey);
    return (
      <ApiKeyEntry
        key={keyIds[index]}
        keyId={keyIds[index]}
        apiKey={apiKey}
        index={index}
        name={names[fingerprint] ?? ''}
        kind={kinds[fingerprint] ?? 'person'}
        speed={speeds[fingerprint] ?? 'standard'}
        agy={agy}
        disabled={disabled}
        onCopy={onCopy}
        onEdit={onEdit}
        onDelete={onDelete}
      />
    );
  };
  const entries = apiKeys.map(renderEntry);
  return <ol className={styles.apiKeyList}>{entries}</ol>;
}

function ApiKeysCardEditorRoot(props: ApiKeysCardEditorProps) {
  const selectApiBase = (state: ReturnType<typeof useAuthStore.getState>) => state.apiBase;
  const apiBase = useAuthStore(selectApiBase);
  return <ScopedApiKeysCardEditor key={apiBase} {...props} />;
}

export const ApiKeysCardEditor = memo(ApiKeysCardEditorRoot);

function ScopedApiKeysCardEditor({
  value,
  googleAccountClients,
  disabled: parentDisabled,
  onChange,
}: ApiKeysCardEditorProps) {
  const [nameValue, setNameValue] = useState('');
  const [kindValue, setKindValue] = useState<ApiKeyKind | null>(null);
  const [speedValue, setSpeedValue] = useState<ApiKeySpeed>('standard');
  const [agyValue, setAgyValue] = useState(false);
  const { t } = useTranslation();
  const selectNotification = (state: ReturnType<typeof useNotificationStore.getState>) =>
    state.showNotification;
  const showNotification = useNotificationStore(selectNotification);
  const apiKeys: string[] = [];
  const lines = value.split('\n');
  for (const line of lines) {
    const key = line.trim();
    if (key) apiKeys.push(key);
  }
  const {
    names,
    kinds,
    speeds,
    busy: namesBusy,
    error: namesError,
    save: saveName,
  } = useApiKeyNames(apiKeys);
  const disabled = parentDisabled || namesBusy;
  const keyIds = apiKeys.map(clientKeyFingerprint);

  const apiKeyInputId = useId();
  const apiKeyHintId = `${apiKeyInputId}-hint`;
  const apiKeyErrorId = `${apiKeyInputId}-error`;
  const [modalOpen, setModalOpen] = useState(false);
  const [editingApiKeyId, setEditingApiKeyId] = useState<string | null>(null);
  const [inputValue, setInputValue] = useState('');
  const [formError, setFormError] = useState('');

  const openAddModal = () => {
    setKindValue(null);
    setSpeedValue('standard');
    setAgyValue(false);
    setNameValue('');
    setEditingApiKeyId(null);
    setInputValue('');
    setFormError('');
    setModalOpen(true);
  };

  const openEditModal = (apiKeyId: string) => {
    const matchesId = (id: string) => id === apiKeyId;
    const editingIndex = keyIds.findIndex(matchesId);
    const apiKey = apiKeys[editingIndex];
    const matchesKey = (client: GoogleAccountClient) => client.apiKey === apiKey;
    const agy = googleAccountClients.some(matchesKey);
    setNameValue(names[apiKeyId] ?? '');
    setKindValue(kinds[apiKeyId] ?? 'person');
    setSpeedValue(speeds[apiKeyId] ?? 'standard');
    setAgyValue(agy);
    setEditingApiKeyId(apiKeyId);
    setInputValue(apiKey);
    setFormError('');
    setModalOpen(true);
  };

  const closeModal = () => {
    setModalOpen(false);
    setInputValue('');
    setEditingApiKeyId(null);
    setFormError('');
  };

  const updateApiKeys = (nextKeys: string[], nextClients: GoogleAccountClient[]) => {
    const patch = { apiKeysText: nextKeys.join('\n'), googleAccountClients: nextClients };
    onChange(patch);
  };

  const handleDelete = (apiKeyId: string) => {
    const matchesId = (id: string) => id === apiKeyId;
    const index = keyIds.findIndex(matchesId);
    if (index < 0) return;
    const keepKey = (_key: string, position: number) => position !== index;
    const nextKeys = apiKeys.filter(keepKey);
    const deletedKey = apiKeys[index];
    const keyStillPresent = nextKeys.includes(deletedKey);
    const keepClient = (client: GoogleAccountClient) =>
      client.apiKey !== deletedKey || keyStillPresent;
    const nextClients = googleAccountClients.filter(keepClient);
    updateApiKeys(nextKeys, nextClients);
  };

  const handleSave = async () => {
    const trimmed = inputValue.trim();
    if (!trimmed) {
      const message = t('config_management.visual.api_keys.error_empty');
      setFormError(message);
      return;
    }
    if (!isValidApiKeyCharset(trimmed)) {
      const message = t('config_management.visual.api_keys.error_invalid');
      setFormError(message);
      return;
    }

    if (!kindValue) {
      const message = t('config_management.visual.api_keys.kind_required');
      setFormError(message);
      return;
    }
    const matchesId = (id: string) => id === editingApiKeyId;
    const editingIndex = keyIds.findIndex(matchesId);
    const duplicateKey = (key: string, index: number) => index !== editingIndex && key === trimmed;
    if (apiKeys.some(duplicateKey)) {
      const message = t('config_management.visual.api_keys.error_duplicate');
      setFormError(message);
      return;
    }
    const nextKeys = [...apiKeys];
    if (editingIndex < 0) nextKeys.push(trimmed);
    else nextKeys[editingIndex] = trimmed;
    const savedName = await saveName(trimmed, nameValue, kindValue, speedValue);
    if (!savedName) {
      const message = t('config_management.visual.api_keys.name_save_error');
      setFormError(message);
      return;
    }
    const previousKey = apiKeys[editingIndex] ?? '';
    const nextClients = updateAgyClient(
      googleAccountClients,
      previousKey,
      trimmed,
      nameValue,
      agyValue
    );
    updateApiKeys(nextKeys, nextClients);
    closeModal();
  };

  const handleCopy = async (apiKey: string) => {
    const copied = await copyToClipboard(apiKey);
    const messageKey = copied ? 'notification.link_copied' : 'notification.copy_failed';
    const message = t(messageKey);
    const severity = copied ? 'success' : 'error';
    showNotification(message, severity);
  };

  const handleGenerate = () => {
    const key = generateSecureApiKey();
    setInputValue(key);
    setFormError('');
  };

  const changeName = (event: ChangeEvent<HTMLInputElement>) => {
    const nextName = event.target.value;
    setNameValue(nextName);
  };
  const changeKey = (event: ChangeEvent<HTMLInputElement>) => {
    const nextKey = event.target.value;
    setInputValue(nextKey);
  };

  return (
    <div className={styles.apiKeys}>
      <div className={styles.apiKeysHeader}>
        <span className={styles.apiKeysTitle}>{t('config_management.visual.api_keys.label')}</span>
        <Button size="sm" onClick={openAddModal} disabled={disabled}>
          {t('config_management.visual.api_keys.add')}
        </Button>
      </div>

      {namesError && (
        <div role="alert" className="error-box">
          {t('config_management.visual.api_keys.name_save_error')}
        </div>
      )}
      <ApiKeyEntries
        apiKeys={apiKeys}
        keyIds={keyIds}
        googleAccountClients={googleAccountClients}
        names={names}
        kinds={kinds}
        speeds={speeds}
        disabled={disabled}
        onCopy={handleCopy}
        onEdit={openEditModal}
        onDelete={handleDelete}
      />

      <Modal
        open={modalOpen}
        onClose={closeModal}
        closeDisabled={disabled}
        title={
          editingApiKeyId !== null
            ? t('config_management.visual.api_keys.edit_title')
            : t('config_management.visual.api_keys.add_title')
        }
        footer={
          <>
            <Button variant="secondary" onClick={closeModal} disabled={disabled || namesBusy}>
              {t('config_management.visual.common.cancel')}
            </Button>
            <Button onClick={handleSave} disabled={disabled || namesBusy}>
              {editingApiKeyId !== null
                ? t('config_management.visual.common.update')
                : t('config_management.visual.common.add')}
            </Button>
          </>
        }
      >
        <Input
          label={t('config_management.visual.api_keys.name_label')}
          hint={t('config_management.visual.api_keys.name_hint')}
          value={nameValue}
          onChange={changeName}
          placeholder={t('config_management.visual.api_keys.name_placeholder')}
          disabled={disabled}
        />
        <ApiKeyKindCards value={kindValue} onChange={setKindValue} disabled={disabled} />
        <ApiKeyClientSettings
          agy={agyValue}
          speed={speedValue}
          onAgyChange={setAgyValue}
          onSpeedChange={setSpeedValue}
          disabled={disabled}
        />
        <div className="form-group">
          <FieldLabel
            htmlFor={apiKeyInputId}
            label={t('config_management.visual.api_keys.input_label')}
            hint={t('config_management.visual.api_keys.input_hint')}
            hintId={apiKeyHintId}
          />
          <div className={styles.apiKeyModalInputRow}>
            <input
              id={apiKeyInputId}
              className="input"
              placeholder={t('config_management.visual.api_keys.input_placeholder')}
              value={inputValue}
              onChange={changeKey}
              disabled={disabled}
              aria-describedby={formError ? `${apiKeyErrorId} ${apiKeyHintId}` : apiKeyHintId}
              aria-invalid={Boolean(formError)}
            />
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={handleGenerate}
              disabled={disabled}
            >
              {t('config_management.visual.api_keys.generate')}
            </Button>
          </div>
          <ApiKeyStrengthMeter value={inputValue} />
          {formError && (
            <div id={apiKeyErrorId} className="error-box">
              {formError}
            </div>
          )}
        </div>
      </Modal>
    </div>
  );
}
