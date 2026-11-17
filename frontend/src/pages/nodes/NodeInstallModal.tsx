import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  Button,
  Checkbox,
  Col,
  Flex,
  Form,
  Input,
  InputNumber,
  Modal,
  Radio,
  Row,
  Typography,
} from 'antd';
import { FormProvider, useForm, useWatch } from 'react-hook-form';

import { keys } from '@/api/queryKeys';
import type { NodeRecord } from '@/api/queries/useNodesQuery';
import { FormField, rhfZodValidate } from '@/components/form/rhf';
import type { NodeInstallJob } from '@/generated/types';
import { NodeInstallFormSchema, type NodeInstallFormValues } from '@/schemas/node';
import { HttpUtil } from '@/utils';

interface NodeInstallModalProps {
  open: boolean;
  // An existing node to update over SSH; none installs a new one.
  node: NodeRecord | null;
  onClose: () => void;
}

const POLL_MS = 1500;

function randomPort(): number {
  return 20000 + Math.floor(Math.random() * 40000);
}

export default function NodeInstallModal({ open, node, onClose }: NodeInstallModalProps) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const updating = !!node;
  const methods = useForm<NodeInstallFormValues>({
    defaultValues: {
      name: '',
      address: '',
      panelPort: randomPort(),
      sshHost: '',
      sshPort: 22,
      sshUser: 'root',
      auth: 'password',
      password: '',
      privateKey: '',
    },
  });
  const auth = useWatch({ control: methods.control, name: 'auth' });
  const [hostKey, setHostKey] = useState('');
  const [accepted, setAccepted] = useState(false);
  const [reading, setReading] = useState(false);
  const [jobId, setJobId] = useState('');
  const [job, setJob] = useState<NodeInstallJob | null>(null);
  const logRef = useRef<HTMLPreElement>(null);

  const [synced, setSynced] = useState<{ open: boolean; node: NodeRecord | null } | null>(null);
  if (!synced || synced.open !== open || synced.node !== node) {
    setSynced({ open, node });
    if (open) {
      methods.reset({
        name: node?.name ?? '',
        address: node?.address ?? '',
        panelPort: node?.port ?? randomPort(),
        sshHost: '',
        sshPort: 22,
        sshUser: 'root',
        auth: 'password',
        password: '',
        privateKey: '',
      });
      setHostKey('');
      setAccepted(false);
      setJobId('');
      setJob(null);
    }
  }

  useEffect(() => {
    if (!jobId || job?.state === 'done' || job?.state === 'failed') return;
    const timer = setInterval(async () => {
      const msg = await HttpUtil.get<NodeInstallJob>(
        `/panel/api/nodes/ssh/job/${jobId}`,
        undefined,
        {
          silent: true,
        },
      );
      if (msg?.success && msg.obj) {
        setJob(msg.obj);
        if (msg.obj.state !== 'running')
          void queryClient.invalidateQueries({ queryKey: keys.nodes.root() });
      }
    }, POLL_MS);
    return () => clearInterval(timer);
  }, [jobId, job?.state, queryClient]);

  useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [job?.log.length]);

  async function readHostKey() {
    const values = methods.getValues();
    const host = values.sshHost.trim() || values.address.trim();
    if (!host) {
      await methods.trigger('address');
      return;
    }
    setReading(true);
    setAccepted(false);
    try {
      const msg = await HttpUtil.post<string>('/panel/api/nodes/ssh/hostKey', {
        host,
        port: values.sshPort,
      });
      setHostKey(msg?.success && msg.obj ? msg.obj : '');
    } finally {
      setReading(false);
    }
  }

  async function start(values: NodeInstallFormValues) {
    const msg = await HttpUtil.post<string>(
      '/panel/api/nodes/ssh/install',
      {
        nodeId: node?.id ?? 0,
        name: values.name.trim(),
        address: values.address.trim(),
        panelPort: values.panelPort,
        sshHost: values.sshHost.trim(),
        sshPort: values.sshPort,
        sshUser: values.sshUser.trim(),
        password: values.auth === 'password' ? values.password : '',
        privateKey: values.auth === 'key' ? values.privateKey : '',
        hostKey,
        update: updating,
      },
      { silentSuccess: true },
    );
    if (msg?.success && msg.obj) {
      setJobId(msg.obj);
      setJob({ id: msg.obj, state: 'running', log: [], nodeId: 0 });
    }
  }

  const running = job?.state === 'running';
  return (
    <Modal
      open={open}
      onCancel={onClose}
      width={720}
      title={
        updating
          ? t('pages.nodes.install.updateTitle', { name: node?.name ?? '' })
          : t('pages.nodes.install.title')
      }
      okText={updating ? t('pages.nodes.install.update') : t('pages.nodes.install.install')}
      okButtonProps={{ disabled: !accepted || !!jobId, loading: running }}
      onOk={methods.handleSubmit(start)}
      cancelText={job && !running ? t('close') : t('cancel')}
      destroyOnHidden
    >
      <FormProvider {...methods}>
        <Form layout="vertical" component="div" disabled={!!jobId}>
          {!updating && (
            <Row gutter={12}>
              <Col xs={24} md={8}>
                <FormField
                  label={t('pages.nodes.name')}
                  name="name"
                  rules={{ validate: rhfZodValidate(NodeInstallFormSchema.shape.name) }}
                >
                  <Input />
                </FormField>
              </Col>
              <Col xs={24} md={10}>
                <FormField
                  label={t('pages.nodes.address')}
                  name="address"
                  rules={{ validate: rhfZodValidate(NodeInstallFormSchema.shape.address) }}
                >
                  <Input placeholder="203.0.113.10" />
                </FormField>
              </Col>
              <Col xs={24} md={6}>
                <FormField
                  label={t('pages.nodes.port')}
                  name="panelPort"
                  tooltip={t('pages.nodes.install.panelPortHint')}
                  rules={{ validate: rhfZodValidate(NodeInstallFormSchema.shape.panelPort) }}
                >
                  <InputNumber min={1} max={65535} style={{ width: '100%' }} />
                </FormField>
              </Col>
            </Row>
          )}
          <Row gutter={12}>
            <Col xs={24} md={10}>
              <FormField label={t('pages.nodes.install.sshHost')} name="sshHost">
                <Input placeholder={t('pages.nodes.install.sshHostPlaceholder')} />
              </FormField>
            </Col>
            <Col xs={12} md={6}>
              <FormField label={t('pages.nodes.install.sshPort')} name="sshPort">
                <InputNumber min={1} max={65535} style={{ width: '100%' }} />
              </FormField>
            </Col>
            <Col xs={12} md={8}>
              <FormField
                label={t('pages.nodes.install.sshUser')}
                name="sshUser"
                tooltip={t('pages.nodes.install.sshUserHint')}
              >
                <Input />
              </FormField>
            </Col>
          </Row>
          <FormField label={t('pages.nodes.install.auth')} name="auth">
            <Radio.Group
              options={[
                { value: 'password', label: t('pages.nodes.install.password') },
                { value: 'key', label: t('pages.nodes.install.privateKey') },
              ]}
            />
          </FormField>
          {auth === 'key' ? (
            <FormField label={t('pages.nodes.install.privateKey')} name="privateKey">
              <Input.TextArea rows={4} placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" />
            </FormField>
          ) : (
            <FormField label={t('pages.nodes.install.password')} name="password">
              <Input.Password autoComplete="off" />
            </FormField>
          )}
          <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>
            {t('pages.nodes.install.notStored')}
          </Typography.Paragraph>
          <Flex gap={8} align="center" wrap>
            <Button onClick={readHostKey} loading={reading} disabled={!!jobId}>
              {t('pages.nodes.install.readHostKey')}
            </Button>
            {hostKey && (
              <Typography.Text code copyable>
                {hostKey}
              </Typography.Text>
            )}
          </Flex>
          {hostKey && (
            <Alert
              style={{ marginTop: 8 }}
              type="info"
              showIcon
              title={t('pages.nodes.install.compareHostKey')}
              description={
                <Checkbox
                  checked={accepted}
                  onChange={(e) => setAccepted(e.target.checked)}
                  disabled={!!jobId}
                >
                  {t('pages.nodes.install.acceptHostKey')}
                </Checkbox>
              }
            />
          )}
        </Form>
      </FormProvider>
      {job && (
        <>
          {job.state === 'done' && (
            <Alert
              style={{ marginTop: 12 }}
              type="success"
              showIcon
              title={t('pages.nodes.install.done')}
            />
          )}
          {job.state === 'failed' && (
            <Alert
              style={{ marginTop: 12 }}
              type="error"
              showIcon
              title={job.error || t('pages.nodes.install.failed')}
            />
          )}
          <pre
            ref={logRef}
            aria-live="polite"
            style={{
              marginTop: 12,
              maxHeight: 280,
              overflow: 'auto',
              fontSize: 12,
              padding: 8,
              borderRadius: 6,
              background: 'var(--ant-color-fill-tertiary)',
              direction: 'ltr',
            }}
          >
            {job.log.join('\n') || '…'}
          </pre>
        </>
      )}
    </Modal>
  );
}
