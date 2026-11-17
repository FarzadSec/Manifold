import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Col, Form, Input, Modal, Row, Segmented, Select, Switch } from 'antd';
import { FormProvider, useForm, useWatch } from 'react-hook-form';

import { selectTagRender } from '@/components/form';
import { FormField, rhfZodValidate } from '@/components/form/rhf';
import type { WebhookView } from '@/generated/types';
import {
  WebhookFormSchema,
  WebhookURLSchema,
  eventGroup,
  type EventGroup,
  type WebhookFormValues,
} from '@/schemas/webhook';

interface WebhookModalProps {
  open: boolean;
  editing: WebhookView | null;
  events: string[];
  onSave: (payload: Record<string, unknown>) => Promise<boolean>;
  onClose: () => void;
}

const EMPTY: WebhookFormValues = {
  id: 0,
  name: '',
  enable: true,
  kind: 'http',
  url: '',
  chatId: '',
  events: [],
  secret: '',
};

function toForm(row: WebhookView | null): WebhookFormValues {
  if (!row) return EMPTY;
  return {
    ...EMPTY,
    id: row.id,
    name: row.name,
    enable: row.enable,
    kind: (row.kind as WebhookFormValues['kind']) || 'http',
    url: row.url,
    chatId: row.chatId,
    events: row.events ?? [],
  };
}

const GROUP_ORDER: EventGroup[] = ['user', 'node', 'backup', 'system'];

export default function WebhookModal({
  open,
  editing,
  events,
  onSave,
  onClose,
}: WebhookModalProps) {
  const { t } = useTranslation();
  const methods = useForm<WebhookFormValues>({ defaultValues: EMPTY });
  const kind = useWatch({ control: methods.control, name: 'kind' });
  const [saving, setSaving] = useState(false);
  const [synced, setSynced] = useState<{ open: boolean; editing: WebhookView | null } | null>(null);
  if (!synced || synced.open !== open || synced.editing !== editing) {
    setSynced({ open, editing });
    if (open) methods.reset(toForm(editing));
  }

  const options = GROUP_ORDER.map((group) => ({
    label: t(`pages.webhooks.group.${group}`),
    options: events.filter((e) => eventGroup(e) === group).map((e) => ({ value: e, label: e })),
  })).filter((g) => g.options.length > 0);

  const secretLabel = {
    http: t('pages.webhooks.signingSecret'),
    ntfy: t('pages.webhooks.ntfyToken'),
    bale: t('pages.webhooks.baleToken'),
  }[kind];
  const secretHint = editing?.hasSecret
    ? t('pages.backups.secretKept')
    : kind === 'http'
      ? t('pages.webhooks.secretGenerated')
      : undefined;

  async function submit(values: WebhookFormValues) {
    const payload: Record<string, unknown> = { ...values };
    if (values.secret === '') delete payload.secret;
    setSaving(true);
    try {
      if (await onSave(payload)) onClose();
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      open={open}
      onCancel={onClose}
      width={640}
      title={editing ? t('pages.webhooks.edit') : t('pages.webhooks.add')}
      okText={t('save')}
      confirmLoading={saving}
      onOk={methods.handleSubmit(submit)}
      destroyOnHidden
    >
      <FormProvider {...methods}>
        <Form layout="vertical" component="div">
          <Row gutter={12}>
            <Col xs={24} md={16}>
              <FormField
                label={t('pages.backups.name')}
                name="name"
                rules={{ validate: rhfZodValidate(WebhookFormSchema.shape.name) }}
              >
                <Input placeholder="mirza" />
              </FormField>
            </Col>
            <Col xs={24} md={8}>
              <FormField label={t('enable')} name="enable" valueProp="checked">
                <Switch />
              </FormField>
            </Col>
          </Row>
          <FormField label={t('pages.backups.kind')} name="kind">
            <Segmented
              options={[
                { value: 'http', label: 'HTTP' },
                { value: 'ntfy', label: 'ntfy' },
                { value: 'bale', label: t('pages.webhooks.bale') },
              ]}
            />
          </FormField>
          {kind !== 'bale' && (
            <FormField
              label={kind === 'ntfy' ? t('pages.webhooks.ntfyTopic') : t('pages.webhooks.url')}
              name="url"
              tooltip={kind === 'ntfy' ? t('pages.webhooks.ntfyHint') : t('pages.webhooks.urlHint')}
              rules={{ validate: rhfZodValidate(WebhookURLSchema) }}
            >
              <Input
                placeholder={
                  kind === 'ntfy' ? 'https://ntfy.sh/my-panel' : 'https://bot.example.com/manifold'
                }
              />
            </FormField>
          )}
          {kind === 'bale' && (
            <FormField
              label={t('pages.webhooks.chatId')}
              name="chatId"
              tooltip={t('pages.webhooks.chatIdHint')}
              rules={{
                validate: rhfZodValidate(
                  WebhookFormSchema.shape.chatId.min(1, 'pages.nodes.toasts.fillRequired'),
                ),
              }}
            >
              <Input placeholder="123456789" />
            </FormField>
          )}
          <FormField label={secretLabel} name="secret" extra={secretHint}>
            <Input.Password autoComplete="new-password" />
          </FormField>
          <FormField
            label={t('pages.webhooks.events')}
            name="events"
            tooltip={t('pages.webhooks.eventsHint')}
          >
            <Select
              mode="multiple"
              allowClear
              tagRender={selectTagRender}
              placeholder={t('pages.webhooks.allEvents')}
              options={options}
            />
          </FormField>
        </Form>
      </FormProvider>
    </Modal>
  );
}
