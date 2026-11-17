import { useTranslation } from 'react-i18next';
import { Progress, Space, Tag, Tooltip } from 'antd';

import type { NodeRecord } from '@/api/queries/useNodesQuery';
import { SizeFormatter } from '@/utils';

// The node's traffic accounting at a glance: its multiplier, and how far into its period's limit it is.
export default function NodeTrafficTags({ record }: { record: NodeRecord }) {
  const { t } = useTranslation();
  const multiplier = record.trafficMultiplier ?? 1;
  const limit = record.usageLimit ?? 0;
  const limited = !!record.usagePeriod && limit > 0;
  if (multiplier === 1 && !limited && !record.usageDisabled) return null;
  const used = record.usageUsed ?? 0;
  const percent = limited ? Math.min(100, Math.round((used / limit) * 100)) : 0;
  return (
    <Space size={4} wrap style={{ marginTop: 2 }}>
      {multiplier !== 1 && (
        <Tooltip title={t('pages.nodes.trafficMultiplierHint')}>
          <Tag color="purple" style={{ margin: 0 }}>
            ×{multiplier}
          </Tag>
        </Tooltip>
      )}
      {record.usageDisabled && (
        <Tag color="red" style={{ margin: 0 }}>
          {t('pages.nodes.usageStopped')}
        </Tag>
      )}
      {limited && (
        <Tooltip
          title={t('pages.nodes.usageOfLimit', {
            used: SizeFormatter.sizeFormat(used),
            limit: SizeFormatter.sizeFormat(limit),
          })}
        >
          <Progress
            percent={percent}
            size={[72, 6]}
            showInfo={false}
            status={percent >= 100 ? 'exception' : percent >= 80 ? 'active' : 'normal'}
            aria-label={t('pages.nodes.usageLimit')}
          />
        </Tooltip>
      )}
    </Space>
  );
}
