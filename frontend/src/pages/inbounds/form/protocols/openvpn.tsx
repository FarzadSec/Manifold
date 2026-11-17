import { useId, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Collapse,
  Descriptions,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Switch,
  Typography,
} from 'antd';
import { DownloadOutlined, KeyOutlined, ImportOutlined } from '@ant-design/icons';
import { useFormContext, useWatch } from 'react-hook-form';

import { selectTagRender } from '@/components/form';
import { NativeSelect } from '@/components/form/NativeSelect';
import { FormField, rhfZodValidate } from '@/components/form/rhf';
import {
  OPENVPN_DATA_CIPHERS,
  OPENVPN_KEY_TYPES,
  OPENVPN_SERVER_NAME,
  OpenvpnServerSchema,
} from '@/schemas/protocols/inbound/openvpn';
import type { OpenVPNCertInfo, OpenVPNKeySet } from '@/generated/types';
import { FileManager, HttpUtil } from '@/utils';

const KEY_FIELDS = ['caCert', 'caKey', 'serverCert', 'serverKey', 'tlsCryptKey'] as const;

function useCertInfo(pem: string | undefined): OpenVPNCertInfo | null {
  const { data } = useQuery({
    queryKey: ['openvpnCertInfo', pem],
    enabled: !!pem,
    staleTime: Infinity,
    queryFn: async () => {
      const msg = await HttpUtil.post<OpenVPNCertInfo>(
        '/panel/api/server/openvpnCertInfo',
        { cert: pem },
        { silent: true },
      );
      return msg.success && msg.obj ? msg.obj : null;
    },
  });
  return pem ? (data ?? null) : null;
}

function CertDescription({ title, info }: { title: string; info: OpenVPNCertInfo | null }) {
  const { t } = useTranslation();
  if (!info) return null;
  return (
    <Descriptions
      size="small"
      column={1}
      title={title}
      style={{ marginBottom: 12 }}
      items={[
        {
          key: 'subject',
          label: t('pages.inbounds.openvpn.certSubject'),
          children: info.subject,
        },
        {
          key: 'key',
          label: t('pages.inbounds.openvpn.certKeyType'),
          children: info.keyType,
        },
        {
          key: 'until',
          label: t('pages.inbounds.openvpn.certValidUntil'),
          children: new Date(info.notAfter).toLocaleDateString(),
        },
        {
          key: 'sha',
          label: t('pages.inbounds.openvpn.certFingerprint'),
          children: (
            <Typography.Text code style={{ wordBreak: 'break-all', fontSize: 11 }}>
              {info.sha256}
            </Typography.Text>
          ),
        },
      ]}
    />
  );
}

interface IssueForm {
  keyType: (typeof OPENVPN_KEY_TYPES)[number];
  days: number;
  caName: string;
  serverName: string;
}

export default function OpenvpnFields() {
  const { t } = useTranslation();
  const uid = useId();
  const { control, setValue } = useFormContext();
  const caCert = useWatch({ control, name: 'settings.server.caCert' }) as string | undefined;
  const serverCert = useWatch({
    control,
    name: 'settings.server.serverCert',
  }) as string | undefined;
  const hasKeys = !!caCert;
  const caInfo = useCertInfo(caCert);
  const serverInfo = useCertInfo(serverCert);

  const [issueOpen, setIssueOpen] = useState(false);
  const [issue, setIssue] = useState<IssueForm>({
    keyType: 'ecdsa-p256',
    days: 3650,
    caName: '',
    serverName: '',
  });
  const [issuing, setIssuing] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const [own, setOwn] = useState<Record<(typeof KEY_FIELDS)[number], string>>({
    caCert: '',
    caKey: '',
    serverCert: '',
    serverKey: '',
    tlsCryptKey: '',
  });
  const [replaced, setReplaced] = useState(false);

  function applyKeys(keys: Partial<Record<(typeof KEY_FIELDS)[number], string>>) {
    for (const field of KEY_FIELDS) {
      const value = keys[field];
      if (value === undefined || (field === 'tlsCryptKey' && value === '')) continue;
      setValue(`settings.server.${field}`, value, { shouldDirty: true });
    }
    setReplaced(true);
  }

  async function issueKeys() {
    setIssuing(true);
    try {
      const msg = await HttpUtil.post<OpenVPNKeySet>('/panel/api/server/openvpnKeys', issue);
      if (msg.success && msg.obj) {
        applyKeys(msg.obj);
        setIssueOpen(false);
      }
    } finally {
      setIssuing(false);
    }
  }

  const serverNameValid = issue.serverName === '' || OPENVPN_SERVER_NAME.test(issue.serverName);

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

      <FormField
        name={['settings', 'server', 'mtu']}
        label={t('pages.inbounds.openvpn.mtu')}
        tooltip={t('pages.inbounds.openvpn.mtuHint')}
        rules={{ validate: rhfZodValidate(OpenvpnServerSchema.shape.mtu) }}
      >
        <InputNumber min={0} max={9000} placeholder="1500" style={{ width: '100%' }} />
      </FormField>

      <FormField
        name={['settings', 'server', 'mssfix']}
        label={t('pages.inbounds.openvpn.mssfix')}
        tooltip={t('pages.inbounds.openvpn.mssfixHint')}
        rules={{ validate: rhfZodValidate(OpenvpnServerSchema.shape.mssfix) }}
      >
        <InputNumber min={0} max={9000} placeholder="1492" style={{ width: '100%' }} />
      </FormField>

      <FormField
        name={['settings', 'server', 'dataCiphers']}
        label={t('pages.inbounds.openvpn.dataCiphers')}
        tooltip={t('pages.inbounds.openvpn.dataCiphersHint')}
      >
        <Select
          mode="multiple"
          allowClear
          tagRender={selectTagRender}
          placeholder={OPENVPN_DATA_CIPHERS.join(', ')}
          options={OPENVPN_DATA_CIPHERS.map((c) => ({ label: c, value: c }))}
        />
      </FormField>

      <FormField
        name={['settings', 'server', 'splitTunnel']}
        label={t('pages.inbounds.openvpn.splitTunnel')}
        tooltip={t('pages.inbounds.openvpn.splitTunnelHint')}
        valueProp="checked"
      >
        <Switch />
      </FormField>

      <FormField
        name={['settings', 'server', 'tlsMode']}
        label={t('pages.inbounds.openvpn.tlsMode')}
        tooltip={t('pages.inbounds.openvpn.tlsModeHint')}
      >
        <NativeSelect
          options={[
            { label: 'tls-crypt', value: 'tls-crypt' },
            { label: 'tls-auth', value: 'tls-auth' },
            { label: t('pages.inbounds.openvpn.tlsModeNone'), value: 'none' },
          ]}
        />
      </FormField>

      <FormField
        name={['settings', 'server', 'tlsVersionMin']}
        label={t('pages.inbounds.openvpn.tlsVersionMin')}
        tooltip={t('pages.inbounds.openvpn.tlsVersionMinHint')}
      >
        <NativeSelect
          options={[
            { label: 'TLS 1.2', value: '' },
            { label: 'TLS 1.3', value: '1.3' },
          ]}
        />
      </FormField>

      <FormField
        name={['settings', 'server', 'renegSec']}
        label={t('pages.inbounds.openvpn.reneg')}
        tooltip={t('pages.inbounds.openvpn.renegHint')}
        rules={{ validate: rhfZodValidate(OpenvpnServerSchema.shape.renegSec) }}
      >
        <InputNumber
          min={-1}
          max={604800}
          placeholder="3600"
          addonAfter="s"
          style={{ width: '100%' }}
        />
      </FormField>

      <FormField
        name={['settings', 'server', 'maxClients']}
        label={t('pages.inbounds.openvpn.maxClients')}
        tooltip={t('pages.inbounds.openvpn.maxClientsHint')}
      >
        <InputNumber min={0} placeholder="0" style={{ width: '100%' }} />
      </FormField>

      <FormField
        name={['settings', 'server', 'searchDomains']}
        label={t('pages.inbounds.openvpn.searchDomains')}
        tooltip={t('pages.inbounds.openvpn.searchDomainsHint')}
        rules={{ validate: rhfZodValidate(OpenvpnServerSchema.shape.searchDomains) }}
      >
        <Select mode="tags" tagRender={selectTagRender} open={false} placeholder="corp.example" />
      </FormField>

      {hasKeys && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 16 }}
          title={t('pages.inbounds.openvpn.profileChangeWarning')}
        />
      )}

      <Collapse
        ghost
        items={[
          {
            key: 'certificates',
            label: t('pages.inbounds.openvpn.certificates'),
            forceRender: true,
            children: (
              <>
                <CertDescription title={t('pages.inbounds.openvpn.caCertificate')} info={caInfo} />
                <CertDescription
                  title={t('pages.inbounds.openvpn.serverCertificate')}
                  info={serverInfo}
                />
                <Space wrap style={{ marginBottom: 12 }}>
                  <Button
                    icon={<DownloadOutlined aria-hidden />}
                    disabled={!caCert}
                    onClick={() => caCert && FileManager.downloadTextFile(caCert, 'openvpn-ca.crt')}
                  >
                    {t('pages.inbounds.openvpn.downloadCa')}
                  </Button>
                  <Button icon={<KeyOutlined aria-hidden />} onClick={() => setIssueOpen(true)}>
                    {t('pages.inbounds.openvpn.issueKeys')}
                  </Button>
                  <Button icon={<ImportOutlined aria-hidden />} onClick={() => setImportOpen(true)}>
                    {t('pages.inbounds.openvpn.importKeys')}
                  </Button>
                </Space>
                {replaced && (
                  <Alert type="info" showIcon title={t('pages.inbounds.openvpn.keysReplaced')} />
                )}
              </>
            ),
          },
          {
            key: 'profile',
            label: t('pages.inbounds.openvpn.clientProfile'),
            forceRender: true,
            children: (
              <FormField
                name={['settings', 'server', 'clientDirectives']}
                label={t('pages.inbounds.openvpn.clientDirectives')}
                tooltip={t('pages.inbounds.openvpn.clientDirectivesHint')}
              >
                <Input.TextArea
                  autoSize={{ minRows: 2, maxRows: 10 }}
                  spellCheck={false}
                  placeholder={'sndbuf 0\nrcvbuf 0'}
                />
              </FormField>
            ),
          },
        ]}
      />

      <Alert
        type="info"
        showIcon
        title={
          hasKeys ? t('pages.inbounds.openvpn.keysKept') : t('pages.inbounds.openvpn.keysIssued')
        }
      />

      <Modal
        open={issueOpen}
        title={t('pages.inbounds.openvpn.issueKeys')}
        okText={t('pages.inbounds.openvpn.issueKeys')}
        confirmLoading={issuing}
        okButtonProps={{ disabled: !serverNameValid }}
        onOk={issueKeys}
        onCancel={() => setIssueOpen(false)}
        destroyOnHidden
      >
        {hasKeys && (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 16 }}
            title={t('pages.inbounds.openvpn.issueKeysWarning')}
          />
        )}
        <Form layout="vertical">
          <Form.Item
            htmlFor={`${uid}-keyType`}
            label={t('pages.inbounds.openvpn.keyType')}
            tooltip={t('pages.inbounds.openvpn.keyTypeHint')}
          >
            <Select
              id={`${uid}-keyType`}
              value={issue.keyType}
              onChange={(keyType) => setIssue((v) => ({ ...v, keyType }))}
              options={OPENVPN_KEY_TYPES.map((k) => ({ label: k, value: k }))}
            />
          </Form.Item>
          <Form.Item htmlFor={`${uid}-days`} label={t('pages.inbounds.openvpn.certDays')}>
            <InputNumber
              id={`${uid}-days`}
              min={1}
              max={36500}
              value={issue.days}
              onChange={(days) => setIssue((v) => ({ ...v, days: Number(days) || 3650 }))}
              style={{ width: '100%' }}
            />
          </Form.Item>
          <Form.Item htmlFor={`${uid}-caName`} label={t('pages.inbounds.openvpn.caName')}>
            <Input
              id={`${uid}-caName`}
              value={issue.caName}
              maxLength={64}
              placeholder="Manifold OpenVPN CA"
              onChange={(e) => setIssue((v) => ({ ...v, caName: e.target.value }))}
            />
          </Form.Item>
          <Form.Item
            htmlFor={`${uid}-serverName`}
            label={t('pages.inbounds.openvpn.serverName')}
            tooltip={t('pages.inbounds.openvpn.serverNameHint')}
            validateStatus={serverNameValid ? undefined : 'error'}
          >
            <Input
              id={`${uid}-serverName`}
              value={issue.serverName}
              maxLength={64}
              placeholder="manifold-openvpn-server"
              onChange={(e) => setIssue((v) => ({ ...v, serverName: e.target.value.trim() }))}
            />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        open={importOpen}
        title={t('pages.inbounds.openvpn.importKeys')}
        okButtonProps={{
          disabled: !own.caCert || !own.serverCert || !own.serverKey,
        }}
        onOk={() => {
          applyKeys(own);
          setImportOpen(false);
        }}
        onCancel={() => setImportOpen(false)}
        destroyOnHidden
      >
        <Typography.Paragraph type="secondary">
          {t('pages.inbounds.openvpn.importKeysHint')}
        </Typography.Paragraph>
        {hasKeys && (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 16 }}
            title={t('pages.inbounds.openvpn.issueKeysWarning')}
          />
        )}
        <Form layout="vertical">
          {(
            [
              ['caCert', 'pages.inbounds.openvpn.caCertificate'],
              ['caKey', 'pages.inbounds.openvpn.caKey'],
              ['serverCert', 'pages.inbounds.openvpn.serverCertificate'],
              ['serverKey', 'pages.inbounds.openvpn.serverKey'],
              ['tlsCryptKey', 'pages.inbounds.openvpn.tlsKey'],
            ] as const
          ).map(([field, label]) => (
            <Form.Item key={field} htmlFor={`${uid}-${field}`} label={t(label)}>
              <Input.TextArea
                id={`${uid}-${field}`}
                autoSize={{ minRows: 2, maxRows: 6 }}
                spellCheck={false}
                value={own[field]}
                onChange={(e) => setOwn((v) => ({ ...v, [field]: e.target.value }))}
              />
            </Form.Item>
          ))}
        </Form>
      </Modal>
    </>
  );
}
