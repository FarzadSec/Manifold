import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Popconfirm, Select, Space, Switch, Table, Tag, Typography } from 'antd';
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons';

import { useAppLocations } from '@/api/queries/useAppLocations';
import { useInboundOptions } from '@/api/queries/useInboundOptions';
import { useNodesQuery } from '@/api/queries/useNodesQuery';
import type { AppLocation } from '@/generated/types';
import type { AllSetting } from '@/models/setting';
import type { InboundOption } from '@/schemas/client';
import { formatInboundLabel } from '@/lib/inbounds/label';
import { countryFlag } from '@/pages/xray/outbounds/outbounds-tab-helpers';
import { SettingListItem } from '@/components/ui';
import AppLocationModal, { type InboundChoice } from './AppLocationModal';

interface SubscriptionAppTabProps {
  allSetting: AllSetting;
  updateSetting: (patch: Partial<AllSetting>) => void;
}

const NEW_LOCATION: AppLocation = {
  id: 0,
  name: '',
  countryCode: '',
  city: '',
  timezone: '',
  sortOrder: 0,
  inboundIds: [],
};
const NO_INBOUNDS: InboundOption[] = [];

export default function SubscriptionAppTab({ allSetting, updateSetting }: SubscriptionAppTabProps) {
  const { t } = useTranslation();
  const { locations, loading, fetchError, save, remove } = useAppLocations();
  const { data: inbounds = NO_INBOUNDS } = useInboundOptions();
  const { nodes } = useNodesQuery();
  const [editing, setEditing] = useState<AppLocation | null>(null);

  const serverName = (nodeId?: number | null) =>
    nodeId
      ? nodes.find((n) => n.id === nodeId)?.name ||
        t('pages.settings.app.nodeLabel', { id: nodeId })
      : t('pages.settings.app.thisServer');
  const inboundName = (ib: InboundOption) => formatInboundLabel(ib.tag, ib.remark);
  const locationOf = useMemo(() => {
    const out = new Map<number, AppLocation>();
    for (const loc of locations) for (const id of loc.inboundIds) out.set(id, loc);
    return out;
  }, [locations]);

  async function saveLocation(loc: AppLocation): Promise<boolean> {
    const msg = await save(loc);
    return !!msg?.success;
  }

  // Moving an inbound takes it out of the location it served before joining the new one.
  async function assign(inboundId: number, locationId: number) {
    const current = locationOf.get(inboundId);
    if (current?.id === locationId) return;
    if (current) {
      const left = current.inboundIds.filter((id) => id !== inboundId);
      if (!(await saveLocation({ ...current, inboundIds: left }))) return;
    }
    const target = locations.find((loc) => loc.id === locationId);
    if (target) {
      const joined = [...target.inboundIds, inboundId].sort((a, b) => a - b);
      await saveLocation({ ...target, inboundIds: joined });
    }
  }

  const choicesFor = (loc: AppLocation): InboundChoice[] =>
    inbounds.map((ib) => {
      const other = locationOf.get(ib.id);
      return {
        value: ib.id,
        label: `${inboundName(ib)} · ${ib.protocol ?? ''} · ${serverName(ib.nodeId)}`,
        takenBy: other && other.id !== loc.id ? other.name : undefined,
      };
    });

  const locationColumns = [
    {
      title: t('pages.settings.app.location'),
      key: 'name',
      render: (_: unknown, loc: AppLocation) => (
        <Space size={8}>
          <span aria-hidden style={{ fontSize: 18 }}>
            {countryFlag(loc.countryCode)}
          </span>
          <span>
            <Typography.Text strong>{loc.name}</Typography.Text>
            {loc.city && (
              <Typography.Text type="secondary" style={{ display: 'block', fontSize: 12 }}>
                {loc.city}
              </Typography.Text>
            )}
          </span>
        </Space>
      ),
    },
    {
      title: t('pages.settings.app.inbounds'),
      key: 'inbounds',
      render: (_: unknown, loc: AppLocation) =>
        loc.inboundIds.length === 0 ? (
          <Typography.Text type="secondary">{t('pages.settings.app.noInbounds')}</Typography.Text>
        ) : (
          <Space size={[4, 4]} wrap>
            {loc.inboundIds.map((id) => {
              const ib = inbounds.find((row) => row.id === id);
              return (
                <Tag key={id}>{ib ? `${inboundName(ib)} · ${ib.protocol ?? ''}` : `#${id}`}</Tag>
              );
            })}
          </Space>
        ),
    },
    {
      title: t('pages.settings.app.timezone'),
      key: 'timezone',
      dataIndex: 'timezone',
    },
    {
      title: '',
      key: 'actions',
      width: 96,
      render: (_: unknown, loc: AppLocation) => (
        <Space size={4}>
          <Button
            size="small"
            icon={<EditOutlined aria-hidden />}
            aria-label={`${t('edit')} ${loc.name}`}
            onClick={() => setEditing(loc)}
          />
          <Popconfirm
            title={t('pages.settings.app.deleteConfirm')}
            onConfirm={() => remove(loc.id)}
          >
            <Button
              size="small"
              danger
              icon={<DeleteOutlined aria-hidden />}
              aria-label={`${t('delete')} ${loc.name}`}
            />
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const mappingColumns = [
    {
      title: t('pages.settings.app.inbound'),
      key: 'inbound',
      render: (_: unknown, ib: InboundOption) => inboundName(ib),
    },
    {
      title: t('protocol'),
      key: 'protocol',
      render: (_: unknown, ib: InboundOption) => <Tag>{ib.protocol}</Tag>,
    },
    {
      title: t('pages.settings.app.server'),
      key: 'server',
      render: (_: unknown, ib: InboundOption) => serverName(ib.nodeId),
    },
    {
      title: t('pages.settings.app.location'),
      key: 'location',
      width: 220,
      render: (_: unknown, ib: InboundOption) => (
        <Select
          id={`app-location-of-${ib.id}`}
          aria-label={t('pages.settings.app.locationOf', { name: inboundName(ib) })}
          size="small"
          style={{ width: '100%' }}
          value={locationOf.get(ib.id)?.id ?? 0}
          onChange={(locationId: number) => assign(ib.id, locationId)}
          options={[
            { value: 0, label: t('pages.settings.app.noLocation') },
            ...locations.map((loc) => ({
              value: loc.id,
              label: `${countryFlag(loc.countryCode)} ${loc.name}`.trim(),
            })),
          ]}
        />
      ),
    },
  ];

  return (
    <>
      <SettingListItem
        paddings="small"
        title={t('pages.settings.app.enable')}
        description={t('pages.settings.app.enableDesc')}
      >
        <Switch
          aria-label={t('pages.settings.app.enable')}
          checked={allSetting.appEnable}
          onChange={(appEnable) => updateSetting({ appEnable })}
        />
      </SettingListItem>
      <Alert
        type="info"
        showIcon
        style={{ margin: '16px 0' }}
        title={t('pages.settings.app.locationsDesc')}
      />
      {fetchError && (
        <Alert type="error" showIcon style={{ marginBottom: 16 }} title={fetchError} />
      )}
      <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 8 }}>
        <Button
          type="primary"
          icon={<PlusOutlined aria-hidden />}
          onClick={() => setEditing(NEW_LOCATION)}
        >
          {t('pages.settings.app.addLocation')}
        </Button>
      </div>
      <Table
        size="small"
        dataSource={locations}
        rowKey="id"
        pagination={false}
        loading={loading && locations.length === 0}
        scroll={{ x: true }}
        columns={locationColumns}
        locale={{ emptyText: t('pages.settings.app.noLocations') }}
      />
      <Typography.Title level={5} style={{ marginTop: 24 }}>
        {t('pages.settings.app.mapping')}
      </Typography.Title>
      <Typography.Paragraph type="secondary">
        {t('pages.settings.app.mappingDesc')}
      </Typography.Paragraph>
      <Table
        size="small"
        dataSource={inbounds}
        rowKey="id"
        pagination={false}
        scroll={{ x: true }}
        columns={mappingColumns}
      />
      {editing && (
        <AppLocationModal
          location={editing}
          choices={choicesFor(editing)}
          onSave={saveLocation}
          onClose={() => setEditing(null)}
        />
      )}
    </>
  );
}
