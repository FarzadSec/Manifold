import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { z } from 'zod';

import { keys } from '@/api/queryKeys';
import { AppReportSchema } from '@/generated/zod';
import type { AppReport } from '@/generated/types';
import { HttpUtil } from '@/utils';
import { parseMsg } from '@/utils/zodValidate';

const AppReportListSchema = z.array(AppReportSchema);
const NO_REPORTS: AppReport[] = [];

async function fetchAppReports(): Promise<AppReport[]> {
  const msg = await HttpUtil.get('/panel/api/app/reports', undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch app reports');
  const validated = parseMsg(msg, AppReportListSchema, 'app-reports');
  return Array.isArray(validated.obj) ? validated.obj : [];
}

export async function fetchAppReport(id: number): Promise<AppReport> {
  const msg = await HttpUtil.get(`/panel/api/app/reports/${id}`, undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch the report');
  return parseMsg(msg, AppReportSchema, 'app-report').obj as AppReport;
}

// The sidebar shows how many reports no one has opened; it polls so a new one shows up.
export function useUnseenAppReports(enabled: boolean): number {
  const query = useQuery({
    queryKey: keys.appReports.unseen(),
    enabled,
    queryFn: async () => {
      const msg = await HttpUtil.get('/panel/api/app/reports/unseen', undefined, { silent: true });
      return msg?.success && typeof msg.obj === 'number' ? msg.obj : 0;
    },
    refetchInterval: 60_000,
  });
  return query.data ?? 0;
}

export function useAppReports() {
  const queryClient = useQueryClient();
  const query = useQuery({ queryKey: keys.appReports.list(), queryFn: fetchAppReports });
  const refresh = () => queryClient.invalidateQueries({ queryKey: keys.appReports.root() });
  const markSeen = useMutation({
    mutationFn: (id: number) =>
      HttpUtil.post(`/panel/api/app/reports/seen/${id}`, undefined, { silent: true }),
    onSuccess: (msg) => {
      if (msg?.success) refresh();
    },
  });
  const remove = useMutation({
    mutationFn: (id: number) => HttpUtil.post(`/panel/api/app/reports/del/${id}`),
    onSuccess: (msg) => {
      if (msg?.success) refresh();
    },
  });
  return {
    reports: query.data ?? NO_REPORTS,
    loading: query.isFetching,
    fetchError: query.error ? (query.error as Error).message : '',
    markSeen: (id: number) => markSeen.mutateAsync(id),
    remove: (id: number) => remove.mutateAsync(id),
  };
}
