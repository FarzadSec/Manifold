import { expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';

import AdminFormModal from '@/pages/access/AdminFormModal';
import { overrideChoices } from '@/pages/access/PermissionOverrides';
import { HttpUtil, Msg } from '@/utils';
import type { Role } from '@/generated/types';
import {
  chooseSelectOption,
  listSelectOptions,
  makeTestQueryClient,
  renderWithProviders,
} from './test-utils';

const CEILINGS = {
  admin: ['users:read', 'users:write', 'plans:write'],
  operator: ['users:read', 'users:write', 'plans:write'],
  reseller: ['users:read', 'users:write'],
};
const READER: Role = {
  id: 3,
  name: 'Reader',
  description: '',
  tier: 'operator',
  permissions: ['users:read'],
  usersReadScope: '',
  usersWriteScope: '',
  maxUsersPerAccount: 0,
  minUserBytes: 0,
  maxUserBytes: 0,
  minPlanDays: 0,
  maxPlanDays: 0,
  minDevicesPerUser: 0,
  maxDevicesPerUser: 0,
  plansOnly: false,
  allowedPlanIds: [],
  inboundsLimited: false,
  allowedInboundIds: [],
  createdAt: 0,
};

function renderForm() {
  vi.spyOn(HttpUtil, 'get').mockResolvedValue(new Msg(true, '', []));
  const post = vi.spyOn(HttpUtil, 'post').mockResolvedValue(new Msg(true, '', null));
  renderWithProviders(
    <AdminFormModal
      open
      admin={null}
      roles={[READER]}
      plans={[]}
      ceilings={CEILINGS}
      onClose={() => undefined}
    />,
    { queryClient: makeTestQueryClient() },
  );
  fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'desk' } });
  fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'long-password' } });
  chooseSelectOption(screen.getByLabelText('Tier').id, 'Operator');
  return () => post.mock.calls.find(([url]) => url === '/panel/api/access/admins/add')?.[1];
}

// The pickers offer what the role leaves out to add, and what it gives to take away.
it('saves what an account adds to and takes from its role', async () => {
  const saved = renderForm();
  chooseSelectOption(screen.getByLabelText('Role').id, 'Reader');
  expect(listSelectOptions(screen.getByLabelText('Also allow').id)).toEqual([
    'Create and edit users',
    'Manage plans',
  ]);
  expect(listSelectOptions(screen.getByLabelText('Take away').id)).toEqual(['See users']);
  chooseSelectOption(screen.getByLabelText('Also allow').id, 'Manage plans');
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(saved()).toBeTruthy());
  expect(saved()).toEqual(
    expect.objectContaining({
      roleId: 3,
      grantedPermissions: ['plans:write'],
      deniedPermissions: [],
    }),
  );
});

// A grant the new role already gives is dropped, not saved as a stale override.
it('drops overrides the chosen role makes meaningless', async () => {
  const saved = renderForm();
  chooseSelectOption(screen.getByLabelText('Role').id, 'Reader');
  chooseSelectOption(screen.getByLabelText('Also allow').id, 'Manage plans');
  chooseSelectOption(screen.getByLabelText('Role').id, 'Operator');
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(saved()).toBeTruthy());
  expect(saved()).toEqual(expect.objectContaining({ roleId: 0, grantedPermissions: [] }));
});

// A role that holds a write holds its read too, so the read is not offered as an addition.
it('counts the read a role write implies as held', () => {
  expect(overrideChoices(CEILINGS.operator, ['users:write'])).toEqual({
    grantable: ['plans:write'],
    deniable: ['users:read', 'users:write'],
  });
});
