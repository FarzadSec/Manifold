import { theme } from 'antd';

export interface AvailabilityStretch {
  from: number;
  state: string;
}

interface AvailabilityStripProps {
  stretches: AvailabilityStretch[];
  labels: Record<'up' | 'down' | 'unwatched', string>;
  formatTime: (unixSec: number) => string;
  height?: number;
}

const WIDTH = 720;

// One cell per stretch; its title names the time and the state for a pointer or a screen reader.
export default function AvailabilityStrip({
  stretches,
  labels,
  formatTime,
  height = 18,
}: AvailabilityStripProps) {
  const { token } = theme.useToken();
  const fill = (state: string) =>
    state === 'up'
      ? token.colorSuccess
      : state === 'down'
        ? token.colorError
        : token.colorFillSecondary;
  const cell = WIDTH / Math.max(stretches.length, 1);
  return (
    <svg
      viewBox={`0 0 ${WIDTH} ${height}`}
      width="100%"
      preserveAspectRatio="none"
      role="img"
      aria-label={`${labels.up} / ${labels.down} / ${labels.unwatched}`}
      style={{ display: 'block', borderRadius: 4 }}
    >
      {stretches.map((s, i) => (
        <rect
          key={s.from}
          x={i * cell}
          y={0}
          width={Math.max(cell - 1, 1)}
          height={height}
          fill={fill(s.state)}
        >
          <title>{`${formatTime(s.from)} · ${labels[s.state as keyof typeof labels] ?? s.state}`}</title>
        </rect>
      ))}
    </svg>
  );
}
