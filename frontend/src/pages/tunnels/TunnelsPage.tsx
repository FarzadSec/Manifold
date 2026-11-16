import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  Badge,
  Button,
  Card,
  ConfigProvider,
  Input,
  Layout,
  Modal,
  Popconfirm,
  Result,
  Space,
  Spin,
  Switch,
  Table,
  Tag,
  Tooltip,
  Typography,
  message,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  ApiOutlined,
  CloudServerOutlined,
  DeleteOutlined,
  EditOutlined,
  ImportOutlined,
  PlusOutlined,
  ShareAltOutlined,
  SyncOutlined,
} from '@ant-design/icons';

import { useTheme } from '@/hooks/useTheme';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import AppSidebar from '@/layouts/AppSidebar';
import { keys } from '@/api/queryKeys';
import { useTunnelRelaysQuery, useTunnelsQuery } from '@/api/queries/useTunnelsQuery';
import type { TunnelPairing, TunnelRelay, TunnelView } from '@/generated/types';
import { ClipboardManager, HttpUtil, SizeFormatter } from '@/utils';
import { setMessageInstance } from '@/utils/messageBus';
import { isTunnelEdition } from '@/utils/edition';
import TunnelFormModal from './TunnelFormModal';
import { STATE_COLORS, endpointOf, kindLabel, stateOf } from './tunnelInfo';

interface RelayDraft {
  id: number;
  name: string;
  url: string;
  token: string;
}

const NO_RELAY: RelayDraft = { id: 0, name: '', url: '', token: '' };

export default function TunnelsPage() {
  const { t } = useTranslation();
  const { isDark, isUltra, antdThemeConfig } = useTheme();
  const { isMobile } = useMediaQuery();
  const queryClient = useQueryClient();
  const [messageApi, messageContextHolder] = message.useMessage();
  useEffect(() => {
    setMessageInstance(messageApi);
  }, [messageApi]);

  const tunnels = useTunnelsQuery();
  const relays = useTunnelRelaysQuery();
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<TunnelView | null>(null);
  const [pairing, setPairing] = useState<{
    tunnel: TunnelView;
    result: TunnelPairing;
  } | null>(null);
  const [pairingAddress, setPairingAddress] = useState('');
  const [importOpen, setImportOpen] = useState(false);
  const [importCode, setImportCode] = useState('');
  const [relaysOpen, setRelaysOpen] = useState(false);
  const [relayDraft, setRelayDraft] = useState<RelayDraft>(NO_RELAY);
  const edition = isTunnelEdition();

  const refresh = () => queryClient.invalidateQueries({ queryKey: keys.tunnels.root() });

  async function openPairing(tunnel: TunnelView, address = '') {
    const msg = await HttpUtil.post<TunnelPairing>(`/panel/api/tunnels/pairing/${tunnel.id}`, {
      address,
    });
    if (msg.success && msg.obj) {
      setPairing({ tunnel, result: msg.obj });
      setPairingAddress(msg.obj.address);
    }
  }

  async function toggle(tunnel: TunnelView, enable: boolean) {
    await HttpUtil.post(
      `/panel/api/tunnels/update/${tunnel.id}`,
      {
        name: tunnel.name,
        kind: tunnel.kind,
        role: tunnel.role,
        enable,
        relayId: tunnel.relayId,
        settings: tunnel.settings,
      },
      { headers: { 'Content-Type': 'application/json' } },
    );
    void refresh();
  }

  async function remove(tunnel: TunnelView) {
    await HttpUtil.post(`/panel/api/tunnels/del/${tunnel.id}`);
    void refresh();
  }

  async function doImport() {
    const msg = await HttpUtil.post('/panel/api/tunnels/import', {
      code: importCode.trim(),
    });
    if (msg.success) {
      setImportOpen(false);
      setImportCode('');
      void refresh();
    }
  }

  async function resync() {
    const msg = await HttpUtil.post<string[]>('/panel/api/tunnels/resync', undefined, {
      silent: true,
    });
    if (!msg.success) return;
    const failed = msg.obj ?? [];
    if (failed.length === 0) messageApi.success(t('pages.tunnels.resyncDone'));
    else messageApi.error(failed.join('\n'));
  }

  async function saveRelay() {
    const url = relayDraft.id
      ? `/panel/api/tunnels/relays/update/${relayDraft.id}`
      : '/panel/api/tunnels/relays/add';
    const msg = await HttpUtil.post(url, {
      name: relayDraft.name,
      url: relayDraft.url,
      token: relayDraft.token,
    });
    if (msg.success) {
      setRelayDraft(NO_RELAY);
      void refresh();
    }
  }

  async function deleteRelay(relay: TunnelRelay) {
    await HttpUtil.post(`/panel/api/tunnels/relays/del/${relay.id}`);
    void refresh();
  }

  const relayName = useMemo(() => {
    const names = new Map((relays.data ?? []).map((r) => [r.id, r.name]));
    return (id: number) => names.get(id);
  }, [relays.data]);

  const columns: ColumnsType<TunnelView> = [
    {
      title: t('pages.tunnels.name'),
      dataIndex: 'name',
      render: (name: string, row) => (
        <Space direction="vertical" size={0}>
          <Typography.Text strong>{name}</Typography.Text>
          <Space size={4} wrap>
            <Tag>{kindLabel(row.kind)}</Tag>
            <Tag color={row.role === 'client' ? 'blue' : 'purple'}>
              {row.role === 'client'
                ? t('pages.tunnels.roleClient')
                : t('pages.tunnels.roleServer')}
            </Tag>
            {row.relayId > 0 && (
              <Tag icon={<CloudServerOutlined aria-hidden />}>
                {relayName(row.relayId) ?? row.relayId}
              </Tag>
            )}
          </Space>
        </Space>
      ),
    },
    {
      title: t('pages.tunnels.state'),
      key: 'state',
      render: (_, row) => {
        const state = stateOf(row);
        const detail = row.status?.error;
        const badge = (
          <Badge color={STATE_COLORS[state]} text={t(`pages.tunnels.states.${state}`)} />
        );
        return detail ? <Tooltip title={detail}>{badge}</Tooltip> : badge;
      },
    },
    {
      title: t('pages.tunnels.quality'),
      key: 'quality',
      responsive: ['md'],
      render: (_, row) =>
        row.status && row.status.latencyMs > 0
          ? `${row.status.latencyMs.toFixed(0)} ms · ${row.status.loss.toFixed(0)}%`
          : '-',
    },
    {
      title: t('pages.tunnels.traffic'),
      key: 'traffic',
      responsive: ['lg'],
      render: (_, row) =>
        row.status
          ? `↓ ${SizeFormatter.sizeFormat(row.status.rxBytes)} · ↑ ${SizeFormatter.sizeFormat(row.status.txBytes)}`
          : '-',
    },
    {
      title: t('pages.tunnels.endpoint'),
      key: 'endpoint',
      responsive: ['md'],
      render: (_, row) => <Typography.Text code>{endpointOf(row)}</Typography.Text>,
    },
    {
      title: t('pages.tunnels.forwards'),
      key: 'forwards',
      responsive: ['lg'],
      render: (_, row) =>
        (row.settings.forwards ?? [])
          .map((f) => (f.target ? `${f.listen}→${f.target}` : f.listen))
          .join(', ') || '-',
    },
    {
      title: t('pages.tunnels.actions'),
      key: 'actions',
      render: (_, row) => (
        <Space size={4}>
          <Switch
            size="small"
            checked={row.enable}
            aria-label={t('enable')}
            onChange={(v) => void toggle(row, v)}
          />
          <Tooltip title={t('pages.tunnels.pairingCode')}>
            <Button
              type="text"
              aria-label={t('pages.tunnels.pairingCode')}
              icon={<ShareAltOutlined aria-hidden />}
              onClick={() => void openPairing(row)}
            />
          </Tooltip>
          <Button
            type="text"
            aria-label={t('edit')}
            icon={<EditOutlined aria-hidden />}
            onClick={() => {
              setEditing(row);
              setFormOpen(true);
            }}
          />
          <Popconfirm title={t('pages.tunnels.deleteConfirm')} onConfirm={() => void remove(row)}>
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
  ];

  const pageClass = ['tunnels-page', isDark ? 'is-dark' : '', isUltra ? 'is-ultra' : '']
    .filter(Boolean)
    .join(' ');

  return (
    <ConfigProvider theme={antdThemeConfig}>
      {messageContextHolder}
      <Layout className={pageClass}>
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <Spin spinning={tunnels.isLoading} delay={200} size="large" aria-live="off">
              {tunnels.isError ? (
                <Result
                  status="error"
                  title={t('somethingWentWrong')}
                  subTitle={String(tunnels.error)}
                  extra={<Button onClick={() => void tunnels.refetch()}>{t('refresh')}</Button>}
                />
              ) : (
                <Card
                  size="small"
                  title={t('pages.tunnels.title')}
                  extra={
                    <Space wrap>
                      <Button
                        type="primary"
                        icon={<PlusOutlined aria-hidden />}
                        onClick={() => {
                          setEditing(null);
                          setFormOpen(true);
                        }}
                      >
                        {isMobile ? null : t('pages.tunnels.add')}
                      </Button>
                      <Button
                        icon={<ImportOutlined aria-hidden />}
                        onClick={() => setImportOpen(true)}
                      >
                        {isMobile ? null : t('pages.tunnels.importCode')}
                      </Button>
                      {!edition && (
                        <Button
                          icon={<ApiOutlined aria-hidden />}
                          onClick={() => setRelaysOpen(true)}
                        >
                          {isMobile ? null : t('pages.tunnels.relays')}
                        </Button>
                      )}
                      {!edition && (relays.data?.length ?? 0) > 0 && (
                        <Tooltip title={t('pages.tunnels.resyncHint')}>
                          <Button icon={<SyncOutlined aria-hidden />} onClick={() => void resync()}>
                            {isMobile ? null : t('pages.tunnels.resync')}
                          </Button>
                        </Tooltip>
                      )}
                    </Space>
                  }
                >
                  <Typography.Paragraph type="secondary">
                    {edition ? t('pages.tunnels.introEdition') : t('pages.tunnels.intro')}
                  </Typography.Paragraph>
                  <Table
                    rowKey="id"
                    size="small"
                    columns={columns}
                    dataSource={tunnels.data ?? []}
                    pagination={false}
                    scroll={{ x: 'max-content' }}
                    locale={{ emptyText: t('pages.tunnels.empty') }}
                  />
                </Card>
              )}
            </Spin>
          </Layout.Content>
        </Layout>

        <TunnelFormModal
          open={formOpen}
          tunnel={editing}
          relays={relays.data ?? []}
          onClose={(saved) => {
            setFormOpen(false);
            if (saved) void refresh();
          }}
        />

        <Modal
          open={!!pairing}
          title={t('pages.tunnels.pairingCode')}
          footer={null}
          onCancel={() => setPairing(null)}
          destroyOnHidden
        >
          <Typography.Paragraph type="secondary">
            {pairing?.tunnel.role === 'client'
              ? t('pages.tunnels.pairingHintClient')
              : t('pages.tunnels.pairingHintServer')}
          </Typography.Paragraph>
          <Space.Compact block style={{ marginBottom: 12 }}>
            <Input
              aria-label={t('pages.tunnels.publicAddr')}
              value={pairingAddress}
              onChange={(e) => setPairingAddress(e.target.value)}
            />
            <Button onClick={() => pairing && void openPairing(pairing.tunnel, pairingAddress)}>
              {t('pages.tunnels.regenerateCode')}
            </Button>
          </Space.Compact>
          <Input.TextArea
            aria-label={t('pages.tunnels.pairingCode')}
            readOnly
            autoSize={{ minRows: 4, maxRows: 10 }}
            value={pairing?.result.code ?? ''}
            style={{ fontFamily: 'monospace' }}
          />
          <Button
            block
            type="primary"
            style={{ marginTop: 12 }}
            onClick={async () => {
              if (await ClipboardManager.copyText(pairing?.result.code ?? ''))
                messageApi.success(t('copied'));
            }}
          >
            {t('copy')}
          </Button>
        </Modal>

        <Modal
          open={importOpen}
          title={t('pages.tunnels.importCode')}
          okText={t('pages.tunnels.importCode')}
          okButtonProps={{
            disabled: !importCode.trim().startsWith('manifold-tunnel://'),
          }}
          onOk={() => void doImport()}
          onCancel={() => setImportOpen(false)}
          destroyOnHidden
        >
          <Typography.Paragraph type="secondary">
            {t('pages.tunnels.importHint')}
          </Typography.Paragraph>
          <Input.TextArea
            aria-label={t('pages.tunnels.pairingCode')}
            autoSize={{ minRows: 4, maxRows: 10 }}
            value={importCode}
            placeholder="manifold-tunnel://..."
            onChange={(e) => setImportCode(e.target.value)}
            style={{ fontFamily: 'monospace' }}
          />
        </Modal>

        <Modal
          open={relaysOpen}
          width={680}
          title={t('pages.tunnels.relays')}
          footer={null}
          onCancel={() => setRelaysOpen(false)}
          destroyOnHidden
        >
          <Alert
            type="info"
            showIcon
            style={{ marginBottom: 12 }}
            title={t('pages.tunnels.relaysHint')}
          />
          <Table
            rowKey="id"
            size="small"
            pagination={false}
            dataSource={relays.data ?? []}
            locale={{ emptyText: t('pages.tunnels.relaysEmpty') }}
            columns={[
              { title: t('pages.tunnels.name'), dataIndex: 'name' },
              {
                title: 'URL',
                dataIndex: 'url',
                render: (u: string) => <Typography.Text code>{u}</Typography.Text>,
              },
              {
                title: t('pages.tunnels.actions'),
                key: 'a',
                render: (_, relay: TunnelRelay) => (
                  <Space size={4}>
                    <Button
                      type="text"
                      aria-label={t('edit')}
                      icon={<EditOutlined aria-hidden />}
                      onClick={() =>
                        setRelayDraft({
                          id: relay.id,
                          name: relay.name,
                          url: relay.url,
                          token: '',
                        })
                      }
                    />
                    <Popconfirm
                      title={t('pages.tunnels.deleteRelayConfirm')}
                      onConfirm={() => void deleteRelay(relay)}
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
          <Space direction="vertical" style={{ width: '100%', marginTop: 16 }}>
            <Input
              aria-label={t('pages.tunnels.name')}
              placeholder={t('pages.tunnels.name')}
              value={relayDraft.name}
              onChange={(e) => setRelayDraft((d) => ({ ...d, name: e.target.value }))}
            />
            <Input
              aria-label="URL"
              placeholder="https://relay.example.com:2053/path/"
              value={relayDraft.url}
              onChange={(e) => setRelayDraft((d) => ({ ...d, url: e.target.value }))}
            />
            <Input.Password
              aria-label={t('pages.tunnels.relayToken')}
              placeholder={
                relayDraft.id ? t('pages.tunnels.relayTokenKeep') : t('pages.tunnels.relayToken')
              }
              value={relayDraft.token}
              onChange={(e) => setRelayDraft((d) => ({ ...d, token: e.target.value }))}
            />
            <Space>
              <Button type="primary" onClick={() => void saveRelay()}>
                {relayDraft.id ? t('save') : t('pages.tunnels.addRelay')}
              </Button>
              {relayDraft.id > 0 && (
                <Button onClick={() => setRelayDraft(NO_RELAY)}>{t('cancel')}</Button>
              )}
            </Space>
          </Space>
        </Modal>
      </Layout>
    </ConfigProvider>
  );
}
