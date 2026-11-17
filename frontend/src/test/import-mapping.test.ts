import { describe, expect, it } from 'vitest';

import { defaultMapping, inboundsFor } from '@/pages/import/importMapping';

const inbounds = [
  { id: 1, protocol: 'vless', port: 443 },
  { id: 2, protocol: 'trojan', port: 8443 },
  { id: 3, protocol: 'trojan', port: 2083 },
  { id: 4, protocol: 'hysteria', port: 4443 },
];

describe('import mapping', () => {
  it('fills a protocol only when one inbound can take it', () => {
    expect(defaultMapping({ vless: 5, trojan: 3, vmess: 1, hysteria2: 2 }, inbounds)).toEqual({
      vless: [1],
      trojan: [],
      vmess: [],
      hysteria2: [4],
    });
  });

  it('offers hysteria inbounds to Hysteria 2 users', () => {
    expect(inboundsFor('hysteria2', inbounds).map((ib) => ib.id)).toEqual([4]);
  });
});
