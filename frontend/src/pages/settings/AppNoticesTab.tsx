import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Input, Popconfirm, Radio, Space, Table, Tag, Typography } from 'antd';
import { DeleteOutlined, SendOutlined } from '@ant-design/icons';

import { useAppNotices, type AppNoticeLevel } from '@/api/queries/useAppNotices';
import type { AppNotice } from '@/generated/types';
import { IntlUtil } from '@/utils';

const LEVELS: AppNoticeLevel[] = ['info', 'warning', 'critical'];
const LEVEL_COLORS: Record<string, string> = { info: 'blue', warning: 'gold', critical: 'red' };
const MAX_LENGTH = 1000;

// The app always shows notices right to left, so they are written and listed that way too.
const persianText = { direction: 'rtl', textAlign: 'right' } as const;

export default function AppNoticesTab() {
  const { t } = useTranslation();
  const { notices, loading, fetchError, publishing, publish, remove } = useAppNotices();
  const [body, setBody] = useState('');
  const [level, setLevel] = useState<AppNoticeLevel>('info');
  const levelLabel = (value: string) => t(`pages.settings.app.notices.levels.${value}`);

  async function submit() {
    const msg = await publish({ body: body.trim(), level });
    if (msg?.success) {
      setBody('');
      setLevel('info');
    }
  }

  const columns = [
    {
      title: t('pages.settings.app.notices.level'),
      key: 'level',
      width: 110,
      render: (_: unknown, n: AppNotice) => (
        <Tag color={LEVEL_COLORS[n.level]}>{levelLabel(n.level)}</Tag>
      ),
    },
    {
      title: t('pages.settings.app.notices.text'),
      key: 'body',
      render: (_: unknown, n: AppNotice) => (
        <div dir="rtl" lang="fa" style={{ ...persianText, whiteSpace: 'pre-wrap' }}>
          {n.body}
        </div>
      ),
    },
    {
      title: t('pages.settings.app.notices.publishedAt'),
      key: 'createdAt',
      width: 180,
      render: (_: unknown, n: AppNotice) => IntlUtil.formatDate(n.createdAt),
    },
    {
      title: '',
      key: 'actions',
      width: 56,
      render: (_: unknown, n: AppNotice) => (
        <Popconfirm
          title={t('pages.settings.app.notices.deleteConfirm')}
          onConfirm={() => remove(n.id)}
        >
          <Button
            size="small"
            danger
            icon={<DeleteOutlined aria-hidden />}
            aria-label={t('delete')}
          />
        </Popconfirm>
      ),
    },
  ];

  return (
    <>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        title={t('pages.settings.app.notices.desc')}
      />
      {fetchError && (
        <Alert type="error" showIcon style={{ marginBottom: 16 }} title={fetchError} />
      )}
      <Space direction="vertical" size={8} style={{ width: '100%', marginBottom: 12 }}>
        <Input.TextArea
          id="app-notice-text"
          aria-label={t('pages.settings.app.notices.text')}
          placeholder={t('pages.settings.app.notices.text')}
          dir="rtl"
          lang="fa"
          style={persianText}
          autoSize={{ minRows: 3, maxRows: 8 }}
          maxLength={MAX_LENGTH}
          showCount
          value={body}
          onChange={(e) => setBody(e.target.value)}
        />
        <div style={{ display: 'flex', gap: 12, alignItems: 'center', flexWrap: 'wrap' }}>
          <Typography.Text>{t('pages.settings.app.notices.level')}</Typography.Text>
          <Radio.Group
            optionType="button"
            buttonStyle="solid"
            value={level}
            onChange={(e) => setLevel(e.target.value as AppNoticeLevel)}
            options={LEVELS.map((value) => ({ value, label: levelLabel(value) }))}
          />
          <Typography.Text type="secondary" style={{ flex: 1, minWidth: 200 }}>
            {t(`pages.settings.app.notices.levelHint.${level}`)}
          </Typography.Text>
          <Button
            type="primary"
            icon={<SendOutlined aria-hidden />}
            loading={publishing}
            disabled={body.trim() === ''}
            onClick={submit}
          >
            {t('pages.settings.app.notices.publish')}
          </Button>
        </div>
      </Space>
      <Table
        size="small"
        dataSource={notices}
        rowKey="id"
        pagination={{ pageSize: 10, hideOnSinglePage: true }}
        loading={loading && notices.length === 0}
        scroll={{ x: true }}
        columns={columns}
        locale={{ emptyText: t('pages.settings.app.notices.empty') }}
      />
    </>
  );
}
