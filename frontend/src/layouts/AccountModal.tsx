import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQueryClient } from '@tanstack/react-query';
import { Button, Divider, Form, Input, Modal, Space, Tag, Typography } from 'antd';
import { SafetyOutlined } from '@ant-design/icons';

import { HttpUtil } from '@/utils';
import { keys } from '@/api/queryKeys';
import { postJson, useAccess } from '@/api/queries/useAccess';
import type { TwoFactorSetupView } from '@/generated/types';
import TwoFactorModal from '@/pages/settings/TwoFactorModal';

// Every account's own password and two-factor, reachable without the settings page.
export default function AccountModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { me } = useAccess();
  const [setup, setSetup] = useState<TwoFactorSetupView | null>(null);
  const [confirmOff, setConfirmOff] = useState(false);
  const [oldPassword, setOldPassword] = useState('');
  const [newUsername, setNewUsername] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [code, setCode] = useState('');
  const [saving, setSaving] = useState(false);
  const refreshMe = () => queryClient.invalidateQueries({ queryKey: keys.access.me() });

  async function beginSetup() {
    const msg = await HttpUtil.get<TwoFactorSetupView>('/panel/api/access/me/twoFactor');
    if (msg.success && msg.obj) setSetup(msg.obj);
  }

  async function changeCredentials() {
    setSaving(true);
    try {
      const msg = await HttpUtil.post('/panel/api/setting/updateUser', {
        oldUsername: me?.username ?? '',
        oldPassword,
        newUsername: newUsername || me?.username || '',
        newPassword,
        twoFactorCode: code,
      });
      if (msg.success) {
        setOldPassword('');
        setNewPassword('');
        setCode('');
        void refreshMe();
      }
    } finally {
      setSaving(false);
    }
  }

  return (
    <>
      <Modal
        open={open}
        title={t('pages.access.myAccount')}
        footer={null}
        onCancel={onClose}
        destroyOnHidden
      >
        <Space direction="vertical" style={{ width: '100%' }}>
          <Typography.Text>
            {me?.username} · <Tag>{t(`pages.access.tier.${me?.tier ?? 'admin'}`)}</Tag>
          </Typography.Text>
          <Divider titlePlacement="start">{t('pages.access.twoFactor')}</Divider>
          <Space>
            <SafetyOutlined aria-hidden />
            {me?.twoFactorEnabled ? (
              <>
                <Tag color="green">{t('pages.access.twoFactorOn')}</Tag>
                <Button danger onClick={() => setConfirmOff(true)}>
                  {t('pages.access.turnOff')}
                </Button>
              </>
            ) : (
              <>
                <Tag>{t('pages.access.twoFactorOff')}</Tag>
                <Button type="primary" onClick={() => void beginSetup()}>
                  {t('pages.access.turnOn')}
                </Button>
              </>
            )}
          </Space>
          <Divider titlePlacement="start">{t('pages.access.changePassword')}</Divider>
          <Form layout="vertical">
            <Form.Item label={t('pages.access.currentPassword')} htmlFor="account-old-password">
              <Input.Password
                id="account-old-password"
                value={oldPassword}
                onChange={(e) => setOldPassword(e.target.value)}
              />
            </Form.Item>
            <Form.Item
              label={t('pages.access.newUsername')}
              htmlFor="account-new-username"
              extra={t('pages.access.newUsernameHint')}
            >
              <Input
                id="account-new-username"
                value={newUsername}
                placeholder={me?.username}
                onChange={(e) => setNewUsername(e.target.value)}
              />
            </Form.Item>
            <Form.Item label={t('pages.access.newPassword')} htmlFor="account-new-password">
              <Input.Password
                id="account-new-password"
                value={newPassword}
                onChange={(e) => setNewPassword(e.target.value)}
              />
            </Form.Item>
            {me?.twoFactorEnabled && (
              <Form.Item label={t('pages.access.twoFactorCode')} htmlFor="account-code">
                <Input
                  id="account-code"
                  value={code}
                  maxLength={6}
                  onChange={(e) => setCode(e.target.value)}
                />
              </Form.Item>
            )}
            <Button
              type="primary"
              loading={saving}
              disabled={!oldPassword || newPassword.length < 8}
              onClick={() => void changeCredentials()}
            >
              {t('save')}
            </Button>
          </Form>
        </Space>
      </Modal>
      <TwoFactorModal
        open={!!setup}
        title={t('pages.access.twoFactor')}
        token={setup?.secret ?? ''}
        type="set"
        onConfirm={async (ok, entered) => {
          if (!ok || !setup) return;
          const msg = await postJson('/panel/api/access/me/twoFactor', {
            enable: true,
            secret: setup.secret,
            code: entered,
          });
          if (msg.success) {
            setSetup(null);
            void refreshMe();
          }
        }}
        onOpenChange={(o) => !o && setSetup(null)}
      />
      <TwoFactorModal
        open={confirmOff}
        title={t('pages.access.turnOff')}
        type="confirm"
        onConfirm={async (ok, entered) => {
          if (!ok) return;
          const msg = await postJson('/panel/api/access/me/twoFactor', {
            enable: false,
            code: entered,
          });
          if (msg.success) {
            setConfirmOff(false);
            void refreshMe();
          }
        }}
        onOpenChange={(o) => !o && setConfirmOff(false)}
      />
    </>
  );
}
