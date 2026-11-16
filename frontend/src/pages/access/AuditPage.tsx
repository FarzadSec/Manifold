import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  Button,
  Card,
  Col,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Row,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ClearOutlined, EyeOutlined } from '@ant-design/icons';
import dayjs from 'dayjs';

import { keys } from '@/api/queryKeys';
import {
  postJson,
  useAuditPage,
  useAuditSettings,
  type AuditFilter,
} from '@/api/queries/useAccess';
import type { AuditEntry } from '@/generated/types';
import PageShell from './PageShell';
import { onNumber } from '@/utils/onNumber';

const ENTITIES = [
  'users',
  'admins',
  'roles',
  'plans',
  'inbounds',
  'nodes',
  'hosts',
  'tunnels',
  'settings',
  'tokens',
  'xray',
  'server',
  'audit',
];
const ACTIONS = [
  'login',
  'login_failed',
  'login_new_ip',
  'two_factor',
  'export',
  'create',
  'update',
  'delete',
  'clear',
];

const ACTION_COLORS: Record<string, string> = {
  login: 'green',
  login_failed: 'red',
  login_new_ip: 'orange',
  delete: 'red',
  create: 'blue',
  export: 'purple',
  clear: 'magenta',
};

function prettyDetail(detail: string): string {
  try {
    return JSON.stringify(JSON.parse(detail), null, 2);
  } catch {
    return detail;
  }
}

export default function AuditPage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [filter, setFilter] = useState<AuditFilter>({ page: 1, pageSize: 50 });
  const [viewing, setViewing] = useState<AuditEntry | null>(null);
  const page = useAuditPage(filter);
  const settings = useAuditSettings();
  const enabled = settings.data?.enabled ?? false;

  const patch = (next: Partial<AuditFilter>) =>
    setFilter((f) => ({ ...f, ...next, page: next.page ?? 1 }));
  const refresh = () => queryClient.invalidateQueries({ queryKey: keys.access.root() });

  async function saveSettings(next: { enabled: boolean; retentionDays: number }) {
    const msg = await postJson('/panel/api/access/audit/settings', next);
    if (msg.success) void refresh();
  }

  const columns: ColumnsType<AuditEntry> = [
    {
      title: t('pages.access.when'),
      key: 'at',
      render: (_, e) => dayjs(e.at).format('YYYY-MM-DD HH:mm:ss'),
    },
    { title: t('pages.access.admin'), key: 'admin', render: (_, e) => e.username || 'system' },
    { title: t('pages.access.entity'), key: 'entity', render: (_, e) => <Tag>{e.entity}</Tag> },
    {
      title: t('pages.access.action'),
      key: 'action',
      render: (_, e) => <Tag color={ACTION_COLORS[e.action]}>{e.action}</Tag>,
    },
    { title: t('pages.access.target'), key: 'target', render: (_, e) => e.target || '—' },
    {
      title: 'IP',
      key: 'ip',
      render: (_, e) => (e.ip ? <Typography.Text code>{e.ip}</Typography.Text> : '—'),
    },
    {
      title: '',
      key: 'view',
      render: (_, e) => (
        <Button
          type="text"
          aria-label={t('pages.access.viewRecord')}
          icon={<EyeOutlined aria-hidden />}
          onClick={() => setViewing(e)}
        />
      ),
    },
  ];

  return (
    <PageShell className="access-page">
      <Space direction="vertical" size={12} style={{ width: '100%' }}>
        <Card size="small" title={t('pages.access.auditSettings')}>
          <Row gutter={[16, 8]} align="middle">
            <Col>
              <Space>
                <Switch
                  aria-label={t('pages.access.logChanges')}
                  checked={enabled}
                  loading={settings.isLoading}
                  onChange={(on) =>
                    void saveSettings({
                      enabled: on,
                      retentionDays: settings.data?.retentionDays ?? 90,
                    })
                  }
                />
                <Typography.Text>{t('pages.access.logChanges')}</Typography.Text>
              </Space>
            </Col>
            <Col>
              <Space>
                <Typography.Text>{t('pages.access.keepDays')}</Typography.Text>
                <InputNumber
                  aria-label={t('pages.access.keepDays')}
                  min={0}
                  value={settings.data?.retentionDays ?? 90}
                  onChange={onNumber((v) => void saveSettings({ enabled, retentionDays: v }))}
                />
              </Space>
            </Col>
          </Row>
          {!enabled && (
            <Alert
              type="info"
              showIcon
              style={{ marginTop: 12 }}
              title={t('pages.access.changeLoggingOff')}
            />
          )}
        </Card>

        <Card
          size="small"
          title={t('menu.audit')}
          extra={
            <Popconfirm
              title={t('pages.access.clearLogConfirm')}
              onConfirm={async () => {
                const msg = await postJson('/panel/api/access/audit/clear');
                if (msg.success) void refresh();
              }}
            >
              <Button danger icon={<ClearOutlined aria-hidden />}>
                {t('pages.access.clearLog')}
              </Button>
            </Popconfirm>
          }
        >
          <Space wrap style={{ marginBottom: 12 }}>
            <Input
              allowClear
              aria-label={t('pages.access.admin')}
              placeholder={t('pages.access.anyAdmin')}
              onChange={(e) => patch({ admin: e.target.value || undefined })}
              style={{ width: 160 }}
            />
            <Select
              allowClear
              aria-label={t('pages.access.entity')}
              placeholder={t('pages.access.anyEntity')}
              style={{ width: 160 }}
              options={ENTITIES.map((v) => ({ label: v, value: v }))}
              onChange={(v) => patch({ entity: v || undefined })}
            />
            <Select
              allowClear
              aria-label={t('pages.access.action')}
              placeholder={t('pages.access.anyAction')}
              style={{ width: 160 }}
              options={ACTIONS.map((v) => ({ label: v, value: v }))}
              onChange={(v) => patch({ action: v || undefined })}
            />
          </Space>
          <Table
            rowKey="id"
            size="small"
            loading={page.isLoading}
            columns={columns}
            dataSource={page.data?.items ?? []}
            scroll={{ x: 'max-content' }}
            pagination={{
              current: filter.page,
              pageSize: filter.pageSize,
              total: page.data?.total ?? 0,
              onChange: (p, size) => setFilter((f) => ({ ...f, page: p, pageSize: size })),
            }}
          />
        </Card>
      </Space>

      <Modal
        open={!!viewing}
        title={t('pages.access.viewRecord')}
        footer={null}
        onCancel={() => setViewing(null)}
        width={680}
      >
        <Input.TextArea
          readOnly
          aria-label={t('pages.access.viewRecord')}
          autoSize={{ minRows: 6, maxRows: 24 }}
          style={{ fontFamily: 'monospace' }}
          value={viewing ? prettyDetail(viewing.detail) || '—' : ''}
        />
      </Modal>
    </PageShell>
  );
}
