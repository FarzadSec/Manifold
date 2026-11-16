import { useQuery } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';
import { keys } from '@/api/queryKeys';
import type { TunnelRelay, TunnelView } from '@/generated/types';

async function fetchTunnels(): Promise<TunnelView[]> {
  const msg = await HttpUtil.get<TunnelView[]>('/panel/api/tunnels/list', undefined, {
    silent: true,
  });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch tunnels');
  return Array.isArray(msg.obj) ? msg.obj : [];
}

async function fetchRelays(): Promise<TunnelRelay[]> {
  const msg = await HttpUtil.get<TunnelRelay[]>('/panel/api/tunnels/relays', undefined, {
    silent: true,
  });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch relays');
  return Array.isArray(msg.obj) ? msg.obj : [];
}

// A tunnel's state changes on its own, so the list is read again while it is shown.
export function useTunnelsQuery() {
  return useQuery({
    queryKey: keys.tunnels.list(),
    queryFn: fetchTunnels,
    refetchInterval: 5000,
  });
}

export function useTunnelRelaysQuery() {
  return useQuery({ queryKey: keys.tunnels.relays(), queryFn: fetchRelays });
}
