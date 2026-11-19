import { describe, expect, it } from 'vitest';

import type { BenchResult, TunnelBenchPick } from '@/generated/types';
import {
  candidateFromKey,
  defaultPanelUrl,
  pickToRequest,
  sortBenchResults,
} from '@/pages/tunnels/benchInfo';

const pick = (over: Partial<TunnelBenchPick>): TunnelBenchPick => ({
  key: 'gre',
  kind: 'gre',
  score: 90,
  tuned: false,
  ...over,
});

describe('a recommended tunnel in the tunnel form', () => {
  it('keeps each kind to the settings it reads', () => {
    expect(pickToRequest(pick({ mtu: 1360, remoteAddr: '198.51.100.7' }), 'gre')).toEqual({
      name: 'gre',
      kind: 'gre',
      role: 'client',
      enable: true,
      relayId: 0,
      settings: { forwards: [], nat: 'masquerade', mtu: 1360, remoteAddr: '198.51.100.7' },
    });
    const reverse = pickToRequest(
      pick({
        key: 'reverse:wsmux',
        kind: 'reverse',
        transport: 'wsmux',
        remoteAddr: '198.51.100.7',
      }),
      'r',
    );
    expect(reverse.settings).toEqual({
      forwards: [],
      nat: 'masquerade',
      transport: 'wsmux',
      remoteAddr: '198.51.100.7',
    });
    // The relay dials this layer-3 tunnel, so the panel's end listens and names no remote.
    const l3 = pickToRequest(
      pick({ key: 'l3:quic:server', kind: 'l3', carrier: 'quic', dialer: 'server', mtu: 1280 }),
      'l',
    );
    expect(l3.settings).toEqual({
      forwards: [],
      nat: 'masquerade',
      carrier: 'quic',
      dialer: 'server',
      mtu: 1280,
    });
  });
});

describe('the panel address a test link offers', () => {
  it('is the address the browser reached, with the base path', () => {
    expect(
      defaultPanelUrl({ origin: 'https://p.example.com:2053', hostname: 'p.example.com' }, '/x/'),
    ).toBe('https://p.example.com:2053/x/');
    expect(
      defaultPanelUrl({ origin: 'http://[2001:db8::1]:2053', hostname: '[2001:db8::1]' }, 'x'),
    ).toBe('http://[2001:db8::1]:2053/x/');
  });

  it('is left to the server when the browser came in through the loopback', () => {
    for (const hostname of ['localhost', '127.0.0.1', '127.0.1.1', '[::1]']) {
      expect(defaultPanelUrl({ origin: `http://${hostname}:2053`, hostname }, '/')).toBe('');
    }
  });
});

describe('the test results table', () => {
  it('lists what worked by score, then what failed, then what was skipped', () => {
    const r = (key: string, state: string, score: number) => ({ key, state, score }) as BenchResult;
    const sorted = sortBenchResults([
      r('sit', 'pending', 0),
      r('ipip', 'skipped', 0),
      r('gre', 'ok', 70),
      r('reverse:udp', 'failed', 0),
      r('l3:udp:client', 'ok', 91),
    ]);
    expect(sorted.map((x) => x.key)).toEqual([
      'l3:udp:client',
      'gre',
      'reverse:udp',
      'ipip',
      'sit',
    ]);
  });

  it('names a candidate under way from its key', () => {
    expect(candidateFromKey('l3:udp:client')).toEqual({
      key: 'l3:udp:client',
      kind: 'l3',
      carrier: 'udp',
      dialer: 'client',
    });
    expect(candidateFromKey('reverse:tcpmux')).toEqual({
      key: 'reverse:tcpmux',
      kind: 'reverse',
      transport: 'tcpmux',
    });
  });
});
