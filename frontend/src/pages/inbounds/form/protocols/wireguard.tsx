import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Collapse, Form, Input, InputNumber, Space, Typography } from 'antd';
import { ReloadOutlined } from '@ant-design/icons';

import { FormField } from '@/components/form/rhf';

// wg-quick's own key names, which is how its documentation and every guide spell them.
const HOOKS = [
  { name: 'preUp', label: 'PreUp' },
  { name: 'postUp', label: 'PostUp' },
  { name: 'preDown', label: 'PreDown' },
  { name: 'postDown', label: 'PostDown' },
] as const;

interface WireguardFieldsProps {
  wgPubKey: string;
  regenInboundWg: () => void;
}

export default function WireguardFields({ wgPubKey, regenInboundWg }: WireguardFieldsProps) {
  const { t } = useTranslation();
  const uid = useId();
  return (
    <>
      <Form.Item htmlFor={`${uid}-secretKey`} label={t('pages.xray.wireguard.secretKey')}>
        <Space.Compact block>
          <FormField name={['settings', 'secretKey']} noStyle>
            <Input id={`${uid}-secretKey`} style={{ width: 'calc(100% - 32px)' }} />
          </FormField>
          <Button
            aria-label={t('regenerate')}
            icon={<ReloadOutlined aria-hidden />}
            onClick={regenInboundWg}
          />
        </Space.Compact>
      </Form.Item>
      <Form.Item htmlFor={`${uid}-publicKey`} label={t('pages.xray.wireguard.publicKey')}>
        <Input id={`${uid}-publicKey`} value={wgPubKey} disabled />
      </Form.Item>
      <FormField
        name={['settings', 'interface']}
        label={t('pages.inbounds.wireguard.interface')}
        tooltip={t('pages.inbounds.wireguard.interfaceHint')}
      >
        <Input placeholder="wg0" maxLength={15} />
      </FormField>
      <FormField name={['settings', 'subnetIp']} label={t('pages.xray.wireguard.subnetIp')}>
        <Input placeholder="10.0.0.0" />
      </FormField>
      <FormField name={['settings', 'subnetCidr']} label={t('pages.xray.wireguard.subnetCidr')}>
        <InputNumber min={1} max={30} style={{ width: '100%' }} />
      </FormField>
      <FormField name={['settings', 'mtu']} label="MTU">
        <InputNumber />
      </FormField>
      <FormField
        name={['settings', 'address']}
        label={t('pages.inbounds.wireguard.address')}
        tooltip={t('pages.inbounds.wireguard.addressHint')}
      >
        <Input placeholder="10.0.0.1/24" />
      </FormField>
      <FormField name={['settings', 'dns']} label={t('pages.inbounds.info.dns')}>
        <Input placeholder="1.1.1.1, 1.0.0.1" />
      </FormField>
      <Collapse
        ghost
        items={[
          {
            key: 'hooks',
            label: t('pages.inbounds.wireguard.hooks'),
            forceRender: true,
            children: (
              <>
                <Typography.Paragraph type="secondary">
                  {t('pages.inbounds.wireguard.hooksHint')}
                </Typography.Paragraph>
                {HOOKS.map((hook) => (
                  <FormField key={hook.name} name={['settings', hook.name]} label={hook.label}>
                    <Input.TextArea autoSize={{ minRows: 1, maxRows: 6 }} spellCheck={false} />
                  </FormField>
                ))}
              </>
            ),
          },
        ]}
      />
    </>
  );
}
