import { useTranslation } from 'react-i18next';
import { IconSidebarOauth, IconSidebarSystem } from '@/components/ui/icons';
import type { ApiKeyKind } from '@/services/api/keyNames';
import styles from './Blocks.module.scss';

export function ApiKeyKindCards({
  value,
  onChange,
  disabled,
}: {
  value: ApiKeyKind | null;
  onChange: (kind: ApiKeyKind) => void;
  disabled?: boolean;
}) {
  const { t } = useTranslation();
  return (
    <div
      className={styles.keyKindCards}
      role="group"
      aria-label={t('config_management.visual.api_keys.kind_label')}
    >
      {(['person', 'system'] as const).map((kind) => (
        <button
          type="button"
          key={kind}
          className={styles.keyKindCard}
          aria-pressed={value === kind}
          disabled={disabled}
          onClick={() => onChange(kind)}
        >
          {kind === 'person' ? <IconSidebarOauth size={24} /> : <IconSidebarSystem size={24} />}
          <strong>{t(`config_management.visual.api_keys.kind_${kind}`)}</strong>
        </button>
      ))}
    </div>
  );
}
