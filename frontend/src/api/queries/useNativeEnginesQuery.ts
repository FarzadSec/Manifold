import { useQuery } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';
import { keys } from '@/api/queryKeys';
import type { EngineStatus } from '@/generated/types';

async function fetchNativeEngines(): Promise<EngineStatus[]> {
  const msg = await HttpUtil.get<EngineStatus[]>('/panel/api/inbounds/engines', undefined, {
    silent: true,
  });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch the engines');
  return Array.isArray(msg.obj) ? msg.obj : [];
}

// Which engines outside Xray the panel server can run; asked only while a form needs it.
export function useNativeEnginesQuery(enabled: boolean) {
  return useQuery({
    queryKey: keys.inbounds.engines(),
    queryFn: fetchNativeEngines,
    enabled,
    staleTime: 30_000,
  });
}
