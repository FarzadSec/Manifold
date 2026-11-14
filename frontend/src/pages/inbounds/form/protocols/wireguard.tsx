import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Form, Input, InputNumber, Space } from 'antd';
import { ReloadOutlined } from '@ant-design/icons';

import { FormField } from '@/components/form/rhf';

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
      <FormField name={['settings', 'dns']} label={t('pages.inbounds.info.dns')}>
        <Input placeholder="1.1.1.1, 1.0.0.1" />
      </FormField>
    </>
  );
}
