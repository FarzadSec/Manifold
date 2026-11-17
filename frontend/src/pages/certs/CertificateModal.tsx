import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Col, Form, Input, InputNumber, Modal, Row, Segmented, Select, Switch } from 'antd';
import { FormProvider, useForm, useWatch } from 'react-hook-form';

import { selectTagRender } from '@/components/form';
import { FormField, rhfZodValidate } from '@/components/form/rhf';
import type { CertificateView } from '@/generated/types';
import {
  CertDomainsSchema,
  challengeMismatch,
  type CertificateFormValues,
} from '@/schemas/certificate';

interface CertificateModalProps {
  open: boolean;
  editing: CertificateView | null;
  nodes: { id: number; name: string }[];
  onSave: (payload: Record<string, unknown>, issue: boolean) => Promise<boolean>;
  onClose: () => void;
}

const EMPTY: CertificateFormValues = {
  id: 0,
  name: '',
  domains: [],
  challenge: 'dns-01',
  port: 0,
  dnsProvider: 'cloudflare',
  dnsToken: '',
  email: '',
  staging: false,
  autoRenew: true,
  nodeIds: [],
};

function toForm(row: CertificateView | null): CertificateFormValues {
  if (!row) return EMPTY;
  return {
    ...EMPTY,
    id: row.id,
    name: row.name,
    domains: row.domains ?? [],
    challenge: (row.challenge as CertificateFormValues['challenge']) || 'dns-01',
    port: row.port,
    dnsProvider: (row.dnsProvider as CertificateFormValues['dnsProvider']) || 'cloudflare',
    email: row.email,
    staging: row.staging,
    autoRenew: row.autoRenew,
    nodeIds: row.nodeIds ?? [],
  };
}

export default function CertificateModal({
  open,
  editing,
  nodes,
  onSave,
  onClose,
}: CertificateModalProps) {
  const { t } = useTranslation();
  const methods = useForm<CertificateFormValues>({ defaultValues: EMPTY });
  const challenge = useWatch({ control: methods.control, name: 'challenge' });
  const provider = useWatch({ control: methods.control, name: 'dnsProvider' });
  const [saving, setSaving] = useState(false);
  const [synced, setSynced] = useState<{
    open: boolean;
    editing: CertificateView | null;
  } | null>(null);
  if (!synced || synced.open !== open || synced.editing !== editing) {
    setSynced({ open, editing });
    if (open) methods.reset(toForm(editing));
  }

  async function submit(values: CertificateFormValues, issue: boolean) {
    const payload: Record<string, unknown> = {
      ...values,
      domains: values.domains.map((d) => d.trim().toLowerCase()),
    };
    if (values.dnsToken === '') delete payload.dnsToken;
    setSaving(true);
    try {
      if (await onSave(payload, issue)) onClose();
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      open={open}
      onCancel={onClose}
      width={680}
      title={editing ? t('pages.certs.edit') : t('pages.certs.add')}
      okText={editing ? t('save') : t('pages.certs.saveAndIssue')}
      confirmLoading={saving}
      onOk={methods.handleSubmit((v) => submit(v, !editing))}
      destroyOnHidden
    >
      <FormProvider {...methods}>
        <Form layout="vertical" component="div">
          <FormField
            label={t('pages.certs.domains')}
            name="domains"
            tooltip={t('pages.certs.domainsHint')}
            rules={{
              validate: (v: string[]) => {
                const shape = rhfZodValidate(CertDomainsSchema)(v);
                return shape === true
                  ? challengeMismatch(v, methods.getValues('challenge'))
                  : shape;
              },
            }}
          >
            <Select
              mode="tags"
              tokenSeparators={[',', ' ']}
              tagRender={selectTagRender}
              open={false}
              placeholder="example.com, *.example.com"
            />
          </FormField>
          <FormField label={t('pages.certs.name')} name="name" tooltip={t('pages.certs.nameHint')}>
            <Input placeholder={t('pages.certs.nameAuto')} />
          </FormField>
          <FormField label={t('pages.certs.challenge')} name="challenge">
            <Segmented
              options={[
                { value: 'dns-01', label: 'DNS-01' },
                { value: 'http-01', label: 'HTTP-01' },
                { value: 'tls-alpn-01', label: 'TLS-ALPN-01' },
              ]}
            />
          </FormField>
          <Alert
            type="info"
            showIcon
            style={{ marginBottom: 16 }}
            title={t(`pages.certs.challengeHint.${challenge}`)}
          />
          {challenge === 'dns-01' ? (
            <Row gutter={12}>
              <Col xs={24} md={10}>
                <FormField label={t('pages.certs.dnsProvider')} name="dnsProvider">
                  <Select
                    options={[
                      { value: 'cloudflare', label: 'Cloudflare' },
                      { value: 'arvancloud', label: t('pages.certs.arvan') },
                    ]}
                  />
                </FormField>
              </Col>
              <Col xs={24} md={14}>
                <FormField
                  label={t('pages.certs.dnsToken')}
                  name="dnsToken"
                  tooltip={t(`pages.certs.tokenHint.${provider}`)}
                  extra={editing?.hasDnsToken ? t('pages.backups.secretKept') : undefined}
                >
                  <Input.Password autoComplete="new-password" />
                </FormField>
              </Col>
            </Row>
          ) : (
            <FormField
              label={t('pages.certs.port')}
              name="port"
              tooltip={t('pages.certs.portHint')}
            >
              <InputNumber
                min={0}
                max={65535}
                placeholder={challenge === 'http-01' ? '80' : '443'}
                style={{ width: '100%' }}
              />
            </FormField>
          )}
          <Row gutter={12}>
            <Col xs={24} md={12}>
              <FormField
                label={t('pages.certs.email')}
                name="email"
                tooltip={t('pages.certs.emailHint')}
              >
                <Input type="email" />
              </FormField>
            </Col>
            <Col xs={12} md={6}>
              <FormField label={t('pages.certs.autoRenew')} name="autoRenew" valueProp="checked">
                <Switch />
              </FormField>
            </Col>
            <Col xs={12} md={6}>
              <FormField
                label={t('pages.certs.staging')}
                name="staging"
                valueProp="checked"
                tooltip={t('pages.certs.stagingHint')}
              >
                <Switch />
              </FormField>
            </Col>
          </Row>
          {nodes.length > 0 && (
            <FormField
              label={t('pages.certs.nodes')}
              name="nodeIds"
              tooltip={t('pages.certs.nodesHint')}
            >
              <Select
                mode="multiple"
                allowClear
                tagRender={selectTagRender}
                options={nodes.map((n) => ({ value: n.id, label: n.name }))}
              />
            </FormField>
          )}
        </Form>
      </FormProvider>
    </Modal>
  );
}
