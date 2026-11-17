import { useTranslation } from 'react-i18next';
import { Col, Select, Switch } from 'antd';
import { useFormContext, useWatch } from 'react-hook-form';

import { FormField } from '@/components/form/rhf';
import { selectTagRender } from '@/components/form';
import type { Plan } from '@/generated/types';
import { useInboundOptions } from '@/api/queries/useInboundOptions';

// SellsFromFields picks the plans and inbounds an account or a role sells from, inside a Row.
export default function SellsFromFields({ plans }: { plans: Plan[] }) {
  const { t } = useTranslation();
  const { control } = useFormContext<{ plansOnly: boolean; inboundsLimited: boolean }>();
  const plansOnly = useWatch({ control, name: 'plansOnly' });
  const inboundsLimited = useWatch({ control, name: 'inboundsLimited' });
  const { data: inbounds = [] } = useInboundOptions();
  return (
    <>
      <Col xs={24} md={12}>
        <FormField
          name="plansOnly"
          label={t('pages.access.plansOnly')}
          valueProp="checked"
          tooltip={t('pages.access.plansOnlyHint')}
        >
          <Switch />
        </FormField>
      </Col>
      {plansOnly && (
        <Col xs={24}>
          <FormField name="allowedPlanIds" label={t('pages.access.allowedPlans')}>
            <Select
              mode="multiple"
              tagRender={selectTagRender}
              options={plans.map((p) => ({ label: p.name, value: p.id }))}
            />
          </FormField>
        </Col>
      )}
      <Col xs={24} md={12}>
        <FormField
          name="inboundsLimited"
          label={t('pages.access.inboundsLimited')}
          valueProp="checked"
          tooltip={t('pages.access.inboundsLimitedHint')}
        >
          <Switch />
        </FormField>
      </Col>
      {inboundsLimited && (
        <Col xs={24}>
          <FormField name="allowedInboundIds" label={t('pages.access.allowedInbounds')}>
            <Select
              mode="multiple"
              tagRender={selectTagRender}
              options={inbounds.map((ib) => ({
                label: `${ib.remark || ib.tag} · ${ib.protocol}:${ib.port}`,
                value: ib.id,
              }))}
            />
          </FormField>
        </Col>
      )}
    </>
  );
}
