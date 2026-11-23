import { useCallback, useEffect, useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Col,
  Divider,
  Form,
  Input,
  InputNumber,
  Modal,
  Row,
  Select,
  Space,
  Switch,
  Typography,
} from 'antd';
import { DeleteOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons';
import { FormProvider, useFieldArray, useForm, useFormContext, useWatch } from 'react-hook-form';

import { FormField } from '@/components/form/rhf';
import type { TunnelRelay, TunnelRequest, TunnelSuggestion, TunnelView } from '@/generated/types';
import { HttpUtil } from '@/utils';
import { isTunnelEdition } from '@/utils/edition';
import { KERNEL_KINDS, L3_CARRIERS, REVERSE_TRANSPORTS, isKernel, kindLabel } from './tunnelInfo';
import { L2TP_FORWARDS } from './groupMembers';

interface TunnelFormModalProps {
  open: boolean;
  tunnel: TunnelView | null;
  // draft fills a new tunnel, as the tunnel test's recommendation does.
  draft?: TunnelRequest | null;
  relays: TunnelRelay[];
  onClose: (saved: boolean) => void;
}

function emptyRequest(): TunnelRequest {
  return {
    name: '',
    kind: 'gre',
    role: isTunnelEdition() ? 'server' : 'client',
    enable: true,
    relayId: 0,
    settings: {
      forwards: [],
      reverseForwards: [],
      nat: 'masquerade',
      transport: 'tcpmux',
      carrier: 'udp',
      dialer: 'server',
    },
  };
}

function fromView(t: TunnelView): TunnelRequest {
  return {
    name: t.name,
    kind: t.kind,
    role: t.role,
    enable: t.enable,
    relayId: t.relayId,
    settings: {
      ...t.settings,
      forwards: t.settings.forwards ?? [],
      reverseForwards: t.settings.reverseForwards ?? [],
    },
  };
}

type ForwardList = 'settings.forwards' | 'settings.reverseForwards';

interface ForwardRowsProps {
  name: ForwardList;
  kernel: boolean;
  listenLabel: string;
  targetLabel: string;
  listenPlaceholder: string;
  targetPlaceholder: string;
  addLabel: string;
  // l2tpPreset offers the ports L2TP/IPsec needs through a kernel link.
  l2tpPreset?: boolean;
}

// ForwardRows edits one direction's forwards: the relay's to this panel, or the client end's to the relay.
function ForwardRows({
  name,
  kernel,
  listenLabel,
  targetLabel,
  listenPlaceholder,
  targetPlaceholder,
  addLabel,
  l2tpPreset = false,
}: ForwardRowsProps) {
  const { t } = useTranslation();
  const { control } = useFormContext<TunnelRequest>();
  const rows = useFieldArray({ control, name });
  const values = useWatch({ control, name }) ?? [];
  return (
    <>
      {rows.fields.map((field, i) => (
        <Row gutter={8} key={field.id} align="middle">
          <Col span={8}>
            <FormField
              name={`${name}.${i}.listen`}
              label={i === 0 ? listenLabel : undefined}
              rules={{ required: values[i]?.proto !== 'esp' }}
            >
              <Input placeholder={listenPlaceholder} disabled={values[i]?.proto === 'esp'} />
            </FormField>
          </Col>
          <Col span={8}>
            <FormField name={`${name}.${i}.target`} label={i === 0 ? targetLabel : undefined}>
              <Input placeholder={targetPlaceholder} disabled={values[i]?.proto === 'esp'} />
            </FormField>
          </Col>
          <Col span={6}>
            <FormField
              name={`${name}.${i}.proto`}
              label={i === 0 ? t('pages.tunnels.forwardProto') : undefined}
            >
              <Select
                options={[
                  { label: 'TCP', value: 'tcp' },
                  ...(kernel ? [{ label: 'UDP', value: 'udp' }] : []),
                  { label: 'TCP + UDP', value: 'both' },
                  ...(kernel ? [{ label: 'ESP (IPsec)', value: 'esp' }] : []),
                ]}
              />
            </FormField>
          </Col>
          <Col span={2}>
            <Button
              type="text"
              danger
              aria-label={t('remove')}
              icon={<DeleteOutlined aria-hidden />}
              onClick={() => rows.remove(i)}
            />
          </Col>
        </Row>
      ))}
      <Button
        icon={<PlusOutlined aria-hidden />}
        onClick={() => rows.append({ listen: '', target: '', proto: 'tcp' })}
      >
        {addLabel}
      </Button>
      {l2tpPreset && (
        <>
          <Button
            style={{ marginInlineStart: 8 }}
            onClick={() => {
              for (const p of L2TP_FORWARDS) {
                if (!values.some((f) => f.proto === p.proto && f.listen === p.listen)) {
                  rows.append(p);
                }
              }
            }}
          >
            {t('pages.tunnels.l2tpPreset')}
          </Button>
          <Typography.Paragraph type="secondary" style={{ marginTop: 8 }}>
            {t('pages.tunnels.l2tpPresetHint')}
          </Typography.Paragraph>
        </>
      )}
    </>
  );
}

export default function TunnelFormModal({
  open,
  tunnel,
  draft,
  relays,
  onClose,
}: TunnelFormModalProps) {
  const { t } = useTranslation();
  const uid = useId();
  const methods = useForm<TunnelRequest>({ defaultValues: emptyRequest() });
  const { control, setValue, reset, handleSubmit } = methods;
  const kind = useWatch({ control, name: 'kind' });
  const role = useWatch({ control, name: 'role' });
  const dialer = useWatch({ control, name: 'settings.dialer' });
  const [saving, setSaving] = useState(false);
  const kernel = isKernel(kind);

  const suggest = useCallback(
    async (fillToken: boolean) => {
      const msg = await HttpUtil.get<TunnelSuggestion>('/panel/api/tunnels/suggest', undefined, {
        silent: true,
      });
      if (!msg.success || !msg.obj) return;
      const s = msg.obj;
      setValue('settings.localIp', s.localIp, { shouldDirty: true });
      setValue('settings.peerIp', s.peerIp, { shouldDirty: true });
      setValue('settings.port', s.port, { shouldDirty: true });
      if (fillToken) setValue('settings.token', s.token, { shouldDirty: true });
    },
    [setValue],
  );

  useEffect(() => {
    if (!open) return;
    const blank = emptyRequest();
    reset(
      tunnel
        ? fromView(tunnel)
        : draft
          ? { ...blank, ...draft, settings: { ...blank.settings, ...draft.settings } }
          : blank,
    );
    if (!tunnel) void suggest(true);
  }, [open, tunnel, draft, reset, suggest]);

  async function onSubmit(values: TunnelRequest) {
    setSaving(true);
    try {
      const url = tunnel ? `/panel/api/tunnels/update/${tunnel.id}` : '/panel/api/tunnels/add';
      const msg = await HttpUtil.post(url, values, {
        headers: { 'Content-Type': 'application/json' },
      });
      if (msg.success) onClose(true);
    } finally {
      setSaving(false);
    }
  }

  const needsRemote =
    kernel || (kind === 'reverse' && role === 'client') || (kind === 'l3' && dialer === role);
  const showForwards = role === 'server' || !isTunnelEdition();

  return (
    <Modal
      open={open}
      width={760}
      title={tunnel ? t('pages.tunnels.edit') : t('pages.tunnels.add')}
      okText={t('save')}
      confirmLoading={saving}
      onOk={handleSubmit(onSubmit)}
      onCancel={() => onClose(false)}
      destroyOnHidden
    >
      <FormProvider {...methods}>
        <Form layout="vertical">
          <Row gutter={12}>
            <Col xs={24} md={12}>
              <FormField name="name" label={t('pages.tunnels.name')} rules={{ required: true }}>
                <Input maxLength={64} placeholder="uk-relay" />
              </FormField>
            </Col>
            <Col xs={24} md={12}>
              <FormField
                name="kind"
                label={t('pages.tunnels.kind')}
                tooltip={t('pages.tunnels.kindHint')}
              >
                <Select
                  disabled={!!tunnel}
                  options={[
                    {
                      label: t('pages.tunnels.kernelGroup'),
                      options: KERNEL_KINDS.map((k) => ({
                        label: kindLabel(k),
                        value: k,
                      })),
                    },
                    {
                      label: 'BackPack',
                      options: ['reverse', 'l3'].map((k) => ({
                        label: kindLabel(k),
                        value: k,
                      })),
                    },
                  ]}
                />
              </FormField>
            </Col>
            <Col xs={24} md={12}>
              <FormField
                name="role"
                label={t('pages.tunnels.role')}
                tooltip={t('pages.tunnels.roleHint')}
              >
                <Select
                  disabled={!!tunnel}
                  options={[
                    { label: t('pages.tunnels.roleClient'), value: 'client' },
                    { label: t('pages.tunnels.roleServer'), value: 'server' },
                  ]}
                />
              </FormField>
            </Col>
            <Col xs={24} md={12}>
              <FormField name="enable" label={t('enable')} valueProp="checked">
                <Switch />
              </FormField>
            </Col>
          </Row>

          {!isTunnelEdition() && role === 'client' && (
            <FormField
              name="relayId"
              label={t('pages.tunnels.relay')}
              tooltip={t('pages.tunnels.relayHint')}
            >
              <Select
                options={[
                  { label: t('pages.tunnels.relayNone'), value: 0 },
                  ...relays.map((r) => ({ label: r.name, value: r.id })),
                ]}
              />
            </FormField>
          )}

          <Divider titlePlacement="start">{t('pages.tunnels.connection')}</Divider>
          <Row gutter={12}>
            {kind === 'reverse' && (
              <Col xs={24} md={12}>
                <FormField
                  name="settings.transport"
                  label={t('pages.tunnels.transport')}
                  tooltip={t('pages.tunnels.transportHint')}
                >
                  <Select
                    options={REVERSE_TRANSPORTS.map((v) => ({
                      label: v,
                      value: v,
                    }))}
                  />
                </FormField>
              </Col>
            )}
            {kind === 'l3' && (
              <>
                <Col xs={24} md={12}>
                  <FormField
                    name="settings.carrier"
                    label={t('pages.tunnels.carrier')}
                    tooltip={t('pages.tunnels.carrierHint')}
                  >
                    <Select options={L3_CARRIERS.map((v) => ({ label: v, value: v }))} />
                  </FormField>
                </Col>
                <Col xs={24} md={12}>
                  <FormField
                    name="settings.dialer"
                    label={t('pages.tunnels.dialer')}
                    tooltip={t('pages.tunnels.dialerHint')}
                  >
                    <Select
                      options={[
                        {
                          label: t('pages.tunnels.roleServer'),
                          value: 'server',
                        },
                        {
                          label: t('pages.tunnels.roleClient'),
                          value: 'client',
                        },
                      ]}
                    />
                  </FormField>
                </Col>
              </>
            )}
            {needsRemote && (
              <Col xs={24} md={12}>
                <FormField
                  name="settings.remoteAddr"
                  label={t('pages.tunnels.remoteAddr')}
                  tooltip={t('pages.tunnels.remoteAddrHint')}
                  rules={{ required: true }}
                >
                  <Input placeholder={kernel ? '198.51.100.7' : 'relay.example.com'} />
                </FormField>
              </Col>
            )}
            {kernel && (
              <Col xs={24} md={12}>
                <FormField
                  name="settings.localAddr"
                  label={t('pages.tunnels.localAddr')}
                  tooltip={t('pages.tunnels.localAddrHint')}
                >
                  <Input placeholder={t('pages.tunnels.automatic')} />
                </FormField>
              </Col>
            )}
            {!kernel && (
              <>
                <Col xs={24} md={12}>
                  <FormField
                    name="settings.port"
                    label={t('pages.tunnels.port')}
                    tooltip={t('pages.tunnels.portHint')}
                  >
                    <InputNumber min={1} max={65535} style={{ width: '100%' }} />
                  </FormField>
                </Col>
                <Col xs={24} md={12}>
                  <Form.Item
                    htmlFor={`${uid}-token`}
                    label={t('pages.tunnels.token')}
                    tooltip={t('pages.tunnels.tokenHint')}
                  >
                    <Space.Compact block>
                      <FormField name="settings.token" noStyle>
                        <Input
                          id={`${uid}-token`}
                          spellCheck={false}
                          autoComplete="off"
                          style={{ width: 'calc(100% - 32px)' }}
                        />
                      </FormField>
                      <Button
                        aria-label={t('regenerate')}
                        icon={<ReloadOutlined aria-hidden />}
                        onClick={() => void suggest(true)}
                      />
                    </Space.Compact>
                  </Form.Item>
                </Col>
              </>
            )}
            <Col xs={24} md={12}>
              <FormField
                name="settings.publicAddr"
                label={t('pages.tunnels.publicAddr')}
                tooltip={t('pages.tunnels.publicAddrHint')}
              >
                <Input placeholder={t('pages.tunnels.automatic')} />
              </FormField>
            </Col>
          </Row>

          {kind !== 'reverse' && (
            <>
              <Divider titlePlacement="start">{t('pages.tunnels.addressing')}</Divider>
              <Row gutter={12}>
                <Col xs={24} md={8}>
                  <FormField
                    name="settings.localIp"
                    label={t('pages.tunnels.localIp')}
                    tooltip={t('pages.tunnels.localIpHint')}
                    rules={{ required: true }}
                  >
                    <Input placeholder="10.80.0.2/30" />
                  </FormField>
                </Col>
                <Col xs={24} md={8}>
                  <FormField
                    name="settings.peerIp"
                    label={t('pages.tunnels.peerIp')}
                    rules={{ required: true }}
                  >
                    <Input placeholder="10.80.0.1" />
                  </FormField>
                </Col>
                <Col xs={24} md={8}>
                  <FormField name="settings.mtu" label="MTU" tooltip={t('pages.tunnels.mtuHint')}>
                    <InputNumber
                      min={0}
                      max={9000}
                      placeholder={t('pages.tunnels.automatic')}
                      style={{ width: '100%' }}
                    />
                  </FormField>
                </Col>
                {kernel && (
                  <>
                    <Col xs={24} md={8}>
                      <FormField name="settings.ttl" label="TTL">
                        <InputNumber
                          min={0}
                          max={255}
                          placeholder="255"
                          style={{ width: '100%' }}
                        />
                      </FormField>
                    </Col>
                    {kind.includes('gre') && (
                      <Col xs={24} md={8}>
                        <FormField
                          name="settings.key"
                          label={t('pages.tunnels.greKey')}
                          tooltip={t('pages.tunnels.greKeyHint')}
                        >
                          <InputNumber min={0} max={4294967295} style={{ width: '100%' }} />
                        </FormField>
                      </Col>
                    )}
                    <Col xs={24} md={8}>
                      <FormField
                        name="settings.nat"
                        label={t('pages.tunnels.nat')}
                        tooltip={t('pages.tunnels.natHint')}
                      >
                        <Select
                          options={[
                            {
                              label: t('pages.tunnels.natMasquerade'),
                              value: 'masquerade',
                            },
                            {
                              label: t('pages.tunnels.natPreserve'),
                              value: 'preserve',
                            },
                          ]}
                        />
                      </FormField>
                    </Col>
                  </>
                )}
              </Row>
              <Space>
                <Button
                  size="small"
                  icon={<ReloadOutlined aria-hidden />}
                  onClick={() => void suggest(false)}
                >
                  {t('pages.tunnels.suggestAddresses')}
                </Button>
              </Space>
            </>
          )}

          {kind === 'reverse' && (
            <FormField
              name="settings.proxyProtocol"
              label={t('pages.tunnels.proxyProtocol')}
              tooltip={t('pages.tunnels.proxyProtocolHint')}
              valueProp="checked"
            >
              <Switch />
            </FormField>
          )}

          {showForwards && (
            <>
              <Divider titlePlacement="start">{t('pages.tunnels.forwards')}</Divider>
              <Typography.Paragraph type="secondary">
                {t('pages.tunnels.forwardsHint')}
              </Typography.Paragraph>
              <ForwardRows
                name="settings.forwards"
                kernel={kernel}
                listenLabel={t('pages.tunnels.forwardListen')}
                targetLabel={t('pages.tunnels.forwardTarget')}
                listenPlaceholder="443"
                targetPlaceholder={t('pages.tunnels.forwardSame')}
                addLabel={t('pages.tunnels.addForward')}
                l2tpPreset={kernel}
              />
            </>
          )}

          {showForwards && kind !== 'reverse' && (
            <>
              <Divider titlePlacement="start">{t('pages.tunnels.reverseForwards')}</Divider>
              <Typography.Paragraph type="secondary">
                {t('pages.tunnels.reverseForwardsHint')}
              </Typography.Paragraph>
              <ForwardRows
                name="settings.reverseForwards"
                kernel={kernel}
                listenLabel={t('pages.tunnels.reverseForwardListen')}
                targetLabel={t('pages.tunnels.reverseForwardTarget')}
                listenPlaceholder="9090"
                targetPlaceholder="19090"
                addLabel={t('pages.tunnels.addReverseForward')}
              />
            </>
          )}

          {kernel && role === 'server' && (
            <Alert
              style={{ marginTop: 16 }}
              type="info"
              showIcon
              title={t('pages.tunnels.kernelServerHint')}
            />
          )}
        </Form>
      </FormProvider>
    </Modal>
  );
}
