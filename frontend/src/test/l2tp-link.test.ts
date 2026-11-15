import { describe, expect, it } from 'vitest';

import {
  isL2tpLink,
  l2tpAccountFileName,
  l2tpAccountFromLink,
  l2tpAccountText,
  l2tpAccountsFromLinks,
} from '@/lib/xray/l2tp-link';

// Rendered by internal/l2tp.ShareLink for a password and a key full of
// characters that mean something in a URL.
const LINK =
  'l2tp://alice%40shop%2F1:p%40ss%3Aw%2Frd%231%20%3F&=@203.0.113.9?ipsec=1&psk=k%26e%3Dy%2B1%3B2%2C3~#DE%20%231%20%2F%20fast';

describe('l2tp links', () => {
  it('gives back what a person types into their device', () => {
    expect(isL2tpLink(`  ${LINK}\n`)).toBe(true);
    expect(l2tpAccountFromLink(LINK)).toEqual({
      host: '203.0.113.9',
      username: 'alice@shop/1',
      password: 'p@ss:w/rd#1 ?&=',
      psk: 'k&e=y+1;2,3~',
      requiresIpsec: true,
      remark: 'DE #1 / fast',
    });
  });

  it('reads an IPv6 server without its brackets and a server that does not require IPsec', () => {
    expect(
      l2tpAccountFromLink('l2tp://bob:secret-pass@[2001:db8::1]?ipsec=0&psk=shared-key-123'),
    ).toEqual({
      host: '2001:db8::1',
      username: 'bob',
      password: 'secret-pass',
      psk: 'shared-key-123',
      requiresIpsec: false,
      remark: '',
    });
  });

  it('returns nothing for a link it cannot read', () => {
    expect(l2tpAccountFromLink('vless://uuid@host:443')).toBeNull();
    expect(l2tpAccountFromLink('l2tp://')).toBeNull();
    expect(l2tpAccountFromLink('l2tp://host-without-a-login')).toBeNull();
    expect(isL2tpLink('pptp://a:b@c')).toBe(false);
  });

  it('lays the account out as the lines a customer copies', () => {
    const account = l2tpAccountFromLink(LINK);
    expect(account).not.toBeNull();
    const text = l2tpAccountText(account!, {
      protocol: 'Protocol',
      server: 'Server',
      username: 'Username',
      password: 'Password',
      psk: 'IPsec key (PSK)',
    });
    expect(text).toBe(
      [
        'Protocol: L2TP/IPsec PSK',
        'Server: 203.0.113.9',
        'Username: alice@shop/1',
        'Password: p@ss:w/rd#1 ?&=',
        'IPsec key (PSK): k&e=y+1;2,3~',
        '',
      ].join('\n'),
    );
    expect(l2tpAccountFileName(account!.remark, 'alice')).toBe('DE_1_fast.txt');
    expect(l2tpAccountFileName('', '')).toBe('l2tp.txt');
  });

  it('keeps only the L2TP accounts of a mixed list, in order', () => {
    const accounts = l2tpAccountsFromLinks([
      'vless://uuid@host:443#x',
      LINK,
      'l2tp://bob:secret-pass@vpn.example.com?ipsec=1&psk=shared-key-123#Berlin',
    ]);
    expect(accounts.map((a) => [a.key, a.account.username, a.account.remark])).toEqual([
      ['l2tp-1', 'alice@shop/1', 'DE #1 / fast'],
      ['l2tp-2', 'bob', 'Berlin'],
    ]);
  });
});
