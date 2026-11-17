import { Fragment, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';

import ConfigBlock from '@/components/clients/ConfigBlock';
import { openvpnProfilesFromLinks } from '@/lib/xray/openvpn-link';

interface OpenvpnProfileBlocksProps {
  links: string[];
  fallbackName: string;
  /** Shown under the profile of the link at this index. */
  footer?: (linkIndex: number) => ReactNode;
}

// A profile is several kilobytes: too much for a QR code, so it is offered as a file.
export default function OpenvpnProfileBlocks({
  links,
  fallbackName,
  footer,
}: OpenvpnProfileBlocksProps) {
  const { t } = useTranslation();
  return (
    <>
      {openvpnProfilesFromLinks(links, fallbackName).map((profile) => (
        <Fragment key={profile.key}>
          <ConfigBlock
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
          {footer?.(profile.index)}
        </Fragment>
      ))}
    </>
  );
}
