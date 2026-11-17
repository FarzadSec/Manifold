import { fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import IngressFormModal from '@/pages/ingress/IngressFormModal';

import { renderWithProviders } from './test-utils';

describe('shared port form', () => {
  it('will not save a route that names no host, and sends the target it was given', async () => {
    const onSave = vi.fn().mockResolvedValue(true);
    renderWithProviders(<IngressFormModal open editing={null} onSave={onSave} onClose={vi.fn()} />);
    fireEvent.change(screen.getByPlaceholderText('127.0.0.1:8443'), {
      target: { value: '127.0.0.1:8443' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText('Name at least one host')).toBeTruthy();
    expect(onSave).not.toHaveBeenCalled();

    const hosts = screen.getByRole('combobox', { name: 'Hosts' }) as HTMLInputElement;
    fireEvent.change(hosts, { target: { value: 'www.speedtest.net' } });
    fireEvent.keyDown(hosts, { key: 'Enter', code: 'Enter', keyCode: 13 });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith(
        expect.objectContaining({
          port: 443,
          routes: [
            expect.objectContaining({ hosts: ['www.speedtest.net'], target: '127.0.0.1:8443' }),
          ],
        }),
      ),
    );
  });
});
