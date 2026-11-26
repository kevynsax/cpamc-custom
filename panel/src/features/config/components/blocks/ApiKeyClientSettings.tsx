// CUSTOM (cpamc-custom)
import { useTranslation } from 'react-i18next';
import { useId } from 'react';
import { IconKey } from '@/components/ui/icons';
import iconAntigravity from '@/assets/icons/antigravity.svg';
import styles from './Blocks.module.scss';
import { ApiKeySpeedCards } from './ApiKeySpeedCards';
import type { ApiKeySpeed } from '@/services/api/keyNames';
import clientStyles from './ApiKeyClientSettings.module.scss';

type ApiKeyClientSettingsProps = {
  agy: boolean;
  speed: ApiKeySpeed;
  disabled: boolean;
  onAgyChange: (agy: boolean) => void;
  onSpeedChange: (speed: ApiKeySpeed) => void;
};

export function ApiKeyClientSettings({
  agy,
  speed,
  disabled,
  onAgyChange,
  onSpeedChange,
}: ApiKeyClientSettingsProps) {
  const { t } = useTranslation();
  const hintId = useId();
  const descriptionId = agy ? hintId : undefined;
  const selectGeneral = () => {
    onAgyChange(false);
  };
  const selectAgy = () => {
    onAgyChange(true);
  };

  return (
    <>
      <div
        className={styles.keyKindCards}
        role="group"
        aria-label={t('config_management.visual.api_keys.purpose_label')}
      >
        <button
          type="button"
          className={styles.keyKindCard}
          aria-pressed={!agy}
          disabled={disabled}
          onClick={selectGeneral}
        >
          <IconKey size={24} />
          <span className={styles.keySpeedCopy}>
            <strong>{t('config_management.visual.api_keys.purpose_general')}</strong>
            <small>{t('config_management.visual.api_keys.purpose_general_hint')}</small>
          </span>
        </button>
        <button
          type="button"
          className={styles.keyKindCard}
          aria-pressed={agy}
          disabled={disabled}
          onClick={selectAgy}
        >
          <img className={clientStyles.icon} src={iconAntigravity} alt="" />
          <span className={styles.keySpeedCopy}>
            <strong>{t('config_management.visual.api_keys.purpose_agy')}</strong>
            <small>{t('config_management.visual.api_keys.purpose_agy_hint')}</small>
          </span>
        </button>
      </div>
      <ApiKeySpeedCards
        value={speed}
        onChange={onSpeedChange}
        disabled={disabled || agy}
        descriptionId={descriptionId}
      />
      <p id={hintId} className={clientStyles.speedHint} hidden={!agy}>
        {t('config_management.visual.api_keys.speed_agy_hint')}
      </p>
    </>
  );
}
