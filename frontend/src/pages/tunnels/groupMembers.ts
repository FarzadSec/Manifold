import type { PortForward, TunnelView } from '@/generated/types';

const KERNEL = ['gre', 'gretap', 'ip6gre', 'ip6gretap', 'ipip', 'sit'];

// groupCandidates are the tunnels that can carry a group's ports: relay ends with a
// tunnel address, as the server checks too.
export function groupCandidates(tunnels: TunnelView[]): TunnelView[] {
  return tunnels.filter((t) => t.role === 'server' && (KERNEL.includes(t.kind) || t.kind === 'l3'));
}

// activeMembers is where a group sends its ports now, as the relay decides it: the members that
// answer, else the backups that do, else every member; failover takes the first of them.
export function activeMembers(
  mode: string,
  members: number[],
  backups: number[],
  tunnels: TunnelView[],
): number[] {
  const answering = (ids: number[]) =>
    ids.filter((id) => {
      const state = tunnels.find((t) => t.id === id)?.status?.state;
      return state === 'up' || state === 'degraded';
    });
  const liveMembers = answering(members);
  const live = liveMembers.length > 0 ? liveMembers : answering(backups);
  const chosen = live.length > 0 ? live : members;
  return mode === 'failover' ? chosen.slice(0, 1) : chosen;
}

// L2TP/IPsec through a relay: IKE, NAT traversal and ESP; UDP 1701 rides inside IPsec.
export const L2TP_FORWARDS: PortForward[] = [
  { listen: '500', target: '', proto: 'udp' },
  { listen: '4500', target: '', proto: 'udp' },
  { listen: '', target: '', proto: 'esp' },
];

// keptForwards drops the empty rows of a form; ESP names no port.
export const keptForwards = (forwards: PortForward[]) =>
  forwards.filter((f) => f.proto === 'esp' || f.listen.trim() !== '');
