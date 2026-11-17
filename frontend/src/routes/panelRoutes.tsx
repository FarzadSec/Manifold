import { lazy } from 'react';
import type { ComponentType } from 'react';
import {
  CloudUploadOutlined,
  ShareAltOutlined,
  ApiOutlined,
  NotificationOutlined,
  AuditOutlined,
  ClusterOutlined,
  CrownOutlined,
  DashboardOutlined,
  DeploymentUnitOutlined,
  ExportOutlined,
  GlobalOutlined,
  ImportOutlined,
  ProfileOutlined,
  SafetyCertificateOutlined,
  SettingOutlined,
  SwapOutlined,
  TagsOutlined,
  TeamOutlined,
  ToolOutlined,
} from '@ant-design/icons';

import { isTunnelEdition } from '@/utils/edition';
import type { Access } from '@/api/queries/useAccess';

const IndexPage = lazy(() => import('@/pages/index/IndexPage'));
const InboundsPage = lazy(() => import('@/pages/inbounds/InboundsPage'));
const ClientsPage = lazy(() => import('@/pages/clients/ClientsPage'));
const GroupsPage = lazy(() => import('@/pages/groups/GroupsPage'));
const NodesPage = lazy(() => import('@/pages/nodes/NodesPage'));
const HostsPage = lazy(() => import('@/pages/hosts/HostsPage'));
const TunnelsPage = lazy(() => import('@/pages/tunnels/TunnelsPage'));
const IngressPage = lazy(() => import('@/pages/ingress/IngressPage'));
const BackupsPage = lazy(() => import('@/pages/backups/BackupsPage'));
const WebhooksPage = lazy(() => import('@/pages/webhooks/WebhooksPage'));
const CertificatesPage = lazy(() => import('@/pages/certs/CertificatesPage'));
const SettingsPage = lazy(() => import('@/pages/settings/SettingsPage'));
const XrayPage = lazy(() => import('@/pages/xray/XrayPage'));
const ApiDocsPage = lazy(() => import('@/pages/api-docs/ApiDocsPage'));
const AdminsPage = lazy(() => import('@/pages/access/AdminsPage'));
const RolesPage = lazy(() => import('@/pages/access/RolesPage'));
const PlansPage = lazy(() => import('@/pages/access/PlansPage'));
const AuditPage = lazy(() => import('@/pages/access/AuditPage'));

export interface PanelRoute {
  path: string;
  titleKey: string;
  icon: ComponentType;
  component: ComponentType;
  // The permission the page needs; staff pages are never a reseller's.
  perm: string;
  staff?: boolean;
}

// The router, the sidebar, the page titles and the command palette all read this list, so a
// page cannot go missing from one of them.
export const PANEL_ROUTES: PanelRoute[] = [
  {
    path: '/',
    titleKey: 'menu.dashboard',
    icon: DashboardOutlined,
    component: IndexPage,
    perm: '',
  },
  {
    path: '/inbounds',
    titleKey: 'menu.inbounds',
    icon: ImportOutlined,
    component: InboundsPage,
    perm: 'pool:read',
  },
  {
    path: '/clients',
    titleKey: 'menu.clients',
    icon: TeamOutlined,
    component: ClientsPage,
    perm: 'users:read',
  },
  {
    path: '/groups',
    titleKey: 'menu.groups',
    icon: TagsOutlined,
    component: GroupsPage,
    perm: 'users:read',
    staff: true,
  },
  {
    path: '/nodes',
    titleKey: 'menu.nodes',
    icon: ClusterOutlined,
    component: NodesPage,
    perm: 'nodes:read',
  },
  {
    path: '/hosts',
    titleKey: 'menu.hosts',
    icon: GlobalOutlined,
    component: HostsPage,
    perm: 'pool:read',
  },
  {
    path: '/tunnels',
    titleKey: 'menu.tunnels',
    icon: DeploymentUnitOutlined,
    component: TunnelsPage,
    perm: 'tunnels:read',
  },
  {
    path: '/ingress',
    titleKey: 'menu.ingress',
    icon: ShareAltOutlined,
    component: IngressPage,
    perm: 'settings:read',
    staff: true,
  },
  {
    path: '/backups',
    titleKey: 'menu.backups',
    icon: CloudUploadOutlined,
    component: BackupsPage,
    perm: 'backup:read',
    staff: true,
  },
  {
    path: '/certs',
    titleKey: 'menu.certs',
    icon: SafetyCertificateOutlined,
    component: CertificatesPage,
    perm: 'settings:read',
    staff: true,
  },
  {
    path: '/webhooks',
    titleKey: 'menu.webhooks',
    icon: NotificationOutlined,
    component: WebhooksPage,
    perm: 'users:read',
  },
  {
    path: '/outbound',
    titleKey: 'menu.outbounds',
    icon: ExportOutlined,
    component: XrayPage,
    perm: 'pool:read',
  },
  {
    path: '/routing',
    titleKey: 'menu.routing',
    icon: SwapOutlined,
    component: XrayPage,
    perm: 'pool:read',
  },
  {
    path: '/settings',
    titleKey: 'menu.settings',
    icon: SettingOutlined,
    component: SettingsPage,
    perm: 'settings:read',
  },
  {
    path: '/xray',
    titleKey: 'menu.xray',
    icon: ToolOutlined,
    component: XrayPage,
    perm: 'pool:read',
  },
  {
    path: '/admins',
    titleKey: 'menu.admins',
    icon: CrownOutlined,
    component: AdminsPage,
    perm: 'admins:write',
  },
  {
    path: '/roles',
    titleKey: 'menu.roles',
    icon: SafetyCertificateOutlined,
    component: RolesPage,
    perm: 'admins:write',
  },
  {
    path: '/plans',
    titleKey: 'menu.plans',
    icon: ProfileOutlined,
    component: PlansPage,
    perm: 'plans:write',
  },
  {
    path: '/audit',
    titleKey: 'menu.audit',
    icon: AuditOutlined,
    component: AuditPage,
    perm: 'audit:read',
  },
  {
    path: '/api-docs',
    titleKey: 'menu.apiDocs',
    icon: ApiOutlined,
    component: ApiDocsPage,
    perm: '',
  },
];

// A relay running the tunnel edition has no inbounds, clients or Xray to manage.
const TUNNEL_EDITION_PATHS = ['/', '/tunnels', '/settings', '/api-docs'];

// The pages the signed-in account may open: the edition's, then its permissions'.
export function navigableRoutes(access?: Access): PanelRoute[] {
  const edition = isTunnelEdition()
    ? PANEL_ROUTES.filter((route) => TUNNEL_EDITION_PATHS.includes(route.path))
    : PANEL_ROUTES;
  if (!access) return edition;
  return edition.filter((route) => access.can(route.perm) && !(route.staff && access.isReseller));
}

export const PANEL_ROUTE_PATHS = PANEL_ROUTES.map((route) => route.path);

export const PANEL_TITLE_KEYS: Record<string, string> = Object.fromEntries(
  PANEL_ROUTES.map((route) => [route.path, route.titleKey]),
);
