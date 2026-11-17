import { useCallback, useEffect, useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Empty,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Spin,
  Switch,
  Tabs,
  Tag,
  message,
} from 'antd';
import { ApiOutlined, SafetyOutlined, UserOutlined } from '@ant-design/icons';
import { ClipboardManager, HttpUtil, IntlUtil, RandomUtil } from '@/utils';
import type { AllSetting } from '@/models/setting';
import { SettingListItem } from '@/components/ui';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import { catTabLabel } from './catTabLabel';
import TwoFactorModal from './TwoFactorModal';
import { onNumber } from '@/utils/onNumber';
import PermissionPicker from '@/pages/access/PermissionPicker';
import { TIERS } from '@/pages/access/accessInfo';
import { useAccess, useAdmins } from '@/api/queries/useAccess';
import './SecurityTab.css';

interface ApiMsg<T = unknown> {
  success?: boolean;
  msg?: string;
  obj?: T;
}

type ApiTokenScope = 'admin' | 'monitor' | 'node-sync' | 'node-admin' | 'tunnel';

interface ApiTokenRow {
  id: number;
  name: string;
  enabled: boolean;
  createdAt: number;
  scope: ApiTokenScope;
  expiresAt: number;
  username?: string;
  tier?: string;
  permissions?: string[];
  rateLimit?: number;
  maxRequests?: number;
  requestCount?: number;
  lastUsedAt?: number;
}

interface SecurityTabProps {
  allSetting: AllSetting;
  updateSetting: (patch: Partial<AllSetting>) => void;
  saveSetting: (payload: Partial<AllSetting> & Record<string, unknown>) => Promise<unknown>;
}

const UNIX_MILLISECONDS_THRESHOLD = 100_000_000_000;

function apiTokenCreatedAtMilliseconds(createdAt: number): number {
  return createdAt < UNIX_MILLISECONDS_THRESHOLD ? createdAt * 1000 : createdAt;
}

type TfaType = 'set' | 'confirm';

interface TfaState {
  open: boolean;
  title: string;
  description: string;
  token: string;
  type: TfaType;
  onConfirm: (success: boolean, code?: string) => void;
}

const TFA_INITIAL: TfaState = {
  open: false,
  title: '',
  description: '',
  token: '',
  type: 'set',
  onConfirm: () => {},
};

export default function SecurityTab({ allSetting, updateSetting, saveSetting }: SecurityTabProps) {
  const { t } = useTranslation();
  const uid = useId();
  const { isMobile } = useMediaQuery();
  const [modal, modalContextHolder] = Modal.useModal();
  const [messageApi, messageContextHolder] = message.useMessage();

  const [tfa, setTfa] = useState<TfaState>(TFA_INITIAL);
  const [user, setUser] = useState({
    oldUsername: '',
    oldPassword: '',
    newUsername: '',
    newPassword: '',
  });
  const [updating, setUpdating] = useState(false);

  const [apiTokens, setApiTokens] = useState<ApiTokenRow[]>([]);
  const [apiTokensLoading, setApiTokensLoading] = useState(true);
  const [createOpen, setCreateOpen] = useState(false);
  const [createName, setCreateName] = useState('');
  const [createScope, setCreateScope] = useState<ApiTokenScope>('admin');
  const access = useAccess();
  const { data: admins = [] } = useAdmins(access.can('admins:write'));
  const [createUserId, setCreateUserId] = useState(0);
  const [createTier, setCreateTier] = useState('');
  const [createPerms, setCreatePerms] = useState<string[] | null>(null);
  const [createRate, setCreateRate] = useState(0);
  const [createDays, setCreateDays] = useState(0);
  const [createMax, setCreateMax] = useState(0);
  const [createDeleteAtMax, setCreateDeleteAtMax] = useState(false);
  const actingTier = admins.find((a) => a.id === createUserId)?.tier ?? access.me?.tier ?? 'admin';
  const tokenTier = createTier || actingTier;
  const [creating, setCreating] = useState(false);
  const [createdToken, setCreatedToken] = useState<{ name: string; token: string } | null>(null);

  const openTfa = useCallback((opts: Omit<TfaState, 'open'>) => {
    setTfa({ ...opts, open: true });
  }, []);

  const onTfaConfirm = useCallback(
    (success: boolean, code?: string) => {
      tfa.onConfirm(success, code);
    },
    [tfa],
  );

  function updateUserField<K extends keyof typeof user>(key: K, value: string) {
    setUser((prev) => ({ ...prev, [key]: value }));
  }

  const sendUpdateUser = useCallback(
    async (twoFactorCode = '') => {
      setUpdating(true);
      try {
        const msg = (await HttpUtil.post('/panel/api/setting/updateUser', {
          ...user,
          twoFactorCode,
        })) as ApiMsg;
        if (msg?.success) {
          await HttpUtil.post('/logout');
          const basePath = window.X_UI_BASE_PATH || '/';
          window.location.replace(basePath);
        }
      } finally {
        setUpdating(false);
      }
    },
    [user],
  );

  function onUpdateUserClick() {
    if (allSetting.twoFactorEnable) {
      openTfa({
        title: t('pages.settings.security.twoFactorModalChangeCredentialsTitle'),
        description: t('pages.settings.security.twoFactorModalChangeCredentialsStep'),
        token: '',
        type: 'confirm',
        onConfirm: (ok: boolean, code?: string) => {
          if (ok) sendUpdateUser(code || '');
        },
      });
    } else {
      sendUpdateUser();
    }
  }

  const fetchApiTokens = useCallback(async () => {
    try {
      const msg = (await HttpUtil.get('/panel/api/setting/apiTokens')) as ApiMsg<ApiTokenRow[]>;
      if (msg?.success) setApiTokens(Array.isArray(msg.obj) ? msg.obj : []);
    } finally {
      setApiTokensLoading(false);
    }
  }, []);

  const loadApiTokens = useCallback(async () => {
    setApiTokensLoading(true);
    await fetchApiTokens();
  }, [fetchApiTokens]);

  useEffect(() => {
    void fetchApiTokens();
  }, [fetchApiTokens]);

  async function copyToken(token: string) {
    if (!token) return;
    const ok = await ClipboardManager.copyText(token);
    if (ok) messageApi.success(t('copySuccess'));
    else messageApi.error(t('copyFail'));
  }

  function openCreateModal() {
    setCreateName('');
    setCreateScope('admin');
    setCreateUserId(0);
    setCreateTier('');
    setCreatePerms(null);
    setCreateRate(0);
    setCreateDays(0);
    setCreateMax(0);
    setCreateDeleteAtMax(false);
    setCreateOpen(true);
  }

  async function confirmCreateToken() {
    const name = createName.trim();
    if (!name) {
      messageApi.error(t('pages.settings.security.apiTokenNameRequired') || 'Name is required');
      return;
    }
    setCreating(true);
    try {
      const msg = (await HttpUtil.post(
        '/panel/api/setting/apiTokens/create',
        {
          name,
          scope: createScope,
          userId: createUserId,
          tier: createTier,
          permissions: createPerms ?? [],
          rateLimit: createRate,
          validityDays: createDays,
          maxRequests: createMax,
          deleteOnLimit: createMax > 0 && createDeleteAtMax,
        },
        { headers: { 'Content-Type': 'application/json' } },
      )) as ApiMsg<{ token?: string }>;
      if (msg?.success) {
        setCreateOpen(false);
        await loadApiTokens();
        if (msg.obj?.token) {
          setCreatedToken({ name, token: msg.obj.token });
        }
      }
    } finally {
      setCreating(false);
    }
  }

  function confirmDeleteToken(row: ApiTokenRow) {
    modal.confirm({
      title: `${t('delete')} "${row.name}"?`,
      content:
        t('pages.settings.security.apiTokenDeleteWarning') ||
        'Any caller using this token will stop authenticating immediately.',
      okText: t('delete'),
      cancelText: t('cancel'),
      okType: 'danger',
      onOk: async () => {
        const msg = (await HttpUtil.post(`/panel/api/setting/apiTokens/delete/${row.id}`, {
          expectedScope: row.scope,
        })) as ApiMsg;
        if (msg?.success) await loadApiTokens();
      },
    });
  }

  async function toggleTokenEnabled(row: ApiTokenRow) {
    const target = !row.enabled;
    const msg = (await HttpUtil.post(`/panel/api/setting/apiTokens/setEnabled/${row.id}`, {
      enabled: target,
      expectedScope: row.scope,
    })) as ApiMsg;
    if (msg?.success) {
      setApiTokens((prev) => prev.map((r) => (r.id === row.id ? { ...r, enabled: target } : r)));
    }
  }

  function formatTokenDate(ts: number): string {
    if (!ts) return '';
    return IntlUtil.formatDate(apiTokenCreatedAtMilliseconds(ts));
  }

  const scopeMeta: Record<ApiTokenScope, { label: string; description: string; color: string }> = {
    admin: {
      label: t('pages.settings.security.apiTokenScopeAdmin'),
      description: t('pages.settings.security.apiTokenScopeAdminDesc'),
      color: 'red',
    },
    monitor: {
      label: t('pages.settings.security.apiTokenScopeMonitor'),
      description: t('pages.settings.security.apiTokenScopeMonitorDesc'),
      color: 'blue',
    },
    'node-sync': {
      label: t('pages.settings.security.apiTokenScopeNodeSync'),
      description: t('pages.settings.security.apiTokenScopeNodeSyncDesc'),
      color: 'gold',
    },
    'node-admin': {
      label: t('pages.settings.security.apiTokenScopeNodeAdmin'),
      description: t('pages.settings.security.apiTokenScopeNodeAdminDesc'),
      color: 'purple',
    },
    tunnel: {
      label: t('pages.settings.security.apiTokenScopeTunnel'),
      description: t('pages.settings.security.apiTokenScopeTunnelDesc'),
      color: 'cyan',
    },
  };

  function toggleTwoFactor() {
    if (!allSetting.twoFactorEnable) {
      const newToken = RandomUtil.randomBase32String();
      openTfa({
        title: t('pages.settings.security.twoFactorModalSetTitle'),
        description: '',
        token: newToken,
        type: 'set',
        onConfirm: (ok: boolean) => {
          if (ok) {
            messageApi.success(t('pages.settings.security.twoFactorModalSetSuccess'));
            updateSetting({ twoFactorToken: newToken, twoFactorEnable: true });
          } else {
            updateSetting({ twoFactorEnable: false });
          }
        },
      });
    } else {
      openTfa({
        title: t('pages.settings.security.twoFactorModalDeleteTitle'),
        description: t('pages.settings.security.twoFactorModalRemoveStep'),
        token: '',
        type: 'confirm',
        onConfirm: async (ok: boolean, code?: string) => {
          if (!ok) return;
          const next = {
            ...allSetting,
            twoFactorEnable: false,
            twoFactorToken: '',
            twoFactorCode: code || '',
          };
          const msg = (await saveSetting(next)) as ApiMsg;
          if (msg?.success) {
            messageApi.success(t('pages.settings.security.twoFactorModalDeleteSuccess'));
            updateSetting({ twoFactorEnable: false, twoFactorToken: '', hasTwoFactorToken: false });
          }
        },
      });
    }
  }

  return (
    <>
      {messageContextHolder}
      {modalContextHolder}
      <Tabs
        defaultActiveKey="1"
        items={[
          {
            key: '1',
            label: catTabLabel(
              <UserOutlined aria-hidden />,
              t('pages.settings.security.admin'),
              isMobile,
            ),
            children: (
              <>
                <SettingListItem paddings="small" title={t('pages.settings.oldUsername')}>
                  <Input
                    value={user.oldUsername}
                    autoComplete="username"
                    onChange={(e) => updateUserField('oldUsername', e.target.value)}
                  />
                </SettingListItem>
                <SettingListItem paddings="small" title={t('pages.settings.currentPassword')}>
                  <Input.Password
                    value={user.oldPassword}
                    autoComplete="current-password"
                    onChange={(e) => updateUserField('oldPassword', e.target.value)}
                  />
                </SettingListItem>
                <SettingListItem paddings="small" title={t('pages.settings.newUsername')}>
                  <Input
                    value={user.newUsername}
                    onChange={(e) => updateUserField('newUsername', e.target.value)}
                  />
                </SettingListItem>
                <SettingListItem paddings="small" title={t('pages.settings.newPassword')}>
                  <Input.Password
                    value={user.newPassword}
                    autoComplete="new-password"
                    onChange={(e) => updateUserField('newPassword', e.target.value)}
                  />
                </SettingListItem>
                <div className="security-actions">
                  <Space style={{ padding: '0 20px' }}>
                    <Button type="primary" loading={updating} onClick={onUpdateUserClick}>
                      {t('confirm')}
                    </Button>
                  </Space>
                </div>
              </>
            ),
          },
          {
            key: '2',
            label: catTabLabel(
              <SafetyOutlined aria-hidden />,
              t('pages.settings.security.twoFactor'),
              isMobile,
            ),
            children: (
              <SettingListItem
                paddings="small"
                title={t('pages.settings.security.twoFactorEnable')}
                description={t('pages.settings.security.twoFactorEnableDesc')}
              >
                <Switch checked={allSetting.twoFactorEnable} onClick={toggleTwoFactor} />
              </SettingListItem>
            ),
          },
          {
            key: '3',
            label: catTabLabel(<ApiOutlined aria-hidden />, t('pages.nodes.apiToken'), isMobile),
            children: (
              <div className="api-token-section">
                <div className="api-token-header">
                  <p className="api-token-hint">{t('pages.nodes.apiTokenHint')}</p>
                  <Button type="primary" size="small" onClick={openCreateModal}>
                    + {t('pages.settings.security.apiTokenNew') || 'New token'}
                  </Button>
                </div>
                <Spin aria-live="off" spinning={apiTokensLoading}>
                  {!apiTokens.length && !apiTokensLoading && (
                    <Empty
                      description={t('pages.settings.security.apiTokenEmpty') || 'No tokens yet'}
                    />
                  )}
                  {apiTokens.map((row) => (
                    <div key={row.id} className={`api-token-row${row.enabled ? '' : ' disabled'}`}>
                      <div className="api-token-row-head">
                        <div className="api-token-name-wrap">
                          <div className="api-token-name-line">
                            <span id={`${uid}-token-${row.id}`} className="api-token-name">
                              {row.name}
                            </span>
                            <Tag color={scopeMeta[row.scope].color}>
                              {scopeMeta[row.scope].label}
                            </Tag>
                            {row.scope === 'admin' && row.username && (
                              <Tag>
                                {t('pages.access.actsAs')} {row.username}
                              </Tag>
                            )}
                            {row.scope === 'admin' && (row.permissions?.length ?? 0) > 0 && (
                              <Tag color="orange">
                                {t('pages.access.permCount', { n: row.permissions?.length ?? 0 })}
                              </Tag>
                            )}
                            {(row.maxRequests ?? 0) > 0 && (
                              <Tag
                                color={
                                  (row.requestCount ?? 0) >= (row.maxRequests ?? 0)
                                    ? 'red'
                                    : 'default'
                                }
                              >
                                {t('pages.access.requestsUsed', {
                                  used: row.requestCount ?? 0,
                                  max: row.maxRequests ?? 0,
                                })}
                              </Tag>
                            )}
                          </div>
                          <span className="api-token-created">
                            {formatTokenDate(row.createdAt)}
                            {(row.lastUsedAt ?? 0) > 0 &&
                              ` · ${t('pages.access.lastUsed', { when: formatTokenDate(row.lastUsedAt ?? 0) })}`}
                          </span>
                        </div>
                        <div className="api-token-actions">
                          <Switch
                            size="small"
                            aria-labelledby={`${uid}-token-${row.id}`}
                            checked={row.enabled}
                            onChange={() => toggleTokenEnabled(row)}
                          />
                          <Button
                            size="small"
                            danger
                            type="text"
                            onClick={() => confirmDeleteToken(row)}
                          >
                            {t('delete')}
                          </Button>
                        </div>
                      </div>
                    </div>
                  ))}
                </Spin>
              </div>
            ),
          },
        ]}
      />

      <Modal
        open={createOpen}
        title={t('pages.settings.security.apiTokenNew') || 'New API token'}
        confirmLoading={creating}
        okText={t('confirm')}
        cancelText={t('cancel')}
        onOk={confirmCreateToken}
        onCancel={() => setCreateOpen(false)}
      >
        <Form layout="vertical">
          <Form.Item
            label={t('pages.settings.security.apiTokenName') || 'Name'}
            htmlFor={`${uid}-tokenName`}
            required
          >
            <Input
              id={`${uid}-tokenName`}
              value={createName}
              maxLength={64}
              placeholder={
                t('pages.settings.security.apiTokenNamePlaceholder') || 'e.g. central-panel-a'
              }
              onChange={(e) => setCreateName(e.target.value)}
              onPressEnter={confirmCreateToken}
            />
          </Form.Item>
          <Form.Item
            label={t('pages.settings.security.apiTokenScope')}
            extra={scopeMeta[createScope].description}
            required
          >
            <Select
              aria-label={t('pages.settings.security.apiTokenScope')}
              value={createScope}
              options={(Object.keys(scopeMeta) as ApiTokenScope[]).map((scope) => ({
                value: scope,
                label: scopeMeta[scope].label,
              }))}
              onChange={setCreateScope}
            />
          </Form.Item>
          {createScope === 'admin' && (
            <>
              {admins.length > 0 && (
                <Form.Item
                  label={t('pages.access.actsAs')}
                  extra={t('pages.access.actsAsHint')}
                  htmlFor={`${uid}-tokenUser`}
                >
                  <Select
                    id={`${uid}-tokenUser`}
                    value={createUserId}
                    onChange={(v) => {
                      setCreateUserId(v);
                      setCreateTier('');
                      setCreatePerms(null);
                    }}
                    options={[
                      { label: t('pages.access.myself'), value: 0 },
                      ...admins.map((a) => ({
                        label: `${a.username} · ${t(`pages.access.tier.${a.tier}`)}`,
                        value: a.id,
                      })),
                    ]}
                  />
                </Form.Item>
              )}
              <Form.Item label={t('pages.access.tierLabel')} htmlFor={`${uid}-tokenTier`}>
                <Select
                  id={`${uid}-tokenTier`}
                  value={createTier}
                  onChange={(v) => {
                    setCreateTier(v);
                    setCreatePerms(null);
                  }}
                  options={[
                    { label: t('pages.access.sameAsAccount'), value: '' },
                    ...TIERS.filter(
                      (tier) =>
                        TIERS.indexOf(tier) >= TIERS.indexOf(actingTier as (typeof TIERS)[number]),
                    ).map((tier) => ({ label: t(`pages.access.tier.${tier}`), value: tier })),
                  ]}
                />
              </Form.Item>
              <Form.Item label={t('pages.access.permissions')}>
                <Space direction="vertical" style={{ width: '100%' }}>
                  <Switch
                    checked={createPerms === null}
                    aria-label={t('pages.access.noRestriction')}
                    onChange={(on) => setCreatePerms(on ? null : [])}
                  />
                  <span>{t('pages.access.noRestriction')}</span>
                  {createPerms !== null && (
                    <PermissionPicker
                      value={createPerms}
                      onChange={setCreatePerms}
                      ceiling={access.me?.tierCeilings?.[tokenTier] ?? []}
                    />
                  )}
                </Space>
              </Form.Item>
              <Space wrap>
                <Form.Item
                  label={t('pages.access.requestsPerMinute')}
                  extra={t('pages.access.requestsPerMinuteHint')}
                  htmlFor={`${uid}-tokenRate`}
                >
                  <InputNumber
                    id={`${uid}-tokenRate`}
                    min={0}
                    value={createRate}
                    onChange={onNumber(setCreateRate)}
                  />
                </Form.Item>
                <Form.Item
                  label={t('pages.access.validityDays')}
                  extra={t('pages.access.zeroNever')}
                  htmlFor={`${uid}-tokenDays`}
                >
                  <InputNumber
                    id={`${uid}-tokenDays`}
                    min={0}
                    value={createDays}
                    onChange={onNumber(setCreateDays)}
                  />
                </Form.Item>
                <Form.Item
                  label={t('pages.access.maxRequests')}
                  extra={t('pages.access.maxRequestsHint')}
                  htmlFor={`${uid}-tokenMax`}
                >
                  <InputNumber
                    id={`${uid}-tokenMax`}
                    min={0}
                    value={createMax}
                    onChange={onNumber(setCreateMax)}
                  />
                </Form.Item>
                {createMax > 0 && (
                  <Form.Item label={t('pages.access.deleteOnLimit')} htmlFor={`${uid}-tokenDel`}>
                    <Switch
                      id={`${uid}-tokenDel`}
                      checked={createDeleteAtMax}
                      onChange={setCreateDeleteAtMax}
                    />
                  </Form.Item>
                )}
              </Space>
            </>
          )}
        </Form>
      </Modal>

      <Modal
        open={!!createdToken}
        title={t('pages.settings.security.apiTokenCreatedTitle') || 'Token created'}
        okText={t('done')}
        onOk={() => setCreatedToken(null)}
        onCancel={() => setCreatedToken(null)}
        cancelButtonProps={{ style: { display: 'none' } }}
      >
        <p className="api-token-created-notice">
          {t('pages.settings.security.apiTokenCreatedNotice') ||
            'Copy this token now. For security it is not stored in readable form and will not be shown again.'}
        </p>
        <div className="api-token-value-wrap">
          <code className="api-token-value">{createdToken?.token}</code>
          <Button
            size="small"
            type="primary"
            onClick={() => createdToken && copyToken(createdToken.token)}
          >
            {t('copy')}
          </Button>
        </div>
      </Modal>

      <TwoFactorModal
        open={tfa.open}
        title={tfa.title}
        description={tfa.description}
        token={tfa.token}
        type={tfa.type}
        onConfirm={onTfaConfirm}
        onOpenChange={(open) => setTfa((prev) => ({ ...prev, open }))}
      />
    </>
  );
}
