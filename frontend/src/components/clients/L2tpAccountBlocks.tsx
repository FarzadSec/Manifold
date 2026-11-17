import { Fragment, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';

import ConfigBlock from '@/components/clients/ConfigBlock';
import { l2tpAccountFileName, l2tpAccountText, l2tpAccountsFromLinks } from '@/lib/xray/l2tp-link';

interface L2tpAccountBlocksProps {
  links: string[];
  fallbackName: string;
  /** Shown under the account of the link at this index. */
  footer?: (linkIndex: number) => ReactNode;
}

// No app imports an L2TP account: a person types these lines into the VPN settings of a device.
export default function L2tpAccountBlocks({ links, fallbackName, footer }: L2tpAccountBlocksProps) {
  const { t } = useTranslation();
  const labels = {
    protocol: t('protocol'),
    server: t('pages.clients.l2tpServer'),
    username: t('username'),
    password: t('password'),
    psk: t('pages.inbounds.l2tp.psk'),
  };
  return (
    <>
      {l2tpAccountsFromLinks(links).map(({ key, index, account }) => (
        <Fragment key={key}>
          <ConfigBlock
            label={
              account.remark
                ? `${t('pages.clients.l2tpAccount')} · ${account.remark}`
                : t('pages.clients.l2tpAccount')
            }
            text={l2tpAccountText(account, labels)}
            fileName={l2tpAccountFileName(account.remark, fallbackName)}
            showQr={false}
            tagColor="green"
            defaultOpen
          />
          {footer?.(index)}
        </Fragment>
      ))}
    </>
  );
}
