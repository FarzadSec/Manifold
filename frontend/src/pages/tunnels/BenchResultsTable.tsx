import { useTranslation } from 'react-i18next';
import { Badge, Space, Table, Tag, Tooltip, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';

import type { BenchResult } from '@/generated/types';
import { benchName, mbps, sortBenchResults } from './benchInfo';

const RESULT_COLORS: Record<string, string> = {
  ok: '#52c41a',
  failed: '#ff4d4f',
  skipped: '#bfbfbf',
  pending: '#d9d9d9',
};

interface BenchResultsTableProps {
  results: BenchResult[];
  // tuning lists each tuned tunnel at its own MTU first, then the MTUs tried.
  tuning?: boolean;
  highlight?: string;
}

export default function BenchResultsTable({ results, tuning, highlight }: BenchResultsTableProps) {
  const { t } = useTranslation();
  const rows = tuning ? results : sortBenchResults(results);

  const columns: ColumnsType<BenchResult> = [
    {
      title: t('pages.tunnels.bench.columns.tunnel'),
      key: 'tunnel',
      fixed: 'left',
      render: (_, r) => (
        <Space orientation="vertical" size={0}>
          <Typography.Text strong={r.key === highlight}>{benchName(r)}</Typography.Text>
          <Space size={4} wrap>
            {r.kind === 'l3' && (
              <Tag>
                {r.dialer === 'client'
                  ? t('pages.tunnels.bench.dialsClient')
                  : t('pages.tunnels.bench.dialsServer')}
              </Tag>
            )}
            {tuning && (
              <Tag color={r.mtu ? 'gold' : undefined}>
                {r.mtu ? `MTU ${r.mtu}` : t('pages.tunnels.bench.mtuDefault')}
              </Tag>
            )}
            {!tuning && r.stage && r.state === 'ok' && (
              <Tag color={r.stage === 'full' ? 'blue' : undefined}>
                {t(`pages.tunnels.bench.stages.${r.stage}`)}
              </Tag>
            )}
            {r.state === 'ok' && r.udp && !r.tcp && (
              <Tag color="purple">{t('pages.tunnels.bench.udpOnly')}</Tag>
            )}
            {r.state === 'ok' && r.tcp && !r.udp && (
              <Tag color="cyan">{t('pages.tunnels.bench.tcpOnly')}</Tag>
            )}
          </Space>
        </Space>
      ),
    },
    {
      title: t('pages.tunnels.bench.columns.state'),
      key: 'state',
      render: (_, r) => {
        const badge = (
          <Badge
            color={RESULT_COLORS[r.state]}
            text={t(`pages.tunnels.bench.resultStates.${r.state}`)}
          />
        );
        const detail = r.error || r.warning;
        return detail ? <Tooltip title={detail}>{badge}</Tooltip> : badge;
      },
    },
    {
      title: t('pages.tunnels.bench.columns.tcp'),
      key: 'tcp',
      render: (_, r) => (r.tcp ? `${mbps(r.downMbps)} / ${mbps(r.upMbps)}` : '-'),
    },
    {
      title: t('pages.tunnels.bench.columns.load'),
      key: 'load',
      responsive: ['md'],
      render: (_, r) =>
        r.tcp && r.streams > 0 ? `${mbps(r.loadDownMbps)} / ${mbps(r.loadUpMbps)}` : '-',
    },
    {
      title: t('pages.tunnels.bench.columns.latency'),
      key: 'latency',
      responsive: ['md'],
      render: (_, r) =>
        r.tcp
          ? `${r.latencyMs.toFixed(1)} ms · ${r.jitterMs.toFixed(1)} · ${r.loss.toFixed(0)}%`
          : '-',
    },
    {
      title: t('pages.tunnels.bench.columns.udp'),
      key: 'udp',
      render: (_, r) => (r.udp ? `${mbps(r.udpDownMbps)} / ${mbps(r.udpUpMbps)}` : '-'),
    },
    {
      title: t('pages.tunnels.bench.columns.udpQuality'),
      key: 'udpQuality',
      responsive: ['lg'],
      render: (_, r) =>
        r.udp
          ? `${r.udpLatencyMs.toFixed(1)} ms · ${Math.max(r.udpLoss, (r.udpDownLoss + r.udpUpLoss) / 2).toFixed(1)}%`
          : '-',
    },
    {
      title: t('pages.tunnels.bench.columns.score'),
      key: 'score',
      render: (_, r) =>
        r.state === 'ok' ? (
          <Tooltip title={`TCP ${r.tcpScore} · UDP ${r.udpScore}`}>
            <Typography.Text strong>{r.score.toFixed(1)}</Typography.Text>
          </Tooltip>
        ) : (
          '-'
        ),
    },
  ];

  return (
    <Table
      rowKey={(r) => `${r.key}:${r.mtu ?? 0}:${r.stage}`}
      size="small"
      columns={columns}
      dataSource={rows}
      pagination={false}
      scroll={{ x: 'max-content' }}
    />
  );
}
