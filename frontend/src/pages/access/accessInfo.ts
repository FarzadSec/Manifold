export const GIB = 1024 ** 3;

export const TIERS = ['admin', 'operator', 'reseller'] as const;

export const WEEKDAYS = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'] as const;

export const TIER_COLORS: Record<string, string> = {
  admin: 'gold',
  operator: 'blue',
  reseller: 'purple',
};

// The role and token forms show the catalogue in these groups, as the API names them.
export const PERMISSION_GROUPS: { key: string; perms: string[] }[] = [
  { key: 'users', perms: ['users:read', 'users:write', 'plans:write'] },
  {
    key: 'fleet',
    perms: ['nodes:read', 'nodes:write', 'tunnels:read', 'tunnels:write'],
  },
  {
    key: 'configuration',
    perms: ['pool:read', 'pool:write', 'settings:read', 'settings:write'],
  },
  { key: 'operations', perms: ['stats:read', 'tools:write', 'services:write'] },
  {
    key: 'administration',
    perms: [
      'admins:write',
      'tokens:write',
      'backup:read',
      'backup:write',
      'backup:deliver',
      'audit:read',
    ],
  },
];

export function permKey(perm: string): string {
  return `pages.access.perm.${perm.replace(':', '_')}`;
}

export function formatBytes(bytes: number): string {
  if (!bytes) return '0';
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  let v = bytes;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i += 1;
  }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`;
}
