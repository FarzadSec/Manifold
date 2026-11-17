import { screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import LogModal from '@/pages/index/LogModal';
import { HttpUtil } from '@/utils';

import { renderWithProviders } from './test-utils';

afterEach(() => vi.restoreAllMocks());

describe('node log window', () => {
  it("reads the node's own log through the node API, without the panel's syslog switch", async () => {
    const post = vi.spyOn(HttpUtil, 'post').mockResolvedValue({
      success: true,
      msg: '',
      obj: ['2026/11/17 10:00:00 INFO - node ready'],
    } as never);
    renderWithProviders(<LogModal open node={{ id: 7, name: 'edge' }} onClose={vi.fn()} />);
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith('/panel/api/nodes/logs/7', { count: 20, level: 'info' }),
    );
    expect(await screen.findByText(/node ready/)).toBeTruthy();
    expect(screen.queryByText('SysLog')).toBeNull();
    expect(post).not.toHaveBeenCalledWith(
      expect.stringContaining('/server/logs'),
      expect.anything(),
    );
  });
});
