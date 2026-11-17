import { useEffect, useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQueryClient } from '@tanstack/react-query';
import {
  Button,
  Card,
  Col,
  Divider,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Row,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons';
import { FormProvider, useForm, useWatch } from 'react-hook-form';

import { FormField } from '@/components/form/rhf';
import { keys } from '@/api/queryKeys';
import { postJson, useAccess, usePlans, useRoles } from '@/api/queries/useAccess';
import type { Plan, Role } from '@/generated/types';
import PageShell from './PageShell';
import PermissionPicker from './PermissionPicker';
import SellsFromFields from './SellsFromFields';
import { TIERS, TIER_COLORS, boundRange, bytesAsGB, formatBytes } from './accessInfo';

function emptyRole(): Role {
  return {
    id: 0,
    name: '',
    description: '',
    tier: 'operator',
    permissions: [],
    usersReadScope: '',
    usersWriteScope: '',
    maxUsersPerAccount: 0,
    minUserBytes: 0,
    maxUserBytes: 0,
    minPlanDays: 0,
    maxPlanDays: 0,
    minDevicesPerUser: 0,
    maxDevicesPerUser: 0,
    plansOnly: false,
    allowedPlanIds: [],
    inboundsLimited: false,
    allowedInboundIds: [],
    usersOffWhenDisabled: false,
    keepUsersWhenLimited: false,
    lockWhenLimited: false,
    createdAt: 0,
  };
}

function RoleFormModal({
  open,
  role,
  ceilings,
  plans,
  onClose,
}: {
  open: boolean;
  role: Role | null;
  ceilings: Record<string, string[]>;
  plans: Plan[];
  onClose: (saved: boolean) => void;
}) {
  const { t } = useTranslation();
  const methods = useForm<Role>({ defaultValues: emptyRole() });
  const { control, reset, handleSubmit, setValue, getValues } = methods;
  const tier = useWatch({ control, name: 'tier' });
  const readScope = useWatch({ control, name: 'usersReadScope' }) || 'all';
  const writeScope = useWatch({ control, name: 'usersWriteScope' }) || 'all';
  const uid = useId();
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (open) {
      reset(
        role
          ? {
              ...role,
              permissions: role.permissions ?? [],
              allowedPlanIds: role.allowedPlanIds ?? [],
              allowedInboundIds: role.allowedInboundIds ?? [],
            }
          : emptyRole(),
      );
    }
  }, [open, role, reset]);

  async function onSubmit(values: Role) {
    setSaving(true);
    try {
      const url = role
        ? `/panel/api/access/roles/update/${role.id}`
        : '/panel/api/access/roles/add';
      const msg = await postJson(url, values);
      if (msg.success) onClose(true);
    } finally {
      setSaving(false);
    }
  }

  const ceiling = ceilings[tier] ?? [];
  return (
    <Modal
      open={open}
      width={760}
      title={role ? t('pages.access.editRole') : t('pages.access.addRole')}
      okText={t('save')}
      confirmLoading={saving}
      onOk={handleSubmit(onSubmit)}
      onCancel={() => onClose(false)}
      destroyOnHidden
    >
      <FormProvider {...methods}>
        <Form layout="vertical">
          <Row gutter={12}>
            <Col xs={24} md={12}>
              <FormField name="name" label={t('pages.access.name')} rules={{ required: true }}>
                <Input maxLength={64} />
              </FormField>
            </Col>
            <Col xs={24} md={12}>
              <FormField
                name="tier"
                label={t('pages.access.basedOn')}
                tooltip={t('pages.access.basedOnHint')}
                onAfterChange={(next) => {
                  const allowed = ceilings[String(next)] ?? [];
                  setValue(
                    'permissions',
                    (getValues('permissions') ?? []).filter((p) => allowed.includes(p)),
                  );
                }}
              >
                <Select
                  options={TIERS.map((v) => ({ label: t(`pages.access.tier.${v}`), value: v }))}
                />
              </FormField>
            </Col>
            {tier !== 'reseller' && (
              <Col xs={24}>
                <Form.Item
                  label={t('pages.access.usersScope')}
                  htmlFor={`${uid}-scope`}
                  tooltip={t('pages.access.usersScopeHint')}
                >
                  <Select
                    id={`${uid}-scope`}
                    value={`${readScope}|${writeScope}`}
                    onChange={(v: string) => {
                      const [read, write] = v.split('|');
                      setValue('usersReadScope', read);
                      setValue('usersWriteScope', write);
                    }}
                    options={[
                      { value: 'all|all', label: t('pages.access.scope.all') },
                      { value: 'all|own', label: t('pages.access.scope.readAll') },
                      { value: 'own|own', label: t('pages.access.scope.own') },
                    ]}
                  />
                </Form.Item>
              </Col>
            )}
            <Col xs={24}>
              <FormField name="description" label={t('pages.access.about')}>
                <Input maxLength={200} />
              </FormField>
            </Col>
          </Row>
          <Divider titlePlacement="start">{t('pages.access.permissions')}</Divider>
          <FormField name="permissions">
            <PermissionPicker ceiling={ceiling} />
          </FormField>
          <Divider titlePlacement="start">{t('pages.access.limits')}</Divider>
          <Typography.Paragraph type="secondary">
            {t('pages.access.limitsHint')}
          </Typography.Paragraph>
          <Row gutter={12}>
            {tier === 'reseller' && (
              <Col xs={24} md={8}>
                <FormField name="maxUsersPerAccount" label={t('pages.access.maxUsersPerAccount')}>
                  <InputNumber min={0} style={{ width: '100%' }} />
                </FormField>
              </Col>
            )}
          </Row>
          <Row gutter={12}>
            <Col xs={12} md={8}>
              <FormField
                name="minUserBytes"
                label={t('pages.access.minUserGB')}
                transform={bytesAsGB}
              >
                <InputNumber min={0} step={10} style={{ width: '100%' }} addonAfter="GB" />
              </FormField>
            </Col>
            <Col xs={12} md={8}>
              <FormField name="minPlanDays" label={t('pages.access.minPlanDays')}>
                <InputNumber min={0} style={{ width: '100%' }} />
              </FormField>
            </Col>
            <Col xs={12} md={8}>
              <FormField name="minDevicesPerUser" label={t('pages.access.minDevicesPerUser')}>
                <InputNumber min={0} style={{ width: '100%' }} />
              </FormField>
            </Col>
            <Col xs={12} md={8}>
              <FormField
                name="maxUserBytes"
                label={t('pages.access.maxUserGB')}
                transform={bytesAsGB}
              >
                <InputNumber min={0} step={10} style={{ width: '100%' }} addonAfter="GB" />
              </FormField>
            </Col>
            <Col xs={12} md={8}>
              <FormField name="maxPlanDays" label={t('pages.access.maxPlanDays')}>
                <InputNumber min={0} style={{ width: '100%' }} />
              </FormField>
            </Col>
            <Col xs={12} md={8}>
              <FormField name="maxDevicesPerUser" label={t('pages.access.maxDevicesPerUser')}>
                <InputNumber min={0} style={{ width: '100%' }} />
              </FormField>
            </Col>
          </Row>
          <Row gutter={12}>
            <SellsFromFields plans={plans} />
          </Row>
          <Row gutter={12}>
            <Col xs={24} md={tier === 'reseller' ? 8 : 24}>
              <FormField
                name="usersOffWhenDisabled"
                label={t('pages.access.usersOffWhenDisabled')}
                valueProp="checked"
              >
                <Switch />
              </FormField>
            </Col>
            {tier === 'reseller' && (
              <>
                <Col xs={24} md={8}>
                  <FormField
                    name="keepUsersWhenLimited"
                    label={t('pages.access.keepUsersWhenLimited')}
                    valueProp="checked"
                  >
                    <Switch />
                  </FormField>
                </Col>
                <Col xs={24} md={8}>
                  <FormField
                    name="lockWhenLimited"
                    label={t('pages.access.lockWhenLimited')}
                    valueProp="checked"
                  >
                    <Switch />
                  </FormField>
                </Col>
              </>
            )}
          </Row>
        </Form>
      </FormProvider>
    </Modal>
  );
}

export default function RolesPage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const access = useAccess();
  const roles = useRoles();
  const plans = usePlans();
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<Role | null>(null);
  const ceilings = access.me?.tierCeilings ?? {};

  const refresh = () => queryClient.invalidateQueries({ queryKey: keys.access.roles() });

  const builtIn = TIERS.map((tier) => ({
    key: `builtin-${tier}`,
    name: t(`pages.access.tier.${tier}`),
    tier,
    count: (ceilings[tier] ?? []).length,
  }));

  const columns: ColumnsType<Role> = [
    {
      title: t('pages.access.name'),
      key: 'name',
      render: (_, r) => (
        <Space direction="vertical" size={0}>
          <Typography.Text strong>{r.name}</Typography.Text>
          {r.description && <Typography.Text type="secondary">{r.description}</Typography.Text>}
        </Space>
      ),
    },
    {
      title: t('pages.access.basedOn'),
      key: 'tier',
      render: (_, r) => <Tag color={TIER_COLORS[r.tier]}>{t(`pages.access.tier.${r.tier}`)}</Tag>,
    },
    {
      title: t('pages.access.permissions'),
      key: 'perms',
      render: (_, r) => `${(r.permissions ?? []).length} / ${(ceilings[r.tier] ?? []).length}`,
    },
    {
      title: t('pages.access.limits'),
      key: 'limits',
      render: (_, r) => (
        <Space size={4} wrap>
          {r.tier !== 'reseller' && r.usersWriteScope === 'own' && (
            <Tag color="purple">
              {t(
                r.usersReadScope === 'own'
                  ? 'pages.access.scope.own'
                  : 'pages.access.scope.readAll',
              )}
            </Tag>
          )}
          {r.maxUsersPerAccount > 0 && (
            <Tag>{t('pages.access.usersCap', { n: r.maxUsersPerAccount })}</Tag>
          )}
          {r.plansOnly && <Tag color="gold">{t('pages.access.plansOnly')}</Tag>}
          {r.inboundsLimited && <Tag color="gold">{t('pages.access.inboundsLimited')}</Tag>}
          {boundRange(r.minUserBytes, r.maxUserBytes) && (
            <Tag>
              {t('pages.access.bound.quota', {
                range: boundRange(r.minUserBytes, r.maxUserBytes, formatBytes),
              })}
            </Tag>
          )}
          {boundRange(r.minPlanDays, r.maxPlanDays) && (
            <Tag>
              {t('pages.access.bound.days', { range: boundRange(r.minPlanDays, r.maxPlanDays) })}
            </Tag>
          )}
          {boundRange(r.minDevicesPerUser, r.maxDevicesPerUser) && (
            <Tag>
              {t('pages.access.bound.devices', {
                range: boundRange(r.minDevicesPerUser, r.maxDevicesPerUser),
              })}
            </Tag>
          )}
        </Space>
      ),
    },
    {
      title: t('pages.access.actions'),
      key: 'actions',
      render: (_, r) => (
        <Space size={2}>
          <Button
            type="text"
            aria-label={t('edit')}
            icon={<EditOutlined aria-hidden />}
            onClick={() => {
              setEditing(r);
              setFormOpen(true);
            }}
          />
          <Popconfirm
            title={t('pages.access.deleteRoleConfirm')}
            onConfirm={async () => {
              const msg = await postJson(`/panel/api/access/roles/del/${r.id}`);
              if (msg.success) void refresh();
            }}
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
  ];

  return (
    <PageShell className="access-page">
      <Card
        size="small"
        title={t('menu.roles')}
        extra={
          <Button
            type="primary"
            icon={<PlusOutlined aria-hidden />}
            onClick={() => {
              setEditing(null);
              setFormOpen(true);
            }}
          >
            {t('pages.access.addRole')}
          </Button>
        }
      >
        <Typography.Paragraph type="secondary">{t('pages.access.rolesIntro')}</Typography.Paragraph>
        <Typography.Title level={5}>{t('pages.access.builtInRoles')}</Typography.Title>
        <Table
          rowKey="key"
          size="small"
          pagination={false}
          dataSource={builtIn}
          style={{ marginBottom: 16 }}
          columns={[
            {
              title: t('pages.access.name'),
              key: 'name',
              render: (_, r) => (
                <Space>
                  <Typography.Text strong>{r.name}</Typography.Text>
                  <Tag>{t('pages.access.builtIn')}</Tag>
                </Space>
              ),
            },
            {
              title: t('pages.access.basedOn'),
              key: 'tier',
              render: (_, r) => (
                <Tag color={TIER_COLORS[r.tier]}>{t(`pages.access.tier.${r.tier}`)}</Tag>
              ),
            },
            { title: t('pages.access.permissions'), key: 'count', render: (_, r) => r.count },
          ]}
        />
        <Typography.Title level={5}>{t('pages.access.customRoles')}</Typography.Title>
        <Table
          rowKey="id"
          size="small"
          loading={roles.isLoading}
          columns={columns}
          dataSource={roles.data ?? []}
          pagination={false}
          scroll={{ x: 'max-content' }}
          locale={{ emptyText: t('pages.access.noCustomRoles') }}
        />
      </Card>
      <RoleFormModal
        open={formOpen}
        role={editing}
        ceilings={ceilings}
        plans={plans.data ?? []}
        onClose={(saved) => {
          setFormOpen(false);
          if (saved) void refresh();
        }}
      />
    </PageShell>
  );
}
