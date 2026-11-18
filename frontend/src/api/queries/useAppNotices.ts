import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { z } from 'zod';

import { keys } from '@/api/queryKeys';
import { AppNoticeSchema } from '@/generated/zod';
import type { AppNotice } from '@/generated/types';
import { HttpUtil } from '@/utils';
import { parseMsg } from '@/utils/zodValidate';

const AppNoticeListSchema = z.array(AppNoticeSchema);
const NO_NOTICES: AppNotice[] = [];
const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;

export type AppNoticeLevel = 'info' | 'warning' | 'critical';

async function fetchAppNotices(): Promise<AppNotice[]> {
  const msg = await HttpUtil.get('/panel/api/app/notices', undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch app notices');
  const validated = parseMsg(msg, AppNoticeListSchema, 'app-notices');
  return Array.isArray(validated.obj) ? validated.obj : [];
}

export function useAppNotices() {
  const queryClient = useQueryClient();
  const query = useQuery({ queryKey: keys.appNotices.list(), queryFn: fetchAppNotices });
  const refresh = () => queryClient.invalidateQueries({ queryKey: keys.appNotices.list() });
  const publish = useMutation({
    mutationFn: (notice: { body: string; level: AppNoticeLevel }) =>
      HttpUtil.post('/panel/api/app/notices', notice, JSON_HEADERS),
    onSuccess: (msg) => {
      if (msg?.success) refresh();
    },
  });
  const remove = useMutation({
    mutationFn: (id: number) => HttpUtil.post(`/panel/api/app/notices/del/${id}`),
    onSuccess: (msg) => {
      if (msg?.success) refresh();
    },
  });
  return {
    notices: query.data ?? NO_NOTICES,
    loading: query.isFetching,
    fetchError: query.error ? (query.error as Error).message : '',
    publishing: publish.isPending,
    publish: (notice: { body: string; level: AppNoticeLevel }) => publish.mutateAsync(notice),
    remove: (id: number) => remove.mutateAsync(id),
  };
}
