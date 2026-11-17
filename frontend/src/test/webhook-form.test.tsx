import { fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import type { WebhookView } from '@/generated/types';
import WebhookModal from '@/pages/webhooks/WebhookModal';
import { eventGroup } from '@/schemas/webhook';

import { renderWithProviders } from './test-utils';

const stored = {
  id: 4,
  name: 'mirza',
  enable: true,
  kind: 'http',
  url: 'https://bot.example.com/h',
  chatId: '',
  events: ['user.limited'],
  userId: 0,
  createdAt: 0,
  hasSecret: true,
} as WebhookView;

const events = ['user.limited', 'users.bulk_changed', 'node.down', 'backup.failed', 'xray.crash'];

describe('webhook form', () => {
  it('keeps the stored secret and events unless changed', async () => {
    const onSave = vi.fn().mockResolvedValue(true);
    renderWithProviders(
      <WebhookModal open editing={stored} events={events} onSave={onSave} onClose={vi.fn()} />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1));
    const sent = onSave.mock.calls[0][0] as Record<string, unknown>;
    expect(sent).toMatchObject({ id: 4, kind: 'http', events: ['user.limited'] });
    expect(sent).not.toHaveProperty('secret');
  });

  it('refuses an address that is not http(s)', async () => {
    const onSave = vi.fn().mockResolvedValue(true);
    renderWithProviders(
      <WebhookModal open editing={null} events={events} onSave={onSave} onClose={vi.fn()} />,
    );
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'bot' } });
    fireEvent.change(screen.getByLabelText('URL'), { target: { value: 'ftp://bot.example.com' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await screen.findByText('Enter an http:// or https:// address');
    expect(onSave).not.toHaveBeenCalled();
  });

  it('files plural prefixes with their group and the rest under system', () => {
    expect(events.map(eventGroup)).toEqual(['user', 'user', 'node', 'backup', 'system']);
  });
});
