import type { TunnelView } from '@/generated/types';

const KERNEL = ['gre', 'gretap', 'ip6gre', 'ip6gretap', 'ipip', 'sit'];

// groupCandidates are the tunnels that can carry a group's ports: relay ends with a
// tunnel address, as the server checks too.
export function groupCandidates(tunnels: TunnelView[]): TunnelView[] {
  return tunnels.filter((t) => t.role === 'server' && (KERNEL.includes(t.kind) || t.kind === 'l3'));
}

// activeMembers is where a group sends its ports now, as the relay decides it: the first
// member that answers in failover, every one that answers in balance, all when none does.
export function activeMembers(mode: string, members: number[], tunnels: TunnelView[]): number[] {
  const answering = members.filter((id) => {
    const state = tunnels.find((t) => t.id === id)?.status?.state;
    return state === 'up' || state === 'degraded';
  });
  const live = answering.length > 0 ? answering : members;
  return mode === 'failover' ? live.slice(0, 1) : live;
}
