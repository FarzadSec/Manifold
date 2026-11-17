import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router';
import { useQueryClient } from '@tanstack/react-query';
import {
  Button,
  Card,
  Popconfirm,
  Progress,
  Result,
  Space,
  Spin,
  Table,
  Tag,
  Tooltip,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  CrownOutlined,
  DeleteOutlined,
  EditOutlined,
  LogoutOutlined,
  PlusOutlined,
  SafetyOutlined,
  SwapOutlined,
} from '@ant-design/icons';
import dayjs from 'dayjs';

import { keys } from '@/api/queryKeys';
import { postJson, useAccess, useAdmins, usePlans, useRoles } from '@/api/queries/useAccess';
import type { AdminView } from '@/generated/types';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import PageShell from './PageShell';
import AdminFormModal from './AdminFormModal';
import { TIER_COLORS, formatBytes } from './accessInfo';

function Allowance({ admin }: { admin: AdminView }) {
  const { t } = useTranslation();
  if (admin.tier !== 'reseller') return <Typography.Text type="secondary">—</Typography.Text>;
  const percent =
    admin.volumeBytes > 0 ? Math.min(100, (admin.cycleUsed / admin.volumeBytes) * 100) : 0;
  return (
    <Space direction="vertical" size={2} style={{ minWidth: 180 }}>
      <Typography.Text>
        {formatBytes(admin.cycleUsed)} /{' '}
        {admin.volumeBytes > 0 ? formatBytes(admin.volumeBytes) : t('pages.access.unlimited')}
      </Typography.Text>
      {admin.volumeBytes > 0 && (
        <Progress
          percent={Math.round(percent)}
          size="small"
          status={admin.suspended === 'volume' ? 'exception' : 'normal'}
          showInfo={false}
        />
      )}
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        {admin.expiresAt > 0
          ? t('pages.access.expiresOn', { date: dayjs(admin.expiresAt).format('YYYY-MM-DD') })
          : t('pages.access.neverExpires')}
      </Typography.Text>
      {admin.suspended && <Tag color="red">{t(`pages.access.suspended.${admin.suspended}`)}</Tag>}
    </Space>
  );
}

export default function AdminsPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { isMobile } = useMediaQuery();
  const queryClient = useQueryClient();
  const access = useAccess();
  const admins = useAdmins();
  const roles = useRoles();
  const plans = usePlans();
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<AdminView | null>(null);

  const refresh = () => queryClient.invalidateQueries({ queryKey: keys.access.root() });

  async function act(url: string) {
    const msg = await postJson(url);
    if (msg.success) void refresh();
  }

  const columns: ColumnsType<AdminView> = [
    {
      title: t('username'),
      key: 'username',
      render: (_, a) => (
        <Space direction="vertical" size={0}>
          <Space size={6}>
            {a.isOwner && (
              <Tooltip title={t('pages.access.owner')}>
                <CrownOutlined style={{ color: '#d4a106' }} aria-label={t('pages.access.owner')} />
              </Tooltip>
            )}
            <Typography.Text strong>{a.username}</Typography.Text>
            {a.twoFactorEnabled && (
              <Tag icon={<SafetyOutlined aria-hidden />} color="green">
                2FA
              </Tag>
            )}
          </Space>
          {a.remark && (
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {a.remark}
            </Typography.Text>
          )}
        </Space>
      ),
    },
    {
      title: t('pages.access.role'),
      key: 'role',
      render: (_, a) => (
        <Space size={4} wrap>
          <Tag color={TIER_COLORS[a.tier]}>
            {a.roleId ? a.roleName : t(`pages.access.tier.${a.tier}`)}
          </Tag>
          {a.disabled && <Tag color="red">{t('pages.access.disabled')}</Tag>}
          {(a.grantedPermissions?.length ?? 0) + (a.deniedPermissions?.length ?? 0) > 0 && (
            <Tag>
              {t('pages.access.overridden', {
                added: a.grantedPermissions?.length ?? 0,
                removed: a.deniedPermissions?.length ?? 0,
              })}
            </Tag>
          )}
        </Space>
      ),
    },
    {
      title: t('pages.access.users'),
      key: 'users',
      render: (_, a) => (
        <Button type="link" size="small" onClick={() => navigate(`/clients?owner=${a.id}`)}>
          {a.ownedUsers}
          {a.tier === 'reseller' && a.maxUsers > 0 ? ` / ${a.maxUsers}` : ''}
        </Button>
      ),
    },
    {
      title: t('pages.access.allowance'),
      key: 'allowance',
      render: (_, a) => <Allowance admin={a} />,
    },
    {
      title: t('pages.access.lastLogin'),
      key: 'login',
      render: (_, a) =>
        a.lastLoginAt ? (
          <Space direction="vertical" size={0}>
            <Typography.Text>{dayjs(a.lastLoginAt).format('YYYY-MM-DD HH:mm')}</Typography.Text>
            <Typography.Text type="secondary" code style={{ fontSize: 12 }}>
              {a.lastLoginIp}
            </Typography.Text>
          </Space>
        ) : (
          <Typography.Text type="secondary">{t('pages.access.never')}</Typography.Text>
        ),
    },
    {
      title: t('pages.access.actions'),
      key: 'actions',
      render: (_, a) => (
        <Space size={2} wrap>
          <Tooltip title={t('edit')}>
            <Button
              type="text"
              aria-label={t('edit')}
              icon={<EditOutlined aria-hidden />}
              onClick={() => {
                setEditing(a);
                setFormOpen(true);
              }}
            />
          </Tooltip>
          <Popconfirm
            title={t('pages.access.endSessionsConfirm')}
            onConfirm={() => void act(`/panel/api/access/admins/endSessions/${a.id}`)}
          >
            <Tooltip title={t('pages.access.endSessions')}>
              <Button
                type="text"
                aria-label={t('pages.access.endSessions')}
                icon={<LogoutOutlined aria-hidden />}
              />
            </Tooltip>
          </Popconfirm>
          {access.me?.isOwner && !a.isOwner && a.tier === 'admin' && (
            <Popconfirm
              title={t('pages.access.handoverConfirm', { name: a.username })}
              onConfirm={() => void act(`/panel/api/access/admins/handover/${a.id}`)}
            >
              <Tooltip title={t('pages.access.handover')}>
                <Button
                  type="text"
                  aria-label={t('pages.access.handover')}
                  icon={<SwapOutlined aria-hidden />}
                />
              </Tooltip>
            </Popconfirm>
          )}
          {!a.isOwner && a.id !== access.me?.id && (
            <Popconfirm
              title={t('pages.access.deleteAdminConfirm')}
              onConfirm={() => void act(`/panel/api/access/admins/del/${a.id}`)}
            >
              <Button
                type="text"
                danger
                aria-label={t('delete')}
                icon={<DeleteOutlined aria-hidden />}
              />
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ];

  return (
    <PageShell className="access-page">
      <Spin spinning={admins.isLoading} delay={200} size="large" aria-live="off">
        {admins.isError ? (
          <Result
            status="error"
            title={t('somethingWentWrong')}
            subTitle={String(admins.error)}
            extra={<Button onClick={() => void admins.refetch()}>{t('refresh')}</Button>}
          />
        ) : (
          <Card
            size="small"
            title={t('menu.admins')}
            extra={
              <Button
                type="primary"
                icon={<PlusOutlined aria-hidden />}
                onClick={() => {
                  setEditing(null);
                  setFormOpen(true);
                }}
              >
                {isMobile ? null : t('pages.access.addAdmin')}
              </Button>
            }
          >
            <Typography.Paragraph type="secondary">
              {t('pages.access.adminsIntro')}
            </Typography.Paragraph>
            <Table
              rowKey="id"
              size="small"
              columns={columns}
              dataSource={admins.data ?? []}
              pagination={false}
              scroll={{ x: 'max-content' }}
            />
          </Card>
        )}
      </Spin>
      <AdminFormModal
        open={formOpen}
        admin={editing}
        roles={roles.data ?? []}
        plans={plans.data ?? []}
        ceilings={access.me?.tierCeilings ?? {}}
        onClose={(saved) => {
          setFormOpen(false);
          if (saved) void refresh();
        }}
      />
    </PageShell>
  );
}
