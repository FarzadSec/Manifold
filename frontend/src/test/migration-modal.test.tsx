import { describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';

import MigrationModal from '@/pages/index/MigrationModal';
import type { MigrationReport } from '@/generated/types';
import { renderWithProviders } from './test-utils';

function report(over: Partial<MigrationReport>): MigrationReport {
  return {
    sourceVersion: '0.5.0',
    sourceAddress: '203.0.113.10',
    thisAddress: '198.51.100.20',
    addressChanged: true,
    createdAt: 0,
    inbounds: 3,
    clients: 40,
    addresses: [
      { address: '203.0.113.10', status: 'changes', usedBy: ['by-ip'] },
      {
        address: 'vpn.example.com',
        status: 'dnsUpdate',
        points: ['203.0.113.10'],
        usedBy: ['by-domain'],
      },
    ],
    tunnels: [{ name: 'uk', kind: 'gre', role: 'client', action: 'repair' }],
    files: [],
    issues: [],
    restored: false,
    ...over,
  };
}

describe('migration report', () => {
  it('names what breaks and what the admin must do before restoring', () => {
    renderWithProviders(
      <MigrationModal
        file={new File([], 'b.tar.gz')}
        report={report({})}
        onClose={vi.fn()}
        onBusy={vi.fn()}
      />,
    );
    expect(
      screen.getByText(/1 address\(es\) users' configs carry would stop leading here/),
    ).toBeTruthy();
    expect(screen.getByText(/Change its A record to 198.51.100.20/)).toBeTruthy();
    expect(screen.getByText(/import this tunnel's new pairing code there/)).toBeTruthy();
    expect(
      screen.getByText(
        'Replace 203.0.113.10 with 198.51.100.20 wherever the panel stores it (users then need their configs again)',
      ),
    ).toBeTruthy();
    expect(
      (screen.getByRole('button', { name: 'Restore here' }) as HTMLButtonElement).disabled,
    ).toBe(false);
  });

  it('refuses to restore while something would stop it', () => {
    renderWithProviders(
      <MigrationModal
        file={new File([], 'b.tar.gz')}
        report={report({
          addressChanged: false,
          issues: [{ level: 'error', code: 'portBusy', params: { port: '443', inbound: 'by-ip' } }],
        })}
        onClose={vi.fn()}
        onBusy={vi.fn()}
      />,
    );
    expect(screen.getByText(/Port 443 of inbound by-ip is held by another program/)).toBeTruthy();
    expect(
      (screen.getByRole('button', { name: 'Restore here' }) as HTMLButtonElement).disabled,
    ).toBe(true);
    expect(screen.queryByText(/Replace 203.0.113.10/)).toBeNull();
  });
});
