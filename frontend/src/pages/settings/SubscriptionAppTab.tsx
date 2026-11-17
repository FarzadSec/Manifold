import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Input, Switch, Table } from 'antd';
import { SaveOutlined } from '@ant-design/icons';

import { useAppLocations } from '@/api/queries/useAppLocations';
import type { AppLocation } from '@/generated/types';
import type { AllSetting } from '@/models/setting';
import { SettingListItem } from '@/components/ui';

interface SubscriptionAppTabProps {
  allSetting: AllSetting;
  updateSetting: (patch: Partial<AllSetting>) => void;
}

export default function SubscriptionAppTab({ allSetting, updateSetting }: SubscriptionAppTabProps) {
  const { t } = useTranslation();
  const { locations, loading, fetchError, save } = useAppLocations();
  const [edits, setEdits] = useState<Record<number, Partial<AppLocation>>>({});

  function edit(nodeId: number, patch: Partial<AppLocation>) {
    setEdits((prev) => ({ ...prev, [nodeId]: { ...prev[nodeId], ...patch } }));
  }

  async function saveRow(loc: AppLocation) {
    const msg = await save({ ...loc, ...edits[loc.nodeId] });
    if (msg?.success) setEdits(({ [loc.nodeId]: _saved, ...rest }) => rest);
  }

  function field(key: 'name' | 'countryCode' | 'city' | 'timezone', placeholder: string) {
    return (_: unknown, loc: AppLocation) => (
      <Input
        aria-label={`${t(`pages.settings.app.${key}`)} ${loc.nodeId}`}
        value={edits[loc.nodeId]?.[key] ?? loc[key]}
        placeholder={placeholder}
        maxLength={key === 'countryCode' ? 2 : 64}
        onChange={(e) => edit(loc.nodeId, { [key]: e.target.value })}
      />
    );
  }

  const columns = [
    {
      title: t('pages.settings.app.server'),
      key: 'server',
      render: (_: unknown, loc: AppLocation) =>
        loc.nodeId === 0
          ? t('pages.settings.app.thisServer')
          : t('pages.settings.app.nodeLabel', { id: loc.nodeId }),
    },
    { title: t('pages.settings.app.name'), key: 'name', render: field('name', 'Germany') },
    {
      title: t('pages.settings.app.countryCode'),
      key: 'countryCode',
      width: 90,
      render: field('countryCode', 'DE'),
    },
    { title: t('pages.settings.app.city'), key: 'city', render: field('city', 'Frankfurt') },
    {
      title: t('pages.settings.app.timezone'),
      key: 'timezone',
      render: field('timezone', 'Europe/Berlin'),
    },
    {
      title: '',
      key: 'save',
      width: 56,
      render: (_: unknown, loc: AppLocation) => (
        <Button
          aria-label={`${t('pages.settings.save')} ${loc.nodeId}`}
          size="small"
          icon={<SaveOutlined aria-hidden />}
          onClick={() => saveRow(loc)}
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
      <Table
        size="small"
        dataSource={locations}
        rowKey={(loc) => loc.nodeId}
        pagination={false}
        loading={loading && locations.length === 0}
        scroll={{ x: true }}
        columns={columns}
      />
    </>
  );
}
