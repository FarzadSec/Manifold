import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Card,
  Col,
  Form,
  Input,
  InputNumber,
  Modal,
  Row,
  Select,
  Switch,
  Typography,
} from 'antd';
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons';
import { FormProvider, useFieldArray, useForm } from 'react-hook-form';

import { FormField, rhfZodValidate } from '@/components/form/rhf';
import { selectTagRender } from '@/components/form';
import type { IngressListener } from '@/generated/types';
import { IngressFormSchema, IngressRouteFormSchema, type IngressFormValues } from '@/schemas/node';

interface IngressFormModalProps {
  open: boolean;
  editing: IngressListener | null;
  onSave: (values: IngressFormValues) => Promise<boolean>;
  onClose: () => void;
}

const EMPTY: IngressFormValues = {
  id: 0,
  remark: '',
  enable: true,
  listen: '',
  port: 443,
  routes: [{ remark: '', hosts: [], alpn: [], target: '', proxyProtocol: 0 }],
  defaultTarget: '',
  defaultProxyProtocol: 0,
};

function toForm(row: IngressListener | null): IngressFormValues {
  if (!row) return EMPTY;
  return {
    id: row.id,
    remark: row.remark,
    enable: row.enable,
    listen: row.listen,
    port: row.port,
    routes: (row.routes ?? []).map((r) => ({
      remark: r.remark,
      hosts: r.hosts ?? [],
      alpn: r.alpn ?? [],
      target: r.target,
      proxyProtocol: r.proxyProtocol,
    })),
    defaultTarget: row.defaultTarget,
    defaultProxyProtocol: row.defaultProxyProtocol,
  };
}

export default function IngressFormModal({
  open,
  editing,
  onSave,
  onClose,
}: IngressFormModalProps) {
  const { t } = useTranslation();
  const methods = useForm<IngressFormValues>({ defaultValues: EMPTY });
  const routes = useFieldArray({ control: methods.control, name: 'routes' });
  const [saving, setSaving] = useState(false);
  const [synced, setSynced] = useState<{ open: boolean; editing: IngressListener | null } | null>(
    null,
  );
  if (!synced || synced.open !== open || synced.editing !== editing) {
    setSynced({ open, editing });
    if (open) methods.reset(toForm(editing));
  }

  const proxyOptions = [
    { value: 0, label: t('pages.ingress.proxyOff') },
    { value: 1, label: 'PROXY v1' },
    { value: 2, label: 'PROXY v2' },
  ];

  async function submit(values: IngressFormValues) {
    setSaving(true);
    try {
      if (await onSave(values)) onClose();
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      open={open}
      onCancel={onClose}
      width={760}
      title={editing ? t('pages.ingress.edit') : t('pages.ingress.add')}
      okText={t('save')}
      confirmLoading={saving}
      onOk={methods.handleSubmit(submit)}
      destroyOnHidden
    >
      <FormProvider {...methods}>
        <Form layout="vertical" component="div">
          <Row gutter={12}>
            <Col xs={24} md={9}>
              <FormField label={t('pages.ingress.remark')} name="remark">
                <Input placeholder="443" />
              </FormField>
            </Col>
            <Col xs={14} md={7}>
              <FormField
                label={t('pages.ingress.listen')}
                name="listen"
                tooltip={t('pages.ingress.listenHint')}
              >
                <Input placeholder="0.0.0.0" />
              </FormField>
            </Col>
            <Col xs={10} md={5}>
              <FormField
                label={t('pages.ingress.port')}
                name="port"
                rules={{ validate: rhfZodValidate(IngressFormSchema.shape.port) }}
              >
                <InputNumber min={1} max={65535} style={{ width: '100%' }} />
              </FormField>
            </Col>
            <Col xs={24} md={3}>
              <FormField label={t('enable')} name="enable" valueProp="checked">
                <Switch />
              </FormField>
            </Col>
          </Row>

          <Typography.Title level={5} style={{ marginTop: 0 }}>
            {t('pages.ingress.routes')}
          </Typography.Title>
          <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>
            {t('pages.ingress.routesHint')}
          </Typography.Paragraph>
          {routes.fields.map((field, i) => (
            <Card
              key={field.id}
              size="small"
              style={{ marginBottom: 8 }}
              extra={
                <Button
                  type="text"
                  danger
                  size="small"
                  icon={<DeleteOutlined aria-hidden />}
                  aria-label={t('delete')}
                  onClick={() => routes.remove(i)}
                />
              }
              title={`#${i + 1}`}
            >
              <Row gutter={12}>
                <Col xs={24} md={12}>
                  <FormField
                    label={t('pages.ingress.hosts')}
                    name={['routes', i, 'hosts']}
                    tooltip={t('pages.ingress.hostsHint')}
                    rules={{ validate: rhfZodValidate(IngressRouteFormSchema.shape.hosts) }}
                  >
                    <Select
                      mode="tags"
                      tagRender={selectTagRender}
                      open={false}
                      placeholder="panel.example.com"
                    />
                  </FormField>
                </Col>
                <Col xs={24} md={12}>
                  <FormField
                    label={t('pages.ingress.target')}
                    name={['routes', i, 'target']}
                    tooltip={t('pages.ingress.targetHint')}
                    rules={{ validate: rhfZodValidate(IngressRouteFormSchema.shape.target) }}
                  >
                    <Input placeholder="127.0.0.1:8443" />
                  </FormField>
                </Col>
                <Col xs={24} md={8}>
                  <FormField
                    label={t('pages.ingress.proxyProtocol')}
                    name={['routes', i, 'proxyProtocol']}
                    tooltip={t('pages.ingress.proxyProtocolHint')}
                  >
                    <Select options={proxyOptions} />
                  </FormField>
                </Col>
                <Col xs={24} md={8}>
                  <FormField
                    label="ALPN"
                    name={['routes', i, 'alpn']}
                    tooltip={t('pages.ingress.alpnHint')}
                  >
                    <Select mode="tags" tagRender={selectTagRender} open={false} placeholder="h2" />
                  </FormField>
                </Col>
                <Col xs={24} md={8}>
                  <FormField label={t('pages.ingress.remark')} name={['routes', i, 'remark']}>
                    <Input />
                  </FormField>
                </Col>
              </Row>
            </Card>
          ))}
          <Button
            icon={<PlusOutlined aria-hidden />}
            onClick={() =>
              routes.append({ remark: '', hosts: [], alpn: [], target: '', proxyProtocol: 0 })
            }
            style={{ marginBottom: 16 }}
          >
            {t('pages.ingress.addRoute')}
          </Button>

          <Row gutter={12}>
            <Col xs={24} md={16}>
              <FormField
                label={t('pages.ingress.defaultTarget')}
                name="defaultTarget"
                tooltip={t('pages.ingress.defaultTargetHint')}
                rules={{ validate: rhfZodValidate(IngressFormSchema.shape.defaultTarget) }}
              >
                <Input placeholder={t('pages.ingress.defaultTargetPlaceholder')} />
              </FormField>
            </Col>
            <Col xs={24} md={8}>
              <FormField label={t('pages.ingress.proxyProtocol')} name="defaultProxyProtocol">
                <Select options={proxyOptions} />
              </FormField>
            </Col>
          </Row>
        </Form>
      </FormProvider>
    </Modal>
  );
}
