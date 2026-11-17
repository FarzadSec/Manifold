import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Col,
  Flex,
  Form,
  Input,
  InputNumber,
  Modal,
  Row,
  Segmented,
  Switch,
  Typography,
} from 'antd';
import { FormProvider, useForm, useWatch } from 'react-hook-form';

import { FormField, rhfZodValidate } from '@/components/form/rhf';
import type { BackupTargetView } from '@/generated/types';
import { BackupTargetFormSchema, type BackupTargetFormValues } from '@/schemas/node';
import { HttpUtil } from '@/utils';

interface BackupTargetModalProps {
  open: boolean;
  editing: BackupTargetView | null;
  onSave: (payload: Record<string, unknown>) => Promise<boolean>;
  onClose: () => void;
}

const EMPTY: BackupTargetFormValues = {
  id: 0,
  name: '',
  enable: true,
  kind: 'local',
  everyHours: 24,
  keep: 7,
  dir: '',
  endpoint: '',
  region: '',
  bucket: '',
  prefix: '',
  accessKey: '',
  host: '',
  port: 22,
  user: '',
  hostKey: '',
  passphrase: '',
  secretKey: '',
  password: '',
  privateKey: '',
};

function toForm(row: BackupTargetView | null): BackupTargetFormValues {
  if (!row) return EMPTY;
  return {
    ...EMPTY,
    id: row.id,
    name: row.name,
    enable: row.enable,
    kind: (row.kind as BackupTargetFormValues['kind']) || 'local',
    everyHours: row.everyHours,
    keep: row.keep,
    dir: row.dir,
    endpoint: row.endpoint,
    region: row.region,
    bucket: row.bucket,
    prefix: row.prefix,
    accessKey: row.accessKey,
    host: row.host,
    port: row.port || 22,
    user: row.user,
    hostKey: row.hostKey,
  };
}

export default function BackupTargetModal({
  open,
  editing,
  onSave,
  onClose,
}: BackupTargetModalProps) {
  const { t } = useTranslation();
  const methods = useForm<BackupTargetFormValues>({ defaultValues: EMPTY });
  const kind = useWatch({ control: methods.control, name: 'kind' });
  const [saving, setSaving] = useState(false);
  const [readingKey, setReadingKey] = useState(false);
  const [synced, setSynced] = useState<{ open: boolean; editing: BackupTargetView | null } | null>(
    null,
  );
  if (!synced || synced.open !== open || synced.editing !== editing) {
    setSynced({ open, editing });
    if (open) methods.reset(toForm(editing));
  }

  const keepHint = (has: boolean | undefined) => (has ? t('pages.backups.secretKept') : undefined);

  async function readHostKey() {
    const { host, port } = methods.getValues();
    if (!host) return;
    setReadingKey(true);
    try {
      const msg = await HttpUtil.post<string>('/panel/api/backups/sshHostKey', { host, port });
      if (msg?.success && msg.obj) methods.setValue('hostKey', msg.obj, { shouldDirty: true });
    } finally {
      setReadingKey(false);
    }
  }

  async function submit(values: BackupTargetFormValues) {
    const payload: Record<string, unknown> = { ...values };
    for (const secret of ['passphrase', 'secretKey', 'password', 'privateKey'] as const) {
      if (values[secret] === '') delete payload[secret];
    }
    setSaving(true);
    try {
      if (await onSave(payload)) onClose();
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      open={open}
      onCancel={onClose}
      width={680}
      title={editing ? t('pages.backups.edit') : t('pages.backups.add')}
      okText={t('save')}
      confirmLoading={saving}
      onOk={methods.handleSubmit(submit)}
      destroyOnHidden
    >
      <FormProvider {...methods}>
        <Form layout="vertical" component="div">
          <Row gutter={12}>
            <Col xs={24} md={14}>
              <FormField
                label={t('pages.backups.name')}
                name="name"
                rules={{ validate: rhfZodValidate(BackupTargetFormSchema.shape.name) }}
              >
                <Input placeholder="offsite" />
              </FormField>
            </Col>
            <Col xs={24} md={10}>
              <FormField label={t('enable')} name="enable" valueProp="checked">
                <Switch />
              </FormField>
            </Col>
          </Row>
          <FormField label={t('pages.backups.kind')} name="kind">
            <Segmented
              options={[
                { value: 'local', label: t('pages.backups.kindLocal') },
                { value: 's3', label: 'S3' },
                { value: 'ssh', label: 'SSH / SFTP' },
              ]}
            />
          </FormField>
          <Row gutter={12}>
            <Col xs={12}>
              <FormField
                label={t('pages.backups.everyHours')}
                name="everyHours"
                tooltip={t('pages.backups.everyHoursHint')}
              >
                <InputNumber min={0} max={720} addonAfter="h" style={{ width: '100%' }} />
              </FormField>
            </Col>
            <Col xs={12}>
              <FormField
                label={t('pages.backups.keep')}
                name="keep"
                tooltip={t('pages.backups.keepHint')}
              >
                <InputNumber min={0} max={1000} style={{ width: '100%' }} />
              </FormField>
            </Col>
          </Row>

          {kind === 'local' && (
            <FormField
              label={t('pages.backups.dir')}
              name="dir"
              tooltip={t('pages.backups.dirHint')}
            >
              <Input placeholder="/etc/manifold/backups" />
            </FormField>
          )}

          {kind === 's3' && (
            <>
              <Row gutter={12}>
                <Col xs={24} md={16}>
                  <FormField
                    label={t('pages.backups.endpoint')}
                    name="endpoint"
                    tooltip={t('pages.backups.endpointHint')}
                  >
                    <Input placeholder="https://s3.ir-thr-at1.arvanstorage.ir" />
                  </FormField>
                </Col>
                <Col xs={24} md={8}>
                  <FormField label={t('pages.backups.region')} name="region">
                    <Input placeholder="us-east-1" />
                  </FormField>
                </Col>
              </Row>
              <Row gutter={12}>
                <Col xs={12}>
                  <FormField label={t('pages.backups.bucket')} name="bucket">
                    <Input />
                  </FormField>
                </Col>
                <Col xs={12}>
                  <FormField label={t('pages.backups.prefix')} name="prefix">
                    <Input placeholder="fra-1" />
                  </FormField>
                </Col>
              </Row>
              <Row gutter={12}>
                <Col xs={24} md={12}>
                  <FormField label={t('pages.backups.accessKey')} name="accessKey">
                    <Input autoComplete="off" />
                  </FormField>
                </Col>
                <Col xs={24} md={12}>
                  <FormField
                    label={t('pages.backups.secretKey')}
                    name="secretKey"
                    extra={keepHint(editing?.hasSecretKey)}
                  >
                    <Input.Password autoComplete="off" />
                  </FormField>
                </Col>
              </Row>
            </>
          )}

          {kind === 'ssh' && (
            <>
              <Row gutter={12}>
                <Col xs={24} md={12}>
                  <FormField label={t('pages.nodes.install.sshHost')} name="host">
                    <Input placeholder="backup.example.com" />
                  </FormField>
                </Col>
                <Col xs={12} md={5}>
                  <FormField label={t('pages.nodes.install.sshPort')} name="port">
                    <InputNumber min={1} max={65535} style={{ width: '100%' }} />
                  </FormField>
                </Col>
                <Col xs={12} md={7}>
                  <FormField label={t('pages.nodes.install.sshUser')} name="user">
                    <Input />
                  </FormField>
                </Col>
              </Row>
              <Row gutter={12}>
                <Col xs={24} md={12}>
                  <FormField
                    label={t('pages.nodes.install.password')}
                    name="password"
                    extra={keepHint(editing?.hasPassword)}
                  >
                    <Input.Password autoComplete="off" />
                  </FormField>
                </Col>
                <Col xs={24} md={12}>
                  <FormField label={t('pages.backups.dir')} name="dir">
                    <Input placeholder="manifold-backups" />
                  </FormField>
                </Col>
              </Row>
              <FormField
                label={t('pages.nodes.install.privateKey')}
                name="privateKey"
                extra={keepHint(editing?.hasPrivateKey)}
              >
                <Input.TextArea rows={3} placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" />
              </FormField>
              <Flex gap={8} align="end">
                <FormField label={t('pages.backups.hostKey')} name="hostKey" style={{ flex: 1 }}>
                  <Input placeholder="SHA256:…" />
                </FormField>
                <Button onClick={readHostKey} loading={readingKey} style={{ marginBottom: 24 }}>
                  {t('pages.nodes.install.readHostKey')}
                </Button>
              </Flex>
              <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>
                {t('pages.nodes.install.compareHostKey')}
              </Typography.Paragraph>
            </>
          )}

          <FormField
            label={t('pages.backups.passphrase')}
            name="passphrase"
            tooltip={t('pages.backups.passphraseHint')}
            extra={keepHint(editing?.hasPassphrase)}
          >
            <Input.Password autoComplete="new-password" />
          </FormField>
          {!editing?.hasPassphrase && (
            <Alert type="info" showIcon title={t('pages.backups.passphraseAdvice')} />
          )}
        </Form>
      </FormProvider>
    </Modal>
  );
}
