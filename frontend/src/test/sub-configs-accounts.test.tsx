import { expect, it, vi } from 'vitest';
import { screen, within } from '@testing-library/react';

import SubConfigsTab from '@/pages/sub/SubConfigsTab';
import { renderWithProviders } from './test-utils';

const PROFILE = btoa('# Germany\nclient\nremote vpn.example.com 1194\nproto udp\n')
  .replace(/\+/g, '-')
  .replace(/\//g, '_')
  .replace(/=+$/, '');

// Under each service the user reads which account it is and how many may use it at once:
// the IP limit for an Xray config, the user limit and the password for OpenVPN.
it('names the account and its limit under every service', () => {
  renderWithProviders(
    <SubConfigsTab
      links={['vless://a@h:443?type=tcp#DE', `openvpn://${PROFILE}`]}
      emails={['ali', 'ali']}
      accounts={{ ali: { limitIp: 2, userLimit: 3, password: 'pw-ali' } }}
      onCopy={vi.fn()}
    />,
  );
  const lines = screen.getAllByTestId('sub-account');
  expect(lines).toHaveLength(2);
  expect(within(lines[0]).getByText('ali')).toBeTruthy();
  expect(within(lines[0]).getByText('2')).toBeTruthy();
  expect(within(lines[0]).queryByText('pw-ali')).toBeNull();
  expect(within(lines[1]).getByText('ali')).toBeTruthy();
  expect(within(lines[1]).getByText('3')).toBeTruthy();
  expect(within(lines[1]).getByText('pw-ali')).toBeTruthy();
});

it('shows an unlimited account as such', () => {
  renderWithProviders(
    <SubConfigsTab
      links={['vless://a@h:443?type=tcp#DE']}
      emails={['bob']}
      accounts={{ bob: { limitIp: 0, userLimit: 0 } }}
      onCopy={vi.fn()}
    />,
  );
  expect(within(screen.getByTestId('sub-account')).getByText('∞')).toBeTruthy();
});
