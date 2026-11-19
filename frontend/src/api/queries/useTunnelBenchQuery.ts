import { useQuery } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';
import { keys } from '@/api/queryKeys';
import type { TunnelBench, TunnelBenchState } from '@/generated/types';

async function fetchBenchState(): Promise<TunnelBenchState> {
  const msg = await HttpUtil.get<TunnelBenchState>('/panel/api/tunnels/bench/state', undefined, {
    silent: true,
  });
  if (!msg?.success || !msg.obj) throw new Error(msg?.msg || 'Failed to fetch the tunnel test');
  return msg.obj;
}

async function fetchBenchHistory(): Promise<TunnelBench[]> {
  const msg = await HttpUtil.get<TunnelBench[]>('/panel/api/tunnels/bench/history', undefined, {
    silent: true,
  });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch the tunnel tests');
  return Array.isArray(msg.obj) ? msg.obj : [];
}

// A test under way, or a link waiting for the other server, moves on its own.
export function useTunnelBenchQuery() {
  return useQuery({
    queryKey: keys.tunnels.bench(),
    queryFn: fetchBenchState,
    refetchInterval: (query) => (query.state.data?.active ? 2000 : 15000),
  });
}

export function useTunnelBenchHistoryQuery(enabled: boolean) {
  return useQuery({
    queryKey: keys.tunnels.benchHistory(),
    queryFn: fetchBenchHistory,
    enabled,
  });
}
