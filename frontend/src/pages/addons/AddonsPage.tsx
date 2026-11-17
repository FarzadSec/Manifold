import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  Button,
  Card,
  Empty,
  Input,
  Modal,
  Popconfirm,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
} from 'antd';
import { DeleteOutlined, ExportOutlined, PlusOutlined } from '@ant-design/icons';

import { keys } from '@/api/queryKeys';
import type { AddonInstalled, AddonView } from '@/generated/types';
import PageShell from '@/pages/access/PageShell';
import { HttpUtil } from '@/utils';

export default function AddonsPage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [installOpen, setInstallOpen] = useState(false);
  const [manifest, setManifest] = useState('');
  const [installing, setInstalling] = useState(false);
  const [installed, setInstalled] = useState<AddonInstalled | null>(null);
  const list = useQuery({
    queryKey: keys.addons.list(),
    queryFn: async () => {
      const msg = await HttpUtil.get<AddonView[]>('/panel/api/addons/list', undefined, {
        silent: true,
      });
      if (!msg?.success) throw new Error(msg?.msg || 'Request failed');
      return Array.isArray(msg.obj) ? msg.obj : [];
    },
  });
  const refresh = () => queryClient.invalidateQueries({ queryKey: keys.addons.list() });

  async function install() {
    setInstalling(true);
    try {
      const msg = await HttpUtil.post<AddonInstalled>('/panel/api/addons/install', manifest, {
        headers: { 'Content-Type': 'application/json' },
      });
      if (msg?.success && msg.obj) {
        setInstallOpen(false);
        setManifest('');
        if (msg.obj.token || msg.obj.webhookSecret) setInstalled(msg.obj);
        await refresh();
      }
    } finally {
      setInstalling(false);
    }
  }

  return (
    <PageShell className="addons-page">
      <Card
        title={t('menu.addons')}
        extra={
          <Button
            type="primary"
            icon={<PlusOutlined aria-hidden />}
            onClick={() => setInstallOpen(true)}
          >
            {t('pages.addons.install')}
          </Button>
        }
      >
        <Typography.Paragraph type="secondary">{t('pages.addons.intro')}</Typography.Paragraph>
        <Table<AddonView>
          rowKey="id"
          loading={list.isLoading}
          dataSource={list.data ?? []}
          pagination={false}
          scroll={{ x: 'max-content' }}
          locale={{ emptyText: <Empty description={t('pages.addons.empty')} /> }}
          columns={[
            {
              title: t('pages.addons.name'),
              key: 'name',
              render: (_v, a) => (
                <Space direction="vertical" size={0}>
                  <Space size={4}>
                    <Typography.Text strong>{a.name}</Typography.Text>
                    {a.version && <Tag bordered={false}>{a.version}</Tag>}
                  </Space>
                  {a.manifest.description && (
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {a.manifest.description}
                    </Typography.Text>
                  )}
                </Space>
              ),
            },
            {
              title: t('pages.addons.permissions'),
              key: 'perms',
              render: (_v, a) => (
                <Space size={[4, 4]} wrap>
                  {a.manifest.permissions.map((p) => (
                    <Tag key={p}>{p}</Tag>
                  ))}
                </Space>
              ),
            },
            {
              title: t('pages.addons.events'),
              key: 'events',
              render: (_v, a) =>
                a.webhookId
                  ? (a.manifest.events?.length ?? 0) || t('pages.webhooks.allEvents')
                  : '—',
            },
            {
              title: t('enable'),
              key: 'enable',
              render: (_v, a) => (
                <Switch
                  size="small"
                  checked={a.enable}
                  aria-label={t('enable')}
                  onChange={async (on) => {
                    const msg = await HttpUtil.post(
                      `/panel/api/addons/${on ? 'enable' : 'disable'}/${a.id}`,
                    );
                    if (msg?.success) await refresh();
                  }}
                />
              ),
            },
            {
              key: 'actions',
              render: (_v, a) => (
                <Space>
                  {a.manifest.panelUrl && (
                    <Button
                      type="text"
                      icon={<ExportOutlined aria-hidden />}
                      aria-label={t('pages.addons.open')}
                      href={a.manifest.panelUrl}
                      target="_blank"
                      rel="noreferrer"
                    />
                  )}
                  <Popconfirm
                    title={t('pages.addons.confirmUninstall')}
                    onConfirm={async () => {
                      const msg = await HttpUtil.post(`/panel/api/addons/uninstall/${a.id}`);
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
          ]}
        />
      </Card>
      <Modal
        open={installOpen}
        title={t('pages.addons.install')}
        okText={t('pages.addons.install')}
        okButtonProps={{ disabled: !manifest.trim().startsWith('{') }}
        confirmLoading={installing}
        onOk={() => void install()}
        onCancel={() => setInstallOpen(false)}
        width={640}
      >
        <Typography.Paragraph type="secondary">
          {t('pages.addons.manifestHint')}
        </Typography.Paragraph>
        <Input.TextArea
          aria-label={t('pages.addons.manifest')}
          dir="ltr"
          rows={10}
          value={manifest}
          placeholder={
            '{\n  "id": "my-bot",\n  "permissions": ["users:read"],\n  "events": ["user.limited"],\n  "webhook": "https://bot.example.com/manifold"\n}'
          }
          onChange={(e) => setManifest(e.target.value)}
          style={{ fontFamily: 'monospace' }}
        />
      </Modal>
      <Modal
        open={!!installed}
        title={installed?.addon.name}
        onOk={() => setInstalled(null)}
        onCancel={() => setInstalled(null)}
        cancelButtonProps={{ style: { display: 'none' } }}
      >
        <Alert
          type="warning"
          showIcon
          title={t('pages.addons.shownOnce')}
          style={{ marginBottom: 12 }}
        />
        {installed?.token && (
          <Typography.Paragraph>
            <Typography.Text strong>{t('pages.addons.token')}</Typography.Text>
            <br />
            <Typography.Text code copyable>
              {installed.token}
            </Typography.Text>
          </Typography.Paragraph>
        )}
        {installed?.webhookSecret && (
          <Typography.Paragraph>
            <Typography.Text strong>{t('pages.webhooks.signingSecret')}</Typography.Text>
            <br />
            <Typography.Text code copyable>
              {installed.webhookSecret}
            </Typography.Text>
          </Typography.Paragraph>
        )}
      </Modal>
    </PageShell>
  );
}
