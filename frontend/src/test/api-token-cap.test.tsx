import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { expect, it, vi } from 'vitest';

import type { AllSetting } from '@/models/setting';
import SecurityTab from '@/pages/settings/SecurityTab';
import { HttpUtil } from '@/utils';

import { renderWithProviders } from './test-utils';

function renderTab(tokens: unknown[]) {
  vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) =>
    url.endsWith('/setting/apiTokens')
      ? { success: true, msg: '', obj: tokens }
      : { success: false, msg: 'not mocked', obj: null },
  );
  renderWithProviders(
    <SecurityTab allSetting={{} as AllSetting} updateSetting={vi.fn()} saveSetting={vi.fn()} />,
  );
  fireEvent.click(screen.getByRole('tab', { name: /API Token/ }));
}

// A capped token shows how many of its requests it has made.
it('shows a capped token use against its cap', async () => {
  renderTab([
    {
      id: 4,
      name: 'bot',
      enabled: true,
      createdAt: 1782485394,
      scope: 'admin',
      expiresAt: 0,
      maxRequests: 500,
      requestCount: 42,
    },
    {
      id: 5,
      name: 'open',
      enabled: true,
      createdAt: 1782485394,
      scope: 'admin',
      expiresAt: 0,
      maxRequests: 0,
      requestCount: 0,
    },
  ]);
  expect(await screen.findByText('42 / 500 requests')).toBeTruthy();
  expect(screen.queryByText('0 / 0 requests')).toBeNull();
});

// The delete switch only means something with a cap, and is sent off without one.
it('creates a token with a request cap that deletes it when used up', async () => {
  renderTab([]);
  const post = vi
    .spyOn(HttpUtil, 'post')
    .mockResolvedValue({ success: true, msg: '', obj: { token: 't' } });
  fireEvent.click(await screen.findByRole('button', { name: /New token/ }));
  fireEvent.change(screen.getByPlaceholderText('e.g. central-panel-a'), {
    target: { value: 'bot' },
  });
  expect(screen.queryByLabelText('Delete it when used up')).toBeNull();
  fireEvent.change(screen.getByLabelText('Requests in total'), { target: { value: '500' } });
  fireEvent.click(screen.getByLabelText('Delete it when used up'));
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Confirm' }));
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/panel/api/setting/apiTokens/create',
      expect.objectContaining({ name: 'bot', maxRequests: 500, deleteOnLimit: true }),
      expect.anything(),
    ),
  );
});
