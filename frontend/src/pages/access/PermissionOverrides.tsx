import { useTranslation } from 'react-i18next';
import { Col, Row, Select, Typography } from 'antd';

import { FormField } from '@/components/form/rhf';
import { selectTagRender } from '@/components/form';
import { permKey } from './accessInfo';

// overrideChoices is what an account may add to its role's permissions and what it may lose.
export function overrideChoices(ceiling: string[], base: string[]) {
  const held = new Set(base.filter((p) => ceiling.includes(p)));
  for (const p of [...held]) {
    const read = p.replace(/:write$/, ':read');
    if (read !== p && ceiling.includes(read)) held.add(read);
  }
  return {
    grantable: ceiling.filter((p) => !held.has(p)),
    deniable: ceiling.filter((p) => held.has(p)),
  };
}

export default function PermissionOverrides({
  grantable,
  deniable,
}: {
  grantable: string[];
  deniable: string[];
}) {
  const { t } = useTranslation();
  const options = (perms: string[]) => perms.map((p) => ({ label: t(permKey(p)), value: p }));
  return (
    <>
      <Typography.Paragraph type="secondary">
        {t('pages.access.overridesHint')}
      </Typography.Paragraph>
      <Row gutter={12}>
        <Col xs={24} md={12}>
          <FormField name="grantedPermissions" label={t('pages.access.granted')}>
            <Select mode="multiple" tagRender={selectTagRender} options={options(grantable)} />
          </FormField>
        </Col>
        <Col xs={24} md={12}>
          <FormField name="deniedPermissions" label={t('pages.access.denied')}>
            <Select mode="multiple" tagRender={selectTagRender} options={options(deniable)} />
          </FormField>
        </Col>
      </Row>
    </>
  );
}
