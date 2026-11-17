import { describe, expect, it } from 'vitest';

import { navigableRoutes } from '@/routes/panelRoutes';
import type { Access } from '@/api/queries/useAccess';

function accessFor(tier: string, perms: string[]): Access {
  const held = new Set(perms);
  return {
    loaded: true,
    can: (p: string) => p === '' || held.has(p),
    isReseller: tier === 'reseller',
    isMainAdmin: tier === 'admin',
  };
}

// The sidebar and the palette read this list, so a page the account cannot open must not be on it.
describe('navigable routes follow the account', () => {
  it('shows a reseller only its dashboard, its users, its webhooks and the API docs', () => {
    const paths = navigableRoutes(
      accessFor('reseller', ['users:read', 'users:write', 'stats:read']),
    ).map((r) => r.path);
    expect(paths).toEqual(['/', '/clients', '/webhooks', '/api-docs']);
  });

  it('keeps the panel administration from an operator', () => {
    const paths = navigableRoutes(
      accessFor('operator', [
        'users:read',
        'users:write',
        'plans:write',
        'pool:read',
        'settings:read',
      ]),
    ).map((r) => r.path);
    expect(paths).toContain('/plans');
    expect(paths).toContain('/groups');
    expect(paths).not.toContain('/admins');
    expect(paths).not.toContain('/audit');
  });
});
