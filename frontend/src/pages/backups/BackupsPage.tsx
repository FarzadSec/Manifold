import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
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
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  CloudUploadOutlined,
  DeleteOutlined,
  EditOutlined,
  FolderOpenOutlined,
  LockOutlined,
  PlusOutlined,
} from '@ant-design/icons';

import { keys } from '@/api/queryKeys';
import { useDatepicker } from '@/hooks/useDatepicker';
import { postJson } from '@/api/queries/useAccess';
import type { BackupTargetView, File as StoredFile } from '@/generated/types';
import PageShell from '@/pages/access/PageShell';
import { HttpUtil, IntlUtil, SizeFormatter } from '@/utils';
import BackupTargetModal from './BackupTargetModal';

function where(t: BackupTargetView): string {
  switch (t.kind) {
    case 's3':
      return `s3://${t.bucket}/${t.prefix}`;
    case 'ssh':
      return `${t.user}@${t.host}:${t.dir || 'manifold-backups'}`;
    default:
      return t.dir || '/etc/manifold/backups';
  }
}

export default function BackupsPage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { datepicker } = useDatepicker();
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<BackupTargetView | null>(null);
  const [running, setRunning] = useState(0);
  const [files, setFiles] = useState<{ target: BackupTargetView; files: StoredFile[] } | null>(
    null,
  );
  const list = useQuery({
    queryKey: keys.backups.targets(),
    queryFn: async () => {
      const msg = await HttpUtil.get<BackupTargetView[]>('/panel/api/backups/targets', undefined, {
        silent: true,
      });
      if (!msg?.success) throw new Error(msg?.msg || 'Request failed');
      return Array.isArray(msg.obj) ? msg.obj : [];
    },
  });
  const refresh = () => queryClient.invalidateQueries({ queryKey: keys.backups.targets() });

  async function save(payload: Record<string, unknown>) {
    const msg = await postJson<BackupTargetView>('/panel/api/backups/targets/save', payload);
    if (msg?.success) await refresh();
    return !!msg?.success;
  }

  async function run(row: BackupTargetView) {
    setRunning(row.id);
    try {
      await postJson(`/panel/api/backups/targets/run/${row.id}`);
      await refresh();
    } finally {
      setRunning(0);
    }
  }

  async function showFiles(row: BackupTargetView) {
    const msg = await HttpUtil.get<StoredFile[]>(`/panel/api/backups/targets/files/${row.id}`);
    if (msg?.success) setFiles({ target: row, files: msg.obj ?? [] });
  }

  const columns: ColumnsType<BackupTargetView> = [
    {
      title: t('pages.backups.name'),
      key: 'name',
      render: (_v, row) => (
        <Space wrap>
          <span>{row.name}</span>
          {!row.enable && <Tag>{t('disabled')}</Tag>}
          {row.hasPassphrase && (
            <Tooltip title={t('pages.backups.sealed')}>
              <LockOutlined aria-label={t('pages.backups.sealed')} />
            </Tooltip>
          )}
        </Space>
      ),
    },
    {
      title: t('pages.backups.where'),
      key: 'where',
      render: (_v, row) => <Typography.Text code>{where(row)}</Typography.Text>,
    },
    {
      title: t('pages.backups.schedule'),
      key: 'schedule',
      render: (_v, row) =>
        row.everyHours > 0
          ? t('pages.backups.everyN', { hours: row.everyHours, keep: row.keep || '∞' })
          : t('pages.backups.byHand'),
    },
    {
      title: t('pages.backups.last'),
      key: 'last',
      render: (_v, row) =>
        row.lastRun === 0 ? (
          '—'
        ) : row.lastError ? (
          <Tooltip title={row.lastError}>
            <Tag color="red">{IntlUtil.formatDate(row.lastRun, datepicker)}</Tag>
          </Tooltip>
        ) : (
          <Tooltip title={row.lastFile}>
            <Tag color="green">
              {IntlUtil.formatDate(row.lastRun, datepicker)} ·{' '}
              {SizeFormatter.sizeFormat(row.lastSize)}
            </Tag>
          </Tooltip>
        ),
    },
    {
      title: t('pages.nodes.actions'),
      key: 'actions',
      render: (_v, row) => (
        <Space>
          <Tooltip title={t('pages.backups.runNow')}>
            <Button
              type="text"
              icon={<CloudUploadOutlined aria-hidden />}
              aria-label={t('pages.backups.runNow')}
              loading={running === row.id}
              onClick={() => run(row)}
            />
          </Tooltip>
          <Tooltip title={t('pages.backups.files')}>
            <Button
              type="text"
              icon={<FolderOpenOutlined aria-hidden />}
              aria-label={t('pages.backups.files')}
              onClick={() => showFiles(row)}
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
            title={t('pages.backups.confirmDelete')}
            onConfirm={async () => {
              const msg = await postJson(`/panel/api/backups/targets/del/${row.id}`);
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
    <PageShell className="backups-page">
      <Card
        title={t('menu.backups')}
        extra={
          <Button
            type="primary"
            icon={<PlusOutlined aria-hidden />}
            onClick={() => {
              setEditing(null);
              setFormOpen(true);
            }}
          >
            {t('pages.backups.add')}
          </Button>
        }
      >
        <Typography.Paragraph type="secondary">{t('pages.backups.intro')}</Typography.Paragraph>
        <Table<BackupTargetView>
          rowKey="id"
          loading={list.isLoading}
          dataSource={list.data ?? []}
          columns={columns}
          pagination={false}
          scroll={{ x: 'max-content' }}
          locale={{ emptyText: <Empty description={t('pages.backups.empty')} /> }}
        />
      </Card>
      <BackupTargetModal
        open={formOpen}
        editing={editing}
        onSave={save}
        onClose={() => setFormOpen(false)}
      />
      <Modal
        open={!!files}
        footer={null}
        onCancel={() => setFiles(null)}
        title={files?.target.name}
        width={640}
      >
        <Table<StoredFile>
          size="small"
          rowKey="name"
          pagination={{ pageSize: 10, hideOnSinglePage: true }}
          dataSource={files?.files ?? []}
          locale={{ emptyText: t('pages.backups.noFiles') }}
          columns={[
            { title: t('pages.backups.file'), dataIndex: 'name', key: 'name' },
            {
              title: t('pages.backups.size'),
              key: 'size',
              render: (_v, f) => SizeFormatter.sizeFormat(f.size),
            },
          ]}
        />
      </Modal>
    </PageShell>
  );
}
