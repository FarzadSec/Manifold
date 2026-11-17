import { fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import NodeInstallModal from '@/pages/nodes/NodeInstallModal';
import { HttpUtil } from '@/utils';

import { renderWithProviders } from './test-utils';

afterEach(() => vi.restoreAllMocks());

describe('node install over SSH', () => {
  it('sends the credentials only after the host key is accepted, then follows the job', async () => {
    const post = vi
      .spyOn(HttpUtil, 'post')
      .mockImplementation(async (url: string) =>
        url.endsWith('/ssh/hostKey')
          ? ({ success: true, msg: '', obj: 'SHA256:abc' } as never)
          : ({ success: true, msg: '', obj: 'job-1' } as never),
      );
    vi.spyOn(HttpUtil, 'get').mockResolvedValue({
      success: true,
      msg: '',
      obj: { id: 'job-1', state: 'done', log: ['Server: Linux', '✓ Added node edge'], nodeId: 4 },
    } as never);
    renderWithProviders(<NodeInstallModal open node={null} onClose={vi.fn()} />);

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'edge' } });
    fireEvent.change(screen.getByLabelText('Address'), { target: { value: '203.0.113.10' } });
    fireEvent.change(screen.getByLabelText('Password', { selector: 'input[type="password"]' }), {
      target: { value: 'pw' },
    });
    const install = screen.getByRole('button', { name: 'Install' }) as HTMLButtonElement;
    expect(install.disabled).toBe(true);

    fireEvent.click(screen.getByRole('button', { name: 'Read host key' }));
    expect(await screen.findByText('SHA256:abc')).toBeTruthy();
    expect(post).toHaveBeenCalledWith('/panel/api/nodes/ssh/hostKey', {
      host: '203.0.113.10',
      port: 22,
    });
    expect(install.disabled).toBe(true);

    fireEvent.click(screen.getByRole('checkbox', { name: "This is my server's key" }));
    fireEvent.click(install);
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith(
        '/panel/api/nodes/ssh/install',
        expect.objectContaining({
          name: 'edge',
          password: 'pw',
          hostKey: 'SHA256:abc',
          update: false,
        }),
        expect.anything(),
      ),
    );
    expect(await screen.findByText(/Added node edge/, undefined, { timeout: 4000 })).toBeTruthy();
  });
});
