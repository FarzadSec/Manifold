import { useTranslation } from 'react-i18next';
import { Alert, Button, Input, InputNumber, Select, Switch } from 'antd';
import { ReloadOutlined } from '@ant-design/icons';
import { useFormContext } from 'react-hook-form';

import { selectTagRender } from '@/components/form';
import { FormField, rhfZodValidate } from '@/components/form/rhf';
import { L2tpServerSchema } from '@/schemas/protocols/inbound/l2tp';
import { RandomUtil } from '@/utils';

export default function L2tpFields() {
  const { t } = useTranslation();
  const { setValue } = useFormContext();

  return (
    <>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        title={t('pages.inbounds.l2tp.portFixed')}
      />

      <FormField
        name={['settings', 'server', 'ipsec']}
        label={t('pages.inbounds.l2tp.ipsec')}
        tooltip={t('pages.inbounds.l2tp.ipsecHint')}
        valueProp="checked"
      >
        <Switch />
      </FormField>

      <FormField
        name={['settings', 'server', 'psk']}
        label={t('pages.inbounds.l2tp.psk')}
        tooltip={t('pages.inbounds.l2tp.pskHint')}
        rules={{ validate: rhfZodValidate(L2tpServerSchema.shape.psk) }}
      >
        <Input
          autoComplete="off"
          spellCheck={false}
          suffix={
            <Button
              type="text"
              size="small"
              aria-label={t('regenerate')}
              icon={<ReloadOutlined aria-hidden />}
              onClick={() =>
                setValue('settings.server.psk', RandomUtil.randomSeq(24), {
                  shouldDirty: true,
                  shouldValidate: true,
                })
              }
            />
          }
        />
      </FormField>

      <FormField
        name={['settings', 'server', 'subnet']}
        label={t('pages.inbounds.openvpn.subnet')}
        tooltip={t('pages.inbounds.openvpn.subnetHint')}
        rules={{ validate: rhfZodValidate(L2tpServerSchema.shape.subnet) }}
      >
        <Input placeholder="10.9.0.0/24" />
      </FormField>

      <FormField
        name={['settings', 'server', 'dns']}
        label={t('pages.inbounds.openvpn.dns')}
        rules={{ validate: rhfZodValidate(L2tpServerSchema.shape.dns) }}
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

      <FormField name={['settings', 'server', 'mtu']} label="MTU">
        <InputNumber min={576} max={1460} />
      </FormField>
    </>
  );
}
