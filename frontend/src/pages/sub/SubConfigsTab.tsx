import { Fragment } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Tag } from 'antd';
import { CopyOutlined } from '@ant-design/icons';

import ConfigBlock from '@/components/clients/ConfigBlock';
import OpenvpnProfileBlocks from '@/components/clients/OpenvpnProfileBlocks';
import {
  amneziawgConfigFromLink,
  isPostQuantumLink,
  wireguardConfigFromLink,
} from '@/lib/xray/inbound-link';
import { LinkTags, parseLinkParts } from '@/lib/xray/link-label';
import { isOpenvpnLink } from '@/lib/xray/openvpn-link';
import L2tpAccountBlocks from '@/components/clients/L2tpAccountBlocks';
import { isL2tpLink } from '@/lib/xray/l2tp-link';
import SubQrButton from './SubQrButton';
import SubAccountLine, { type SubAccount } from './SubAccountLine';

interface SubConfigsTabProps {
  links: string[];
  /** The user each link belongs to, by position. */
  emails?: string[];
  accounts?: Record<string, SubAccount>;
  onCopy: (value: string, toast?: string) => void;
}

export default function SubConfigsTab({
  links: allLinks,
  emails = [],
  accounts = {},
  onCopy,
}: SubConfigsTabProps) {
  const { t } = useTranslation();
  // An OpenVPN profile is a file to download and an L2TP account is typed in by hand; neither is a link an app imports.
  const rows = allLinks
    .map((link, index) => ({ link, index }))
    .filter(({ link }) => !isOpenvpnLink(link) && !isL2tpLink(link));
  const links = rows.map(({ link }) => link);
  const accountLine = (index: number, tunnel: boolean) => {
    const username = emails[index];
    const account = username ? accounts[username] : undefined;
    if (!username || !account) return null;
    return (
      <SubAccountLine
        username={username}
        users={(tunnel ? account.userLimit : account.limitIp) ?? 0}
        password={tunnel ? account.password : undefined}
        onCopy={onCopy}
      />
    );
  };

  return (
    <div className="sub-rows">
      <div className="sub-configs-bar">
        <Button
          icon={<CopyOutlined aria-hidden />}
          onClick={() => onCopy(links.join('\n'), t('subscription.copyAllConfigsCopied'))}
        >
          {t('subscription.copyAllConfigs')}
        </Button>
      </div>
      {rows.map(({ link, index: linkIndex }, idx) => {
        const parts = parseLinkParts(link);
        const rowTitle = parts?.remark || `Link ${idx + 1}`;
        const isWireguardLink = link.startsWith('wireguard://') || link.startsWith('wg://');
        const wireguardConfig = isWireguardLink ? wireguardConfigFromLink(link, rowTitle) : '';
        const amneziawgConfig = link.startsWith('vpn://') ? amneziawgConfigFromLink(link) : '';
        return (
          <Fragment key={link}>
            <div className="sub-row">
              {parts ? <LinkTags parts={parts} /> : <Tag className="sub-row-tag">LINK</Tag>}
              <span className="sub-row-title" dir="auto" title={rowTitle}>
                {rowTitle}
              </span>
              <div className="sub-row-actions">
                <Button
                  icon={<CopyOutlined aria-hidden />}
                  onClick={() => onCopy(link)}
                  aria-label={t('copy')}
                  title={t('copy')}
                />
                {!isPostQuantumLink(link) && (
                  <SubQrButton value={link} label={rowTitle} onCopy={onCopy} />
                )}
              </div>
            </div>
            {accountLine(linkIndex, false)}
            {wireguardConfig && (
              <ConfigBlock
                label={t('pages.clients.wireguardConfig')}
                text={wireguardConfig}
                fileName={`${rowTitle || 'peer'}.conf`}
                qrRemark={rowTitle}
                tagColor="cyan"
              />
            )}
            {amneziawgConfig && (
              <ConfigBlock
                label={t('pages.clients.amneziaWgConfig')}
                text={amneziawgConfig}
                fileName={`${rowTitle || 'peer'}.conf`}
                qrRemark={rowTitle}
                tagColor="purple"
              />
            )}
          </Fragment>
        );
      })}
      <OpenvpnProfileBlocks
        links={allLinks}
        fallbackName="openvpn"
        footer={(index) => accountLine(index, true)}
      />
      <L2tpAccountBlocks
        links={allLinks}
        fallbackName="l2tp"
        footer={(index) => accountLine(index, true)}
      />
    </div>
  );
}
