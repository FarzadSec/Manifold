import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent, waitFor, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import { ThemeProvider } from '@/hooks/useTheme';
import ClientFormModal from '@/pages/clients/ClientFormModal';
import type { ClientRecord, InboundOption } from '@/hooks/useClients';

const VLESS = { id: 1, port: 443, protocol: 'vless', tag: 'in-443', enable: true } as InboundOption;
const OPENVPN = {
  id: 2,
  port: 1194,
  protocol: 'openvpn',
  tag: 'in-1194',
  enable: true,
} as InboundOption;

const CLIENT = {
  email: 'ali',
  uuid: '11111111-1111-1111-1111-111111111111',
  password: 'pw-ali-123',
  subId: 'subid123',
  enable: true,
  limitIp: 4,
  userLimit: 2,
} as unknown as ClientRecord;

function renderEdit(attachedIds: number[], save = vi.fn().mockResolvedValue({ success: true })) {
  render(
    <ThemeProvider>
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <ClientFormModal
          open
          mode="edit"
          client={CLIENT}
          inbounds={[VLESS, OPENVPN]}
          attachedIds={attachedIds}
          save={save}
          onOpenChange={() => {}}
        />
      </QueryClientProvider>
    </ThemeProvider>,
  );
  return save;
}

const fieldLabels = () =>
  Array.from(document.querySelectorAll('.ant-form-item-label label')).map((l) =>
    (l.textContent ?? '').trim(),
  );

// One field once meant IPs on Xray and connected devices on OpenVPN and L2TP; each
// limit now shows only where it is enforced and is saved on its own.
describe('ClientFormModal limits', () => {
  it('offers only the user limit for an OpenVPN-only client', async () => {
    renderEdit([2]);
    await screen.findByRole('button', { name: /save/i });
    expect(fieldLabels()).toContain('User limit');
    expect(fieldLabels()).not.toContain('IP Limit');
    expect((screen.getByLabelText('User limit') as HTMLInputElement).value).toBe('2');
  });

  it('offers only the IP limit for an Xray-only client', async () => {
    renderEdit([1]);
    await screen.findByRole('button', { name: /save/i });
    expect(fieldLabels()).toContain('IP Limit');
    expect(fieldLabels()).not.toContain('User limit');
  });

  it('saves the user limit apart from the IP limit', async () => {
    const save = renderEdit([1, 2]);
    const users = await screen.findByLabelText('User limit');
    fireEvent.change(users, { target: { value: '3' } });
    fireEvent.blur(users);
    fireEvent.click(screen.getByRole('button', { name: /save/i }));
    await waitFor(() => expect(save).toHaveBeenCalled());
    const payload = save.mock.calls[0][0] as Record<string, unknown>;
    expect(payload.userLimit).toBe(3);
    expect(payload.limitIp).toBe(4);
  });
});
