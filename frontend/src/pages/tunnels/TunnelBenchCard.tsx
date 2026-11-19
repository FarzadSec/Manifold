import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  Button,
  Card,
  Checkbox,
  Col,
  Descriptions,
  Empty,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Progress,
  Radio,
  Row,
  Select,
  Space,
  Table,
  Tag,
  Typography,
  message,
} from 'antd';
import {
  CopyOutlined,
  DeleteOutlined,
  ExperimentOutlined,
  EyeOutlined,
  HistoryOutlined,
  LinkOutlined,
  PlayCircleOutlined,
  StopOutlined,
  TrophyOutlined,
} from '@ant-design/icons';
import { FormProvider, useForm, useWatch } from 'react-hook-form';

import { FormField } from '@/components/form/rhf';
import { keys } from '@/api/queryKeys';
import { useTunnelBenchHistoryQuery, useTunnelBenchQuery } from '@/api/queries/useTunnelBenchQuery';
import type {
  TunnelBench,
  TunnelBenchLink,
  TunnelBenchLinkRequest,
  TunnelBenchPick,
  TunnelBenchReport,
} from '@/generated/types';
import { useDatepicker } from '@/hooks/useDatepicker';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import { ClipboardManager, HttpUtil, IntlUtil, SizeFormatter } from '@/utils';
import { isTunnelEdition } from '@/utils/edition';
import {
  BENCH_FAMILIES,
  benchFamily,
  benchName,
  candidateFromKey,
  defaultPanelUrl,
  isolate,
} from './benchInfo';
import BenchResultsTable from './BenchResultsTable';

const BENCH_PORT_SPAN = 80;

interface TunnelBenchCardProps {
  onUsePick: (pick: TunnelBenchPick) => void;
}

interface RunForm {
  code: string;
  profile: string;
  keys: string[];
  addr4: string;
  addr6: string;
}

export default function TunnelBenchCard({ onUsePick }: TunnelBenchCardProps) {
  const { t } = useTranslation();
  const { isMobile } = useMediaQuery();
  const queryClient = useQueryClient();
  const [messageApi, contextHolder] = message.useMessage();
  const bench = useTunnelBenchQuery();
  const [linkOpen, setLinkOpen] = useState(false);
  const [runOpen, setRunOpen] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [made, setMade] = useState<TunnelBenchLink | null>(null);
  const refresh = () => queryClient.invalidateQueries({ queryKey: keys.tunnels.bench() });

  const state = bench.data;
  const active = state?.active ?? null;
  const link = state?.link ?? null;
  const shown = active ?? state?.last ?? null;

  async function cancel() {
    await HttpUtil.post('/panel/api/tunnels/bench/cancel');
    void refresh();
  }

  async function copy(text: string) {
    if (await ClipboardManager.copyText(text)) messageApi.success(t('copied'));
  }

  return (
    <Card
      size="small"
      style={{ marginTop: 16 }}
      title={
        <Space>
          <ExperimentOutlined aria-hidden />
          {t('pages.tunnels.bench.title')}
        </Space>
      }
      extra={
        <Space wrap>
          <Button
            type="primary"
            aria-label={t('pages.tunnels.bench.makeLink')}
            icon={<LinkOutlined aria-hidden />}
            disabled={!!active}
            onClick={() => {
              setMade(null);
              setLinkOpen(true);
            }}
          >
            {isMobile ? null : t('pages.tunnels.bench.makeLink')}
          </Button>
          <Button
            aria-label={t('pages.tunnels.bench.runLink')}
            icon={<PlayCircleOutlined aria-hidden />}
            disabled={!!active}
            onClick={() => setRunOpen(true)}
          >
            {isMobile ? null : t('pages.tunnels.bench.runLink')}
          </Button>
          <Button
            aria-label={t('pages.tunnels.bench.history')}
            icon={<HistoryOutlined aria-hidden />}
            onClick={() => setHistoryOpen(true)}
          >
            {isMobile ? null : t('pages.tunnels.bench.history')}
          </Button>
        </Space>
      }
    >
      {contextHolder}
      <Typography.Paragraph type="secondary">{t('pages.tunnels.bench.intro')}</Typography.Paragraph>

      {link && (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 12 }}
          title={t('pages.tunnels.bench.waiting')}
          description={
            <LinkDetails link={link} onCopy={(c) => void copy(c)} onCancel={() => void cancel()} />
          }
        />
      )}

      {active && !link && <BenchProgress report={active} onCancel={() => void cancel()} />}

      {shown ? (
        <BenchReportView report={shown} onUsePick={onUsePick} />
      ) : (
        !link && <Empty description={t('pages.tunnels.bench.empty')} />
      )}

      <MakeLinkModal
        open={linkOpen}
        made={made}
        onMade={(l) => {
          setMade(l);
          void refresh();
        }}
        onCopy={(c) => void copy(c)}
        onClose={() => setLinkOpen(false)}
      />
      <RunLinkModal
        open={runOpen}
        catalogue={state?.catalogue ?? []}
        profiles={state?.profiles ?? []}
        onClose={(started) => {
          setRunOpen(false);
          if (started) void refresh();
        }}
      />
      <HistoryModal
        open={historyOpen}
        onClose={() => setHistoryOpen(false)}
        onUsePick={onUsePick}
      />
    </Card>
  );
}

function LinkDetails({
  link,
  onCopy,
  onCancel,
}: {
  link: TunnelBenchLink;
  onCopy: (code: string) => void;
  onCancel: () => void;
}) {
  const { t } = useTranslation();
  const { datepicker } = useDatepicker();
  return (
    <Space orientation="vertical" style={{ width: '100%' }}>
      <Input.TextArea
        aria-label={t('pages.tunnels.bench.code')}
        readOnly
        autoSize={{ minRows: 3, maxRows: 6 }}
        value={link.code}
        style={{ fontFamily: 'monospace' }}
      />
      <Typography.Text type="secondary">
        {t('pages.tunnels.bench.expires', {
          time: isolate(IntlUtil.formatDate(link.expires, datepicker)),
        })}
      </Typography.Text>
      <Typography.Text type="secondary">
        {t('pages.tunnels.bench.ports', {
          from: isolate(link.port),
          to: isolate(link.port + BENCH_PORT_SPAN - 1),
        })}
      </Typography.Text>
      <Space wrap>
        <Button
          type="primary"
          icon={<CopyOutlined aria-hidden />}
          onClick={() => onCopy(link.code)}
        >
          {t('copy')}
        </Button>
        <Button danger icon={<StopOutlined aria-hidden />} onClick={onCancel}>
          {t('pages.tunnels.bench.withdraw')}
        </Button>
      </Space>
    </Space>
  );
}

function BenchProgress({ report, onCancel }: { report: TunnelBenchReport; onCancel: () => void }) {
  const { t } = useTranslation();
  const percent = report.total > 0 ? Math.round((report.done * 100) / report.total) : 0;
  const current = report.current ? benchName(candidateFromKey(report.current)) : '';
  return (
    <Card size="small" style={{ marginBottom: 12 }}>
      <Space orientation="vertical" style={{ width: '100%' }}>
        <Space wrap>
          <Typography.Text strong>
            {report.stage ? t(`pages.tunnels.bench.stageNames.${report.stage}`) : ''}
          </Typography.Text>
          {current && (
            <Tag color="processing">
              {current}
              {report.phase ? ` · ${t(`pages.tunnels.bench.phases.${report.phase}`)}` : ''}
            </Tag>
          )}
        </Space>
        <Progress
          percent={percent}
          status="active"
          format={() =>
            t('pages.tunnels.bench.progress', { done: report.done, total: report.total })
          }
        />
        <Space wrap separator="·">
          <Typography.Text type="secondary">
            {t('pages.tunnels.bench.peer', { peer: isolate(report.peer) })}
          </Typography.Text>
          <Typography.Text type="secondary">
            {t('pages.tunnels.bench.moved', {
              bytes: isolate(SizeFormatter.sizeFormat(report.bytes)),
            })}
          </Typography.Text>
          {!report.initiator && (
            <Typography.Text type="secondary">{t('pages.tunnels.bench.ranThere')}</Typography.Text>
          )}
        </Space>
        <Popconfirm title={t('pages.tunnels.bench.cancelConfirm')} onConfirm={onCancel}>
          <Button danger icon={<StopOutlined aria-hidden />}>
            {t('pages.tunnels.bench.cancel')}
          </Button>
        </Popconfirm>
      </Space>
    </Card>
  );
}

// BenchReportView shows a test's outcome: its picks, what to know, and every result.
export function BenchReportView({
  report,
  onUsePick,
}: {
  report: TunnelBenchReport;
  onUsePick: (pick: TunnelBenchPick) => void;
}) {
  const { t } = useTranslation();
  const { datepicker } = useDatepicker();
  const finished = report.state !== 'running' && report.state !== 'waiting';
  // The client end is where a tunnel is made; on the relay the pick is shown to copy there.
  const canUse = report.role === 'client';
  const extraPicks = [
    report.bestTcp && report.bestTcp.key !== report.best?.key
      ? {
          pick: report.bestTcp,
          title: t('pages.tunnels.bench.bestTcp'),
          hint: t('pages.tunnels.bench.bestTcpHint'),
        }
      : null,
    report.bestUdp && report.bestUdp.key !== report.best?.key
      ? {
          pick: report.bestUdp,
          title: t('pages.tunnels.bench.bestUdp'),
          hint: t('pages.tunnels.bench.bestUdpHint'),
        }
      : null,
  ].filter((p): p is { pick: TunnelBenchPick; title: string; hint: string } => p !== null);

  return (
    <Space orientation="vertical" style={{ width: '100%' }}>
      {finished && (
        <Space wrap>
          <Tag
            color={
              report.state === 'done' ? 'success' : report.state === 'failed' ? 'error' : 'default'
            }
          >
            {t(`pages.tunnels.bench.states.${report.state}`)}
          </Tag>
          <Typography.Text type="secondary">
            {IntlUtil.formatDate(report.finishedAt, datepicker)}
          </Typography.Text>
          <Typography.Text type="secondary">
            {t('pages.tunnels.bench.peer', { peer: isolate(report.peer) })}
          </Typography.Text>
          <Typography.Text type="secondary">
            {t('pages.tunnels.bench.moved', {
              bytes: isolate(SizeFormatter.sizeFormat(report.bytes)),
            })}
          </Typography.Text>
        </Space>
      )}
      {report.error && <Alert type="error" showIcon title={report.error} />}
      {report.best && (
        <PickCard
          pick={report.best}
          title={t('pages.tunnels.bench.best')}
          hint={t('pages.tunnels.bench.bestHint')}
          primary
          canUse={canUse}
          onUse={onUsePick}
        />
      )}
      {extraPicks.length > 0 && (
        <Row gutter={[12, 12]}>
          {extraPicks.map(({ pick, title, hint }) => (
            <Col key={title} xs={24} md={12}>
              <PickCard pick={pick} title={title} hint={hint} canUse={canUse} onUse={onUsePick} />
            </Col>
          ))}
        </Row>
      )}
      {report.best && !canUse && (
        <Typography.Text type="secondary">{t('pages.tunnels.bench.useOnPanel')}</Typography.Text>
      )}
      {report.warnings.length > 0 && (
        <Alert
          type="warning"
          showIcon
          title={t('pages.tunnels.bench.notes')}
          description={
            <ul style={{ margin: 0, paddingInlineStart: 18 }}>
              {report.warnings.map((w) => (
                <li key={w}>{w}</li>
              ))}
            </ul>
          }
        />
      )}
      {report.results.length > 0 && (
        <BenchResultsTable results={report.results} highlight={report.best?.key} />
      )}
      {report.tuning.length > 0 && (
        <>
          <Typography.Title level={5} style={{ marginTop: 8 }}>
            {t('pages.tunnels.bench.tuning')}
          </Typography.Title>
          <BenchResultsTable results={report.tuning} tuning />
        </>
      )}
    </Space>
  );
}

function PickCard({
  pick,
  title,
  hint,
  primary,
  canUse,
  onUse,
}: {
  pick: TunnelBenchPick;
  title: string;
  hint?: string;
  primary?: boolean;
  canUse: boolean;
  onUse: (pick: TunnelBenchPick) => void;
}) {
  const { t } = useTranslation();
  const items = [
    { key: 'tunnel', label: t('pages.tunnels.bench.columns.tunnel'), children: benchName(pick) },
    pick.kind === 'l3'
      ? {
          key: 'dialer',
          label: t('pages.tunnels.dialer'),
          children:
            pick.dialer === 'client'
              ? t('pages.tunnels.bench.dialsClient')
              : t('pages.tunnels.bench.dialsServer'),
        }
      : null,
    {
      key: 'mtu',
      label: 'MTU',
      children: pick.mtu
        ? pick.tuned
          ? t('pages.tunnels.bench.tunedMtu', { mtu: isolate(pick.mtu) })
          : String(pick.mtu)
        : t('pages.tunnels.bench.mtuDefault'),
    },
    pick.remoteAddr
      ? { key: 'remote', label: t('pages.tunnels.remoteAddr'), children: pick.remoteAddr }
      : null,
    {
      key: 'score',
      label: t('pages.tunnels.bench.columns.score'),
      children: pick.score.toFixed(1),
    },
  ].filter((i): i is { key: string; label: string; children: string } => i !== null);
  return (
    <Card
      size="small"
      style={primary ? { borderColor: '#52c41a' } : undefined}
      title={
        <Space>
          {primary && <TrophyOutlined aria-hidden style={{ color: '#faad14' }} />}
          {title}
        </Space>
      }
      extra={
        canUse && (
          <Button type={primary ? 'primary' : 'default'} size="small" onClick={() => onUse(pick)}>
            {t('pages.tunnels.bench.useTunnel')}
          </Button>
        )
      }
    >
      {hint && <Typography.Paragraph type="secondary">{hint}</Typography.Paragraph>}
      <Descriptions size="small" column={{ xs: 1, md: 2 }} items={items} />
    </Card>
  );
}

function MakeLinkModal({
  open,
  made,
  onMade,
  onCopy,
  onClose,
}: {
  open: boolean;
  made: TunnelBenchLink | null;
  onMade: (link: TunnelBenchLink) => void;
  onCopy: (code: string) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const methods = useForm<TunnelBenchLinkRequest>();
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open) return;
    methods.reset({
      role: isTunnelEdition() ? 'server' : 'client',
      url: defaultPanelUrl(window.location, window.X_UI_BASE_PATH ?? '/'),
      addr4: '',
      addr6: '',
      minutes: 30,
    });
  }, [open, methods]);

  async function submit(values: TunnelBenchLinkRequest) {
    setSaving(true);
    try {
      const msg = await HttpUtil.post<TunnelBenchLink>('/panel/api/tunnels/bench/link', values, {
        headers: { 'Content-Type': 'application/json' },
      });
      if (msg.success && msg.obj) onMade(msg.obj);
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      open={open}
      width={640}
      title={t('pages.tunnels.bench.makeLink')}
      okText={t('pages.tunnels.bench.create')}
      okButtonProps={{ style: made ? { display: 'none' } : undefined }}
      cancelText={made ? t('close') : t('cancel')}
      confirmLoading={saving}
      onOk={methods.handleSubmit(submit)}
      onCancel={onClose}
      destroyOnHidden
    >
      {made ? (
        <Space orientation="vertical" style={{ width: '100%' }}>
          <Typography.Paragraph type="secondary">
            {t('pages.tunnels.bench.linkHint')}
          </Typography.Paragraph>
          <LinkDetails link={made} onCopy={onCopy} onCancel={onClose} />
        </Space>
      ) : (
        <FormProvider {...methods}>
          <Form layout="vertical">
            <FormField name="role" label={t('pages.tunnels.bench.side')}>
              <Radio.Group
                options={[
                  { value: 'client', label: t('pages.tunnels.bench.sideClient') },
                  { value: 'server', label: t('pages.tunnels.bench.sideServer') },
                ]}
              />
            </FormField>
            <FormField
              name="url"
              label={t('pages.tunnels.bench.panelUrl')}
              tooltip={t('pages.tunnels.bench.panelUrlHint')}
            >
              <Input placeholder="https://panel.example.com:2053/path/" />
            </FormField>
            <Row gutter={12}>
              <Col xs={24} md={12}>
                <FormField
                  name="addr4"
                  label={t('pages.tunnels.bench.addr4')}
                  tooltip={t('pages.tunnels.bench.addrHint')}
                >
                  <Input placeholder={t('pages.tunnels.automatic')} />
                </FormField>
              </Col>
              <Col xs={24} md={12}>
                <FormField
                  name="addr6"
                  label={t('pages.tunnels.bench.addr6')}
                  tooltip={t('pages.tunnels.bench.addrHint')}
                >
                  <Input placeholder={t('pages.tunnels.automatic')} />
                </FormField>
              </Col>
            </Row>
            <FormField name="minutes" label={t('pages.tunnels.bench.minutes')}>
              <InputNumber min={5} max={1440} />
            </FormField>
          </Form>
        </FormProvider>
      )}
    </Modal>
  );
}

function RunLinkModal({
  open,
  catalogue,
  profiles,
  onClose,
}: {
  open: boolean;
  catalogue: { key: string; kind: string; transport?: string; carrier?: string; dialer?: string }[];
  profiles: { name: string; minutes: number; maxBytes: number }[];
  onClose: (started: boolean) => void;
}) {
  const { t } = useTranslation();
  const methods = useForm<RunForm>();
  const [saving, setSaving] = useState(false);
  const chosen = useWatch({ control: methods.control, name: 'keys' }) ?? [];
  const code = useWatch({ control: methods.control, name: 'code' }) ?? '';
  const profileName = useWatch({ control: methods.control, name: 'profile' });
  const chosenProfile = profiles.find((p) => p.name === profileName);

  useEffect(() => {
    if (!open) return;
    methods.reset({
      code: '',
      profile: 'standard',
      keys: catalogue.map((c) => c.key),
      addr4: '',
      addr6: '',
    });
  }, [open, methods, catalogue]);

  const families = useMemo(
    () =>
      BENCH_FAMILIES.map((family) => ({
        family,
        options: catalogue
          .filter((c) => benchFamily(c.kind) === family)
          .map((c) => ({
            value: c.key,
            label:
              c.kind === 'l3'
                ? `${c.carrier} · ${c.dialer === 'client' ? t('pages.tunnels.bench.dialsClient') : t('pages.tunnels.bench.dialsServer')}`
                : c.kind === 'reverse'
                  ? (c.transport ?? '')
                  : benchName(c),
          })),
      })),
    [catalogue, t],
  );

  async function submit(values: RunForm) {
    setSaving(true);
    try {
      const msg = await HttpUtil.post(
        '/panel/api/tunnels/bench/start',
        {
          code: values.code.trim(),
          profile: values.profile,
          keys: values.keys,
          addr4: values.addr4.trim(),
          addr6: values.addr6.trim(),
        },
        { headers: { 'Content-Type': 'application/json' } },
      );
      if (msg.success) onClose(true);
    } finally {
      setSaving(false);
    }
  }

  const setFamily = (family: string, on: boolean) => {
    const keysOf = families.find((f) => f.family === family)?.options.map((o) => o.value) ?? [];
    const rest = chosen.filter((k) => !keysOf.includes(k));
    methods.setValue('keys', on ? [...rest, ...keysOf] : rest, { shouldDirty: true });
  };

  return (
    <Modal
      open={open}
      width={720}
      title={t('pages.tunnels.bench.runLink')}
      okText={t('pages.tunnels.bench.start')}
      okButtonProps={{
        disabled: !code.trim().startsWith('manifold-tunnel-test://') || chosen.length === 0,
      }}
      confirmLoading={saving}
      onOk={methods.handleSubmit(submit)}
      onCancel={() => onClose(false)}
      destroyOnHidden
    >
      <Typography.Paragraph type="secondary">
        {t('pages.tunnels.bench.runHint')}
      </Typography.Paragraph>
      <FormProvider {...methods}>
        <Form layout="vertical">
          <FormField name="code" label={t('pages.tunnels.bench.code')} rules={{ required: true }}>
            <Input.TextArea
              autoSize={{ minRows: 3, maxRows: 6 }}
              placeholder="manifold-tunnel-test://..."
              style={{ fontFamily: 'monospace' }}
            />
          </FormField>
          <FormField
            name="profile"
            label={t('pages.tunnels.bench.profile')}
            extra={
              chosenProfile && (
                <>
                  {t(`pages.tunnels.bench.profileHints.${chosenProfile.name}`)}{' '}
                  {t('pages.tunnels.bench.profileDetail', {
                    minutes: chosenProfile.minutes,
                    bytes: isolate(SizeFormatter.sizeFormat(chosenProfile.maxBytes)),
                  })}
                </>
              )
            }
          >
            <Select
              options={profiles.map((p) => ({
                value: p.name,
                label: t(`pages.tunnels.bench.profiles.${p.name}`),
              }))}
            />
          </FormField>
          <Row gutter={12}>
            <Col xs={24} md={12}>
              <FormField
                name="addr4"
                label={t('pages.tunnels.bench.addr4')}
                tooltip={t('pages.tunnels.bench.addrHint')}
              >
                <Input placeholder={t('pages.tunnels.automatic')} />
              </FormField>
            </Col>
            <Col xs={24} md={12}>
              <FormField
                name="addr6"
                label={t('pages.tunnels.bench.addr6')}
                tooltip={t('pages.tunnels.bench.addrHint')}
              >
                <Input placeholder={t('pages.tunnels.automatic')} />
              </FormField>
            </Col>
          </Row>
          <Typography.Text strong>{t('pages.tunnels.bench.tunnels')}</Typography.Text>
          {families.map(({ family, options }) => {
            const on = options.filter((o) => chosen.includes(o.value)).length;
            return (
              <div key={family} style={{ marginTop: 8 }}>
                <Checkbox
                  checked={on === options.length && on > 0}
                  indeterminate={on > 0 && on < options.length}
                  onChange={(e) => setFamily(family, e.target.checked)}
                >
                  <Typography.Text strong>
                    {t(`pages.tunnels.bench.families.${family}`)}
                  </Typography.Text>
                </Checkbox>
                <div style={{ paddingInlineStart: 24, marginTop: 4 }}>
                  <Checkbox.Group
                    options={options}
                    value={chosen}
                    onChange={(next) =>
                      methods.setValue(
                        'keys',
                        [
                          ...chosen.filter((k) => !options.some((o) => o.value === k)),
                          ...(next as string[]),
                        ],
                        { shouldDirty: true },
                      )
                    }
                  />
                </div>
              </div>
            );
          })}
        </Form>
      </FormProvider>
    </Modal>
  );
}

function HistoryModal({
  open,
  onClose,
  onUsePick,
}: {
  open: boolean;
  onClose: () => void;
  onUsePick: (pick: TunnelBenchPick) => void;
}) {
  const { t } = useTranslation();
  const { datepicker } = useDatepicker();
  const queryClient = useQueryClient();
  const history = useTunnelBenchHistoryQuery(open);
  const [report, setReport] = useState<TunnelBenchReport | null>(null);

  async function view(row: TunnelBench) {
    const msg = await HttpUtil.get<TunnelBenchReport>(`/panel/api/tunnels/bench/report/${row.id}`);
    if (msg.success && msg.obj) setReport(msg.obj);
  }

  async function remove(row: TunnelBench) {
    await HttpUtil.post(`/panel/api/tunnels/bench/del/${row.id}`);
    void queryClient.invalidateQueries({ queryKey: keys.tunnels.benchHistory() });
  }

  return (
    <Modal
      open={open}
      width={report ? 1100 : 760}
      title={t('pages.tunnels.bench.history')}
      footer={null}
      onCancel={() => {
        setReport(null);
        onClose();
      }}
      destroyOnHidden
    >
      {report ? (
        <Space orientation="vertical" style={{ width: '100%' }}>
          <Button onClick={() => setReport(null)}>{t('pages.tunnels.bench.back')}</Button>
          <BenchReportView
            report={report}
            onUsePick={(pick) => {
              setReport(null);
              onClose();
              onUsePick(pick);
            }}
          />
        </Space>
      ) : (
        <Table<TunnelBench>
          rowKey="id"
          size="small"
          loading={history.isLoading}
          dataSource={history.data ?? []}
          pagination={{ pageSize: 10, hideOnSinglePage: true }}
          scroll={{ x: 'max-content' }}
          locale={{ emptyText: t('pages.tunnels.bench.historyEmpty') }}
          columns={[
            {
              title: t('pages.tunnels.bench.columns.when'),
              key: 'when',
              render: (_, r) => IntlUtil.formatDate(r.finishedAt || r.createdAt, datepicker),
            },
            {
              title: t('pages.tunnels.bench.columns.peer'),
              dataIndex: 'peer',
            },
            {
              title: t('pages.tunnels.bench.columns.state'),
              key: 'state',
              render: (_, r) => (
                <Space size={4}>
                  <Tag>{t(`pages.tunnels.bench.states.${r.state}`)}</Tag>
                  {r.initiator && <Tag color="blue">{t('pages.tunnels.bench.ranHere')}</Tag>}
                </Space>
              ),
            },
            {
              title: t('pages.tunnels.bench.best'),
              key: 'best',
              render: (_, r) => r.best || '-',
            },
            {
              title: t('pages.tunnels.actions'),
              key: 'actions',
              render: (_, r) => (
                <Space size={4}>
                  <Button
                    type="text"
                    aria-label={t('pages.tunnels.bench.view')}
                    icon={<EyeOutlined aria-hidden />}
                    onClick={() => void view(r)}
                  />
                  <Popconfirm
                    title={t('pages.tunnels.bench.deleteConfirm')}
                    onConfirm={() => void remove(r)}
                  >
                    <Button
                      type="text"
                      danger
                      aria-label={t('delete')}
                      icon={<DeleteOutlined aria-hidden />}
                    />
                  </Popconfirm>
                </Space>
              ),
            },
          ]}
        />
      )}
    </Modal>
  );
}
