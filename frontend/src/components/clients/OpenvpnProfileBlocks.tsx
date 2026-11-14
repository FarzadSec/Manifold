import { useTranslation } from 'react-i18next';

import ConfigBlock from '@/components/clients/ConfigBlock';
import { openvpnProfilesFromLinks } from '@/lib/xray/openvpn-link';

interface OpenvpnProfileBlocksProps {
  links: string[];
  fallbackName: string;
}

// A profile is several kilobytes: too much for a QR code, so it is offered as a file.
export default function OpenvpnProfileBlocks({ links, fallbackName }: OpenvpnProfileBlocksProps) {
  const { t } = useTranslation();
  return (
    <>
      {openvpnProfilesFromLinks(links, fallbackName).map((profile) => (
        <ConfigBlock
          key={profile.key}
          label={
            profile.remark
              ? `${t('pages.clients.openvpnProfile')} · ${profile.remark}`
              : t('pages.clients.openvpnProfile')
          }
          text={profile.text}
          fileName={profile.fileName}
          showQr={false}
          tagColor="gold"
        />
      ))}
    </>
  );
}
