import { useTranslation } from 'react-i18next';
import { Checkbox, Col, Row, Space, Typography } from 'antd';

import { PERMISSION_GROUPS, permKey } from './accessInfo';

// Ticks the permissions a role keeps; what its tier does not include cannot be ticked.
export default function PermissionPicker({
  value,
  onChange,
  ceiling,
}: {
  value?: string[];
  onChange?: (next: string[]) => void;
  ceiling: string[];
}) {
  const { t } = useTranslation();
  const held = new Set(value ?? []);
  const toggle = (perm: string, on: boolean) => {
    const next = new Set(held);
    if (on) next.add(perm);
    else next.delete(perm);
    onChange?.([...next]);
  };
  return (
    <Row gutter={[12, 8]}>
      {PERMISSION_GROUPS.map((group) => (
        <Col xs={24} md={12} key={group.key}>
          <Typography.Text strong>{t(`pages.access.permGroup.${group.key}`)}</Typography.Text>
          <Space direction="vertical" size={2} style={{ display: 'flex', marginTop: 4 }}>
            {group.perms.map((perm) => (
              <Checkbox
                key={perm}
                checked={held.has(perm)}
                disabled={!ceiling.includes(perm)}
                onChange={(e) => toggle(perm, e.target.checked)}
              >
                {t(permKey(perm))}{' '}
                <Typography.Text type="secondary" code>
                  {perm}
                </Typography.Text>
              </Checkbox>
            ))}
          </Space>
        </Col>
      ))}
    </Row>
  );
}
