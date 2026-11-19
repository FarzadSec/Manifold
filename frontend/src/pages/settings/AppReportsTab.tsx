import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Descriptions,
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

export default function AppReportsTab() {
  const { t } = useTranslation();
  const [messageApi, messageHolder] = message.useMessage();
  const { reports, loading, fetchError, markSeen, remove } = useAppReports();
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

  const columns = [
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

  return (
    <>
      {messageHolder}
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        title={t('pages.settings.app.reports.desc')}
      />
      {fetchError && (
        <Alert type="error" showIcon style={{ marginBottom: 16 }} title={fetchError} />
      )}
      <Table
        size="small"
        dataSource={reports}
        rowKey="id"
        pagination={{ pageSize: 20, hideOnSinglePage: true }}
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
