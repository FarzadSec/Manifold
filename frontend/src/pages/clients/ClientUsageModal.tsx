import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { Button, Empty, Flex, Modal, Segmented, Select, Spin, Statistic, Table } from 'antd';
import { DownloadOutlined } from '@ant-design/icons';

import { keys } from '@/api/queryKeys';
import { UsageBars } from '@/components/viz';
import type { ClientUsageView, NodeUsage } from '@/generated/types';
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

type WindowKey = '24h' | '7d' | '30d' | 'month';

interface ClientUsageModalProps {
  open: boolean;
  email: string;
  onClose: () => void;
}

function recentMonths(calendar: 'gregorian' | 'jalalian', count: number): YearMonth[] {
  const out: YearMonth[] = [monthOf(new Date(), calendar)];
  while (out.length < count) out.push(previousMonth(out[out.length - 1]));
  return out;
}

export default function ClientUsageModal({ open, email, onClose }: ClientUsageModalProps) {
  const { t, i18n } = useTranslation();
  const { datepicker } = useDatepicker();
  const [windowKey, setWindowKey] = useState<WindowKey>('24h');
  const months = useMemo(() => recentMonths(datepicker, 13), [datepicker]);
  const [month, setMonth] = useState<string>(() => monthParam(months[0]));
  const params = {
    window: windowKey,
    ...(windowKey === 'month'
      ? { month, calendar: datepicker === 'jalalian' ? 'persian' : 'gregorian' }
      : {}),
  };
  const query = useQuery({
    queryKey: keys.clients.usage(email, params),
    enabled: open && email !== '',
    queryFn: async () => {
      const msg = await HttpUtil.get<ClientUsageView>(
        `/panel/api/clients/usage/${encodeURIComponent(email)}`,
        params,
        { silent: true },
      );
      if (!msg?.success || !msg.obj) throw new Error(msg?.msg || 'Request failed');
      return msg.obj;
    },
  });
  const view = query.data;
  const hourly = !!view && !view.daily && windowKey !== '30d' && windowKey !== 'month';
  const bars = useMemo(() => {
    if (!view) return [];
    return view.points.map((p) => ({
      label: slotLabel(p.start, datepicker, hourly),
      up: p.up,
      down: p.down,
    }));
  }, [view, datepicker, hourly]);
  const totalUp = view?.nodes.reduce((sum, n) => sum + n.up, 0) ?? 0;
  const totalDown = view?.nodes.reduce((sum, n) => sum + n.down, 0) ?? 0;
  const serverName = (n: NodeUsage) =>
    n.name || (n.nodeId === 0 ? t('pages.usage.thisPanel') : `#${n.nodeId}`);

  const downloadCsv = () => {
    if (!view) return;
    const rows: (string | number)[][] = [
      [
        t('pages.usage.server'),
        t('pages.usage.upload'),
        t('pages.usage.download'),
        t('pages.usage.total'),
      ],
      ...view.nodes.map((n) => [serverName(n), n.up, n.down, n.up + n.down]),
      [],
      [t('pages.usage.slot'), t('pages.usage.upload'), t('pages.usage.download')],
      ...view.points.map((p) => [slotLabel(p.start, datepicker, hourly), p.up, p.down]),
    ];
    const suffix = windowKey === 'month' ? month : windowKey;
    FileManager.downloadTextFile(toCsv(rows), `usage-${email}-${suffix}.csv`, {
      type: 'text/csv;charset=utf-8',
    });
  };

  return (
    <Modal
      open={open}
      onCancel={onClose}
      footer={null}
      width={820}
      title={t('pages.usage.title', { name: email })}
      destroyOnHidden
    >
      <Flex gap={8} wrap align="center" style={{ marginBottom: 12 }}>
        <Segmented<WindowKey>
          value={windowKey}
          onChange={setWindowKey}
          options={[
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
            title={t('pages.usage.download')}
            value={SizeFormatter.sizeFormat(totalDown)}
          />
          <Statistic title={t('pages.usage.upload')} value={SizeFormatter.sizeFormat(totalUp)} />
          <Statistic
            title={t('pages.usage.total')}
            value={SizeFormatter.sizeFormat(totalUp + totalDown)}
          />
        </Flex>
        {bars.length === 0 ? (
          <Empty description={t('pages.usage.empty')} />
        ) : (
          <UsageBars
            bars={bars}
            upLabel={t('pages.usage.upload')}
            downLabel={t('pages.usage.download')}
          />
        )}
        <Table<NodeUsage>
          size="small"
          style={{ marginTop: 16 }}
          rowKey="nodeId"
          pagination={false}
          dataSource={view?.nodes ?? []}
          locale={{ emptyText: t('pages.usage.empty') }}
          columns={[
            { title: t('pages.usage.server'), key: 'server', render: (_v, n) => serverName(n) },
            {
              title: t('pages.usage.download'),
              key: 'down',
              render: (_v, n) => SizeFormatter.sizeFormat(n.down),
            },
            {
              title: t('pages.usage.upload'),
              key: 'up',
              render: (_v, n) => SizeFormatter.sizeFormat(n.up),
            },
            {
              title: t('pages.usage.share'),
              key: 'share',
              render: (_v, n) => {
                const all = totalUp + totalDown;
                return all > 0 ? `${Math.round(((n.up + n.down) / all) * 100)}%` : '—';
              },
            },
          ]}
        />
      </Spin>
    </Modal>
  );
}
