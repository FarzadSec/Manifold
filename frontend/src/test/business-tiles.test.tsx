import { screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { describe, expect, it } from 'vitest';

import type { PanelSummary } from '@/generated/types';
import { BusinessTiles } from '@/pages/index/BusinessStrip';

import { renderWithProviders } from './test-utils';

const base: PanelSummary = {
  users: { total: 10, active: 7, online: 3, disabled: 1, expired: 2, expiring: 0, limited: 1 },
  trafficToday: 1024 * 1024 * 1024,
  nodesTotal: 0,
  nodesOnline: 0,
  webhooksPending: 0,
  webhooksFailed: 0,
  backupsFailing: 0,
  lastBackup: 0,
};

function render(summary: PanelSummary) {
  return renderWithProviders(
    <MemoryRouter>
      <BusinessTiles summary={summary} />
    </MemoryRouter>,
  );
}

describe('dashboard business tiles', () => {
  it('leaves out the node tile and the alerts while there is nothing to say', () => {
    render(base);
    expect(screen.getByRole('group', { name: 'Active users' }).textContent).toContain('of 10');
    expect(screen.queryByRole('group', { name: 'Nodes online' })).toBeNull();
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('flags nodes that are down and backups and webhooks that failed', () => {
    render({ ...base, nodesTotal: 3, nodesOnline: 2, backupsFailing: 1, webhooksFailed: 4 });
    const nodes = screen.getByRole('group', { name: 'Nodes online' });
    expect(nodes.textContent).toContain('2 / 3');
    expect(nodes.textContent).toContain('1 down');
    const alerts = screen.getAllByRole('alert').map((a) => a.textContent);
    expect(alerts).toEqual([
      expect.stringContaining('1 backup targets failed their last run'),
      expect.stringContaining('4 webhook deliveries were given up'),
    ]);
  });
});
