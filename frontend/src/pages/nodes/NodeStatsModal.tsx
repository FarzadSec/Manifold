import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import {
  Button,
  Empty,
  Flex,
  Modal,
  Segmented,
  Select,
  Spin,
  Statistic,
  Table,
  Typography,
} from 'antd';
import { DownloadOutlined } from '@ant-design/icons';

import { keys } from '@/api/queryKeys';
import { AvailabilityStrip, Sparkline } from '@/components/viz';
import type { NodeStatsView, UserUsage } from '@/generated/types';
import { useDatepicker } from '@/hooks/useDatepicker';
import {
  monthName,
  monthOf,
  monthParam,
  previousMonth,
  slotLabel,
  toCsv,
  type YearMonth,
} from '@/lib/usage/calendar';
import { FileManager, HttpUtil, SizeFormatter } from '@/utils';

type WindowKey = '1h' | '24h' | '7d' | '30d' | 'month';

interface NodeStatsModalProps {
  node: { id: number; name?: string } | null;
  onClose: () => void;
}

function recentMonths(calendar: 'gregorian' | 'jalalian', count: number): YearMonth[] {
  const out: YearMonth[] = [monthOf(new Date(), calendar)];
  while (out.length < count) out.push(previousMonth(out[out.length - 1]));
  return out;
}

export default function NodeStatsModal({ node, onClose }: NodeStatsModalProps) {
  const { t, i18n } = useTranslation();
  const { datepicker } = useDatepicker();
  const [windowKey, setWindowKey] = useState<WindowKey>('24h');
  const months = useMemo(() => recentMonths(datepicker, 4), [datepicker]);
  const [month, setMonth] = useState<string>(() => monthParam(months[0]));
  const params = {
    window: windowKey,
    ...(windowKey === 'month'
      ? { month, calendar: datepicker === 'jalalian' ? 'persian' : 'gregorian' }
      : {}),
  };
  const id = node?.id ?? 0;
  const query = useQuery({
    queryKey: [...keys.nodes.root(), 'stats', id, params],
    enabled: id > 0,
    queryFn: async () => {
      const msg = await HttpUtil.get<NodeStatsView>(`/panel/api/nodes/stats/${id}`, params, {
        silent: true,
      });
      if (!msg?.success || !msg.obj) throw new Error(msg?.msg || 'Request failed');
      return msg.obj;
    },
  });
  const view = query.data;
  const hourly = windowKey === '1h' || windowKey === '24h';
  const timeLabel = (at: number) => slotLabel(at, datepicker, hourly || windowKey === '7d');
  const users = view?.users ?? [];
  const totalUp = users.reduce((sum, u) => sum + u.up, 0);
  const totalDown = users.reduce((sum, u) => sum + u.down, 0);

  const downloadCsv = () => {
    if (!view) return;
    const rows: (string | number)[][] = [
      [t('pages.nodes.stats.uptime'), `${view.uptimePct.toFixed(2)}%`],
      [],
      [
        t('pages.usage.user'),
        t('pages.usage.upload'),
        t('pages.usage.download'),
        t('pages.usage.total'),
      ],
      ...users.map((u) => [u.email, u.up, u.down, u.up + u.down]),
      [],
      [t('pages.usage.slot'), t('pages.nodes.cpu'), t('pages.nodes.mem')],
      ...view.points.map((p) => [timeLabel(p.at), p.cpu.toFixed(1), p.mem.toFixed(1)]),
    ];
    const suffix = windowKey === 'month' ? month : windowKey;
    FileManager.downloadTextFile(toCsv(rows), `node-${node?.name || id}-${suffix}.csv`, {
      type: 'text/csv;charset=utf-8',
    });
  };

  return (
    <Modal
      open={!!node}
      onCancel={onClose}
      footer={null}
      width={820}
      title={t('pages.nodes.stats.title', { name: node?.name || '' })}
      destroyOnHidden
    >
      <Flex gap={8} wrap align="center" style={{ marginBottom: 12 }}>
        <Segmented<WindowKey>
          value={windowKey}
          onChange={setWindowKey}
          options={[
            { label: t('pages.nodes.stats.hour'), value: '1h' },
            { label: t('pages.usage.window.24h'), value: '24h' },
            { label: t('pages.usage.window.7d'), value: '7d' },
            { label: t('pages.usage.window.30d'), value: '30d' },
            { label: t('pages.usage.window.month'), value: 'month' },
          ]}
        />
        {windowKey === 'month' && (
          <Select
            value={month}
            onChange={setMonth}
            style={{ minWidth: 170 }}
            aria-label={t('pages.usage.window.month')}
            options={months.map((m) => ({
              value: monthParam(m),
              label: monthName(m, datepicker, i18n.language),
            }))}
          />
        )}
        <Button
          icon={<DownloadOutlined aria-hidden />}
          onClick={downloadCsv}
          disabled={!view}
          style={{ marginInlineStart: 'auto' }}
        >
          CSV
        </Button>
      </Flex>
      <Spin spinning={query.isFetching} aria-live="off">
        <Flex gap={24} wrap style={{ marginBottom: 12 }}>
          <Statistic
            title={t('pages.nodes.stats.uptime')}
            value={view && view.watchedPct > 0 ? `${view.uptimePct.toFixed(2)}%` : '—'}
          />
          <Statistic
            title={t('pages.usage.download')}
            value={SizeFormatter.sizeFormat(totalDown)}
          />
          <Statistic title={t('pages.usage.upload')} value={SizeFormatter.sizeFormat(totalUp)} />
        </Flex>
        <Typography.Text type="secondary">{t('pages.nodes.stats.availability')}</Typography.Text>
        {view && view.availability.length > 0 ? (
          <AvailabilityStrip
            stretches={view.availability}
            formatTime={timeLabel}
            labels={{
              up: t('pages.nodes.stats.up'),
              down: t('pages.nodes.stats.down'),
              unwatched: t('pages.nodes.stats.unwatched'),
            }}
          />
        ) : null}
        <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginTop: 4 }}>
          {t('pages.nodes.stats.availabilityHint')}
        </Typography.Paragraph>
        {view && view.points.length > 1 ? (
          <Sparkline
            data={view.points.map((p) => p.cpu)}
            data2={view.points.map((p) => p.mem)}
            name1={t('pages.nodes.cpu')}
            name2={t('pages.nodes.mem')}
            labels={view.points.map((p) => timeLabel(p.at))}
            showAxes
            showLegend
            showTooltip
            valueMax={100}
            yFormatter={(v) => `${Math.round(v)}%`}
          />
        ) : (
          <Empty description={t('pages.nodes.stats.noSamples')} />
        )}
        <Table<UserUsage>
          size="small"
          style={{ marginTop: 16 }}
          rowKey="email"
          pagination={{ pageSize: 10, hideOnSinglePage: true }}
          dataSource={users}
          locale={{ emptyText: t('pages.usage.empty') }}
          columns={[
            { title: t('pages.usage.user'), dataIndex: 'email', key: 'email' },
            {
              title: t('pages.usage.download'),
              key: 'down',
              render: (_v, u) => SizeFormatter.sizeFormat(u.down),
            },
            {
              title: t('pages.usage.upload'),
              key: 'up',
              render: (_v, u) => SizeFormatter.sizeFormat(u.up),
            },
            {
              title: t('pages.usage.total'),
              key: 'total',
              render: (_v, u) => SizeFormatter.sizeFormat(u.up + u.down),
            },
          ]}
        />
      </Spin>
    </Modal>
  );
}
