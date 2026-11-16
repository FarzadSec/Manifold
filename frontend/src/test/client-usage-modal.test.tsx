import { fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import ClientUsageModal from '@/pages/clients/ClientUsageModal';
import { HttpUtil } from '@/utils';

import { renderWithProviders } from './test-utils';

const GB = 1024 ** 3;

describe('client usage window', () => {
  it('splits the traffic per server and asks for a whole month in the panel calendar', async () => {
    const get = vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
      if (url.includes('/clients/usage/')) {
        return {
          success: true,
          msg: '',
          obj: {
            from: 0,
            to: 0,
            daily: false,
            points: [{ start: 1_763_337_600, up: GB, down: 3 * GB }],
            nodes: [
              { nodeId: 0, name: '', up: GB, down: 2 * GB },
              { nodeId: 4, name: 'fra', up: 0, down: GB },
            ],
          },
        };
      }
      return { success: false, msg: 'not mocked', obj: null };
    });
    renderWithProviders(<ClientUsageModal open email="alice" onClose={vi.fn()} />);

    expect(await screen.findByText('This server')).toBeTruthy();
    expect(screen.getByText('fra')).toBeTruthy();
    expect(screen.getByText('75%')).toBeTruthy();
    expect(screen.getByText('25%')).toBeTruthy();

    fireEvent.click(screen.getByText('Month'));
    await waitFor(() =>
      expect(get).toHaveBeenCalledWith(
        '/panel/api/clients/usage/alice',
        expect.objectContaining({ window: 'month', calendar: 'gregorian' }),
        expect.anything(),
      ),
    );
  });
});
