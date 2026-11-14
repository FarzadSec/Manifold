import { useTranslation } from 'react-i18next';
import { Alert, Input, Select, Switch } from 'antd';
import { useFormContext, useWatch } from 'react-hook-form';

import { selectTagRender } from '@/components/form';
import { NativeSelect } from '@/components/form/NativeSelect';
import { FormField, rhfZodValidate } from '@/components/form/rhf';
import { OpenvpnServerSchema } from '@/schemas/protocols/inbound/openvpn';

export default function OpenvpnFields() {
  const { t } = useTranslation();
  const { control } = useFormContext();
  const hasKeys = !!useWatch({ control, name: 'settings.server.caCert' });

  return (
    <>
      <FormField
        name={['settings', 'server', 'proto']}
        label={t('pages.inbounds.openvpn.transport')}
        tooltip={t('pages.inbounds.openvpn.transportHint')}
      >
        <NativeSelect
          options={[
            { label: 'UDP', value: 'udp' },
            { label: 'TCP', value: 'tcp' },
          ]}
        />
      </FormField>

      <FormField
        name={['settings', 'server', 'subnet']}
        label={t('pages.inbounds.openvpn.subnet')}
        tooltip={t('pages.inbounds.openvpn.subnetHint')}
        rules={{ validate: rhfZodValidate(OpenvpnServerSchema.shape.subnet) }}
      >
        <Input placeholder="10.8.0.0/24" />
      </FormField>

      <FormField
        name={['settings', 'server', 'dns']}
        label={t('pages.inbounds.openvpn.dns')}
        rules={{ validate: rhfZodValidate(OpenvpnServerSchema.shape.dns) }}
      >
        <Select
          mode="tags"
          tagRender={selectTagRender}
          tokenSeparators={[',', ' ']}
          options={[
            { label: '1.1.1.1', value: '1.1.1.1' },
            { label: '8.8.8.8', value: '8.8.8.8' },
            { label: '9.9.9.9', value: '9.9.9.9' },
          ]}
        />
      </FormField>

      <FormField
        name={['settings', 'server', 'promptCredentials']}
        label={t('pages.inbounds.openvpn.promptCredentials')}
        tooltip={t('pages.inbounds.openvpn.promptCredentialsHint')}
        valueProp="checked"
      >
        <Switch />
      </FormField>

      <Alert
        type="info"
        showIcon
        title={
          hasKeys ? t('pages.inbounds.openvpn.keysKept') : t('pages.inbounds.openvpn.keysIssued')
        }
      />
    </>
  );
}
