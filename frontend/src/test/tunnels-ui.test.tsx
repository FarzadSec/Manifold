import { describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';

import TunnelFormModal from '@/pages/tunnels/TunnelFormModal';
import TunnelsCard from '@/pages/index/TunnelsCard';
import type { TunnelView } from '@/generated/types';
import { HttpUtil, Msg } from '@/utils';
import { fieldLabels, renderWithProviders } from './test-utils';

function tunnel(over: Partial<TunnelView>): TunnelView {
  return {
    id: 7,
    pairId: 'abcdef0123456789abcdef01',
    name: 'uk',
    kind: 'gre',
    role: 'client',
    enable: true,
    relayId: 0,
    settings: { remoteAddr: '198.51.100.7', localIp: '10.80.0.2/30', peerIp: '10.80.0.1' },
    createdAt: 0,
    updatedAt: 0,
    ...over,
  };
}

describe('tunnel form', () => {
  it('asks a reverse client for its relay and no tunnel addresses', async () => {
    renderWithProviders(
      <TunnelFormModal
        open
        relays={[]}
        tunnel={tunnel({
          kind: 'reverse',
          settings: { transport: 'tcpmux', port: 8443, remoteAddr: 'r.example' },
        })}
        onClose={vi.fn()}
      />,
    );
    await waitFor(() => expect(fieldLabels()).toContain('Transport'));
    const labels = fieldLabels();
    expect(labels).toContain("Other server's address");
    expect(labels).toContain('Tunnel port');
    expect(labels).not.toContain("This end's address");
    expect(labels).not.toContain('GRE key');
  });

  it('saves an edited GRE end as JSON, keeping its settings', async () => {
    const post = vi.mocked(HttpUtil.post).mockResolvedValue(new Msg(true, '', {}));
    const onClose = vi.fn();
    renderWithProviders(<TunnelFormModal open relays={[]} tunnel={tunnel({})} onClose={onClose} />);
    await waitFor(() => expect(fieldLabels()).toContain('GRE key'));
    expect(fieldLabels()).toContain("Users' addresses");

    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(onClose).toHaveBeenCalledWith(true));
    const [url, body, options] = post.mock.calls.at(-1)!;
    expect(url).toBe('/panel/api/tunnels/update/7');
    expect(options).toEqual({ headers: { 'Content-Type': 'application/json' } });
    expect(body).toMatchObject({
      kind: 'gre',
      role: 'client',
      settings: { remoteAddr: '198.51.100.7', localIp: '10.80.0.2/30', peerIp: '10.80.0.1' },
    });
  });
});

describe('overview tunnels card', () => {
  it('counts the tunnels by state and names the one that is down', async () => {
    vi.mocked(HttpUtil.get).mockImplementation(async (url: string) =>
      url === '/panel/api/tunnels/list'
        ? new Msg(true, '', [
            tunnel({
              id: 1,
              name: 'uk',
              status: {
                id: 1,
                state: 'up',
                latencyMs: 40,
                loss: 0,
                rxBytes: 0,
                txBytes: 0,
                since: 0,
              },
            }),
            tunnel({
              id: 2,
              name: 'de',
              status: {
                id: 2,
                state: 'down',
                latencyMs: 0,
                loss: 100,
                rxBytes: 0,
                txBytes: 0,
                since: 0,
              },
            }),
          ])
        : new Msg(true, '', []),
    );
    renderWithProviders(
      <MemoryRouter>
        <TunnelsCard />
      </MemoryRouter>,
    );
    expect(await screen.findByText('1 Up')).toBeTruthy();
    expect(screen.getByText('1 Down')).toBeTruthy();
    expect(screen.getByText('de')).toBeTruthy();
  });
});
