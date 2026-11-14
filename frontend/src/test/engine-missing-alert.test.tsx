import { describe, expect, it, vi } from 'vitest';
import { waitFor } from '@testing-library/react';

import EngineMissingAlert from '@/pages/inbounds/form/EngineMissingAlert';
import { HttpUtil, Msg } from '@/utils';
import { makeTestQueryClient, renderWithProviders } from './test-utils';

const engines = new Msg(true, '', [
  { protocol: 'openvpn', installed: true, package: 'openvpn' },
  { protocol: 'wireguard', installed: false, package: 'wireguard-tools' },
]);

describe('missing engine warning in the inbound form', () => {
  it('names the package when the chosen protocol has no engine on the panel server', async () => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue(engines);

    const { getByRole } = renderWithProviders(<EngineMissingAlert protocol="wireguard" />, {
      queryClient: makeTestQueryClient(),
    });

    await waitFor(() => expect(getByRole('alert').textContent).toContain('wireguard-tools'));
  });

  it('stays silent for a protocol whose engine is installed', async () => {
    const get = vi.spyOn(HttpUtil, 'get').mockResolvedValue(engines);

    const { container } = renderWithProviders(<EngineMissingAlert protocol="openvpn" />, {
      queryClient: makeTestQueryClient(),
    });

    await waitFor(() => expect(get).toHaveBeenCalled());
    expect(container.querySelector('.ant-alert')).toBeNull();
  });
});
