import { describe, expect, it } from 'vitest';

import type { TunnelView } from '@/generated/types';
import { activeMembers, groupCandidates } from '@/pages/tunnels/groupMembers';

const tunnel = (id: number, kind: string, role: string, state?: string) =>
  ({
    id,
    kind,
    role,
    name: `t${id}`,
    settings: {},
    status: state ? { state } : undefined,
  }) as TunnelView;

const tunnels = [
  tunnel(1, 'gre', 'server', 'down'),
  tunnel(2, 'ipip', 'server', 'up'),
  tunnel(3, 'l3', 'server', 'degraded'),
  tunnel(4, 'reverse', 'server', 'up'),
  tunnel(5, 'gre', 'client', 'up'),
];

describe('tunnel groups', () => {
  it('offers only relay ends with a tunnel address', () => {
    expect(groupCandidates(tunnels).map((t) => t.id)).toEqual([1, 2, 3]);
  });

  it('marks where the relay sends the ports now', () => {
    expect(activeMembers('failover', [1, 2, 3], tunnels)).toEqual([2]);
    expect(activeMembers('balance', [1, 2, 3], tunnels)).toEqual([2, 3]);
    expect(activeMembers('balance', [1], tunnels)).toEqual([1]);
  });
});
