import { expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';

import RolesPage from '@/pages/access/RolesPage';
import { HttpUtil, Msg } from '@/utils';
import { chooseSelectOption, makeTestQueryClient, renderWithProviders } from './test-utils';

const GB = 1024 ** 3;
const ME = {
  id: 1,
  username: 'owner',
  tier: 'admin',
  isOwner: true,
  permissions: ['admins:write', 'users:write', 'users:read'],
  tierCeilings: { admin: [], operator: ['users:write'], reseller: ['users:write'] },
};
const SELLER = {
  id: 7,
  name: 'Seller',
  description: '',
  tier: 'reseller',
  permissions: ['users:write'],
  usersReadScope: 'own',
  usersWriteScope: 'own',
  maxUsersPerAccount: 0,
  minUserBytes: GB,
  maxUserBytes: 10 * GB,
  minPlanDays: 7,
  maxPlanDays: 0,
  minDevicesPerUser: 0,
  maxDevicesPerUser: 3,
  createdAt: 0,
};

const PLAN = {
  id: 4,
  name: 'Month 50 GB',
  sortOrder: 0,
  totalBytes: 50 * GB,
  days: 30,
  startOnFirstUse: false,
  limitIp: 0,
  userLimit: 0,
  limitHwid: 0,
  speedLimit: 0,
  reset: 0,
  trafficReset: 'never',
  trafficResetDay: 1,
  trafficResetMonth: 0,
  trafficResetEvery: 0,
  trafficResetCalendar: '',
  allInbounds: true,
  inboundIds: [],
  group: '',
  namePrefix: '',
  nameSuffix: '',
  description: '',
  createdAt: 0,
};
const INBOUND = {
  id: 9,
  remark: 'Germany',
  tag: 'in-9',
  protocol: 'vless',
  port: 443,
  enable: true,
};

function renderPage() {
  vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
    if (url === '/panel/api/access/me') return new Msg(true, '', ME);
    if (url === '/panel/api/access/roles') return new Msg(true, '', [SELLER]);
    if (url === '/panel/api/access/plans') return new Msg(true, '', [PLAN]);
    if (url === '/panel/api/inbounds/options') return new Msg(true, '', [INBOUND]);
    return new Msg(true, '', []);
  });
  renderWithProviders(
    <MemoryRouter>
      <RolesPage />
    </MemoryRouter>,
    { queryClient: makeTestQueryClient() },
  );
}

// The list shows each bound as the range a role allows, open sides included.
it('lists a role bounds as ranges', async () => {
  renderPage();
  expect(await screen.findByText('1.0 GB–10.0 GB per user')).toBeTruthy();
  expect(screen.getByText('≥ 7 days')).toBeTruthy();
  expect(screen.getByText('≤ 3 devices')).toBeTruthy();
});

// The quota bounds are typed in GB but stored in bytes, as the server checks them.
it('saves quota bounds in bytes', async () => {
  renderPage();
  const post = vi.spyOn(HttpUtil, 'post').mockResolvedValue(new Msg(true, '', null));
  fireEvent.click(await screen.findByRole('button', { name: /Add role/ }));
  fireEvent.change(await screen.findByLabelText('Name'), { target: { value: 'Bounded' } });
  fireEvent.change(screen.getByLabelText('Smallest quota per user'), { target: { value: '5' } });
  fireEvent.change(screen.getByLabelText('Largest quota per user'), { target: { value: '50' } });
  fireEvent.change(screen.getByLabelText('Shortest plan (days)'), { target: { value: '30' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));

  const saved = () => post.mock.calls.find(([url]) => url === '/panel/api/access/roles/add');
  await waitFor(() => expect(saved()).toBeTruthy());
  expect(saved()?.[1]).toEqual(
    expect.objectContaining({ minUserBytes: 5 * GB, maxUserBytes: 50 * GB, minPlanDays: 30 }),
  );
});

// A role can name the plans and inbounds its accounts sell from, as an account can.
it('saves the plans and inbounds a role sells from', async () => {
  renderPage();
  const post = vi.spyOn(HttpUtil, 'post').mockResolvedValue(new Msg(true, '', null));
  fireEvent.click(await screen.findByRole('button', { name: /Add role/ }));
  fireEvent.change(await screen.findByLabelText('Name'), { target: { value: 'Desk' } });
  fireEvent.click(screen.getByRole('switch', { name: 'Sell from plans only' }));
  chooseSelectOption((await screen.findByLabelText('Allowed plans')).id, 'Month 50 GB');
  fireEvent.click(screen.getByRole('switch', { name: 'Only chosen inbounds' }));
  chooseSelectOption((await screen.findByLabelText('Allowed inbounds')).id, 'Germany · vless:443');
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));

  const saved = () => post.mock.calls.find(([url]) => url === '/panel/api/access/roles/add');
  await waitFor(() => expect(saved()).toBeTruthy());
  expect(saved()?.[1]).toEqual(
    expect.objectContaining({
      plansOnly: true,
      allowedPlanIds: [4],
      inboundsLimited: true,
      allowedInboundIds: [9],
    }),
  );
});

// What switching off and running out do is set per role; the allowance only exists for resellers.
it('saves what switching off and running out do to a role', async () => {
  renderPage();
  const post = vi.spyOn(HttpUtil, 'post').mockResolvedValue(new Msg(true, '', null));
  fireEvent.click(await screen.findByRole('button', { name: /Add role/ }));
  fireEvent.change(await screen.findByLabelText('Name'), { target: { value: 'Shop' } });
  expect(
    screen.queryByRole('switch', { name: 'Lock sign-in while the allowance is used up' }),
  ).toBeNull();
  chooseSelectOption(screen.getByLabelText('Based on').id, 'Reseller');
  fireEvent.click(
    screen.getByRole('switch', { name: 'Switch its users off while the account is off' }),
  );
  fireEvent.click(
    screen.getByRole('switch', { name: 'Keep its users on when the allowance runs out' }),
  );
  fireEvent.click(
    screen.getByRole('switch', { name: 'Lock sign-in while the allowance is used up' }),
  );
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));

  const saved = () => post.mock.calls.find(([url]) => url === '/panel/api/access/roles/add');
  await waitFor(() => expect(saved()).toBeTruthy());
  expect(saved()?.[1]).toEqual(
    expect.objectContaining({
      tier: 'reseller',
      usersOffWhenDisabled: true,
      keepUsersWhenLimited: true,
      lockWhenLimited: true,
    }),
  );
});
