import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Card, Empty, Popconfirm, Space, Table, Tag, Tooltip, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  SyncOutlined,
  UploadOutlined,
} from '@ant-design/icons';

import { keys } from '@/api/queryKeys';
import { postJson } from '@/api/queries/useAccess';
import { useNodesQuery } from '@/api/queries/useNodesQuery';
import type { CertificateView } from '@/generated/types';
import { useDatepicker } from '@/hooks/useDatepicker';
import PageShell from '@/pages/access/PageShell';
import { daysLeft, expiryColor } from '@/schemas/certificate';
import { HttpUtil, IntlUtil } from '@/utils';
import CertificateModal from './CertificateModal';
import UploadCertModal from './UploadCertModal';

export function ExpiryTag({ cert }: { cert: CertificateView }) {
  const { t } = useTranslation();
  const { datepicker } = useDatepicker();
  if (cert.issuing) {
    return (
      <Tag icon={<SyncOutlined spin aria-hidden />} color="processing">
        {t('pages.certs.issuing')}
      </Tag>
    );
  }
  if (!cert.notAfter) return <Tag>{t('pages.certs.notIssued')}</Tag>;
  const days = daysLeft(cert.notAfter);
  const color = expiryColor(cert.notBefore, cert.notAfter);
  return (
    <Tooltip title={IntlUtil.formatDate(cert.notAfter, datepicker)}>
      <Tag color={color}>
        {days < 0 ? t('pages.certs.expired') : t('pages.certs.daysLeft', { count: days })}
      </Tag>
    </Tooltip>
  );
}

export default function CertificatesPage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { nodes } = useNodesQuery();
  const [formOpen, setFormOpen] = useState(false);
  const [uploadOpen, setUploadOpen] = useState(false);
  const [editing, setEditing] = useState<CertificateView | null>(null);

  const list = useQuery({
    queryKey: keys.certs.list(),
    queryFn: async () => {
      const msg = await HttpUtil.get<CertificateView[]>('/panel/api/certs/list', undefined, {
        silent: true,
      });
      if (!msg?.success) throw new Error(msg?.msg || 'Request failed');
      return Array.isArray(msg.obj) ? msg.obj : [];
    },
    refetchInterval: (q) => (q.state.data?.some((c) => c.issuing) ? 2000 : false),
  });
  const refresh = () => queryClient.invalidateQueries({ queryKey: keys.certs.list() });
  const nodeOptions = nodes.map((n) => ({ id: n.id, name: n.name ?? `#${n.id}` }));
  const nodeName = (id: number) => nodes.find((n) => n.id === id)?.name ?? `#${id}`;

  async function save(payload: Record<string, unknown>, issue: boolean) {
    const msg = await postJson<CertificateView>('/panel/api/certs/save', payload);
    if (!msg?.success) return false;
    if (issue && msg.obj) await postJson(`/panel/api/certs/issue/${msg.obj.id}`);
    await refresh();
    return true;
  }

  async function upload(payload: { name: string; cert: string; key: string; nodeIds: number[] }) {
    const msg = await postJson<CertificateView>('/panel/api/certs/upload', payload);
    if (msg?.success) await refresh();
    return !!msg?.success;
  }

  async function issue(row: CertificateView) {
    await postJson(`/panel/api/certs/issue/${row.id}`);
    await refresh();
  }

  const sourceLabel: Record<string, string> = {
    acme: "Let's Encrypt",
    upload: t('pages.certs.uploaded'),
    master: t('pages.certs.fromMaster'),
  };

  const columns: ColumnsType<CertificateView> = [
    {
      title: t('pages.certs.name'),
      key: 'name',
      render: (_v, row) => (
        <Space direction="vertical" size={2}>
          <Typography.Text strong>{row.name}</Typography.Text>
          <Space size={[4, 4]} wrap>
            {row.domains.map((d) => (
              <Tag key={d} bordered={false}>
                {d}
              </Tag>
            ))}
          </Space>
        </Space>
      ),
    },
    {
      title: t('pages.certs.source'),
      key: 'source',
      render: (_v, row) => (
        <Space size={4} wrap>
          <Tag color={row.source === 'acme' ? 'blue' : undefined}>
            {sourceLabel[row.source] ?? row.source}
            {row.staging ? ` · ${t('pages.certs.staging')}` : ''}
          </Tag>
          {row.source === 'acme' && <Tag bordered={false}>{row.challenge.toUpperCase()}</Tag>}
        </Space>
      ),
    },
    {
      title: t('pages.certs.expires'),
      key: 'expires',
      render: (_v, row) => (
        <Space size={4} wrap>
          <ExpiryTag cert={row} />
          {row.source === 'acme' && row.autoRenew && (
            <Tag bordered={false}>{t('pages.certs.auto')}</Tag>
          )}
          {row.lastError && (
            <Tooltip title={row.lastError}>
              <Tag color="red">{t('pages.certs.lastFailed')}</Tag>
            </Tooltip>
          )}
        </Space>
      ),
    },
    {
      title: t('pages.certs.files'),
      key: 'files',
      render: (_v, row) =>
        row.notAfter ? (
          <Space direction="vertical" size={0}>
            <Typography.Text code copyable style={{ fontSize: 12 }}>
              {row.certFile}
            </Typography.Text>
            <Typography.Text code copyable style={{ fontSize: 12 }}>
              {row.keyFile}
            </Typography.Text>
            {row.nodeIds.length > 0 && (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                {t('pages.certs.onNodes', { nodes: row.nodeIds.map(nodeName).join(', ') })}
              </Typography.Text>
            )}
          </Space>
        ) : (
          '—'
        ),
    },
    {
      title: t('pages.nodes.actions'),
      key: 'actions',
      render: (_v, row) => (
        <Space>
          {row.source === 'acme' && (
            <Tooltip title={row.notAfter ? t('pages.certs.renewNow') : t('pages.certs.issueNow')}>
              <Button
                type="text"
                icon={<SyncOutlined aria-hidden />}
                aria-label={row.notAfter ? t('pages.certs.renewNow') : t('pages.certs.issueNow')}
                disabled={row.issuing}
                onClick={() => issue(row)}
              />
            </Tooltip>
          )}
          {row.source === 'acme' && (
            <Button
              type="text"
              icon={<EditOutlined aria-hidden />}
              aria-label={t('edit')}
              onClick={() => {
                setEditing(row);
                setFormOpen(true);
              }}
            />
          )}
          <Popconfirm
            title={t('pages.certs.confirmDelete')}
            onConfirm={async () => {
              const msg = await postJson(`/panel/api/certs/del/${row.id}`);
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

  return (
    <PageShell className="certs-page">
      <Card
        title={t('menu.certs')}
        extra={
          <Space wrap>
            <Button icon={<UploadOutlined aria-hidden />} onClick={() => setUploadOpen(true)}>
              {t('pages.certs.upload')}
            </Button>
            <Button
              type="primary"
              icon={<PlusOutlined aria-hidden />}
              onClick={() => {
                setEditing(null);
                setFormOpen(true);
              }}
            >
              {t('pages.certs.add')}
            </Button>
          </Space>
        }
      >
        <Typography.Paragraph type="secondary">{t('pages.certs.intro')}</Typography.Paragraph>
        <Table<CertificateView>
          rowKey="id"
          loading={list.isLoading}
          dataSource={list.data ?? []}
          columns={columns}
          pagination={false}
          scroll={{ x: 'max-content' }}
          locale={{ emptyText: <Empty description={t('pages.certs.empty')} /> }}
        />
      </Card>
      <CertificateModal
        open={formOpen}
        editing={editing}
        nodes={nodeOptions}
        onSave={save}
        onClose={() => setFormOpen(false)}
      />
      <UploadCertModal
        open={uploadOpen}
        nodes={nodeOptions}
        onUpload={upload}
        onClose={() => setUploadOpen(false)}
      />
    </PageShell>
  );
}
