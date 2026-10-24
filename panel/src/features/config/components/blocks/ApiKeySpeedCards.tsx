import { useTranslation } from 'react-i18next';
import { IconGauge, IconRocket, IconZap } from '@/components/ui/icons';
import { API_KEY_SPEEDS, type ApiKeySpeed } from '@/services/api/keyNames';
import styles from './Blocks.module.scss';

// CUSTOM (cpamc-custom): fastest OpenAI service tier this key may request.
const SPEED_ICONS = { standard: IconGauge, fast: IconZap, ultrafast: IconRocket } as const;

export function ApiKeySpeedCards({
  value,
  onChange,
  disabled,
}: {
  value: ApiKeySpeed;
  onChange: (speed: ApiKeySpeed) => void;
  disabled?: boolean;
}) {
  const { t } = useTranslation();
  return (
    <div
      className={styles.keySpeedCards}
      role="group"
      aria-label={t('config_management.visual.api_keys.speed_label')}
    >
      {API_KEY_SPEEDS.map((speed) => {
        const Icon = SPEED_ICONS[speed];
        return (
          <button
            type="button"
            key={speed}
            className={styles.keyKindCard}
            aria-pressed={value === speed}
            disabled={disabled}
            onClick={() => onChange(speed)}
          >
            <Icon size={24} />
            <span className={styles.keySpeedCopy}>
              <strong>{t(`config_management.visual.api_keys.speed_${speed}`)}</strong>
              <small>{t(`config_management.visual.api_keys.speed_${speed}_hint`)}</small>
            </span>
          </button>
        );
      })}
    </div>
  );
}
