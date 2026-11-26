// Shared rendering for frequent fields in SectionCommon and their canonical sections.
// Only the active tab is mounted, so setting-row DOM IDs stay unique.

import { useTranslation } from 'react-i18next';
import type { VisualConfigValues } from '@/types/visualConfig';
import { SPONSORS } from '../../sponsors';
import { ApiKeysCardEditor } from '../blocks/ApiKeysCardEditor';
import { SettingCell, SettingList, TextSetting, ToggleSetting } from './FieldPrimitives';
import fieldStyles from './Field.module.scss';

export type SharedFieldProps = {
  values: VisualConfigValues;
  disabled: boolean;
  onChange: (patch: Partial<VisualConfigValues>) => void;
  /**
   * Passed to the setting row. SettingList reads direct children to arrange columns,
   * so full-width layout must be declared by the caller or injected by SettingList.
   */
  wide?: boolean;
};

export function HostField({ values, disabled, onChange, wide }: SharedFieldProps) {
  const { t } = useTranslation();
  return (
    <TextSetting
      fieldId="host"
      wide={wide}
      label={t('config_management.visual.sections.server.host')}
      placeholder="0.0.0.0"
      value={values.host}
      onChange={(host) => onChange({ host })}
      disabled={disabled}
    />
  );
}

export function PortField({
  values,
  disabled,
  onChange,
  error,
  wide,
}: SharedFieldProps & { error?: string }) {
  const { t } = useTranslation();
  return (
    <TextSetting
      fieldId="port"
      wide={wide}
      label={t('config_management.visual.sections.server.port')}
      type="number"
      size="sm"
      placeholder="8317"
      value={values.port}
      onChange={(port) => onChange({ port })}
      disabled={disabled}
      error={error}
    />
  );
}

export function ProxyUrlField({ values, disabled, onChange, wide }: SharedFieldProps) {
  const { t } = useTranslation();
  // Long proxy URLs use a full-width row. Sponsor links stay visible in the aside
  // because links inside InfoTip would not be keyboard-accessible.
  const sponsor = SPONSORS[0];
  return (
    <TextSetting
      fieldId="proxyUrl"
      wide={wide}
      size="lg"
      label={t('config_management.visual.sections.network.proxy_url')}
      aside={
        sponsor ? (
          <>
            {t('config_management.visual.sections.network.proxy_url_sponsor_hint')}{' '}
            <a
              className={fieldStyles.fieldSponsorLink}
              href={sponsor.url}
              target="_blank"
              rel="noopener noreferrer sponsored"
            >
              {sponsor.logo ? (
                <img className={fieldStyles.fieldSponsorLogo} src={sponsor.logo} alt="" />
              ) : null}
              {sponsor.name}
            </a>
          </>
        ) : undefined
      }
      placeholder="socks5://user:pass@127.0.0.1:1080/"
      value={values.proxyUrl}
      onChange={(proxyUrl) => onChange({ proxyUrl })}
      disabled={disabled}
    />
  );
}

export function ApiKeysField({ values, disabled, onChange }: SharedFieldProps) {
  return (
    <SettingList>
      <SettingCell fieldId="apiKeys">
        <ApiKeysCardEditor
          value={values.apiKeysText}
          googleAccountClients={values.googleAccountClients}
          disabled={disabled}
          onChange={onChange}
        />
      </SettingCell>
    </SettingList>
  );
}

export function DebugToggle({ values, disabled, onChange, wide }: SharedFieldProps) {
  const { t } = useTranslation();
  return (
    <ToggleSetting
      fieldId="debug"
      wide={wide}
      label={t('config_management.visual.sections.system.debug')}
      checked={values.debug}
      disabled={disabled}
      onChange={(debug) => onChange({ debug })}
    />
  );
}

export function LoggingToFileToggle({ values, disabled, onChange, wide }: SharedFieldProps) {
  const { t } = useTranslation();
  return (
    <ToggleSetting
      fieldId="loggingToFile"
      wide={wide}
      label={t('config_management.visual.sections.system.logging_to_file')}
      checked={values.loggingToFile}
      disabled={disabled}
      onChange={(loggingToFile) => onChange({ loggingToFile })}
    />
  );
}
