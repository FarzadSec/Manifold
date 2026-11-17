import { fireEvent, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';

import NodeFormModal from '@/pages/nodes/NodeFormModal';
import { HttpUtil, Msg } from '@/utils';

import { makeTestQueryClient, renderWithProviders } from './test-utils';

function renderForm() {
  vi.spyOn(HttpUtil, 'get').mockResolvedValue(new Msg(true, '', []));
  const testConnection = vi.fn().mockResolvedValue(new Msg(true, '', { status: 'online' }));
  const save = vi.fn().mockResolvedValue(new Msg(true, '', null));
  renderWithProviders(
    <NodeFormModal
      open
      mode="add"
      node={null}
      testConnection={testConnection}
      fetchFingerprint={vi.fn()}
      fetchInbounds={vi.fn()}
      save={save}
      onOpenChange={vi.fn()}
    />,
    { queryClient: makeTestQueryClient() },
  );
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'far' } });
  fireEvent.change(screen.getByLabelText('Address'), { target: { value: 'node.example.com' } });
  fireEvent.change(screen.getByLabelText('API Token'), { target: { value: 'tok' } });
  return { save, testConnection };
}

// A node reached through a proxy sends it with every test and save.
it('saves the proxy a node is reached through', async () => {
  const { save, testConnection } = renderForm();
  fireEvent.change(screen.getByLabelText('Reach through a proxy'), {
    target: { value: ' socks5://127.0.0.1:1080 ' },
  });
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(save).toHaveBeenCalled());
  expect(testConnection.mock.calls[0][0]).toEqual(
    expect.objectContaining({ proxyUrl: 'socks5://127.0.0.1:1080', outboundTag: '' }),
  );
  expect(save.mock.calls[0][0]).toEqual(
    expect.objectContaining({ proxyUrl: 'socks5://127.0.0.1:1080' }),
  );
});

it('refuses a proxy that is not a proxy URL', async () => {
  const { save } = renderForm();
  fireEvent.change(screen.getByLabelText('Reach through a proxy'), {
    target: { value: 'ftp://127.0.0.1:21' },
  });
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  expect(
    await screen.findByText('Use a URL like socks5://host:port, with http, https or socks5'),
  ).toBeTruthy();
  expect(save).not.toHaveBeenCalled();
});
