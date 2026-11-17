import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, Form, InputNumber, Modal, message } from 'antd';

import { keys } from '@/api/queryKeys';
import { HttpUtil } from '@/utils';
import { onNumber } from '@/utils/onNumber';

interface DelExpiredModalProps {
  open: boolean;
  onClose: () => void;
}

async function delExpired(days: number, dryRun: boolean): Promise<number> {
  const msg = await HttpUtil.post<{ count: number }>(
    '/panel/api/clients/delExpired',
    { days, dryRun },
    { headers: { 'Content-Type': 'application/json' } },
  );
  if (!msg?.success) throw new Error(msg?.msg || 'Request failed');
  return msg.obj?.count ?? 0;
}

// Deletes every user whose fixed expiry passed more than N days ago, after showing how many.
export default function DelExpiredModal({ open, onClose }: DelExpiredModalProps) {
  const { t } = useTranslation();
  const [messageApi, contextHolder] = message.useMessage();
  const queryClient = useQueryClient();
  const [days, setDays] = useState(30);
  const [deleting, setDeleting] = useState(false);
  const preview = useQuery({
    queryKey: ['clients', 'delExpired', days],
    queryFn: () => delExpired(days, true),
    enabled: open,
  });
  const count = preview.data ?? 0;

  async function confirm() {
    setDeleting(true);
    try {
      const deleted = await delExpired(days, false);
      messageApi.success(t('pages.clients.toasts.delDepleted', { count: deleted }));
      await queryClient.invalidateQueries({ queryKey: keys.clients.root() });
      onClose();
    } catch {
      messageApi.error(t('somethingWentWrong'));
    } finally {
      setDeleting(false);
    }
  }

  return (
    <Modal
      open={open}
      title={t('pages.clients.delExpired')}
      okText={t('delete')}
      okButtonProps={{ danger: true, disabled: count === 0, loading: deleting }}
      cancelText={t('cancel')}
      onOk={confirm}
      onCancel={onClose}
    >
      {contextHolder}
      <Form layout="vertical">
        <Form.Item
          htmlFor="del-expired-days"
          label={t('pages.clients.delExpiredDays')}
          extra={t('pages.clients.delExpiredHint')}
        >
          <InputNumber
            id="del-expired-days"
            min={0}
            max={3650}
            value={days}
            onChange={onNumber((v) => setDays(Math.max(0, Math.trunc(v))))}
            style={{ width: '100%' }}
          />
        </Form.Item>
      </Form>
      <Alert
        type={count > 0 ? 'warning' : 'info'}
        showIcon
        title={t('pages.clients.delExpiredCount', { count })}
      />
    </Modal>
  );
}
