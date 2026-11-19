import { describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';

import { BenchReportView } from '@/pages/tunnels/TunnelBenchCard';
import TunnelBenchCard from '@/pages/tunnels/TunnelBenchCard';
import TunnelFormModal from '@/pages/tunnels/TunnelFormModal';
import { pickToRequest } from '@/pages/tunnels/benchInfo';
import type { TunnelBenchPick, TunnelBenchReport, TunnelBenchState } from '@/generated/types';
import { HttpUtil, Msg } from '@/utils';
import { fieldLabels, renderWithProviders } from './test-utils';

// Values sit in Unicode isolates inside translated text; what a reader sees has none.
const withoutIsolates = {
  normalizer: (text: string) => text.replace(/[\u2068\u2069]/g, '').trim(),
};

const best: TunnelBenchPick = {
  key: 'l3:udp:client',
  kind: 'l3',
  carrier: 'udp',
  dialer: 'client',
  mtu: 1360,
  score: 92.4,
  remoteAddr: '198.51.100.7',
  tuned: true,
};

function report(over: Partial<TunnelBenchReport>): TunnelBenchReport {
  return {
    id: 'abcdef0123456789abcdef01',
    state: 'done',
    stage: 'tune',
    initiator: true,
    role: 'client',
    peer: '198.51.100.7',
    peerVersion: '0.22.0',
    profile: 'standard',
    current: '',
    phase: '',
    done: 10,
    total: 10,
    results: [],
    tuning: [],
    best,
    warnings: [],
    bytes: 1 << 30,
    startedAt: 1,
    finishedAt: 2,
    ...over,
  };
}

describe('tunnel test report', () => {
  it('opens the recommended tunnel in the form on the panel that serves the accounts', () => {
    const onUse = vi.fn();
    renderWithProviders(<BenchReportView report={report({})} onUsePick={onUse} />);
    expect(screen.getByText('Recommended tunnel')).toBeTruthy();
    expect(screen.getByText('1360, found best by the tuning round', withoutIsolates)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Create this tunnel' }));
    expect(onUse).toHaveBeenCalledWith(best);
  });

  it('on the relay, says where to create it instead', () => {
    renderWithProviders(
      <BenchReportView report={report({ role: 'server', initiator: false })} onUsePick={vi.fn()} />,
    );
    expect(screen.queryByRole('button', { name: 'Create this tunnel' })).toBeNull();
    expect(
      screen.getByText(/Create it on the Manifold panel that serves the accounts/),
    ).toBeTruthy();
  });

  it('fills the tunnel form with the pick, which saves as that tunnel', async () => {
    const post = vi.mocked(HttpUtil.post).mockResolvedValue(new Msg(true, '', {}));
    vi.mocked(HttpUtil.get).mockImplementation(async (url: string) =>
      url === '/panel/api/tunnels/suggest'
        ? new Msg(true, '', {
            localIp: '10.80.0.2/30',
            peerIp: '10.80.0.1',
            port: 24000,
            token: 'ab'.repeat(24),
            address: '203.0.113.10',
          })
        : new Msg(true, '', {}),
    );
    const onClose = vi.fn();
    renderWithProviders(
      <TunnelFormModal
        open
        tunnel={null}
        draft={pickToRequest(best, 'l3-udp-client')}
        relays={[]}
        onClose={onClose}
      />,
    );
    await waitFor(() => expect(fieldLabels()).toContain('Carrier'));
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(onClose).toHaveBeenCalledWith(true));
    const [url, body] = post.mock.calls.at(-1)!;
    expect(url).toBe('/panel/api/tunnels/add');
    expect(body).toMatchObject({
      name: 'l3-udp-client',
      kind: 'l3',
      role: 'client',
      settings: {
        carrier: 'udp',
        dialer: 'client',
        mtu: 1360,
        remoteAddr: '198.51.100.7',
        localIp: '10.80.0.2/30',
        port: 24000,
      },
    });
  });
});

describe('tunnel test card', () => {
  it('shows the link waiting for the other server, with its ports', async () => {
    const state: TunnelBenchState = {
      active: report({ state: 'waiting', best: null, role: 'server', initiator: false }),
      link: {
        code: 'manifold-tunnel-test://abc',
        role: 'server',
        url: 'https://r.example.com/',
        addr4: '198.51.100.7',
        addr6: '',
        port: 41000,
        expires: Date.now() + 60_000,
      },
      catalogue: [],
      profiles: [],
    };
    vi.mocked(HttpUtil.get).mockImplementation(async (url: string) =>
      url === '/panel/api/tunnels/bench/state' ? new Msg(true, '', state) : new Msg(true, '', {}),
    );
    renderWithProviders(<TunnelBenchCard onUsePick={vi.fn()} />);
    await waitFor(() =>
      expect(screen.getByText('Waiting for the other server to run this link')).toBeTruthy(),
    );
    expect(screen.getByDisplayValue('manifold-tunnel-test://abc')).toBeTruthy();
    expect(screen.getByText(/uses ports 41000 to 41079/, withoutIsolates)).toBeTruthy();
    // A link waiting here holds the server; another test cannot start beside it.
    expect(screen.getByRole('button', { name: /Run a test link/ }).hasAttribute('disabled')).toBe(
      true,
    );
  });
});
