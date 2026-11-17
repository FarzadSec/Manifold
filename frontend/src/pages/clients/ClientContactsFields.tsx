import { useTranslation } from 'react-i18next';
import { useFieldArray, useFormContext } from 'react-hook-form';
import { Button, Col, Collapse, Flex, Input, Row } from 'antd';
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons';

import { FormField } from '@/components/form/rhf';

/** The customer fields the panel searches and exports; any other field is only kept. */
export const CONTACT_FIELDS = ['telegram', 'email', 'phone', 'bale', 'soroush', 'rubika'] as const;

export interface ContactRow {
  key: string;
  value: string;
}

/** Splits stored contacts into the standard fields and the custom rows the form edits. */
export function splitContacts(contacts?: Record<string, string> | null): {
  standard: Record<string, string>;
  custom: ContactRow[];
} {
  const standard: Record<string, string> = {};
  const custom: ContactRow[] = [];
  for (const [key, value] of Object.entries(contacts ?? {})) {
    if ((CONTACT_FIELDS as readonly string[]).includes(key)) standard[key] = value;
    else custom.push({ key, value });
  }
  return { standard, custom };
}

/** The contacts a save sends: empty fields dropped, a custom name never overriding a standard one. */
export function joinContacts(
  standard: Record<string, string> | undefined,
  custom: ContactRow[] | undefined,
): Record<string, string> {
  const out: Record<string, string> = {};
  for (const row of custom ?? []) {
    const key = row.key.trim();
    const value = row.value.trim();
    if (key && value) out[key] = value;
  }
  for (const field of CONTACT_FIELDS) {
    const value = (standard?.[field] ?? '').trim();
    if (value) out[field] = value;
    else delete out[field];
  }
  return out;
}

export default function ClientContactsFields() {
  const { t } = useTranslation();
  const { control } = useFormContext();
  const { fields, append, remove } = useFieldArray({ control, name: 'customContacts' });
  return (
    <Collapse
      size="small"
      style={{ marginBottom: 16 }}
      items={[
        {
          key: 'contacts',
          label: t('pages.clients.contacts.title'),
          children: (
            <>
              <Row gutter={16}>
                {CONTACT_FIELDS.map((field) => (
                  <Col xs={24} md={12} key={field}>
                    <FormField
                      name={`contacts.${field}`}
                      label={t(`pages.clients.contacts.${field}`)}
                    >
                      <Input allowClear />
                    </FormField>
                  </Col>
                ))}
              </Row>
              {fields.map((row, i) => (
                <Flex gap={8} key={row.id} style={{ marginBottom: 8 }}>
                  <FormField name={`customContacts.${i}.key`} noStyle>
                    <Input
                      aria-label={t('pages.clients.contacts.fieldName')}
                      placeholder={t('pages.clients.contacts.fieldName')}
                    />
                  </FormField>
                  <FormField name={`customContacts.${i}.value`} noStyle>
                    <Input
                      aria-label={t('pages.clients.contacts.fieldValue')}
                      placeholder={t('pages.clients.contacts.fieldValue')}
                    />
                  </FormField>
                  <Button
                    aria-label={t('delete')}
                    icon={<DeleteOutlined aria-hidden />}
                    onClick={() => remove(i)}
                  />
                </Flex>
              ))}
              <Button
                icon={<PlusOutlined aria-hidden />}
                onClick={() => append({ key: '', value: '' })}
              >
                {t('pages.clients.contacts.add')}
              </Button>
            </>
          ),
        },
      ]}
    />
  );
}
