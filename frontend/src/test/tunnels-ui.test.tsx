import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';

import TunnelFormModal from '@/pages/tunnels/TunnelFormModal';
import RelaysTable from '@/pages/tunnels/RelaysTable';
import TunnelsCard from '@/pages/index/TunnelsCard';
import TunnelGroupsCard from '@/pages/tunnels/TunnelGroupsCard';
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

describe('tunnel forwards in both directions', () => {
  it("saves the client end's own forwards to the relay beside the relay's", async () => {
    const post = vi.mocked(HttpUtil.post).mockResolvedValue(new Msg(true, '', {}));
    const onClose = vi.fn();
    renderWithProviders(
      <TunnelFormModal
        open
        relays={[]}
        tunnel={tunnel({
          settings: {
            remoteAddr: '198.51.100.7',
            localIp: '10.80.0.2/30',
            peerIp: '10.80.0.1',
            forwards: [{ listen: '443', proto: 'tcp' }],
          },
        })}
        onClose={onClose}
      />,
    );
    await waitFor(() => expect(fieldLabels()).toContain('Relay port'));
    fireEvent.click(screen.getByRole('button', { name: 'Add client end port' }));
    await waitFor(() => expect(fieldLabels()).toContain('Client end port'));
    fireEvent.change(screen.getByPlaceholderText('9090'), { target: { value: '9090' } });
    fireEvent.change(screen.getByPlaceholderText('19090'), { target: { value: '19090' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(onClose).toHaveBeenCalledWith(true));
    expect(post.mock.calls.at(-1)![1]).toMatchObject({
      settings: {
        forwards: [{ listen: '443', proto: 'tcp' }],
        reverseForwards: [{ listen: '9090', target: '19090', proto: 'tcp' }],
      },
    });
  });

  it('offers no client-end forwards on a BackPack reverse tunnel, which cannot carry them', async () => {
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
    expect(screen.queryByRole('button', { name: 'Add client end port' })).toBeNull();
  });
});

describe('tunnel balancer on the managing panel', () => {
  it("builds a group from this panel's ends of one relay's tunnels and places it on that relay", async () => {
    vi.mocked(HttpUtil.get).mockResolvedValue(new Msg(true, '', []));
    const post = vi.mocked(HttpUtil.post).mockResolvedValue(new Msg(true, '', {}));
    const relayed = (id: number, name: string) =>
      tunnel({
        id,
        name,
        relayId: 4,
        settings: { remoteAddr: '198.51.100.7', peerIp: '10.80.0.1' },
      });
    renderWithProviders(
      <TunnelGroupsCard
        tunnels={[relayed(1, 'gre-path'), relayed(2, 'ipip-path')]}
        relays={[{ id: 4, name: 'uk relay', url: 'https://uk/', version: '', createdAt: 0 }]}
      />,
    );
    fireEvent.click(await screen.findByRole('button', { name: 'Add a group' }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText('uk relay')).toBeTruthy();
    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'uk paths' } });
    fireEvent.mouseDown(within(dialog).getByLabelText('Tunnels'));
    fireEvent.click(await screen.findByText('gre-path · 198.51.100.7'));
    fireEvent.click(await screen.findByText('ipip-path · 198.51.100.7'));
    fireEvent.change(within(dialog).getByLabelText('Relay port'), { target: { value: '443' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(post).toHaveBeenCalled());
    expect(post.mock.calls.at(-1)![1]).toMatchObject({
      name: 'uk paths',
      relayId: 4,
      members: [1, 2],
      forwards: [{ listen: '443', proto: 'tcp' }],
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

describe('Manifold Relay', () => {
  afterEach(() => {
    delete window.X_UI_EDITION;
  });

  it('lists every end on the overview with its forwarded ports and the peer', async () => {
    window.X_UI_EDITION = 'tunnel';
    vi.mocked(HttpUtil.get).mockResolvedValue(
      new Msg(true, '', [
        tunnel({
          kind: 'reverse',
          role: 'server',
          name: 'origin-de',
          settings: {
            transport: 'tcpmux',
            port: 8443,
            forwards: [{ listen: '443' }, { listen: '2201', target: '22' }],
          },
          status: {
            id: 7,
            state: 'up',
            latencyMs: 0,
            loss: 0,
            rxBytes: 0,
            txBytes: 0,
            peer: '198.51.100.7:52011',
            since: 0,
          },
        }),
      ]),
    );
    renderWithProviders(
      <MemoryRouter>
        <TunnelsCard />
      </MemoryRouter>,
    );
    expect(await screen.findByText('443, 2201 → 22')).toBeTruthy();
    expect(screen.getByText('198.51.100.7:52011')).toBeTruthy();
  });

  it('checks each relay and marks one that runs another version or does not answer', async () => {
    vi.mocked(HttpUtil.get).mockImplementation(async (url: string) =>
      url === '/panel/api/tunnels/relays/1/tunnels'
        ? new Msg(true, '', [])
        : new Msg(false, 'de: connection refused'),
    );
    const onChecked = vi.fn();
    renderWithProviders(
      <RelaysTable
        relays={[
          { id: 1, name: 'uk', url: 'https://uk.example/', version: '0.13.0', createdAt: 0 },
          { id: 2, name: 'de', url: 'https://de.example/', version: '0.14.0', createdAt: 0 },
        ]}
        panelVersion="0.14.0"
        onEdit={vi.fn()}
        onDelete={vi.fn()}
        onChecked={onChecked}
      />,
    );
    expect(await screen.findByText('Answering')).toBeTruthy();
    expect(await screen.findByText('Not answering')).toBeTruthy();
    expect(screen.getByText('0.13.0').closest('.ant-tag')?.className).toContain('orange');
    expect(screen.getByText('0.14.0').closest('.ant-tag')?.className).toContain('green');
    await waitFor(() => expect(onChecked).toHaveBeenCalled());
  });
});
