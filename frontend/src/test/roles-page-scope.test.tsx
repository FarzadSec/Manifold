import { expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';

import RolesPage from '@/pages/access/RolesPage';
import { HttpUtil, Msg } from '@/utils';
import { chooseSelectOption, makeTestQueryClient, renderWithProviders } from './test-utils';

const ME = {
  id: 1,
  username: 'owner',
  tier: 'admin',
  isOwner: true,
  permissions: ['admins:write', 'users:write', 'users:read'],
  tierCeilings: {
    admin: ['users:read', 'users:write'],
    operator: ['users:read', 'users:write'],
    reseller: ['users:read', 'users:write'],
  },
};

// A support role sees every user but changes only the ones it made.
it('saves which users a role reads and changes', async () => {
  vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
    if (url === '/panel/api/access/me') return new Msg(true, '', ME);
    if (url === '/panel/api/access/roles') return new Msg(true, '', []);
    return new Msg(true, '', []);
  });
  const post = vi.spyOn(HttpUtil, 'post').mockResolvedValue(new Msg(true, '', null));
  renderWithProviders(
    <MemoryRouter>
      <RolesPage />
    </MemoryRouter>,
    { queryClient: makeTestQueryClient() },
  );

  fireEvent.click(await screen.findByRole('button', { name: /Add role/ }));
  fireEvent.change(await screen.findByLabelText('Name'), { target: { value: 'Support' } });
  chooseSelectOption(
    screen.getByLabelText('Users it reaches').id,
    'Sees all users, changes only its own',
  );
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));

  const saved = () => post.mock.calls.find(([url]) => url === '/panel/api/access/roles/add');
  await waitFor(() => expect(saved()).toBeTruthy());
  expect(saved()?.[1]).toEqual(
    expect.objectContaining({ name: 'Support', usersReadScope: 'all', usersWriteScope: 'own' }),
  );
});
