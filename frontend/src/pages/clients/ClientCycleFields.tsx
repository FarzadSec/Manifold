import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { useFormContext, useWatch } from 'react-hook-form';
import { Col, InputNumber, Row, Select } from 'antd';

import { FormField } from '@/components/form/rhf';
import { useDatepicker } from '@/hooks/useDatepicker';
import { CLIENT_TRAFFIC_RESETS } from '@/schemas/primitives';
import { monthName } from '@/lib/usage/calendar';

const WEEKDAYS = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'] as const;

interface ClientCycleFieldsProps {
  /** Wider screens give each field half the row. */
  columns?: boolean;
}

// The cycle a client's used traffic goes back to zero on: its period, and the weekday,
// day, month, calendar or number of days that period needs.
export default function ClientCycleFields({ columns = true }: ClientCycleFieldsProps) {
  const { t, i18n } = useTranslation();
  const { control, getValues, setValue, getFieldState } = useFormContext();
  const { datepicker } = useDatepicker();
  const period = useWatch({ control, name: 'trafficReset' }) as string;
  // A value left from another period would name a weekday or month that does not exist.
  useEffect(() => {
    if (period === 'weekly' && Number(getValues('trafficResetDay')) > 7)
      setValue('trafficResetDay', 7);
    if (period === 'yearly' && !(Number(getValues('trafficResetMonth')) > 0))
      setValue('trafficResetMonth', 1);
    if (period === 'days' && !(Number(getValues('trafficResetEvery')) > 0))
      setValue('trafficResetEvery', 30);
    // A cycle picked now follows the calendar the panel shows dates in, until set otherwise.
    const picked =
      getFieldState('trafficReset').isDirty && !getFieldState('trafficResetCalendar').isDirty;
    if ((period === 'monthly' || period === 'yearly') && picked && datepicker === 'jalalian') {
      setValue('trafficResetCalendar', 'persian');
    }
  }, [period, getValues, setValue, getFieldState, datepicker]);
  const calendar = useWatch({ control, name: 'trafficResetCalendar' }) as string;
  const span = columns ? { xs: 24, md: 12 } : { xs: 24 };
  const dated = period === 'monthly' || period === 'yearly';
  const kind = calendar === 'persian' ? 'jalalian' : 'gregorian';
  return (
    <Row gutter={16}>
      <Col {...span}>
        <FormField name="trafficReset" label={t('pages.inbounds.periodicTrafficResetTitle')}>
          <Select
            options={CLIENT_TRAFFIC_RESETS.map((r) => ({
              value: r,
              label: t(`pages.clients.cycle.${r}`),
            }))}
          />
        </FormField>
      </Col>
      {dated && (
        <Col {...span}>
          <FormField name="trafficResetCalendar" label={t('pages.clients.cycle.calendar')}>
            <Select
              options={[
                { value: 'gregorian', label: t('pages.clients.cycle.gregorian') },
                { value: 'persian', label: t('pages.clients.cycle.persian') },
              ]}
            />
          </FormField>
        </Col>
      )}
      {period === 'yearly' && (
        <Col {...span}>
          <FormField
            name="trafficResetMonth"
            label={t('pages.clients.cycle.month')}
            transform={{ output: (v) => Number(v) || 1 }}
          >
            <Select
              options={Array.from({ length: 12 }, (_, i) => ({
                value: i + 1,
                label: monthName({ year: 1405, month: i + 1 }, kind, i18n.language).replace(
                  /\s*\d+$/,
                  '',
                ),
              }))}
            />
          </FormField>
        </Col>
      )}
      {dated && (
        <Col {...span}>
          <FormField
            name="trafficResetDay"
            label={t('pages.inbounds.periodicTrafficResetDay')}
            tooltip={t('pages.clients.cycle.dayHint')}
            transform={{ output: (v) => Number(v) || 1 }}
          >
            <InputNumber min={1} max={31} style={{ width: '100%' }} />
          </FormField>
        </Col>
      )}
      {period === 'weekly' && (
        <Col {...span}>
          <FormField
            name="trafficResetDay"
            label={t('pages.clients.cycle.weekday')}
            transform={{ output: (v) => Number(v) || 7 }}
          >
            <Select
              options={WEEKDAYS.map((d, i) => ({
                value: i + 1,
                label: t(`pages.access.weekday.${d}`),
              }))}
            />
          </FormField>
        </Col>
      )}
      {period === 'days' && (
        <Col {...span}>
          <FormField
            name="trafficResetEvery"
            label={t('pages.clients.cycle.every')}
            tooltip={t('pages.clients.cycle.everyHint')}
            transform={{ output: (v) => Number(v) || 1 }}
          >
            <InputNumber min={1} max={3650} style={{ width: '100%' }} />
          </FormField>
        </Col>
      )}
    </Row>
  );
}
