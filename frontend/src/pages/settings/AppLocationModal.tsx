import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Form, Input, Modal, Select } from 'antd';

import type { AppLocation } from '@/generated/types';
import { selectTagRender } from '@/components/form';

export interface InboundChoice {
  value: number;
  label: string;
  /** The other location this inbound already serves, if any. */
  takenBy?: string;
}

interface AppLocationModalProps {
  location: AppLocation;
  choices: InboundChoice[];
  onSave: (location: AppLocation) => Promise<boolean>;
  onClose: () => void;
}

// Mounted per edit, so its fields start from the location it was opened for.
export default function AppLocationModal({
  location,
  choices,
  onSave,
  onClose,
}: AppLocationModalProps) {
  const { t } = useTranslation();
  const uid = useId();
  const [draft, setDraft] = useState(location);
  const [saving, setSaving] = useState(false);
  const set = (patch: Partial<AppLocation>) => setDraft((prev) => ({ ...prev, ...patch }));

  async function submit() {
    setSaving(true);
    try {
      const saved = await onSave({
        ...draft,
        countryCode: draft.countryCode.trim().toUpperCase(),
        inboundIds: [...draft.inboundIds].sort((a, b) => a - b),
      });
      if (saved) onClose();
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      open
      title={t(location.id ? 'pages.settings.app.editLocation' : 'pages.settings.app.addLocation')}
      onOk={submit}
      onCancel={onClose}
      confirmLoading={saving}
      destroyOnHidden
    >
      <Form layout="vertical">
        <Form.Item label={t('pages.settings.app.name')} htmlFor={`${uid}-name`} required>
          <Input
            id={`${uid}-name`}
            value={draft.name}
            placeholder="Germany"
            maxLength={64}
            onChange={(e) => set({ name: e.target.value })}
          />
        </Form.Item>
        <Form.Item label={t('pages.settings.app.countryCode')} htmlFor={`${uid}-country`}>
          <Input
            id={`${uid}-country`}
            value={draft.countryCode}
            placeholder="DE"
            maxLength={2}
            onChange={(e) => set({ countryCode: e.target.value })}
          />
        </Form.Item>
        <Form.Item label={t('pages.settings.app.city')} htmlFor={`${uid}-city`}>
          <Input
            id={`${uid}-city`}
            value={draft.city}
            placeholder="Frankfurt"
            maxLength={64}
            onChange={(e) => set({ city: e.target.value })}
          />
        </Form.Item>
        <Form.Item
          label={t('pages.settings.app.timezone')}
          htmlFor={`${uid}-timezone`}
          tooltip={t('pages.settings.app.timezoneDesc')}
        >
          <Input
            id={`${uid}-timezone`}
            value={draft.timezone}
            placeholder="Europe/Berlin"
            maxLength={64}
            onChange={(e) => set({ timezone: e.target.value })}
          />
        </Form.Item>
        <Form.Item
          label={t('pages.settings.app.inbounds')}
          htmlFor={`${uid}-inbounds`}
          tooltip={t('pages.settings.app.inboundsDesc')}
        >
          <Select
            id={`${uid}-inbounds`}
            mode="multiple"
            tagRender={selectTagRender}
            value={draft.inboundIds}
            optionFilterProp="label"
            onChange={(inboundIds: number[]) => set({ inboundIds })}
            options={choices.map((c) => ({
              value: c.value,
              label:
                c.takenBy && !draft.inboundIds.includes(c.value)
                  ? `${c.label} (${c.takenBy})`
                  : c.label,
              disabled: !!c.takenBy && !draft.inboundIds.includes(c.value),
            }))}
          />
        </Form.Item>
      </Form>
    </Modal>
  );
}
