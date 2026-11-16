import { theme } from 'antd';

import { SizeFormatter } from '@/utils';

export interface UsageBar {
  label: string;
  up: number;
  down: number;
}

interface UsageBarsProps {
  bars: UsageBar[];
  upLabel: string;
  downLabel: string;
  height?: number;
}

const WIDTH = 720;
const AXIS = 22;

// Download sits under upload in each bar; the title of a bar is what a pointer or a
// screen reader gets, so the chart needs no tooltip machinery of its own.
export default function UsageBars({ bars, upLabel, downLabel, height = 200 }: UsageBarsProps) {
  const { token } = theme.useToken();
  const peak = Math.max(1, ...bars.map((b) => b.up + b.down));
  const plot = height - AXIS;
  const slot = WIDTH / Math.max(bars.length, 1);
  const barWidth = Math.max(1, slot * 0.72);
  const labelEvery = Math.max(1, Math.ceil(bars.length / 8));
  return (
    <svg
      viewBox={`0 0 ${WIDTH} ${height}`}
      width="100%"
      role="img"
      aria-label={`${downLabel} / ${upLabel}`}
      style={{ display: 'block' }}
    >
      <line x1={0} x2={WIDTH} y1={plot} y2={plot} stroke={token.colorBorderSecondary} />
      <text x={2} y={11} fontSize={11} fill={token.colorTextSecondary}>
        {SizeFormatter.sizeFormat(peak)}
      </text>
      {bars.map((b, i) => {
        const x = i * slot + (slot - barWidth) / 2;
        const downH = (b.down / peak) * (plot - 14);
        const upH = (b.up / peak) * (plot - 14);
        return (
          <g key={`${b.label}-${i}`}>
            <title>
              {`${b.label}\n${downLabel}: ${SizeFormatter.sizeFormat(b.down)}\n${upLabel}: ${SizeFormatter.sizeFormat(b.up)}`}
            </title>
            <rect
              x={x}
              y={plot - downH}
              width={barWidth}
              height={downH}
              rx={1.5}
              fill={token.colorPrimary}
            />
            <rect
              x={x}
              y={plot - downH - upH}
              width={barWidth}
              height={upH}
              rx={1.5}
              fill={token.colorSuccess}
            />
            {i % labelEvery === 0 && (
              <text
                x={x + barWidth / 2}
                y={height - 6}
                fontSize={10}
                textAnchor="middle"
                fill={token.colorTextTertiary}
              >
                {b.label.length > 10 ? b.label.slice(5) : b.label}
              </text>
            )}
          </g>
        );
      })}
    </svg>
  );
}
