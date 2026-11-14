import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Empty, Flex, Modal, Space, Spin, Table, Tag, Typography } from 'antd';
import type { TableColumnsType } from 'antd';

import { HttpUtil } from '@/utils';
import { keys } from '@/api/queryKeys';
import type {
  WireGuardImportable,
  WireGuardImportResult,
  WireGuardImportSources,
} from '@/generated/types';

const JSON_HEADERS = {
  headers: { 'Content-Type': 'application/json' },
} as const;

async function fetchImportable(): Promise<WireGuardImportSources> {
  const msg = await HttpUtil.get<WireGuardImportSources>(
    '/panel/api/inbounds/wireguard/importable',
    undefined,
    { silent: true },
  );
  if (!msg?.success || !msg.obj) throw new Error(msg?.msg || 'Failed to list WireGuard interfaces');
  return msg.obj;
}

interface ImportOutcome {
  name: string;
  result?: WireGuardImportResult;
  error?: string;
}

interface ImportWireguardModalProps {
  open: boolean;
  onClose: () => void;
  onImported: () => void | Promise<void>;
}

export default function ImportWireguardModal({
  open,
  onClose,
  onImported,
}: ImportWireguardModalProps) {
  const { t } = useTranslation();
  return (
    <Modal
      open={open}
      width={760}
      title={t('pages.inbounds.wireguard.importTitle')}
      footer={null}
      onCancel={onClose}
      destroyOnHidden
    >
      <ImportWireguardBody onClose={onClose} onImported={onImported} />
    </Modal>
  );
}

// Mounted only while the dialog is open, so every opening starts from what the server has now.
function ImportWireguardBody({
  onClose,
  onImported,
}: Pick<ImportWireguardModalProps, 'onClose' | 'onImported'>) {
  const { t } = useTranslation();
  const [selected, setSelected] = useState<string[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [outcomes, setOutcomes] = useState<ImportOutcome[]>([]);
  // Never cached: the list is what /etc/wireguard holds at this moment.
  const sourcesQuery = useQuery({
    queryKey: keys.wireguard.importable(),
    queryFn: fetchImportable,
    gcTime: 0,
    staleTime: 0,
  });
  const sources = sourcesQuery.data ?? null;

  async function submit() {
    setSubmitting(true);
    try {
      // One at a time: each import restarts an interface, and its result reads better in order.
      const done: ImportOutcome[] = [];
      for (const name of selected) {
        const msg = await HttpUtil.post<WireGuardImportResult>(
          '/panel/api/inbounds/wireguard/import',
          { interface: name, dashboardDb: sources?.dashboardDb ?? '' },
          { ...JSON_HEADERS, silent: true },
        );
        done.push(
          msg?.success && msg.obj ? { name, result: msg.obj } : { name, error: msg?.msg || '' },
        );
      }
      setOutcomes(done);
      setSelected([]);
      if (done.some((outcome) => outcome.result)) await onImported();
      await sourcesQuery.refetch();
    } finally {
      setSubmitting(false);
    }
  }

  const columns: TableColumnsType<WireGuardImportable> = [
    {
      title: t('pages.inbounds.wireguard.interface'),
      dataIndex: 'name',
      render: (name: string, row) => (
        <Space orientation="vertical" size={0}>
          <Typography.Text strong>{name}</Typography.Text>
          {row.problem && <Typography.Text type="danger">{row.problem}</Typography.Text>}
        </Space>
      ),
    },
    {
      title: t('pages.inbounds.port'),
      dataIndex: 'port',
      render: (port: number) => port || '',
    },
    { title: t('pages.inbounds.address'), dataIndex: 'address' },
    { title: t('pages.inbounds.wireguard.importPeers'), dataIndex: 'peers' },
    {
      title: t('pages.inbounds.wireguard.importRestricted'),
      dataIndex: 'restricted',
      render: (restricted: number) => restricted || '',
    },
    {
      title: t('status'),
      dataIndex: 'up',
      render: (up: boolean) => (
        <Tag color={up ? 'green' : 'default'}>
          {t(
            up
              ? 'pages.inbounds.wireguard.importRunning'
              : 'pages.inbounds.wireguard.importStopped',
          )}
        </Tag>
      ),
    },
  ];

  const interfaces = sources?.interfaces ?? [];

  return (
    <>
      <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
        <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
          {t('pages.inbounds.wireguard.importIntro')}
        </Typography.Paragraph>
        {outcomes.map((outcome) =>
          outcome.result ? (
            <Alert
              key={outcome.name}
              type="success"
              showIcon
              title={t('pages.inbounds.wireguard.importDone', {
                name: outcome.name,
                clients: outcome.result.clients,
                disabled: outcome.result.disabled,
                withoutKey: outcome.result.withoutKey,
              })}
              description={
                <>
                  {outcome.result.withoutKey > 0 && (
                    <div>{t('pages.inbounds.wireguard.importWithoutKeyHint')}</div>
                  )}
                  {(outcome.result.skipped ?? []).length > 0 && (
                    <div>
                      {t('pages.inbounds.wireguard.importSkipped', {
                        list: (outcome.result.skipped ?? []).join('; '),
                      })}
                    </div>
                  )}
                  {(outcome.result.dropped ?? []).length > 0 && (
                    <div>
                      {t('pages.inbounds.wireguard.importDropped', {
                        list: (outcome.result.dropped ?? []).join(', '),
                      })}
                    </div>
                  )}
                </>
              }
            />
          ) : (
            <Alert
              key={outcome.name}
              type="error"
              showIcon
              title={`${outcome.name}: ${outcome.error}`}
            />
          ),
        )}
        {sourcesQuery.isError && (
          <Alert type="error" showIcon title={(sourcesQuery.error as Error).message} />
        )}
        {sources === null ? (
          !sourcesQuery.isError && <Spin />
        ) : (
          <>
            {!sources.toolsInstalled && (
              <Alert
                type="error"
                showIcon
                title={t('pages.inbounds.wireguard.importToolsMissing')}
              />
            )}
            {sources.dashboardRunning && (
              <Alert
                type="warning"
                showIcon
                title={t('pages.inbounds.wireguard.importDashboardRunning')}
              />
            )}
            {sources.dashboardDb && (
              <Typography.Text type="secondary">
                {t('pages.inbounds.wireguard.importDashboardDb', {
                  path: sources.dashboardDb,
                })}
              </Typography.Text>
            )}
            {interfaces.length === 0 ? (
              <Empty description={t('pages.inbounds.wireguard.importNone')} />
            ) : (
              <Table<WireGuardImportable>
                size="small"
                rowKey="name"
                pagination={false}
                columns={columns}
                dataSource={interfaces}
                rowSelection={{
                  selectedRowKeys: selected,
                  onChange: (keys) => setSelected(keys.map(String)),
                  getCheckboxProps: (row) => ({
                    disabled: !!row.problem || !sources.toolsInstalled,
                  }),
                }}
              />
            )}
          </>
        )}
      </Space>
      <Flex justify="flex-end" gap="small" style={{ marginTop: 16 }}>
        <Button onClick={onClose}>{t('close')}</Button>
        <Button
          type="primary"
          disabled={selected.length === 0}
          loading={submitting}
          onClick={submit}
        >
          {t('pages.inbounds.import')}
        </Button>
      </Flex>
    </>
  );
}
