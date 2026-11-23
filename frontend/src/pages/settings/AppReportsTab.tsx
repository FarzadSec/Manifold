import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Descriptions,
  Divider,
  Input,
  Modal,
  Popconfirm,
  Space,
  Spin,
  Table,
  Tag,
  Typography,
  message,
} from 'antd';
import { CopyOutlined, DeleteOutlined, DownloadOutlined, EyeOutlined } from '@ant-design/icons';

import { fetchAppReport, useAppReports } from '@/api/queries/useAppReports';
import type { AppReport } from '@/generated/types';
import { ClipboardManager, IntlUtil, SizeFormatter } from '@/utils';

const logStyle = {
  maxHeight: '60vh',
  overflow: 'auto',
  margin: 0,
  padding: 12,
  fontSize: 12,
  lineHeight: 1.5,
  whiteSpace: 'pre-wrap',
  overflowWrap: 'anywhere',
  borderRadius: 8,
  background: 'rgba(127, 127, 127, 0.08)',
} as const;

function deviceText(r: AppReport): string {
  return [r.deviceName, [r.deviceOs, r.osVersion].filter(Boolean).join(' ')]
    .filter(Boolean)
    .join(' · ');
}

function downloadLog(r: AppReport) {
  const blob = new Blob([r.log ?? ''], { type: 'text/plain;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `dt-connect-report-${r.id}-${r.email.replace(/[^\w.-]+/g, '_')}.txt`;
  a.click();
  URL.revokeObjectURL(url);
}

function matches(r: AppReport, query: string): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  return [r.email, r.tgId ? String(r.tgId) : '', r.message, r.deviceName].some((field) =>
    field.toLowerCase().includes(q),
  );
}

export default function AppReportsTab() {
  const { t } = useTranslation();
  const { reports, loading, fetchError } = useAppReports();
  const [query, setQuery] = useState('');
  const shown = useMemo(() => reports.filter((r) => matches(r, query)), [reports, query]);
  return (
    <>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        title={t('pages.settings.app.reports.desc')}
      />
      {fetchError && (
        <Alert type="error" showIcon style={{ marginBottom: 16 }} title={fetchError} />
      )}
      <Input.Search
        allowClear
        type="search"
        style={{ marginBottom: 12, maxWidth: 360 }}
        placeholder={t('pages.settings.app.reports.search')}
        aria-label={t('pages.settings.app.reports.search')}
        value={query}
        onChange={(e) => setQuery(e.target.value)}
      />
      <AppReportsTable reports={shown} loading={loading} />
    </>
  );
}

// ClientAppReports lists one user's reports in the user's own window, and nothing when there are none.
export function ClientAppReports({ email }: { email: string }) {
  const { t } = useTranslation();
  const { reports, loading } = useAppReports();
  const own = useMemo(
    () => reports.filter((r) => r.email.toLowerCase() === email.toLowerCase()),
    [reports, email],
  );
  if (own.length === 0) return null;
  return (
    <>
      <Divider titlePlacement="start">{t('pages.settings.app.reports.menu')}</Divider>
      <AppReportsTable reports={own} loading={loading} compact />
    </>
  );
}

function AppReportsTable({
  reports,
  loading,
  compact = false,
}: {
  reports: AppReport[];
  loading: boolean;
  compact?: boolean;
}) {
  const { t } = useTranslation();
  const [messageApi, messageHolder] = message.useMessage();
  const { markSeen, remove } = useAppReports();
  const [open, setOpen] = useState<AppReport | null>(null);
  const [opening, setOpening] = useState(false);
  const [openError, setOpenError] = useState('');

  async function show(r: AppReport) {
    setOpen(r);
    setOpening(true);
    setOpenError('');
    try {
      setOpen(await fetchAppReport(r.id));
      if (!r.seen) await markSeen(r.id);
    } catch (err) {
      setOpenError((err as Error).message);
    } finally {
      setOpening(false);
    }
  }

  async function copyLog(r: AppReport) {
    if (await ClipboardManager.copyText(r.log ?? '')) {
      messageApi.success(t('pages.settings.app.reports.copied'));
    }
  }

  const allColumns = [
    {
      title: t('pages.settings.app.reports.time'),
      key: 'createdAt',
      width: 170,
      render: (_: unknown, r: AppReport) => IntlUtil.formatDate(r.createdAt),
    },
    {
      title: t('pages.settings.app.reports.user'),
      key: 'email',
      render: (_: unknown, r: AppReport) => (
        <Space size={6} wrap>
          <Typography.Text strong={!r.seen} style={{ whiteSpace: 'nowrap' }}>
            {r.email}
          </Typography.Text>
          {!r.seen && <Tag color="red">{t('pages.settings.app.reports.new')}</Tag>}
        </Space>
      ),
    },
    {
      title: t('pages.settings.app.reports.tgId'),
      key: 'tgId',
      width: 150,
      render: (_: unknown, r: AppReport) =>
        r.tgId ? <Typography.Text copyable>{String(r.tgId)}</Typography.Text> : '—',
    },
    {
      title: t('pages.settings.app.reports.device'),
      key: 'device',
      render: (_: unknown, r: AppReport) => deviceText(r),
    },
    {
      title: t('pages.settings.app.reports.app'),
      key: 'appVersion',
      width: 80,
      render: (_: unknown, r: AppReport) => r.appVersion,
    },
    {
      title: t('pages.settings.app.reports.message'),
      key: 'message',
      render: (_: unknown, r: AppReport) =>
        r.message ? (
          <Typography.Paragraph
            dir="auto"
            ellipsis={{ rows: 2 }}
            style={{ margin: 0, maxWidth: 360 }}
          >
            {r.message}
          </Typography.Paragraph>
        ) : (
          <Typography.Text type="secondary">
            {t('pages.settings.app.reports.noMessage')}
          </Typography.Text>
        ),
    },
    {
      title: t('pages.settings.app.reports.logSize'),
      key: 'logSize',
      width: 100,
      render: (_: unknown, r: AppReport) => SizeFormatter.sizeFormat(r.logSize),
    },
    {
      title: '',
      key: 'actions',
      width: 96,
      render: (_: unknown, r: AppReport) => (
        <Space size={4}>
          <Button
            size="small"
            icon={<EyeOutlined aria-hidden />}
            aria-label={t('pages.settings.app.reports.open')}
            onClick={() => void show(r)}
          />
          <Popconfirm
            title={t('pages.settings.app.reports.deleteConfirm')}
            onConfirm={() => remove(r.id)}
          >
            <Button
              size="small"
              danger
              icon={<DeleteOutlined aria-hidden />}
              aria-label={t('delete')}
            />
          </Popconfirm>
        </Space>
      ),
    },
  ];

  // The user's own window already names the user and is narrow.
  const columns = compact
    ? allColumns.filter((c) => !['email', 'tgId', 'logSize'].includes(c.key))
    : allColumns;

  return (
    <>
      {messageHolder}
      <Table
        size="small"
        dataSource={reports}
        rowKey="id"
        pagination={{ pageSize: compact ? 5 : 20, hideOnSinglePage: true }}
        loading={loading && reports.length === 0}
        scroll={{ x: true }}
        columns={columns}
        onRow={(r) => ({ onDoubleClick: () => void show(r) })}
        locale={{ emptyText: t('pages.settings.app.reports.empty') }}
      />
      <Modal
        open={open !== null}
        width={960}
        title={open ? `#${open.id} · ${open.email}` : ''}
        onCancel={() => setOpen(null)}
        footer={
          open && (
            <Space wrap>
              <Button
                icon={<CopyOutlined aria-hidden />}
                disabled={opening || !open.log}
                onClick={() => void copyLog(open)}
              >
                {t('pages.settings.app.reports.copy')}
              </Button>
              <Button
                type="primary"
                icon={<DownloadOutlined aria-hidden />}
                disabled={opening || !open.log}
                onClick={() => downloadLog(open)}
              >
                {t('pages.settings.app.reports.download')}
              </Button>
            </Space>
          )
        }
      >
        {open && (
          <Space direction="vertical" size={12} style={{ width: '100%' }}>
            <Descriptions size="small" column={{ xs: 1, sm: 2 }} bordered>
              <Descriptions.Item label={t('pages.settings.app.reports.time')}>
                {IntlUtil.formatDate(open.createdAt)}
              </Descriptions.Item>
              <Descriptions.Item label={t('pages.settings.app.reports.tgId')}>
                {open.tgId ? <Typography.Text copyable>{String(open.tgId)}</Typography.Text> : '—'}
              </Descriptions.Item>
              <Descriptions.Item label={t('pages.settings.app.reports.device')}>
                {deviceText(open)}
              </Descriptions.Item>
              <Descriptions.Item label={t('pages.settings.app.reports.app')}>
                {open.appVersion}
              </Descriptions.Item>
              <Descriptions.Item label={t('pages.settings.app.reports.message')} span={2}>
                {open.message ? (
                  <div dir="auto" style={{ whiteSpace: 'pre-wrap' }}>
                    {open.message}
                  </div>
                ) : (
                  <Typography.Text type="secondary">
                    {t('pages.settings.app.reports.noMessage')}
                  </Typography.Text>
                )}
              </Descriptions.Item>
            </Descriptions>
            <Typography.Text strong>
              {t('pages.settings.app.reports.log')} · {SizeFormatter.sizeFormat(open.logSize)}
            </Typography.Text>
            {openError && <Alert type="error" showIcon title={openError} />}
            <Spin aria-live="off" spinning={opening}>
              <pre dir="ltr" style={logStyle}>
                {open.log ?? ''}
              </pre>
            </Spin>
          </Space>
        )}
      </Modal>
    </>
  );
}
