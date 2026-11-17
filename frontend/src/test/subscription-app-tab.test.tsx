import { describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';

import SubscriptionAppTab from '@/pages/settings/SubscriptionAppTab';
import { AllSetting } from '@/models/setting';
import { HttpUtil, Msg } from '@/utils';
import { chooseSelectOption, makeTestQueryClient, renderWithProviders } from './test-utils';

const LOCATIONS = [
  {
    id: 1,
    name: 'Iran',
    countryCode: 'IR',
    city: 'Tehran',
    timezone: '',
    sortOrder: 0,
    inboundIds: [1],
  },
];
const INBOUNDS = [
  { id: 1, remark: 'vless-ir', protocol: 'vless', port: 443 },
  { id: 2, remark: 'ovpn-de', protocol: 'openvpn', port: 1194, nodeId: 3 },
  { id: 3, remark: 'l2tp-de', protocol: 'l2tp', port: 1701, nodeId: 3 },
];

function mockPanel() {
  vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
    if (url === '/panel/api/app/locations') return new Msg(true, '', LOCATIONS);
    if (url === '/panel/api/inbounds/options') return new Msg(true, '', INBOUNDS);
    if (url === '/panel/api/nodes/list') return new Msg(true, '', [{ id: 3, name: 'de-fra-1' }]);
    return new Msg(false, 'unexpected ' + url, null);
  });
  return vi.spyOn(HttpUtil, 'post').mockResolvedValue(new Msg(true, '', LOCATIONS[0]));
}

function renderTab(updateSetting = vi.fn()) {
  renderWithProviders(
    <SubscriptionAppTab allSetting={new AllSetting()} updateSetting={updateSetting} />,
    {
      queryClient: makeTestQueryClient(),
    },
  );
}

describe('SubscriptionAppTab', () => {
  it('gathers inbounds of any protocol and server into a new location', async () => {
    const post = mockPanel();
    renderTab();
    fireEvent.click(await screen.findByRole('button', { name: 'Add location' }));
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Germany' } });
    fireEvent.change(screen.getByLabelText('Country'), { target: { value: 'de' } });
    const field = screen.getByLabelText('Inbounds').id;
    chooseSelectOption(field, 'ovpn-de · openvpn · de-fra-1');
    chooseSelectOption(field, 'l2tp-de · l2tp · de-fra-1');
    fireEvent.click(screen.getByRole('button', { name: 'OK' }));

    await waitFor(() => expect(post).toHaveBeenCalled());
    expect(post.mock.calls[0][0]).toBe('/panel/api/app/locations');
    expect(post.mock.calls[0][1]).toEqual({
      id: 0,
      name: 'Germany',
      countryCode: 'DE',
      city: '',
      timezone: '',
      sortOrder: 0,
      inboundIds: [2, 3],
    });
  });

  it('shows which location every inbound serves and moves one from there', async () => {
    const post = mockPanel();
    renderTab();
    const unplaced = (await screen.findByText('ovpn-de')).closest('tr') as HTMLElement;
    expect(within(unplaced).getByText('de-fra-1')).toBeTruthy();
    expect(within(unplaced).getByText('No location')).toBeTruthy();
    const placed = screen.getByText('vless-ir').closest('tr') as HTMLElement;
    expect(within(placed).getByText('This server')).toBeTruthy();

    chooseSelectOption(within(unplaced).getByLabelText('Location of ovpn-de').id, '🇮🇷 Iran');
    await waitFor(() => expect(post).toHaveBeenCalled());
    expect(post.mock.calls[0][1]).toEqual(expect.objectContaining({ id: 1, inboundIds: [1, 2] }));

    chooseSelectOption(within(placed).getByLabelText('Location of vless-ir').id, 'No location');
    await waitFor(() => expect(post).toHaveBeenCalledTimes(2));
    expect(post.mock.calls[1][1]).toEqual(expect.objectContaining({ id: 1, inboundIds: [] }));
  });

  it('turns app access on through the settings form', async () => {
    mockPanel();
    const updateSetting = vi.fn();
    renderTab(updateSetting);
    fireEvent.click(screen.getByRole('switch', { name: 'App access' }));
    expect(updateSetting).toHaveBeenCalledWith({ appEnable: true });
  });
});
