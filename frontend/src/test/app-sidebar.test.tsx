import { act, fireEvent, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { afterEach, expect, test, vi } from 'vitest';

import AppSidebar from '@/layouts/AppSidebar';
import { renderWithProviders } from './test-utils';

vi.mock('@/api/queries/useAllSettings', () => ({
  useAllSettings: () => ({ allSetting: {} }),
}));

afterEach(() => {
  localStorage.clear();
});

// rc-menu registers its items in a microtask after render; settle it inside act().
async function renderSidebar() {
  const view = renderWithProviders(
    <MemoryRouter>
      <AppSidebar />
    </MemoryRouter>,
  );
  await act(async () => {});
  return view;
}

// The rail used to spring open under the pointer and cover the page; it now
// changes width only from its own button, and remembers the choice.
test('folds and unfolds only from its button, never under the pointer', async () => {
  const first = await renderSidebar();
  const sidebar = first.container.querySelector('.ant-layout-sider');
  const sidebarRoot = first.container.querySelector('.ant-sidebar');

  expect(sidebar?.classList.contains('ant-layout-sider-collapsed')).toBe(false);
  fireEvent.mouseLeave(sidebarRoot!);
  expect(sidebar?.classList.contains('ant-layout-sider-collapsed')).toBe(false);

  fireEvent.click(screen.getByRole('button', { name: 'Collapse sidebar' }));
  fireEvent.mouseEnter(sidebarRoot!);

  expect(sidebar?.classList.contains('ant-layout-sider-collapsed')).toBe(true);
  expect(sidebarRoot?.getAttribute('style')).toContain('--sider-rail: 72px');
  expect(localStorage.getItem('sidebar-collapsed')).toBe('true');
  first.unmount();

  const second = await renderSidebar();
  const restored = second.container.querySelector('.ant-layout-sider');
  expect(restored?.classList.contains('ant-layout-sider-collapsed')).toBe(true);

  fireEvent.click(screen.getByRole('button', { name: 'Expand sidebar' }));
  expect(restored?.classList.contains('ant-layout-sider-collapsed')).toBe(false);
  expect(second.container.querySelector('.ant-sidebar')?.getAttribute('style')).toContain(
    '--sider-rail: 220px',
  );
  expect(localStorage.getItem('sidebar-collapsed')).toBe('false');
});

test('labels the palette shortcut with the modifier the platform actually uses', async () => {
  const view = await renderSidebar();
  const chip = view.container.querySelector('.sidebar-command-kbd');
  expect(chip?.textContent).toBe('CtrlK');
});

test('groups the pages under section headings, and only separates them on the rail', async () => {
  const pinned = await renderSidebar();
  const titles = [
    ...pinned.container.querySelectorAll('.sider-nav .ant-menu-item-group-title'),
  ].map((el) => el.textContent);
  expect(titles).toEqual(['Users', 'Network', 'Operations', 'System']);
  const certs = screen.getByText('Certificates').closest('.ant-menu-item-group');
  expect(certs?.querySelector('.ant-menu-item-group-title')?.textContent).toBe('Network');
  pinned.unmount();

  localStorage.setItem('sidebar-collapsed', 'true');
  const rail = await renderSidebar();
  expect(rail.container.querySelectorAll('.sider-nav .ant-menu-item-group-title')).toHaveLength(0);
  expect(rail.container.querySelectorAll('.sider-nav .ant-menu-item-divider')).toHaveLength(4);
});

test('names Manifold Relay on a relay', async () => {
  window.X_UI_EDITION = 'tunnel';
  try {
    await renderSidebar();
    expect(await screen.findByText('Manifold Relay')).toBeTruthy();
  } finally {
    delete window.X_UI_EDITION;
  }
});
