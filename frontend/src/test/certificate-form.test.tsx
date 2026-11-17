import { fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import type { CertificateView } from '@/generated/types';
import CertificateModal from '@/pages/certs/CertificateModal';
import { ExpiryTag } from '@/pages/certs/CertificatesPage';

import { renderWithProviders } from './test-utils';

const stored = {
  id: 2,
  name: 'example.com',
  domains: ['*.example.com', 'example.com'],
  source: 'acme',
  challenge: 'dns-01',
  port: 0,
  dnsProvider: 'arvancloud',
  email: '',
  staging: false,
  autoRenew: true,
  nodeIds: [],
  issuer: 'R11',
  notBefore: 0,
  notAfter: 0,
  issuing: false,
  lastRun: 0,
  lastError: '',
  createdAt: 0,
  certFile: '/etc/manifold/certs/example.com/fullchain.pem',
  keyFile: '/etc/manifold/certs/example.com/privkey.pem',
  hasDnsToken: true,
} as CertificateView;

describe('certificate form', () => {
  it('keeps the stored DNS token and saves without issuing when editing', async () => {
    const onSave = vi.fn().mockResolvedValue(true);
    renderWithProviders(
      <CertificateModal open editing={stored} nodes={[]} onSave={onSave} onClose={vi.fn()} />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1));
    const [payload, issue] = onSave.mock.calls[0];
    expect(payload).toMatchObject({
      id: 2,
      dnsProvider: 'arvancloud',
      domains: ['*.example.com', 'example.com'],
    });
    expect(payload).not.toHaveProperty('dnsToken');
    expect(issue).toBe(false);
  });

  it('refuses a wildcard over HTTP-01 before anything is sent', async () => {
    const onSave = vi.fn().mockResolvedValue(true);
    renderWithProviders(
      <CertificateModal
        open
        editing={{ ...stored, challenge: 'http-01' }}
        nodes={[]}
        onSave={onSave}
        onClose={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await screen.findByText('A wildcard can only be proved with DNS-01');
    expect(onSave).not.toHaveBeenCalled();
  });

  it('takes an IP address over HTTP-01 and refuses it over DNS-01', async () => {
    const onSave = vi.fn().mockResolvedValue(true);
    const ipCert = { ...stored, name: '203.0.113.9', domains: ['203.0.113.9', '2001:db8::9'] };
    const { unmount } = renderWithProviders(
      <CertificateModal
        open
        editing={{ ...ipCert, challenge: 'http-01' }}
        nodes={[]}
        onSave={onSave}
        onClose={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1));
    unmount();

    renderWithProviders(
      <CertificateModal open editing={ipCert} nodes={[]} onSave={onSave} onClose={vi.fn()} />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await screen.findByText('An IP address can only be proved with HTTP-01');
    expect(onSave).toHaveBeenCalledTimes(1);
  });
});

describe('certificate expiry', () => {
  it('warns two weeks ahead and marks an expired one', () => {
    const day = 86_400_000;
    const { rerender } = renderWithProviders(
      <ExpiryTag cert={{ ...stored, notAfter: Date.now() + 10 * day + 1000 }} />,
    );
    expect(screen.getByText('10 days left').closest('.ant-tag')?.className).toContain('orange');
    rerender(<ExpiryTag cert={{ ...stored, notAfter: Date.now() - day }} />);
    expect(screen.getByText('Expired').closest('.ant-tag')?.className).toContain('red');
  });

  it('measures a six-day IP certificate against its own life', () => {
    const hour = 3_600_000;
    const now = Date.now();
    const sixDay = { ...stored, notBefore: now - 10 * hour, notAfter: now + 150 * hour };
    const { rerender } = renderWithProviders(<ExpiryTag cert={sixDay} />);
    expect(screen.getByText('6 days left').closest('.ant-tag')?.className).toContain('green');
    rerender(
      <ExpiryTag cert={{ ...sixDay, notBefore: now - 140 * hour, notAfter: now + 20 * hour }} />,
    );
    expect(screen.getByText('0 days left').closest('.ant-tag')?.className).toContain('orange');
  });
});
