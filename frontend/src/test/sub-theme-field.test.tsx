import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import SubThemeField from '@/pages/settings/SubThemeField';
import { HttpUtil } from '@/utils';

import { renderWithProviders } from './test-utils';

const installed = [
  { id: 'builtin:aurora', name: 'aurora', title: 'Aurora (dark)', builtin: true },
  { id: 'installed:neon', name: 'neon', title: 'Neon', version: '1.0.0', builtin: false },
];
const catalog = [
  {
    name: 'neon',
    version: '1.1.0',
    installed: '1.0.0',
    title: { en: 'Neon' },
    description: {},
    files: ['index.html'],
  },
  {
    name: 'paperless',
    version: '2.0.0',
    installed: '2.0.0',
    title: { en: 'Paperless' },
    description: {},
    files: ['index.html'],
  },
];

function mockApi() {
  vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
    if (url.endsWith('/setting/subThemes')) return { success: true, msg: '', obj: installed };
    if (url.endsWith('/setting/subThemes/catalog')) return { success: true, msg: '', obj: catalog };
    return { success: false, msg: 'not mocked', obj: null };
  });
  return vi.spyOn(HttpUtil, 'post').mockResolvedValue({ success: true, msg: '', obj: null });
}

afterEach(() => vi.restoreAllMocks());

describe('subscription page theme picker', () => {
  it('updates an outdated theme and puts the page back when the theme in use is removed', async () => {
    const post = mockApi();
    const onChange = vi.fn();
    renderWithProviders(
      <SubThemeField
        value="installed:paperless"
        onChange={onChange}
        catalogURL=""
        onCatalogURLChange={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: /Catalogue/ }));
    const dialog = await screen.findByRole('dialog');
    const neon = (await within(dialog).findByText('Neon')).closest('li') as HTMLElement;
    fireEvent.click(within(neon).getByRole('button', { name: 'Update' }));
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith(
        '/panel/api/setting/subThemes/install',
        { name: 'neon' },
        expect.anything(),
      ),
    );
    expect(onChange).not.toHaveBeenCalled();

    const current = within(dialog).getByText('Paperless').closest('li') as HTMLElement;
    expect(
      (within(current).getByRole('button', { name: 'Installed' }) as HTMLButtonElement).disabled,
    ).toBe(true);
    fireEvent.click(within(current).getByRole('button', { name: 'Remove' }));
    await waitFor(() => expect(onChange).toHaveBeenCalledWith(''));
  });
});
