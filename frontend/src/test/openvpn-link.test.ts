import { describe, expect, it } from 'vitest';

import {
  isOpenvpnLink,
  openvpnProfileFileName,
  openvpnProfileFromLink,
  summarizeOpenvpnProfile,
} from '@/lib/xray/openvpn-link';

// What internal/openvpn renders, with a remark whose bytes need both of
// base64url's replaced characters and a non-ASCII name.
const PROFILE = [
  '# آلمان >>>???',
  'client',
  'dev tun',
  'proto tcp-client',
  'remote vpn.example.com 8443',
  '<auth-user-pass>',
  'alice',
  'p@ss word',
  '</auth-user-pass>',
  '',
].join('\n');

function linkFor(profile: string): string {
  const bytes = new TextEncoder().encode(profile);
  let binary = '';
  for (const b of bytes) binary += String.fromCharCode(b);
  return `openvpn://${btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')}`;
}

describe('openvpn links', () => {
  it('gives back the profile exactly as the panel rendered it', () => {
    const link = linkFor(PROFILE);
    expect(link).toMatch(/[-_]/);
    expect(openvpnProfileFromLink(`  ${link}\n`)).toBe(PROFILE);
    expect(isOpenvpnLink(link)).toBe(true);
  });

  it('reads the row label out of the profile', () => {
    expect(summarizeOpenvpnProfile(PROFILE)).toEqual({
      remark: 'آلمان >>>???',
      port: '8443',
      network: 'tcp',
    });
    expect(summarizeOpenvpnProfile('client\nproto udp\nremote 192.0.2.1 1194\n')).toEqual({
      remark: '',
      port: '1194',
      network: 'udp',
    });
  });

  it('returns nothing for a link it cannot read', () => {
    expect(openvpnProfileFromLink('vless://uuid@host:443')).toBe('');
    expect(openvpnProfileFromLink('openvpn://%%%')).toBe('');
    expect(isOpenvpnLink('vpn://abc')).toBe(false);
  });

  it('names the download after the remark without characters a file system refuses', () => {
    expect(openvpnProfileFileName('Germany / alice@x.io', 'alice')).toBe('Germany_alice_x.io.ovpn');
    expect(openvpnProfileFileName('', 'alice')).toBe('alice.ovpn');
    expect(openvpnProfileFileName('آلمان', '')).toBe('profile.ovpn');
  });
});
