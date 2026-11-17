import { useTranslation } from 'react-i18next';
import { Badge, Button, Card, Empty, List, Space, Table, Tag, Typography } from 'antd';
import { RightOutlined } from '@ant-design/icons';
import { useNavigate } from 'react-router';

import { useTunnelsQuery } from '@/api/queries/useTunnelsQuery';
import type { PortForward, TunnelView } from '@/generated/types';
import { SizeFormatter } from '@/utils';
import { isTunnelEdition } from '@/utils/edition';
import { STATE_COLORS, kindLabel, stateOf } from '@/pages/tunnels/tunnelInfo';

export function forwardsText(forwards?: PortForward[] | null): string {
  if (!forwards?.length) return '-';
  return forwards
    .map((f) => (f.target && f.target !== f.listen ? `${f.listen} → ${f.target}` : f.listen))
    .join(', ');
}

// On a relay the tunnels are the whole job, so the overview shows each one in full.
function RelayTunnels({ tunnels }: { tunnels: TunnelView[] }) {
  const { t } = useTranslation();
  return (
    <Table
      rowKey="id"
      size="small"
      pagination={false}
      scroll={{ x: true }}
      dataSource={tunnels}
      columns={[
        {
          title: t('pages.tunnels.name'),
          key: 'name',
          render: (_, row: TunnelView) => (
            <Badge color={STATE_COLORS[stateOf(row)]} text={row.name} />
          ),
        },
        {
          title: t('pages.tunnels.kind'),
          key: 'kind',
          render: (_, row: TunnelView) => kindLabel(row.kind),
        },
        {
          title: t('pages.tunnels.state'),
          key: 'state',
          render: (_, row: TunnelView) => t(`pages.tunnels.states.${stateOf(row)}`),
        },
        {
          title: t('pages.tunnels.quality'),
          key: 'quality',
          render: (_, row: TunnelView) =>
            row.status && row.status.latencyMs > 0
              ? `${row.status.latencyMs.toFixed(0)} ms · ${row.status.loss.toFixed(0)}%`
              : '-',
        },
        {
          title: t('pages.tunnels.traffic'),
          key: 'traffic',
          render: (_, row: TunnelView) =>
            row.status
              ? `↓ ${SizeFormatter.sizeFormat(row.status.rxBytes)} ↑ ${SizeFormatter.sizeFormat(row.status.txBytes)}`
              : '-',
        },
        {
          title: t('pages.tunnels.forwards'),
          key: 'forwards',
          render: (_, row: TunnelView) => forwardsText(row.settings.forwards),
        },
        {
          title: t('pages.tunnels.endpoint'),
          key: 'peer',
          render: (_, row: TunnelView) => row.status?.peer || row.settings.remoteAddr || '-',
        },
      ]}
    />
  );
}

// One line per tunnel end, so a link that went down is seen from the overview.
export default function TunnelsCard() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { data } = useTunnelsQuery();
  const tunnels = data ?? [];
  const counts = tunnels.reduce<Record<string, number>>((acc, row) => {
    const state = stateOf(row);
    acc[state] = (acc[state] ?? 0) + 1;
    return acc;
  }, {});

  return (
    <Card
      size="small"
      className="ov-tunnels"
      title={
        <Space size={8} wrap>
          <span>{t('pages.tunnels.title')}</span>
          {Object.entries(counts).map(([state, n]) => (
            <Badge
              key={state}
              color={STATE_COLORS[state]}
              text={`${n} ${t(`pages.tunnels.states.${state}`)}`}
            />
          ))}
        </Space>
      }
      extra={
        <Button type="link" size="small" onClick={() => void navigate('/tunnels')}>
          {t('pages.tunnels.manage')} <RightOutlined aria-hidden />
        </Button>
      }
    >
      {tunnels.length === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('pages.tunnels.empty')} />
      ) : isTunnelEdition() ? (
        <RelayTunnels tunnels={tunnels} />
      ) : (
        <List
          size="small"
          dataSource={tunnels}
          renderItem={(row) => {
            const state = stateOf(row);
            const st = row.status;
            return (
              <List.Item
                extra={
                  <Typography.Text type="secondary">
                    {st && st.latencyMs > 0 ? `${st.latencyMs.toFixed(0)} ms · ` : ''}
                    {st
                      ? `↓ ${SizeFormatter.sizeFormat(st.rxBytes)} ↑ ${SizeFormatter.sizeFormat(st.txBytes)}`
                      : ''}
                  </Typography.Text>
                }
              >
                <Space size={8} wrap>
                  <Badge color={STATE_COLORS[state]} text={row.name} />
                  <Tag>{kindLabel(row.kind)}</Tag>
                  <Typography.Text
                    type={
                      state === 'up' ? 'success' : state === 'disabled' ? 'secondary' : 'danger'
                    }
                  >
                    {t(`pages.tunnels.states.${state}`)}
                  </Typography.Text>
                </Space>
              </List.Item>
            );
          }}
        />
      )}
    </Card>
  );
}
