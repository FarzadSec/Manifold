import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';
import type { Msg } from '@/utils';
import { keys } from '@/api/queryKeys';
import type {
  AdminView,
  AuditPage,
  AuditSettings,
  MeView,
  OwnerView,
  Plan,
  Role,
} from '@/generated/types';

async function getObj<T>(url: string, params?: unknown): Promise<T> {
  const msg = await HttpUtil.get<T>(url, params, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || 'Request failed');
  return msg.obj as T;
}

export function postJson<T = unknown>(url: string, body?: unknown): Promise<Msg<T>> {
  return HttpUtil.post<T>(url, body ?? {}, { headers: { 'Content-Type': 'application/json' } });
}

export function useMe() {
  return useQuery({
    queryKey: keys.access.me(),
    queryFn: () => getObj<MeView>('/panel/api/access/me'),
    staleTime: 60_000,
  });
}

export interface Access {
  loaded: boolean;
  me?: MeView;
  can: (perm: string) => boolean;
  isReseller: boolean;
  isMainAdmin: boolean;
}

// Until the account is known every permission reads as held, so a main admin's pages never
// flicker; the server refuses what the account may not do whatever the page shows.
export function useAccess(): Access {
  const me = useMe();
  return useMemo(() => {
    const perms = new Set(me.data?.permissions ?? []);
    const loaded = !!me.data;
    return {
      loaded,
      me: me.data,
      can: (perm: string) => !loaded || perm === '' || perms.has(perm),
      isReseller: me.data?.tier === 'reseller',
      isMainAdmin: me.data?.tier === 'admin',
    };
  }, [me.data]);
}

export function useAdmins(enabled = true) {
  return useQuery({
    queryKey: keys.access.admins(),
    queryFn: async () => (await getObj<AdminView[]>('/panel/api/access/admins')) ?? [],
    enabled,
  });
}

export function useRoles(enabled = true) {
  return useQuery({
    queryKey: keys.access.roles(),
    queryFn: async () => (await getObj<Role[]>('/panel/api/access/roles')) ?? [],
    enabled,
  });
}

export function usePlans(enabled = true) {
  return useQuery({
    queryKey: keys.access.plans(),
    queryFn: async () => (await getObj<Plan[]>('/panel/api/access/plans')) ?? [],
    enabled,
  });
}

export function useOwners(enabled = true) {
  return useQuery({
    queryKey: keys.access.owners(),
    queryFn: async () => (await getObj<OwnerView[]>('/panel/api/access/owners')) ?? [],
    enabled,
    staleTime: 60_000,
  });
}

export interface AuditFilter {
  admin?: string;
  entity?: string;
  action?: string;
  from?: number;
  to?: number;
  page: number;
  pageSize: number;
}

export function useAuditPage(filter: AuditFilter) {
  return useQuery({
    queryKey: keys.access.audit({ ...filter }),
    queryFn: () => getObj<AuditPage>('/panel/api/access/audit', filter),
  });
}

export function useAuditSettings() {
  return useQuery({
    queryKey: keys.access.auditSettings(),
    queryFn: () => getObj<AuditSettings>('/panel/api/access/audit/settings'),
  });
}
