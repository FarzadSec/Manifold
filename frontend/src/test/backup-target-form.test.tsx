import { fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import type { BackupTargetView } from '@/generated/types';
import BackupTargetModal from '@/pages/backups/BackupTargetModal';

import { renderWithProviders } from './test-utils';

const stored = {
  id: 3,
  name: 'offsite',
  enable: true,
  kind: 's3',
  everyHours: 24,
  keep: 7,
  dir: '',
  endpoint: 'https://s3.example.com',
  region: 'us-east-1',
  bucket: 'b',
  prefix: 'fra',
  accessKey: 'AK',
  host: '',
  port: 0,
  user: '',
  hostKey: '',
  lastRun: 0,
  lastFile: '',
  lastSize: 0,
  lastError: '',
  hasPassphrase: true,
  hasSecretKey: true,
  hasPassword: false,
  hasPrivateKey: false,
} as BackupTargetView;

describe('backup target form', () => {
  it('leaves stored secrets out of a save unless one is typed', async () => {
    const onSave = vi.fn().mockResolvedValue(true);
    renderWithProviders(
      <BackupTargetModal open editing={stored} onSave={onSave} onClose={vi.fn()} />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1));
    const first = onSave.mock.calls[0][0] as Record<string, unknown>;
    expect(first).toMatchObject({ id: 3, kind: 's3', bucket: 'b' });
    expect(first).not.toHaveProperty('secretKey');
    expect(first).not.toHaveProperty('passphrase');

    fireEvent.change(screen.getByLabelText('Passphrase'), { target: { value: 'new pass' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(2));
    expect(onSave.mock.calls[1][0]).toMatchObject({ passphrase: 'new pass' });
  });
});
