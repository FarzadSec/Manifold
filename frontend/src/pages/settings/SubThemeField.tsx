import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Button,
  Empty,
  Flex,
  Input,
  List,
  Modal,
  Select,
  Spin,
  Tag,
  Typography,
  message,
} from 'antd';
import { AppstoreOutlined } from '@ant-design/icons';

import { keys } from '@/api/queryKeys';
import type { CatalogTheme, Theme } from '@/generated/types';
import { HttpUtil } from '@/utils';

interface SubThemeFieldProps {
  value: string;
  onChange: (id: string) => void;
  catalogURL: string;
  onCatalogURLChange: (url: string) => void;
}

const INSTALLED = 'installed:';

// localized picks a catalogue text in the panel's language, English when it has none.
function localized(texts: Record<string, string> | undefined, lang: string): string {
  if (!texts) return '';
  return texts[lang.slice(0, 2)] || texts.en || Object.values(texts)[0] || '';
}

export default function SubThemeField({
  value,
  onChange,
  catalogURL,
  onCatalogURLChange,
}: SubThemeFieldProps) {
  const { t, i18n } = useTranslation();
  const [messageApi, messageContextHolder] = message.useMessage();
  const queryClient = useQueryClient();
  const [catalogOpen, setCatalogOpen] = useState(false);
  const [busy, setBusy] = useState('');

  const themes = useQuery({
    queryKey: keys.settings.subThemes(),
    queryFn: async () => {
      const msg = await HttpUtil.get<Theme[]>('/panel/api/setting/subThemes', undefined, {
        silent: true,
      });
      return msg?.success && Array.isArray(msg.obj) ? msg.obj : [];
    },
  });
  const catalog = useQuery({
    queryKey: keys.settings.subThemeCatalog(),
    enabled: catalogOpen,
    queryFn: async () => {
      const msg = await HttpUtil.get<CatalogTheme[]>(
        '/panel/api/setting/subThemes/catalog',
        undefined,
        {
          silentSuccess: true,
        },
      );
      if (!msg?.success) throw new Error(msg?.msg || 'catalogue');
      return Array.isArray(msg.obj) ? msg.obj : [];
    },
  });

  const refresh = () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: keys.settings.subThemes() }),
      queryClient.invalidateQueries({ queryKey: keys.settings.subThemeCatalog() }),
    ]);

  async function install(name: string) {
    setBusy(name);
    try {
      const msg = await HttpUtil.post<Theme>(
        '/panel/api/setting/subThemes/install',
        { name },
        { silentSuccess: true },
      );
      if (msg?.success) {
        messageApi.success(t('pages.settings.subThemeInstalled', { name }));
        await refresh();
      }
    } finally {
      setBusy('');
    }
  }

  async function remove(name: string) {
    setBusy(name);
    try {
      const msg = await HttpUtil.post(
        '/panel/api/setting/subThemes/remove',
        { name },
        { silentSuccess: true },
      );
      if (msg?.success) {
        if (value === INSTALLED + name) onChange('');
        await refresh();
      }
    } finally {
      setBusy('');
    }
  }

  const options = [
    { value: '', label: t('pages.settings.subThemePanel') },
    ...(themes.data ?? []).map((theme) => ({
      value: theme.id,
      label: theme.builtin
        ? theme.title
        : `${theme.title} · ${t('pages.settings.subThemeInstalledTag')}`,
    })),
  ];
  if (value && !options.some((o) => o.value === value)) {
    options.push({ value, label: `${value} · ${t('pages.settings.subThemeMissing')}` });
  }

  return (
    <Flex gap={8} wrap>
      {messageContextHolder}
      <Select
        aria-label={t('pages.settings.subTheme')}
        style={{ minWidth: 220, flex: 1 }}
        value={value}
        loading={themes.isLoading}
        options={options}
        onChange={onChange}
      />
      <Button icon={<AppstoreOutlined aria-hidden />} onClick={() => setCatalogOpen(true)}>
        {t('pages.settings.subThemeCatalog')}
      </Button>
      <Modal
        open={catalogOpen}
        title={t('pages.settings.subThemeCatalog')}
        footer={null}
        width={640}
        onCancel={() => setCatalogOpen(false)}
        destroyOnHidden
      >
        <Flex vertical gap={12}>
          <div>
            <label htmlFor="sub-theme-catalog-url">
              <Typography.Text type="secondary">
                {t('pages.settings.subThemeCatalogURL')}
              </Typography.Text>
            </label>
            <Input
              id="sub-theme-catalog-url"
              value={catalogURL}
              placeholder="https://raw.githubusercontent.com/FarzadSec/Manifold/main/sub-themes/catalog.json"
              onChange={(e) => onCatalogURLChange(e.target.value)}
            />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {t('pages.settings.subThemeCatalogURLDesc')}
            </Typography.Text>
          </div>
          {catalog.isLoading ? (
            <Flex justify="center" style={{ padding: 24 }}>
              <Spin aria-live="off" />
            </Flex>
          ) : catalog.isError ? (
            <Empty description={t('pages.settings.subThemeCatalogFailed')} />
          ) : (
            <List
              dataSource={catalog.data ?? []}
              locale={{
                emptyText: <Empty description={t('pages.settings.subThemeCatalogEmpty')} />,
              }}
              renderItem={(theme) => {
                const installed = theme.installed !== undefined && theme.installed !== '';
                const outdated = installed && theme.installed !== theme.version;
                return (
                  <List.Item
                    actions={[
                      <Button
                        key="install"
                        type={installed && !outdated ? 'default' : 'primary'}
                        size="small"
                        loading={busy === theme.name}
                        disabled={installed && !outdated}
                        onClick={() => install(theme.name)}
                      >
                        {outdated
                          ? t('pages.settings.subThemeUpdate')
                          : installed
                            ? t('pages.settings.subThemeInstalledTag')
                            : t('pages.settings.subThemeInstall')}
                      </Button>,
                      ...(installed
                        ? [
                            <Button
                              key="remove"
                              size="small"
                              danger
                              disabled={busy === theme.name}
                              onClick={() => remove(theme.name)}
                            >
                              {t('pages.settings.subThemeRemove')}
                            </Button>,
                          ]
                        : []),
                    ]}
                  >
                    <List.Item.Meta
                      title={
                        <Flex gap={6} align="center" wrap>
                          <span>{localized(theme.title, i18n.language) || theme.name}</span>
                          <Tag>{theme.version}</Tag>
                          {theme.author && (
                            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                              {theme.author}
                            </Typography.Text>
                          )}
                        </Flex>
                      }
                      description={localized(theme.description, i18n.language)}
                    />
                  </List.Item>
                );
              }}
            />
          )}
        </Flex>
      </Modal>
    </Flex>
  );
}
