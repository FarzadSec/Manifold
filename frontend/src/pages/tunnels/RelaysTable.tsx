import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Badge, Button, Popconfirm, Space, Table, Tag, Tooltip, Typography } from 'antd';
import { DeleteOutlined, EditOutlined } from '@ant-design/icons';

import type { TunnelRelay } from '@/generated/types';
import { HttpUtil } from '@/utils';

interface Props {
  relays: TunnelRelay[];
  panelVersion: string;
  onEdit: (relay: TunnelRelay) => void;
  onDelete: (relay: TunnelRelay) => void;
  onChecked: () => void;
}

type Reach = { ok: boolean; error?: string };

// RelaysTable asks each relay for its tunnels when it opens: the answer shows whether the
// relay is reachable with its token, and records its version for the version column.
export default function RelaysTable({ relays, panelVersion, onEdit, onDelete, onChecked }: Props) {
  const { t } = useTranslation();
  const [reach, setReach] = useState<Record<number, Reach>>({});
  const ids = relays.map((r) => r.id).join(',');

  useEffect(() => {
    if (!ids) return;
    let live = true;
    void Promise.all(
      ids.split(',').map(async (id) => {
        const msg = await HttpUtil.get(`/panel/api/tunnels/relays/${id}/tunnels`, undefined, {
          silent: true,
        });
        return [Number(id), { ok: !!msg?.success, error: msg?.msg }] as const;
      }),
    ).then((answers) => {
      if (!live) return;
      setReach(Object.fromEntries(answers));
      onChecked();
    });
    return () => {
      live = false;
    };
    // onChecked only refetches the list; checking again on its identity would loop.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ids]);

  return (
    <Table
      rowKey="id"
      size="small"
      pagination={false}
      dataSource={relays}
      locale={{ emptyText: t('pages.tunnels.relaysEmpty') }}
      columns={[
        { title: t('pages.tunnels.name'), dataIndex: 'name' },
        {
          title: 'URL',
          dataIndex: 'url',
          render: (u: string) => <Typography.Text code>{u}</Typography.Text>,
        },
        {
          title: t('pages.tunnels.state'),
          key: 'reach',
          render: (_, relay: TunnelRelay) => {
            const r = reach[relay.id];
            if (!r) return <Badge status="processing" text={t('pages.tunnels.relayChecking')} />;
            return r.ok ? (
              <Badge status="success" text={t('pages.tunnels.relayAnswering')} />
            ) : (
              <Tooltip title={r.error}>
                <Badge status="error" text={t('pages.tunnels.relayNotAnswering')} />
              </Tooltip>
            );
          },
        },
        {
          title: t('pages.tunnels.relayVersion'),
          dataIndex: 'version',
          render: (version: string) => {
            if (!version) return '-';
            if (version === panelVersion) return <Tag color="green">{version}</Tag>;
            return (
              <Tooltip
                title={t('pages.tunnels.relayVersionDiffers', {
                  relay: version,
                  panel: panelVersion,
                })}
              >
                <Tag color="orange">{version}</Tag>
              </Tooltip>
            );
          },
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
                onClick={() => onEdit(relay)}
              />
              <Popconfirm
                title={t('pages.tunnels.deleteRelayConfirm')}
                onConfirm={() => onDelete(relay)}
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
  );
}
