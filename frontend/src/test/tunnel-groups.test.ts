import { describe, expect, it } from 'vitest';

import type { TunnelView } from '@/generated/types';
import {
  L2TP_FORWARDS,
  activeMembers,
  groupCandidates,
  keptForwards,
} from '@/pages/tunnels/groupMembers';

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
    expect(activeMembers('failover', [1, 2, 3], [], tunnels)).toEqual([2]);
    expect(activeMembers('balance', [1, 2, 3], [], tunnels)).toEqual([2, 3]);
    expect(activeMembers('balance', [1], [], tunnels)).toEqual([1]);
  });

  // As the relay decides it: backups only while no member answers, then every member if none does.
  it('turns to the backups only when no member answers', () => {
    expect(activeMembers('balance', [2], [3], tunnels)).toEqual([2]);
    expect(activeMembers('balance', [1], [2, 3], tunnels)).toEqual([2, 3]);
    expect(activeMembers('failover', [1], [2, 3], tunnels)).toEqual([2]);
    expect(
      activeMembers(
        'balance',
        [1],
        [4],
        [tunnel(1, 'gre', 'server', 'down'), tunnel(4, 'gre', 'server', 'down')],
      ),
    ).toEqual([1]);
  });
});

describe('forward rows', () => {
  // ESP names no port, so a save must not drop it with the empty rows.
  it('keeps the ESP row of the L2TP preset', () => {
    const rows = [...L2TP_FORWARDS, { listen: ' ', target: '', proto: 'tcp' }];
    expect(keptForwards(rows).map((f) => f.proto)).toEqual(['udp', 'udp', 'esp']);
  });
});
