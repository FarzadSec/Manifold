import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  Button,
  Card,
  Empty,
  Flex,
  Popconfirm,
  Space,
  Spin,
  Table,
  Tag,
  Typography,
} from 'antd';
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons';

import { keys } from '@/api/queryKeys';
import { postJson } from '@/api/queries/useAccess';
import type { IngressListener, IngressRoute, IngressStatus } from '@/generated/types';
import PageShell from '@/pages/access/PageShell';
import type { IngressFormValues } from '@/schemas/node';
import { HttpUtil } from '@/utils';
import IngressFormModal from './IngressFormModal';

const PROXY_LABEL = ['', 'PROXY v1', 'PROXY v2'];

export default function IngressPage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<IngressListener | null>(null);
  const list = useQuery({
    queryKey: keys.ingress.list(),
    queryFn: async () => {
      const msg = await HttpUtil.get<IngressStatus[]>('/panel/api/ingress/list', undefined, {
        silent: true,
      });
      if (!msg?.success) throw new Error(msg?.msg || 'Request failed');
      return Array.isArray(msg.obj) ? msg.obj : [];
    },
  });
  const refresh = () => queryClient.invalidateQueries({ queryKey: keys.ingress.list() });

  async function save(values: IngressFormValues): Promise<boolean> {
    const msg = await postJson<IngressListener>('/panel/api/ingress/save', values);
    if (msg?.success) await refresh();
    return !!msg?.success;
  }

  async function remove(id: number) {
    const msg = await postJson(`/panel/api/ingress/del/${id}`);
    if (msg?.success) await refresh();
  }

  return (
    <PageShell className="ingress-page">
      <Card
        title={t('menu.ingress')}
        extra={
          <Button
            type="primary"
            icon={<PlusOutlined aria-hidden />}
            onClick={() => {
              setEditing(null);
              setFormOpen(true);
            }}
          >
            {t('pages.ingress.add')}
          </Button>
        }
      >
        <Typography.Paragraph type="secondary">{t('pages.ingress.intro')}</Typography.Paragraph>
        <Spin spinning={list.isLoading} aria-live="off">
          {(list.data ?? []).length === 0 && !list.isLoading ? (
            <Empty description={t('pages.ingress.empty')} />
          ) : (
            <Flex vertical gap={12}>
              {(list.data ?? []).map((row) => (
                <Card
                  key={row.id}
                  size="small"
                  title={
                    <Space wrap>
                      <span>{row.remark || `${row.listen || '*'}:${row.port}`}</span>
                      <Tag>{`${row.listen || '*'}:${row.port}`}</Tag>
                      {!row.enable && <Tag>{t('disabled')}</Tag>}
                    </Space>
                  }
                  extra={
                    <Space>
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
                        title={t('pages.ingress.confirmDelete')}
                        onConfirm={() => remove(row.id)}
                      >
                        <Button
                          type="text"
                          danger
                          icon={<DeleteOutlined aria-hidden />}
                          aria-label={t('delete')}
                        />
                      </Popconfirm>
                    </Space>
                  }
                >
                  {row.error && (
                    <Alert
                      type="error"
                      showIcon
                      style={{ marginBottom: 8 }}
                      title={t('pages.ingress.notServing', { reason: row.error })}
                    />
                  )}
                  <Table<IngressRoute>
                    size="small"
                    pagination={false}
                    rowKey={(_r, i) => String(i)}
                    dataSource={row.routes ?? []}
                    columns={[
                      {
                        title: t('pages.ingress.hosts'),
                        key: 'hosts',
                        render: (_v, r) => (r.hosts ?? []).map((h) => <Tag key={h}>{h}</Tag>),
                      },
                      {
                        title: t('pages.ingress.target'),
                        key: 'target',
                        render: (_v, r) => (
                          <Space size={4}>
                            <Typography.Text code>{r.target}</Typography.Text>
                            {r.proxyProtocol > 0 && (
                              <Tag color="blue">{PROXY_LABEL[r.proxyProtocol]}</Tag>
                            )}
                            {(r.alpn ?? []).length > 0 && (
                              <Tag>ALPN {(r.alpn ?? []).join(', ')}</Tag>
                            )}
                          </Space>
                        ),
                      },
                      { title: t('pages.ingress.remark'), dataIndex: 'remark', key: 'remark' },
                    ]}
                  />
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {row.defaultTarget
                      ? t('pages.ingress.defaultIs', { target: row.defaultTarget })
                      : t('pages.ingress.defaultClosed')}
                  </Typography.Text>
                </Card>
              ))}
            </Flex>
          )}
        </Spin>
      </Card>
      <IngressFormModal
        open={formOpen}
        editing={editing}
        onSave={save}
        onClose={() => setFormOpen(false)}
      />
    </PageShell>
  );
}
