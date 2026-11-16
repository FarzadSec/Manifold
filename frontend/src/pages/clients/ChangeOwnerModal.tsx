import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Form, Modal, Select, Typography } from 'antd';

import { postJson } from '@/api/queries/useAccess';
import type { OwnerView } from '@/generated/types';

interface ChangeOwnerModalProps {
  open: boolean;
  emails: string[];
  owners: OwnerView[];
  onClose: (changed: boolean) => void;
}

// Hands the selected users to another account; a reseller's user limit is checked for all at once.
export default function ChangeOwnerModal({ open, emails, owners, onClose }: ChangeOwnerModalProps) {
  const { t } = useTranslation();
  const [ownerId, setOwnerId] = useState(0);
  const [saving, setSaving] = useState(false);

  async function submit() {
    setSaving(true);
    try {
      const msg = await postJson('/panel/api/clients/changeOwner', { emails, ownerId });
      if (msg.success) onClose(true);
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      open={open}
      title={t('pages.access.changeOwner')}
      okText={t('save')}
      confirmLoading={saving}
      onOk={() => void submit()}
      onCancel={() => onClose(false)}
      destroyOnHidden
    >
      <Typography.Paragraph type="secondary">
        {t('pages.access.changeOwnerHint', { count: emails.length })}
      </Typography.Paragraph>
      <Form layout="vertical">
        <Form.Item label={t('pages.access.newOwner')} htmlFor="change-owner-select">
          <Select
            id="change-owner-select"
            value={ownerId}
            onChange={setOwnerId}
            options={[
              { label: t('pages.access.panel'), value: 0 },
              ...owners.map((o) => ({
                label: `${o.username} · ${t(`pages.access.tier.${o.tier}`)}`,
                value: o.id,
              })),
            ]}
          />
        </Form.Item>
      </Form>
    </Modal>
  );
}
