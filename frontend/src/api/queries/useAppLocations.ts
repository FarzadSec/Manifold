import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { z } from 'zod';

import { keys } from '@/api/queryKeys';
import { AppLocationSchema } from '@/generated/zod';
import type { AppLocation } from '@/generated/types';
import { HttpUtil } from '@/utils';
import { parseMsg } from '@/utils/zodValidate';

const AppLocationListSchema = z.array(AppLocationSchema);
const NO_LOCATIONS: AppLocation[] = [];

async function fetchAppLocations(): Promise<AppLocation[]> {
  const msg = await HttpUtil.get('/panel/api/app/locations', undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch app locations');
  const validated = parseMsg(msg, AppLocationListSchema, 'app-locations');
  return Array.isArray(validated.obj) ? validated.obj : [];
}

export function useAppLocations() {
  const queryClient = useQueryClient();
  const query = useQuery({ queryKey: keys.appLocations.list(), queryFn: fetchAppLocations });
  const save = useMutation({
    mutationFn: (location: AppLocation) => HttpUtil.post('/panel/api/app/locations', location),
    onSuccess: (msg) => {
      if (msg?.success) queryClient.invalidateQueries({ queryKey: keys.appLocations.list() });
    },
  });
  return {
    locations: query.data ?? NO_LOCATIONS,
    loading: query.isFetching,
    fetchError: query.error ? (query.error as Error).message : '',
    save: (location: AppLocation) => save.mutateAsync(location),
  };
}
