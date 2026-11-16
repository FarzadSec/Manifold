import { useEffect, useState } from 'react';
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
import { selectTagRender } from '@/components/form';
import { keys } from '@/api/queryKeys';
import { postJson, usePlans } from '@/api/queries/useAccess';
import { useInboundOptions } from '@/api/queries/useInboundOptions';
import type { Plan } from '@/generated/types';
import PageShell from './PageShell';
import { GIB, formatBytes } from './accessInfo';

function emptyPlan(): Plan {
  return {
    id: 0,
    name: '',
    sortOrder: 0,
    totalBytes: 0,
    days: 30,
    startOnFirstUse: false,
    limitIp: 0,
    limitHwid: 0,
    reset: 0,
    trafficReset: 'never',
    trafficResetDay: 1,
    allInbounds: true,
    inboundIds: [],
    group: '',
    namePrefix: '',
    nameSuffix: '',
    description: '',
    createdAt: 0,
  };
}

const bytesAsGB = {
  input: (v: unknown) => (Number(v) || 0) / GIB,
  output: (v: unknown) => Math.round((Number(v) || 0) * GIB),
};

function PlanFormModal({
  open,
  plan,
  onClose,
}: {
  open: boolean;
  plan: Plan | null;
  onClose: (saved: boolean) => void;
}) {
  const { t } = useTranslation();
  const methods = useForm<Plan>({ defaultValues: emptyPlan() });
  const { control, reset, handleSubmit } = methods;
  const allInbounds = useWatch({ control, name: 'allInbounds' });
  const trafficReset = useWatch({ control, name: 'trafficReset' });
  const { data: inbounds = [] } = useInboundOptions();
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (open) reset(plan ? { ...plan, inboundIds: plan.inboundIds ?? [] } : emptyPlan());
  }, [open, plan, reset]);

  async function onSubmit(values: Plan) {
    setSaving(true);
    try {
      const url = plan
        ? `/panel/api/access/plans/update/${plan.id}`
        : '/panel/api/access/plans/add';
      const msg = await postJson(url, values);
      if (msg.success) onClose(true);
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      open={open}
      width={760}
      title={plan ? t('pages.access.editPlan') : t('pages.access.addPlan')}
      okText={t('save')}
      confirmLoading={saving}
      onOk={handleSubmit(onSubmit)}
      onCancel={() => onClose(false)}
      destroyOnHidden
    >
      <FormProvider {...methods}>
        <Form layout="vertical">
          <Row gutter={12}>
            <Col xs={24} md={16}>
              <FormField name="name" label={t('pages.access.name')} rules={{ required: true }}>
                <Input maxLength={64} placeholder="30 days · 50 GB" />
              </FormField>
            </Col>
            <Col xs={24} md={8}>
              <FormField name="sortOrder" label={t('pages.access.order')}>
                <InputNumber style={{ width: '100%' }} />
              </FormField>
            </Col>
            <Col xs={24} md={8}>
              <FormField
                name="totalBytes"
                label={t('pages.access.volumeGB')}
                tooltip={t('pages.access.zeroUnlimited')}
                transform={bytesAsGB}
              >
                <InputNumber min={0} style={{ width: '100%' }} addonAfter="GB" />
              </FormField>
            </Col>
            <Col xs={24} md={8}>
              <FormField
                name="days"
                label={t('pages.access.lengthDays')}
                tooltip={t('pages.access.zeroNever')}
              >
                <InputNumber min={0} style={{ width: '100%' }} />
              </FormField>
            </Col>
            <Col xs={24} md={8}>
              <FormField
                name="startOnFirstUse"
                label={t('pages.access.startOnFirstUse')}
                valueProp="checked"
                tooltip={t('pages.access.startOnFirstUseHint')}
              >
                <Switch />
              </FormField>
            </Col>
            <Col xs={24} md={8}>
              <FormField
                name="limitIp"
                label={t('pages.access.addressLimit')}
                tooltip={t('pages.access.zeroUnlimited')}
              >
                <InputNumber min={0} style={{ width: '100%' }} />
              </FormField>
            </Col>
            <Col xs={24} md={8}>
              <FormField
                name="limitHwid"
                label={t('pages.access.deviceLimit')}
                tooltip={t('pages.access.zeroUnlimited')}
              >
                <InputNumber min={0} style={{ width: '100%' }} />
              </FormField>
            </Col>
          </Row>
          <Divider titlePlacement="start">{t('pages.access.autoReset')}</Divider>
          <Row gutter={12}>
            <Col xs={24} md={8}>
              <FormField name="trafficReset" label={t('pages.access.trafficReset')}>
                <Select
                  options={['never', 'daily', 'weekly', 'monthly'].map((v) => ({
                    label: t(`pages.access.resetEvery.${v}`),
                    value: v,
                  }))}
                />
              </FormField>
            </Col>
            {trafficReset === 'monthly' && (
              <Col xs={24} md={8}>
                <FormField name="trafficResetDay" label={t('pages.access.day')}>
                  <InputNumber min={1} max={31} style={{ width: '100%' }} />
                </FormField>
              </Col>
            )}
            <Col xs={24} md={8}>
              <FormField
                name="reset"
                label={t('pages.access.renewEveryDays')}
                tooltip={t('pages.access.renewEveryDaysHint')}
              >
                <InputNumber min={0} style={{ width: '100%' }} />
              </FormField>
            </Col>
          </Row>
          <Divider titlePlacement="start">{t('pages.access.where')}</Divider>
          <FormField name="allInbounds" label={t('pages.access.allInbounds')} valueProp="checked">
            <Switch />
          </FormField>
          {!allInbounds && (
            <FormField name="inboundIds" label={t('pages.access.inbounds')}>
              <Select
                mode="multiple"
                tagRender={selectTagRender}
                options={inbounds.map((ib) => ({
                  label: `${ib.remark || ib.tag} · ${ib.protocol}:${ib.port}`,
                  value: ib.id,
                }))}
              />
            </FormField>
          )}
          <Row gutter={12}>
            <Col xs={24} md={8}>
              <FormField name="group" label={t('pages.access.group')}>
                <Input maxLength={64} />
              </FormField>
            </Col>
            <Col xs={24} md={8}>
              <FormField name="namePrefix" label={t('pages.access.namePrefix')}>
                <Input maxLength={32} placeholder="r1_" />
              </FormField>
            </Col>
            <Col xs={24} md={8}>
              <FormField name="nameSuffix" label={t('pages.access.nameSuffix')}>
                <Input maxLength={32} />
              </FormField>
            </Col>
            <Col xs={24}>
              <FormField name="description" label={t('pages.access.about')}>
                <Input.TextArea maxLength={500} autoSize={{ minRows: 2, maxRows: 5 }} />
              </FormField>
            </Col>
          </Row>
        </Form>
      </FormProvider>
    </Modal>
  );
}

export default function PlansPage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const plans = usePlans();
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<Plan | null>(null);
  const refresh = () => queryClient.invalidateQueries({ queryKey: keys.access.plans() });

  const columns: ColumnsType<Plan> = [
    {
      title: t('pages.access.name'),
      key: 'name',
      render: (_, p) => (
        <Space direction="vertical" size={0}>
          <Typography.Text strong>{p.name}</Typography.Text>
          {p.description && <Typography.Text type="secondary">{p.description}</Typography.Text>}
        </Space>
      ),
    },
    {
      title: t('pages.access.volume'),
      key: 'volume',
      render: (_, p) =>
        p.totalBytes > 0 ? formatBytes(p.totalBytes) : t('pages.access.unlimited'),
    },
    {
      title: t('pages.access.length'),
      key: 'length',
      render: (_, p) =>
        p.days > 0 ? (
          <Space size={4}>
            {t('pages.access.daysCount', { n: p.days })}
            {p.startOnFirstUse && <Tag color="cyan">{t('pages.access.fromFirstUse')}</Tag>}
          </Space>
        ) : (
          t('pages.access.neverExpires')
        ),
    },
    {
      title: t('pages.access.limits'),
      key: 'limits',
      render: (_, p) => (
        <Space size={4} wrap>
          {p.limitIp > 0 && <Tag>{t('pages.access.addressesCap', { n: p.limitIp })}</Tag>}
          {p.limitHwid > 0 && <Tag>{t('pages.access.devicesCap', { n: p.limitHwid })}</Tag>}
          {p.trafficReset !== 'never' && (
            <Tag>{t(`pages.access.resetEvery.${p.trafficReset}`)}</Tag>
          )}
        </Space>
      ),
    },
    {
      title: t('pages.access.inbounds'),
      key: 'inbounds',
      render: (_, p) =>
        p.allInbounds ? t('pages.access.allInbounds') : (p.inboundIds ?? []).length,
    },
    {
      title: t('pages.access.actions'),
      key: 'actions',
      render: (_, p) => (
        <Space size={2}>
          <Button
            type="text"
            aria-label={t('edit')}
            icon={<EditOutlined aria-hidden />}
            onClick={() => {
              setEditing(p);
              setFormOpen(true);
            }}
          />
          <Popconfirm
            title={t('pages.access.deletePlanConfirm')}
            onConfirm={async () => {
              const msg = await postJson(`/panel/api/access/plans/del/${p.id}`);
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
        title={t('menu.plans')}
        extra={
          <Button
            type="primary"
            icon={<PlusOutlined aria-hidden />}
            onClick={() => {
              setEditing(null);
              setFormOpen(true);
            }}
          >
            {t('pages.access.addPlan')}
          </Button>
        }
      >
        <Typography.Paragraph type="secondary">{t('pages.access.plansIntro')}</Typography.Paragraph>
        <Table
          rowKey="id"
          size="small"
          loading={plans.isLoading}
          columns={columns}
          dataSource={plans.data ?? []}
          pagination={false}
          scroll={{ x: 'max-content' }}
          locale={{ emptyText: t('pages.access.noPlans') }}
        />
      </Card>
      <PlanFormModal
        open={formOpen}
        plan={editing}
        onClose={(saved) => {
          setFormOpen(false);
          if (saved) void refresh();
        }}
      />
    </PageShell>
  );
}
