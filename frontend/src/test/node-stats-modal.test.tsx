import { screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import NodeStatsModal from '@/pages/nodes/NodeStatsModal';
import { HttpUtil } from '@/utils';

import { renderWithProviders } from './test-utils';

function mockStats(obj: unknown) {
  return vi.spyOn(HttpUtil, 'get').mockResolvedValue({ success: true, msg: '', obj } as never);
}

afterEach(() => vi.restoreAllMocks());

describe('node stats window', () => {
  it('reports uptime over watched time and lists who used the node', async () => {
    const get = mockStats({
      from: 0,
      to: 0,
      uptimePct: 83.3333,
      watchedPct: 50,
      availability: [
        { from: 1, state: 'up' },
        { from: 2, state: 'down' },
        { from: 3, state: 'unwatched' },
      ],
      points: [],
      users: [{ email: 'alice', up: 10, down: 20 }],
    });
    renderWithProviders(<NodeStatsModal node={{ id: 7, name: 'edge' }} onClose={vi.fn()} />);
    expect(await screen.findByText('83.33%')).toBeTruthy();
    expect(screen.getByText('alice')).toBeTruthy();
    expect(get).toHaveBeenCalledWith(
      '/panel/api/nodes/stats/7',
      { window: '24h' },
      { silent: true },
    );
  });

  it('shows no uptime figure for a window the panel never watched', async () => {
    mockStats({
      from: 0,
      to: 0,
      uptimePct: 0,
      watchedPct: 0,
      availability: [],
      points: [],
      users: [{ email: 'bob', up: 1, down: 1 }],
    });
    renderWithProviders(<NodeStatsModal node={{ id: 7, name: 'edge' }} onClose={vi.fn()} />);
    expect(await screen.findByText('bob')).toBeTruthy();
    expect(screen.getByText('—')).toBeTruthy();
    expect(screen.queryByText('0.00%')).toBeNull();
  });
});
