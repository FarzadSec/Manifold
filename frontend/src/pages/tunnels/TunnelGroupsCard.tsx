import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Button,
  Card,
  Col,
  Form,
  Input,
  Modal,
  Popconfirm,
  Row,
  Segmented,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
} from 'antd';
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons';

import { keys } from '@/api/queryKeys';
import { selectTagRender } from '@/components/form';
import type { PortForward, TunnelGroupView, TunnelView } from '@/generated/types';
import { HttpUtil } from '@/utils';
import { L2TP_FORWARDS, activeMembers, groupCandidates, keptForwards } from './groupMembers';

interface GroupForm {
  id: number;
  name: string;
  mode: 'failover' | 'balance';
  members: number[];
  backups: number[];
  sticky: boolean;
  forwards: PortForward[];
  enable: boolean;
}

const EMPTY: GroupForm = {
  id: 0,
  name: '',
  mode: 'failover',
  members: [],
  backups: [],
  sticky: false,
  forwards: [{ listen: '', target: '', proto: 'tcp' }],
  enable: true,
};

export default function TunnelGroupsCard({ tunnels }: { tunnels: TunnelView[] }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [form, setForm] = useState<GroupForm | null>(null);
  const [saving, setSaving] = useState(false);
  const groups = useQuery({
    queryKey: keys.tunnels.groups(),
    queryFn: async () => {
      const msg = await HttpUtil.get<TunnelGroupView[]>('/panel/api/tunnels/groups', undefined, {
        silent: true,
      });
      return msg?.success && Array.isArray(msg.obj) ? msg.obj : [];
    },
  });
  const candidates = groupCandidates(tunnels);
  if (candidates.length < 2 && (groups.data?.length ?? 0) === 0) return null;
  const nameOf = (id: number) => tunnels.find((x) => x.id === id)?.name ?? `#${id}`;
  const refresh = () => queryClient.invalidateQueries({ queryKey: keys.tunnels.groups() });

  async function save() {
    if (!form) return;
    setSaving(true);
    try {
      const msg = await HttpUtil.post<TunnelGroupView>(
        '/panel/api/tunnels/groups/save',
        { ...form, forwards: keptForwards(form.forwards) },
        { headers: { 'Content-Type': 'application/json' } },
      );
      if (msg?.success) {
        setForm(null);
        await refresh();
      }
    } finally {
      setSaving(false);
    }
  }

  const setForward = (i: number, patch: Partial<PortForward>) =>
    form &&
    setForm({ ...form, forwards: form.forwards.map((f, j) => (j === i ? { ...f, ...patch } : f)) });

  return (
    <Card
      size="small"
      style={{ marginTop: 16 }}
      title={t('pages.tunnels.groups.title')}
      extra={
        <Button
          icon={<PlusOutlined aria-hidden />}
          disabled={candidates.length < 2}
          onClick={() => setForm({ ...EMPTY, forwards: [...EMPTY.forwards] })}
        >
          {t('pages.tunnels.groups.add')}
        </Button>
      }
    >
      <Typography.Paragraph type="secondary">
        {t('pages.tunnels.groups.intro')}
      </Typography.Paragraph>
      <Table<TunnelGroupView>
        rowKey="id"
        size="small"
        pagination={false}
        dataSource={groups.data ?? []}
        locale={{ emptyText: t('pages.tunnels.groups.empty') }}
        scroll={{ x: 'max-content' }}
        columns={[
          {
            title: t('pages.tunnels.groups.name'),
            key: 'name',
            render: (_v, g) => (
              <Space size={4}>
                <span>{g.name}</span>
                {!g.enable && <Tag>{t('disabled')}</Tag>}
              </Space>
            ),
          },
          {
            title: t('pages.tunnels.groups.mode'),
            key: 'mode',
            render: (_v, g) => <Tag color="blue">{t(`pages.tunnels.groups.${g.mode}`)}</Tag>,
          },
          {
            title: t('pages.tunnels.groups.members'),
            key: 'members',
            render: (_v, g) => {
              const backups = g.backups ?? [];
              const active = activeMembers(g.mode, g.members, backups, tunnels);
              return (
                <Space size={[4, 4]} wrap>
                  {g.members.map((id) => (
                    <Tag key={id} color={active.includes(id) ? 'green' : undefined}>
                      {nameOf(id)}
                    </Tag>
                  ))}
                  {backups.map((id) => (
                    <Tag key={id} color={active.includes(id) ? 'green' : undefined}>
                      {nameOf(id)} · {t('pages.tunnels.groups.backup')}
                    </Tag>
                  ))}
                  {g.sticky && <Tag color="purple">{t('pages.tunnels.groups.stickyTag')}</Tag>}
                </Space>
              );
            },
          },
          {
            title: t('pages.tunnels.forwards'),
            key: 'forwards',
            render: (_v, g) =>
              g.forwards
                .map((f) => `${f.listen}${f.target ? `→${f.target}` : ''}/${f.proto}`)
                .join(', '),
          },
          {
            key: 'actions',
            render: (_v, g) => (
              <Space>
                <Button
                  type="text"
                  icon={<EditOutlined aria-hidden />}
                  aria-label={t('edit')}
                  onClick={() =>
                    setForm({
                      id: g.id,
                      name: g.name,
                      mode: g.mode === 'balance' ? 'balance' : 'failover',
                      members: g.members,
                      backups: g.backups ?? [],
                      sticky: g.sticky ?? false,
                      forwards: g.forwards.length ? g.forwards : [...EMPTY.forwards],
                      enable: g.enable,
                    })
                  }
                />
                <Popconfirm
                  title={t('pages.tunnels.groups.confirmDelete')}
                  onConfirm={async () => {
                    const msg = await HttpUtil.post(`/panel/api/tunnels/groups/del/${g.id}`);
                    if (msg?.success) await refresh();
                  }}
                >
                  <Button
                    type="text"
                    danger
                    icon={<DeleteOutlined aria-hidden />}
                    aria-label={t('delete')}
                  />
                </Popconfirm>
              </Space>
            ),
          },
        ]}
      />
      <Modal
        open={!!form}
        title={form?.id ? t('pages.tunnels.groups.edit') : t('pages.tunnels.groups.add')}
        okText={t('save')}
        confirmLoading={saving}
        onOk={() => void save()}
        onCancel={() => setForm(null)}
        width={620}
        destroyOnHidden
      >
        {form && (
          <Form layout="vertical" component="div">
            <Row gutter={12}>
              <Col xs={24} md={16}>
                <Form.Item label={t('pages.tunnels.groups.name')} htmlFor="tg-name">
                  <Input
                    id="tg-name"
                    value={form.name}
                    onChange={(e) => setForm({ ...form, name: e.target.value })}
                  />
                </Form.Item>
              </Col>
              <Col xs={24} md={8}>
                <Form.Item label={t('enable')} htmlFor="tg-enable">
                  <Switch
                    id="tg-enable"
                    checked={form.enable}
                    onChange={(v) => setForm({ ...form, enable: v })}
                  />
                </Form.Item>
              </Col>
            </Row>
            <Form.Item label={t('pages.tunnels.groups.mode')} htmlFor="tg-mode">
              <Segmented
                id="tg-mode"
                value={form.mode}
                onChange={(v) => setForm({ ...form, mode: v as GroupForm['mode'] })}
                options={[
                  { value: 'failover', label: t('pages.tunnels.groups.failover') },
                  { value: 'balance', label: t('pages.tunnels.groups.balance') },
                ]}
              />
            </Form.Item>
            <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginTop: -12 }}>
              {t(`pages.tunnels.groups.${form.mode}Hint`)}
            </Typography.Paragraph>
            <Form.Item
              label={t('pages.tunnels.groups.members')}
              htmlFor="tg-members"
              tooltip={t('pages.tunnels.groups.membersHint')}
            >
              <Select
                id="tg-members"
                mode="multiple"
                tagRender={selectTagRender}
                value={form.members}
                onChange={(members) =>
                  setForm({
                    ...form,
                    members,
                    backups: form.backups.filter((id) => !members.includes(id)),
                  })
                }
                options={candidates.map((c) => ({
                  value: c.id,
                  label: `${c.name} · ${c.settings.remoteAddr || c.kind}`,
                }))}
              />
            </Form.Item>
            <Form.Item
              label={t('pages.tunnels.groups.backups')}
              htmlFor="tg-backups"
              tooltip={t('pages.tunnels.groups.backupsHint')}
            >
              <Select
                id="tg-backups"
                mode="multiple"
                tagRender={selectTagRender}
                value={form.backups}
                onChange={(backups) => setForm({ ...form, backups })}
                options={candidates
                  .filter((c) => !form.members.includes(c.id))
                  .map((c) => ({
                    value: c.id,
                    label: `${c.name} · ${c.settings.remoteAddr || c.kind}`,
                  }))}
              />
            </Form.Item>
            {form.mode === 'balance' && (
              <Form.Item
                label={t('pages.tunnels.groups.sticky')}
                htmlFor="tg-sticky"
                tooltip={t('pages.tunnels.groups.stickyHint')}
              >
                <Switch
                  id="tg-sticky"
                  checked={form.sticky}
                  onChange={(sticky) => setForm({ ...form, sticky })}
                />
              </Form.Item>
            )}
            <Typography.Text strong>{t('pages.tunnels.forwards')}</Typography.Text>
            {form.forwards.map((f, i) => (
              <Row gutter={8} key={i} align="middle" style={{ marginTop: 8 }}>
                <Col span={8}>
                  <Input
                    aria-label={t('pages.tunnels.forwardListen')}
                    placeholder="443"
                    disabled={f.proto === 'esp'}
                    value={f.listen}
                    onChange={(e) => setForward(i, { listen: e.target.value })}
                  />
                </Col>
                <Col span={8}>
                  <Input
                    aria-label={t('pages.tunnels.forwardTarget')}
                    placeholder={t('pages.tunnels.forwardSame')}
                    disabled={f.proto === 'esp'}
                    value={f.target ?? ''}
                    onChange={(e) => setForward(i, { target: e.target.value })}
                  />
                </Col>
                <Col span={6}>
                  <Select
                    aria-label={t('pages.tunnels.forwardProto')}
                    style={{ width: '100%' }}
                    value={f.proto || 'tcp'}
                    onChange={(proto) =>
                      setForward(i, proto === 'esp' ? { proto, listen: '', target: '' } : { proto })
                    }
                    options={[
                      { label: 'TCP', value: 'tcp' },
                      { label: 'UDP', value: 'udp' },
                      { label: 'TCP + UDP', value: 'both' },
                      { label: 'ESP (IPsec)', value: 'esp' },
                    ]}
                  />
                </Col>
                <Col span={2}>
                  <Button
                    type="text"
                    danger
                    aria-label={t('remove')}
                    icon={<DeleteOutlined aria-hidden />}
                    onClick={() =>
                      setForm({ ...form, forwards: form.forwards.filter((_, j) => j !== i) })
                    }
                  />
                </Col>
              </Row>
            ))}
            <Button
              style={{ marginTop: 8 }}
              icon={<PlusOutlined aria-hidden />}
              onClick={() =>
                setForm({
                  ...form,
                  forwards: [...form.forwards, { listen: '', target: '', proto: 'tcp' }],
                })
              }
            >
              {t('pages.tunnels.addForward')}
            </Button>
            <Button
              style={{ marginTop: 8, marginInlineStart: 8 }}
              onClick={() =>
                setForm({
                  ...form,
                  sticky: form.mode === 'balance' ? true : form.sticky,
                  forwards: [
                    ...keptForwards(form.forwards),
                    ...L2TP_FORWARDS.filter(
                      (p) =>
                        !form.forwards.some((f) => f.proto === p.proto && f.listen === p.listen),
                    ),
                  ],
                })
              }
            >
              {t('pages.tunnels.l2tpPreset')}
            </Button>
            <Typography.Paragraph type="secondary" style={{ marginTop: 8 }}>
              {t('pages.tunnels.l2tpPresetHint')}
            </Typography.Paragraph>
          </Form>
        )}
      </Modal>
    </Card>
  );
}
