import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router';
import { useQuery } from '@tanstack/react-query';
import { Alert, Button, Card } from 'antd';
import {
  ClockCircleOutlined,
  ClusterOutlined,
  CloudDownloadOutlined,
  StopOutlined,
  TeamOutlined,
  WifiOutlined,
} from '@ant-design/icons';

import { keys } from '@/api/queryKeys';
import type { PanelSummary } from '@/generated/types';
import { HttpUtil, SizeFormatter } from '@/utils';
import { isTunnelEdition } from '@/utils/edition';

interface StatProps {
  icon: ReactNode;
  label: string;
  value: ReactNode;
  detail?: ReactNode;
  tone?: 'warn' | 'bad';
  onOpen?: () => void;
}

function Stat({ icon, label, value, detail, tone, onOpen }: StatProps) {
  return (
    <Card
      hoverable={!!onOpen}
      className={`ov-biz-tile${tone ? ` ov-biz-${tone}` : ''}`}
      styles={{ body: { padding: 0 } }}
      onClick={onOpen}
      role="group"
      aria-label={label}
    >
      <div className="ov-tile-head">
        <span className="ov-tile-icon">{icon}</span>
        <span className="ov-kicker">{label}</span>
      </div>
      <div className="ov-biz-value">{value}</div>
      <div className="ov-tile-detail">{detail ?? ' '}</div>
    </Card>
  );
}

export function BusinessTiles({ summary }: { summary: PanelSummary }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const u = summary.users;
  const toUsers = () => void navigate('/clients');
  const nodesDown = summary.nodesTotal - summary.nodesOnline;
  return (
    <>
      <div className="ov-biz">
        <Stat
          icon={<TeamOutlined aria-hidden />}
          label={t('pages.index.biz.active')}
          value={u.active}
          detail={t('pages.index.biz.ofTotal', { total: u.total })}
          onOpen={toUsers}
        />
        <Stat
          icon={<WifiOutlined aria-hidden />}
          label={t('pages.index.biz.online')}
          value={u.online}
          onOpen={toUsers}
        />
        <Stat
          icon={<ClockCircleOutlined aria-hidden />}
          label={t('pages.index.biz.expiring')}
          value={u.expiring}
          detail={t('pages.index.biz.expired', { count: u.expired })}
          tone={u.expiring > 0 ? 'warn' : undefined}
          onOpen={toUsers}
        />
        <Stat
          icon={<StopOutlined aria-hidden />}
          label={t('pages.index.biz.limited')}
          value={u.limited}
          detail={t('pages.index.biz.disabled', { count: u.disabled })}
          onOpen={toUsers}
        />
        <Stat
          icon={<CloudDownloadOutlined aria-hidden />}
          label={t('pages.index.biz.today')}
          value={SizeFormatter.sizeFormat(summary.trafficToday)}
        />
        {summary.nodesTotal > 0 && (
          <Stat
            icon={<ClusterOutlined aria-hidden />}
            label={t('pages.index.biz.nodes')}
            value={`${summary.nodesOnline} / ${summary.nodesTotal}`}
            detail={
              nodesDown > 0 ? t('pages.index.biz.nodesDown', { count: nodesDown }) : undefined
            }
            tone={nodesDown > 0 ? 'bad' : undefined}
            onOpen={() => void navigate('/nodes')}
          />
        )}
      </div>
      {summary.backupsFailing > 0 && (
        <Alert
          type="error"
          showIcon
          className="ov-biz-alert"
          title={t('pages.index.biz.backupsFailing', { count: summary.backupsFailing })}
          action={
            <Button type="link" size="small" onClick={() => void navigate('/backups')}>
              {t('pages.index.biz.open')}
            </Button>
          }
        />
      )}
      {summary.webhooksFailed > 0 && (
        <Alert
          type="warning"
          showIcon
          className="ov-biz-alert"
          title={t('pages.index.biz.webhooksFailed', { count: summary.webhooksFailed })}
          action={
            <Button type="link" size="small" onClick={() => void navigate('/webhooks')}>
              {t('pages.index.biz.open')}
            </Button>
          }
        />
      )}
    </>
  );
}

// A relay has no users, sales or nodes to count.
export default function BusinessStrip() {
  const relay = isTunnelEdition();
  const summary = useQuery({
    enabled: !relay,
    queryKey: keys.server.summary(),
    queryFn: async () => {
      const msg = await HttpUtil.get<PanelSummary>('/panel/api/server/summary', undefined, {
        silent: true,
      });
      if (!msg?.success || !msg.obj) throw new Error(msg?.msg || 'Request failed');
      return msg.obj;
    },
    refetchInterval: 15_000,
  });
  return summary.data && !relay ? <BusinessTiles summary={summary.data} /> : null;
}
