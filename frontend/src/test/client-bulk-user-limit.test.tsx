import { expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';

import ClientBulkAddModal from '@/pages/clients/ClientBulkAddModal';
import ClientBulkAdjustModal from '@/pages/clients/ClientBulkAdjustModal';
import { HttpUtil, Msg } from '@/utils';
import { renderWithProviders } from './test-utils';

const { bulkCreate } = vi.hoisted(() => ({ bulkCreate: vi.fn() }));
vi.mock('@/hooks/useClients', () => ({ useClients: () => ({ bulkCreate }) }));

const labels = () =>
  Array.from(document.querySelectorAll('.ant-form-item-label label')).map((l) =>
    (l.textContent ?? '').trim(),
  );

// Users made in bulk for OpenVPN get their connection cap, and no IP limit that would not apply.
it('creates OpenVPN users in bulk with a user limit', async () => {
  bulkCreate.mockResolvedValue(new Msg(true, '', { created: 1, skipped: [] }));
  vi.spyOn(HttpUtil, 'post').mockResolvedValue(new Msg(true, '', { datepicker: 'gregorian' }));
  renderWithProviders(
    <ClientBulkAddModal
      open
      inbounds={[{ id: 7, protocol: 'openvpn', tag: 'ovpn' }]}
      onOpenChange={() => {}}
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Select all' }));
  await waitFor(() => expect(labels()).not.toContain('IP Limit'));
  const users = screen.getByLabelText('User limit');
  fireEvent.change(users, { target: { value: '3' } });
  fireEvent.blur(users);
  const create = document.querySelector('.ant-modal-footer .ant-btn-primary') as HTMLElement;
  fireEvent.click(create);
  await waitFor(() =>
    expect(bulkCreate).toHaveBeenCalledWith([
      expect.objectContaining({ client: expect.objectContaining({ userLimit: 3 }) }),
    ]),
  );
});

it('sets a user limit on many clients without touching their IP limit', async () => {
  const onSubmit = vi.fn().mockResolvedValue({ adjusted: 2 });
  renderWithProviders(
    <ClientBulkAdjustModal open count={2} onOpenChange={() => {}} onSubmit={onSubmit} />,
  );
  const users = screen.getByLabelText('User limit');
  fireEvent.change(users, { target: { value: '2' } });
  fireEvent.blur(users);
  fireEvent.click(document.querySelector('.ant-modal-footer .ant-btn-primary') as HTMLElement);
  await waitFor(() => expect(onSubmit).toHaveBeenCalled());
  const limits = onSubmit.mock.calls[0][5];
  expect(limits).toEqual(expect.objectContaining({ userLimit: 2, limitIp: null }));
});
