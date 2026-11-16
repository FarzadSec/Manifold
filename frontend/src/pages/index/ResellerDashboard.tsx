import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router';
import { useQuery } from '@tanstack/react-query';
import { Alert, Button, Card, Col, Progress, Row, Statistic, Typography } from 'antd';
import { TeamOutlined } from '@ant-design/icons';
import dayjs from 'dayjs';

import { HttpUtil } from '@/utils';
import { useAccess } from '@/api/queries/useAccess';
import type { ClientPageResponse } from '@/generated/types';
import PageShell from '@/pages/access/PageShell';
import { formatBytes } from '@/pages/access/accessInfo';

// A reseller's overview: its allowance and its own users, instead of the panel's machine.
export default function ResellerDashboard() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { me } = useAccess();
  const page = useQuery({
    queryKey: ['access', 'resellerSummary'],
    queryFn: async () => {
      const msg = await HttpUtil.get<ClientPageResponse>(
        '/panel/api/clients/list/paged',
        { pageSize: 1 },
        { silent: true },
      );
      if (!msg.success) throw new Error(msg.msg);
      return msg.obj;
    },
    refetchInterval: 15000,
  });
  const summary = page.data?.summary;
  const volume = me?.volumeBytes ?? 0;
  const used = me?.cycleUsed ?? 0;
  const percent = volume > 0 ? Math.min(100, Math.round((used / volume) * 100)) : 0;

  return (
    <PageShell className="index-page">
      {me?.suspended && (
        <Alert
          type="error"
          showIcon
          style={{ marginBottom: 12 }}
          title={t(`pages.access.suspendedNotice.${me.suspended}`)}
        />
      )}
      <Row gutter={[12, 12]}>
        <Col xs={24} lg={12}>
          <Card size="small" title={t('pages.access.myAllowance')}>
            <Statistic
              title={t('pages.access.usedThisCycle')}
              value={formatBytes(used)}
              suffix={volume > 0 ? `/ ${formatBytes(volume)}` : `/ ${t('pages.access.unlimited')}`}
            />
            {volume > 0 && (
              <Progress
                percent={percent}
                status={me?.suspended === 'volume' ? 'exception' : 'active'}
              />
            )}
            <Typography.Paragraph type="secondary" style={{ marginTop: 8, marginBottom: 0 }}>
              {t(`pages.access.cycle.${me?.resetCycle || 'none'}`)}
              {me?.cycleStart
                ? ` · ${t('pages.access.cycleSince', { date: dayjs(me.cycleStart).format('YYYY-MM-DD') })}`
                : ''}
              {' · '}
              {me?.expiresAt
                ? t('pages.access.expiresOn', { date: dayjs(me.expiresAt).format('YYYY-MM-DD') })
                : t('pages.access.neverExpires')}
            </Typography.Paragraph>
          </Card>
        </Col>
        <Col xs={24} lg={12}>
          <Card
            size="small"
            title={t('pages.access.myUsers')}
            extra={
              <Button
                type="primary"
                icon={<TeamOutlined aria-hidden />}
                onClick={() => navigate('/clients')}
              >
                {t('menu.clients')}
              </Button>
            }
          >
            <Row gutter={12}>
              <Col span={8}>
                <Statistic
                  title={t('pages.access.users')}
                  value={me?.ownedUsers ?? 0}
                  suffix={me?.maxUsers ? `/ ${me.maxUsers}` : undefined}
                />
              </Col>
              <Col span={8}>
                <Statistic title={t('pages.access.online')} value={summary?.onlineCount ?? 0} />
              </Col>
              <Col span={8}>
                <Statistic title={t('pages.access.depleted')} value={summary?.depletedCount ?? 0} />
              </Col>
            </Row>
          </Card>
        </Col>
      </Row>
    </PageShell>
  );
}
