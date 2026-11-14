import { useTranslation } from 'react-i18next';
import { Alert } from 'antd';

import { useNativeEnginesQuery } from '@/api/queries/useNativeEnginesQuery';

// Shown for an inbound of the panel server itself; a node speaks for its own engines.
export default function EngineMissingAlert({ protocol }: { protocol: string }) {
  const { t } = useTranslation();
  const { data: engines } = useNativeEnginesQuery(true);
  const missing = engines?.find((engine) => engine.protocol === protocol && !engine.installed);
  if (!missing) return null;
  return (
    <Alert
      type="warning"
      showIcon
      style={{ marginBottom: 16 }}
      title={t('pages.inbounds.engineMissing', { package: missing.package })}
    />
  );
}
