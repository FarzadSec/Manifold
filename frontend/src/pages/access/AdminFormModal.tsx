import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Col, Divider, Form, Input, InputNumber, Modal, Row, Select, Switch } from 'antd';
import { FormProvider, useForm, useWatch } from 'react-hook-form';
import dayjs, { type Dayjs } from 'dayjs';

import { FormField } from '@/components/form/rhf';
import DateTimePicker from '@/components/form/DateTimePicker';
import type { AdminInput, AdminView, Plan, Role } from '@/generated/types';
import { postJson } from '@/api/queries/useAccess';
import { TIERS, WEEKDAYS, bytesAsGB } from './accessInfo';
import PermissionOverrides, { overrideChoices } from './PermissionOverrides';
import SellsFromFields from './SellsFromFields';

interface AdminFormModalProps {
  open: boolean;
  admin: AdminView | null;
  roles: Role[];
  plans: Plan[];
  ceilings: Record<string, string[]>;
  onClose: (saved: boolean) => void;
}

function emptyInput(): AdminInput {
  return {
    username: '',
    password: '',
    tier: 'reseller',
    roleId: 0,
    remark: '',
    volumeBytes: 0,
    resetCycle: 'monthly',
    resetDay: 1,
    resetMonth: 1,
    expiresAt: 0,
    maxUsers: 0,
    mayEditUsers: true,
    plansOnly: false,
    allowedPlanIds: [],
    inboundsLimited: false,
    allowedInboundIds: [],
    grantedPermissions: [],
    deniedPermissions: [],
    subDomain: '',
    brandTitle: '',
    brandSupportUrl: '',
    brandAnnounce: '',
  };
}

function fromView(a: AdminView): AdminInput {
  return {
    ...emptyInput(),
    ...a,
    password: '',
    allowedPlanIds: a.allowedPlanIds ?? [],
    allowedInboundIds: a.allowedInboundIds ?? [],
    grantedPermissions: a.grantedPermissions ?? [],
    deniedPermissions: a.deniedPermissions ?? [],
  };
}

const msAsDay = {
  input: (v: unknown) => (Number(v) > 0 ? dayjs(Number(v)) : null),
  output: (v: unknown) => (v ? (v as Dayjs).valueOf() : 0),
};

export default function AdminFormModal({
  open,
  admin,
  roles,
  plans,
  ceilings,
  onClose,
}: AdminFormModalProps) {
  const { t } = useTranslation();
  const methods = useForm<AdminInput>({ defaultValues: emptyInput() });
  const { control, reset, handleSubmit } = methods;
  const tier = useWatch({ control, name: 'tier' });
  const cycle = useWatch({ control, name: 'resetCycle' });
  const roleId = useWatch({ control, name: 'roleId' });
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (open) reset(admin ? fromView(admin) : emptyInput());
  }, [open, admin, reset]);

  const roleOptions = useMemo(
    () => [
      { label: t(`pages.access.tier.${tier}`), value: 0 },
      ...roles.filter((r) => r.tier === tier).map((r) => ({ label: r.name, value: r.id })),
    ],
    [roles, tier, t],
  );

  const ceiling = ceilings[tier] ?? [];
  const { grantable, deniable } = overrideChoices(
    ceiling,
    roles.find((r) => r.id === roleId)?.permissions ?? ceiling,
  );

  async function onSubmit(input: AdminInput) {
    const values = {
      ...input,
      grantedPermissions: (input.grantedPermissions ?? []).filter((p) => grantable.includes(p)),
      deniedPermissions: (input.deniedPermissions ?? []).filter((p) => deniable.includes(p)),
    };
    setSaving(true);
    try {
      const url = admin
        ? `/panel/api/access/admins/update/${admin.id}`
        : '/panel/api/access/admins/add';
      const msg = await postJson(url, values);
      if (msg.success) onClose(true);
    } finally {
      setSaving(false);
    }
  }

  const reseller = tier === 'reseller';
  return (
    <Modal
      open={open}
      width={780}
      title={admin ? t('pages.access.editAdmin') : t('pages.access.addAdmin')}
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
              <FormField name="username" label={t('username')} rules={{ required: true }}>
                <Input maxLength={64} autoComplete="off" />
              </FormField>
            </Col>
            <Col xs={24} md={12}>
              <FormField
                name="password"
                label={t('password')}
                extra={admin ? t('pages.access.passwordKeep') : t('pages.access.passwordMin')}
                rules={admin ? undefined : { required: true, minLength: 8 }}
              >
                <Input.Password autoComplete="new-password" />
              </FormField>
            </Col>
            <Col xs={24} md={12}>
              <FormField
                name="tier"
                label={t('pages.access.tierLabel')}
                tooltip={t('pages.access.tierHint')}
                onAfterChange={() => methods.setValue('roleId', 0)}
              >
                <Select
                  disabled={!!admin?.isOwner}
                  options={TIERS.map((v) => ({ label: t(`pages.access.tier.${v}`), value: v }))}
                />
              </FormField>
            </Col>
            <Col xs={24} md={12}>
              <FormField name="roleId" label={t('pages.access.role')}>
                <Select options={roleOptions} />
              </FormField>
            </Col>
            <Col xs={24}>
              <FormField name="remark" label={t('pages.access.remark')}>
                <Input maxLength={200} />
              </FormField>
            </Col>
          </Row>

          {!admin?.isOwner && (
            <>
              <Divider titlePlacement="start">{t('pages.access.overrides')}</Divider>
              <PermissionOverrides grantable={grantable} deniable={deniable} />
            </>
          )}

          {reseller && (
            <>
              <Divider titlePlacement="start">{t('pages.access.allowance')}</Divider>
              <Row gutter={12}>
                <Col xs={24} md={8}>
                  <FormField
                    name="volumeBytes"
                    label={t('pages.access.volumeGB')}
                    tooltip={t('pages.access.volumeHint')}
                    transform={bytesAsGB}
                  >
                    <InputNumber min={0} step={10} style={{ width: '100%' }} addonAfter="GB" />
                  </FormField>
                </Col>
                <Col xs={24} md={8}>
                  <FormField name="resetCycle" label={t('pages.access.resetCycle')}>
                    <Select
                      options={['none', 'weekly', 'monthly', 'yearly'].map((v) => ({
                        label: t(`pages.access.cycle.${v}`),
                        value: v,
                      }))}
                    />
                  </FormField>
                </Col>
                <Col xs={24} md={8}>
                  {cycle === 'weekly' && (
                    <FormField name="resetDay" label={t('pages.access.resetsOn')}>
                      <Select
                        options={WEEKDAYS.map((d, i) => ({
                          label: t(`pages.access.weekday.${d}`),
                          value: i + 1,
                        }))}
                      />
                    </FormField>
                  )}
                  {(cycle === 'monthly' || cycle === 'yearly') && (
                    <Row gutter={8}>
                      {cycle === 'yearly' && (
                        <Col span={12}>
                          <FormField name="resetMonth" label={t('pages.access.month')}>
                            <InputNumber min={1} max={12} style={{ width: '100%' }} />
                          </FormField>
                        </Col>
                      )}
                      <Col span={cycle === 'yearly' ? 12 : 24}>
                        <FormField name="resetDay" label={t('pages.access.day')}>
                          <InputNumber min={1} max={31} style={{ width: '100%' }} />
                        </FormField>
                      </Col>
                    </Row>
                  )}
                </Col>
                <Col xs={24} md={12}>
                  <FormField
                    name="expiresAt"
                    label={t('pages.access.accountExpiry')}
                    tooltip={t('pages.access.accountExpiryHint')}
                    transform={msAsDay}
                  >
                    <DateTimePicker value={null} onChange={() => undefined} />
                  </FormField>
                </Col>
                <Col xs={24} md={12}>
                  <FormField
                    name="maxUsers"
                    label={t('pages.access.maxUsers')}
                    tooltip={t('pages.access.zeroUnlimited')}
                  >
                    <InputNumber min={0} style={{ width: '100%' }} />
                  </FormField>
                </Col>
              </Row>

              <Divider titlePlacement="start">{t('pages.access.whatItMayDo')}</Divider>
              <Row gutter={12}>
                <Col xs={24} md={12}>
                  <FormField
                    name="mayEditUsers"
                    label={t('pages.access.mayEditUsers')}
                    valueProp="checked"
                    tooltip={t('pages.access.mayEditUsersHint')}
                  >
                    <Switch />
                  </FormField>
                </Col>
                <SellsFromFields plans={plans} />
              </Row>

              <Divider titlePlacement="start">{t('pages.access.branding')}</Divider>
              <Alert
                type="info"
                showIcon
                style={{ marginBottom: 12 }}
                title={t('pages.access.brandingHint')}
              />
              <Row gutter={12}>
                <Col xs={24} md={12}>
                  <FormField name="brandTitle" label={t('pages.access.brandTitle')}>
                    <Input maxLength={100} />
                  </FormField>
                </Col>
                <Col xs={24} md={12}>
                  <FormField name="brandSupportUrl" label={t('pages.access.brandSupportUrl')}>
                    <Input placeholder="https://t.me/support" />
                  </FormField>
                </Col>
                <Col xs={24} md={12}>
                  <FormField name="brandAnnounce" label={t('pages.access.brandAnnounce')}>
                    <Input maxLength={200} />
                  </FormField>
                </Col>
                <Col xs={24} md={12}>
                  <FormField
                    name="subDomain"
                    label={t('pages.access.subDomain')}
                    tooltip={t('pages.access.subDomainHint')}
                  >
                    <Input placeholder="sub.reseller.com" />
                  </FormField>
                </Col>
              </Row>
            </>
          )}
        </Form>
      </FormProvider>
    </Modal>
  );
}
