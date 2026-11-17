import { describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';

import SubscriptionAppTab from '@/pages/settings/SubscriptionAppTab';
import { AllSetting } from '@/models/setting';
import { HttpUtil, Msg } from '@/utils';
import { makeTestQueryClient, renderWithProviders } from './test-utils';

const locations = new Msg(true, '', [
  { nodeId: 0, name: '', countryCode: '', city: '', timezone: '' },
  { nodeId: 3, name: 'de-fra-1', countryCode: 'DE', city: 'Frankfurt', timezone: 'Europe/Berlin' },
]);

describe('SubscriptionAppTab', () => {
  it('saves the edited location of the row it belongs to', async () => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue(locations);
    const post = vi.spyOn(HttpUtil, 'post').mockResolvedValue(new Msg(true, '', null));
    renderWithProviders(
      <SubscriptionAppTab allSetting={new AllSetting()} updateSetting={vi.fn()} />,
      {
        queryClient: makeTestQueryClient(),
      },
    );

    const country = await screen.findByLabelText('Country 0');
    fireEvent.change(country, { target: { value: 'IR' } });
    fireEvent.change(screen.getByLabelText('Timezone 0'), { target: { value: 'Asia/Tehran' } });
    fireEvent.click(screen.getByLabelText('Save 0'));

    await waitFor(() => expect(post).toHaveBeenCalled());
    expect(post).toHaveBeenCalledWith('/panel/api/app/locations', {
      nodeId: 0,
      name: '',
      countryCode: 'IR',
      city: '',
      timezone: 'Asia/Tehran',
    });
    expect(screen.getByText('Node #3')).toBeTruthy();
    expect((screen.getByLabelText('City 3') as HTMLInputElement).value).toBe('Frankfurt');
  });

  it('turns app access on through the settings form', async () => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue(locations);
    const updateSetting = vi.fn();
    renderWithProviders(
      <SubscriptionAppTab allSetting={new AllSetting()} updateSetting={updateSetting} />,
      {
        queryClient: makeTestQueryClient(),
      },
    );
    fireEvent.click(screen.getByRole('switch', { name: 'App access' }));
    expect(updateSetting).toHaveBeenCalledWith({ appEnable: true });
  });
});
