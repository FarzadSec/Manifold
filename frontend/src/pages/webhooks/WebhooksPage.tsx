import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  Button,
  Card,
  Empty,
  Modal,
  Popconfirm,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
  message,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  DeleteOutlined,
  EditOutlined,
  HistoryOutlined,
  PlusOutlined,
  RedoOutlined,
  SendOutlined,
} from '@ant-design/icons';

import { keys } from '@/api/queryKeys';
import { postJson } from '@/api/queries/useAccess';
import type { WebhookDelivery, WebhookView } from '@/generated/types';
import { useDatepicker } from '@/hooks/useDatepicker';
import PageShell from '@/pages/access/PageShell';
import { HttpUtil, IntlUtil } from '@/utils';
import WebhookModal from './WebhookModal';
import './WebhooksPage.css';

const STATUS_COLOR: Record<string, string> = {
  delivered: 'green',
  pending: 'orange',
  failed: 'red',
};

function target(row: WebhookView): string {
  return row.kind === 'bale' ? `chat ${row.chatId}` : row.url;
}

export default function WebhooksPage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { datepicker } = useDatepicker();
  const [messageApi, contextHolder] = message.useMessage();
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<WebhookView | null>(null);
  const [secret, setSecret] = useState('');
  const [log, setLog] = useState<{ hook: WebhookView; rows: WebhookDelivery[] } | null>(null);

  const list = useQuery({
    queryKey: keys.webhooks.list(),
    queryFn: async () => {
      const msg = await HttpUtil.get<WebhookView[]>('/panel/api/webhooks/list', undefined, {
        silent: true,
      });
      if (!msg?.success) throw new Error(msg?.msg || 'Request failed');
      return Array.isArray(msg.obj) ? msg.obj : [];
    },
  });
  const events = useQuery({
    queryKey: keys.webhooks.events(),
    queryFn: async () => {
      const msg = await HttpUtil.get<string[]>('/panel/api/webhooks/events', undefined, {
        silent: true,
      });
      return msg?.success && Array.isArray(msg.obj) ? msg.obj : [];
    },
    staleTime: Infinity,
  });
  const refresh = () => queryClient.invalidateQueries({ queryKey: keys.webhooks.list() });

  async function save(payload: Record<string, unknown>) {
    const msg = await postJson<WebhookView>('/panel/api/webhooks/save', payload);
    if (!msg?.success) return false;
    if (msg.obj?.secret) setSecret(msg.obj.secret);
    await refresh();
    return true;
  }

  async function openLog(hook: WebhookView) {
    const msg = await HttpUtil.get<WebhookDelivery[]>(`/panel/api/webhooks/deliveries/${hook.id}`);
    if (msg?.success) setLog({ hook, rows: msg.obj ?? [] });
  }

  async function test(hook: WebhookView) {
    const msg = await postJson(`/panel/api/webhooks/test/${hook.id}`);
    if (msg?.success) messageApi.success(t('pages.webhooks.testQueued'));
  }

  async function replay(row: WebhookDelivery) {
    const msg = await postJson(`/panel/api/webhooks/replay/${row.id}`);
    if (msg?.success && log) {
      setTimeout(() => openLog(log.hook), 1500);
    }
  }

  const columns: ColumnsType<WebhookView> = [
    {
      title: t('pages.backups.name'),
      key: 'name',
      render: (_v, row) => (
        <Space wrap>
          <span>{row.name}</span>
          {!row.enable && <Tag>{t('disabled')}</Tag>}
        </Space>
      ),
    },
    {
      title: t('pages.backups.kind'),
      key: 'kind',
      render: (_v, row) => (
        <Tag color={row.kind === 'http' ? 'blue' : row.kind === 'ntfy' ? 'geekblue' : 'cyan'}>
          {row.kind === 'bale' ? t('pages.webhooks.bale') : row.kind}
        </Tag>
      ),
    },
    {
      title: t('pages.webhooks.target'),
      key: 'target',
      render: (_v, row) => <Typography.Text code>{target(row)}</Typography.Text>,
    },
    {
      title: t('pages.webhooks.events'),
      key: 'events',
      render: (_v, row) =>
        row.events.length === 0 ? (
          <Tag>{t('pages.webhooks.allEvents')}</Tag>
        ) : (
          <Tooltip title={row.events.join(', ')}>
            <Tag>{t('pages.webhooks.nEvents', { count: row.events.length })}</Tag>
          </Tooltip>
        ),
    },
    {
      title: t('pages.nodes.actions'),
      key: 'actions',
      render: (_v, row) => (
        <Space>
          <Tooltip title={t('pages.webhooks.test')}>
            <Button
              type="text"
              icon={<SendOutlined aria-hidden />}
              aria-label={t('pages.webhooks.test')}
              onClick={() => test(row)}
            />
          </Tooltip>
          <Tooltip title={t('pages.webhooks.log')}>
            <Button
              type="text"
              icon={<HistoryOutlined aria-hidden />}
              aria-label={t('pages.webhooks.log')}
              onClick={() => openLog(row)}
            />
          </Tooltip>
          <Button
            type="text"
            icon={<EditOutlined aria-hidden />}
            aria-label={t('edit')}
            onClick={() => {
              setEditing(row);
              setFormOpen(true);
            }}
          />
          <Popconfirm
            title={t('pages.webhooks.confirmDelete')}
            onConfirm={async () => {
              const msg = await postJson(`/panel/api/webhooks/del/${row.id}`);
              if (msg?.success) await refresh();
            }}
          >
            <Button
              type="text"
              danger
              icon={<DeleteOutlined aria-hidden />}
              aria-label={t('delete')}
            />
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const deliveryColumns: ColumnsType<WebhookDelivery> = [
    {
      title: t('pages.webhooks.time'),
      key: 'time',
      render: (_v, d) => IntlUtil.formatDate(d.createdAt, datepicker),
    },
    { title: t('pages.webhooks.event'), dataIndex: 'event', key: 'event' },
    {
      title: t('pages.webhooks.status'),
      key: 'status',
      render: (_v, d) => {
        const tag = (
          <Tag color={STATUS_COLOR[d.status]}>
            {t(`pages.webhooks.state.${d.status}`, { defaultValue: d.status })}
            {d.attempts > 1 ? ` ×${d.attempts}` : ''}
          </Tag>
        );
        return d.lastError ? <Tooltip title={d.lastError}>{tag}</Tooltip> : tag;
      },
    },
    {
      title: t('pages.webhooks.answer'),
      key: 'code',
      render: (_v, d) => (d.lastCode ? d.lastCode : '—'),
    },
    {
      key: 'replay',
      render: (_v, d) =>
        d.status === 'pending' ? null : (
          <Tooltip title={t('pages.webhooks.replay')}>
            <Button
              type="text"
              size="small"
              icon={<RedoOutlined aria-hidden />}
              aria-label={t('pages.webhooks.replay')}
              onClick={() => replay(d)}
            />
          </Tooltip>
        ),
    },
  ];

  return (
    <PageShell className="webhooks-page">
      {contextHolder}
      <Card
        title={t('menu.webhooks')}
        extra={
          <Button
            type="primary"
            icon={<PlusOutlined aria-hidden />}
            onClick={() => {
              setEditing(null);
              setFormOpen(true);
            }}
          >
            {t('pages.webhooks.add')}
          </Button>
        }
      >
        <Typography.Paragraph type="secondary">{t('pages.webhooks.intro')}</Typography.Paragraph>
        <Table<WebhookView>
          rowKey="id"
          loading={list.isLoading}
          dataSource={list.data ?? []}
          columns={columns}
          pagination={false}
          scroll={{ x: 'max-content' }}
          locale={{ emptyText: <Empty description={t('pages.webhooks.empty')} /> }}
        />
      </Card>
      <WebhookModal
        open={formOpen}
        editing={editing}
        events={events.data ?? []}
        onSave={save}
        onClose={() => setFormOpen(false)}
      />
      <Modal
        open={!!secret}
        title={t('pages.webhooks.secretOnce')}
        onCancel={() => setSecret('')}
        onOk={() => setSecret('')}
        cancelButtonProps={{ style: { display: 'none' } }}
      >
        <Alert type="warning" showIcon title={t('pages.webhooks.secretOnceHint')} />
        <Typography.Paragraph style={{ marginTop: 12 }}>
          <Typography.Text code copyable>
            {secret}
          </Typography.Text>
        </Typography.Paragraph>
      </Modal>
      <Modal
        open={!!log}
        footer={null}
        onCancel={() => setLog(null)}
        title={log ? t('pages.webhooks.logOf', { name: log.hook.name }) : ''}
        width={760}
      >
        <Table<WebhookDelivery>
          size="small"
          rowKey="id"
          dataSource={log?.rows ?? []}
          columns={deliveryColumns}
          pagination={{ pageSize: 10, hideOnSinglePage: true }}
          locale={{ emptyText: t('pages.webhooks.noDeliveries') }}
          expandable={{
            expandedRowRender: (d) => (
              <pre className="webhook-payload" dir="ltr">
                {JSON.stringify(JSON.parse(d.payload || '{}'), null, 2)}
              </pre>
            ),
          }}
        />
      </Modal>
    </PageShell>
  );
}
