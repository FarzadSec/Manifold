import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Checkbox, Descriptions, List, Modal, Space, Tag, Typography } from 'antd';

import type { MigrationReport } from '@/generated/types';
import { HttpUtil, PromiseUtil } from '@/utils';

interface MigrationModalProps {
  file: File;
  report: MigrationReport;
  onClose: () => void;
  onBusy: (e: { busy: boolean; tip?: string }) => void;
}

const ADDRESS_COLORS: Record<string, string> = {
  same: 'green',
  relay: 'green',
  dnsHere: 'green',
  dnsUpdate: 'orange',
  changes: 'red',
  dnsElsewhere: 'blue',
  elsewhere: 'blue',
  unresolved: 'default',
};

const ISSUE_TYPES: Record<string, 'error' | 'warning' | 'info'> = {
  error: 'error',
  warn: 'warning',
  info: 'info',
};

// What a restore would do on this server, read before anything is changed.
export default function MigrationModal({ file, report, onClose, onBusy }: MigrationModalProps) {
  const { t } = useTranslation();
  const [keepPanelAccess, setKeepPanelAccess] = useState(true);
  const [rewriteAddress, setRewriteAddress] = useState(false);
  const blocked = report.issues.some((i) => i.level === 'error');
  const breaking = report.addresses.filter((a) => a.status === 'changes');

  async function restore() {
    const form = new FormData();
    form.append('backup', file);
    form.append('keepPanelAccess', String(keepPanelAccess));
    form.append('rewriteAddress', String(rewriteAddress));
    onClose();
    onBusy({ busy: true, tip: `${t('pages.index.migration.restoring')}…` });
    const msg = await HttpUtil.post<MigrationReport>('/panel/api/server/restoreBackup', form, {
      headers: { 'Content-Type': 'multipart/form-data' },
    });
    if (!msg?.success) {
      onBusy({ busy: false });
      return;
    }
    onBusy({ busy: true, tip: `${t('pages.settings.restartPanel')}…` });
    await PromiseUtil.sleep(5000);
    window.location.reload();
  }

  return (
    <Modal
      open
      width={760}
      title={t('pages.index.migration.reportTitle')}
      okText={t('pages.index.migration.restore')}
      okButtonProps={{ danger: true, disabled: blocked }}
      onOk={() => void restore()}
      onCancel={onClose}
    >
      <Descriptions
        size="small"
        column={2}
        items={[
          {
            key: 'from',
            label: t('pages.index.migration.from'),
            children: `${report.sourceAddress || '?'} · v${report.sourceVersion}`,
          },
          { key: 'to', label: t('pages.index.migration.to'), children: report.thisAddress || '?' },
          {
            key: 'inbounds',
            label: t('pages.index.migration.inbounds'),
            children: report.inbounds,
          },
          { key: 'clients', label: t('pages.index.migration.clients'), children: report.clients },
        ]}
      />

      <Alert
        style={{ margin: '12px 0' }}
        type={breaking.length ? 'warning' : 'success'}
        showIcon
        title={
          breaking.length
            ? t('pages.index.migration.summaryBreaks', { count: breaking.length })
            : t('pages.index.migration.summaryKeeps')
        }
      />

      {report.issues.length > 0 && (
        <Space direction="vertical" style={{ width: '100%', marginBottom: 12 }}>
          {report.issues.map((i, n) => (
            <Alert
              key={`${i.code}-${n}`}
              type={ISSUE_TYPES[i.level] ?? 'info'}
              showIcon
              title={t(`pages.index.migration.issues.${i.code}`, i.params)}
            />
          ))}
        </Space>
      )}

      <Typography.Title level={5}>{t('pages.index.migration.addresses')}</Typography.Title>
      <List
        size="small"
        dataSource={report.addresses}
        renderItem={(a) => (
          <List.Item>
            <Space direction="vertical" size={0} style={{ width: '100%' }}>
              <Space wrap>
                <Typography.Text code>{a.address}</Typography.Text>
                <Tag color={ADDRESS_COLORS[a.status] ?? 'default'}>
                  {t(`pages.index.migration.status.${a.status}`)}
                </Tag>
              </Space>
              <Typography.Text type="secondary">
                {t(`pages.index.migration.explain.${a.status}`, {
                  address: a.address,
                  here: report.thisAddress,
                  points: (a.points ?? []).join(', '),
                })}
              </Typography.Text>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                {a.usedBy.join(', ')}
              </Typography.Text>
            </Space>
          </List.Item>
        )}
      />

      {report.tunnels.length > 0 && (
        <>
          <Typography.Title level={5}>{t('pages.tunnels.title')}</Typography.Title>
          <List
            size="small"
            dataSource={report.tunnels}
            renderItem={(tun) => (
              <List.Item>
                <Space wrap>
                  <Typography.Text strong>{tun.name}</Typography.Text>
                  <Tag>{tun.kind}</Tag>
                  <Typography.Text type="secondary">
                    {t(`pages.index.migration.tunnelAction.${tun.action}`)}
                  </Typography.Text>
                </Space>
              </List.Item>
            )}
          />
        </>
      )}

      {report.files.length > 0 && (
        <Typography.Paragraph type="secondary" style={{ marginTop: 12 }}>
          {t('pages.index.migration.files', { count: report.files.length })}{' '}
          {report.files.map((f) => f.path).join(', ')}
        </Typography.Paragraph>
      )}

      <Space direction="vertical" style={{ marginTop: 8 }}>
        <Checkbox checked={keepPanelAccess} onChange={(e) => setKeepPanelAccess(e.target.checked)}>
          {t('pages.index.migration.keepPanelAccess')}
        </Checkbox>
        {report.addressChanged && (
          <Checkbox checked={rewriteAddress} onChange={(e) => setRewriteAddress(e.target.checked)}>
            {t('pages.index.migration.rewriteAddress', {
              from: report.sourceAddress,
              to: report.thisAddress,
            })}
          </Checkbox>
        )}
        <Typography.Text type="secondary">{t('pages.index.migration.loginNote')}</Typography.Text>
      </Space>
    </Modal>
  );
}
