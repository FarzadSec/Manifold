import { describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';

import AppReportsTab from '@/pages/settings/AppReportsTab';
import { HttpUtil, Msg } from '@/utils';
import { makeTestQueryClient, renderWithProviders } from './test-utils';

const REPORT = {
  id: 7,
  createdAt: 1767225600000,
  subId: 'k3x9q2m7',
  email: 'dana@example.com',
  tgId: 987654321,
  deviceName: 'Pixel 8',
  deviceOs: 'android',
  osVersion: '15',
  appVersion: '1.1.0',
  message: 'به هیچ لوکیشنی وصل نمی‌شود',
  logSize: 41,
  seen: false,
};
const LOG = '=== app ===\n[vpn] no config connected\n';

function mockPanel() {
  const get = vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
    if (url === '/panel/api/app/reports') return new Msg(true, '', [REPORT]);
    if (url === '/panel/api/app/reports/7') return new Msg(true, '', { ...REPORT, log: LOG });
    return new Msg(false, 'unexpected ' + url, null);
  });
  const post = vi.spyOn(HttpUtil, 'post').mockResolvedValue(new Msg(true, '', null));
  return { get, post };
}

describe('AppReportsTab', () => {
  it("lists a report with the user's Telegram id, then opens its log and marks it read", async () => {
    const { get, post } = mockPanel();
    renderWithProviders(<AppReportsTab />, { queryClient: makeTestQueryClient() });

    const row = (await screen.findByText('dana@example.com')).closest('tr') as HTMLElement;
    expect(within(row).getByText('987654321')).toBeTruthy();
    expect(within(row).getByText('New')).toBeTruthy();
    expect(within(row).getByText('Pixel 8 · android 15')).toBeTruthy();

    fireEvent.click(within(row).getByRole('button', { name: 'Open' }));
    const dialog = await screen.findByRole('dialog');
    await waitFor(() => expect(within(dialog).getByText(/no config connected/)).toBeTruthy());
    expect(get).toHaveBeenCalledWith('/panel/api/app/reports/7', undefined, { silent: true });
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith('/panel/api/app/reports/seen/7', undefined, {
        silent: true,
      }),
    );
    expect(within(dialog).getByRole('button', { name: /Download log/ })).toBeTruthy();
  });
});
