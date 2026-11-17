import { describe, expect, it } from 'vitest';

import { joinContacts, splitContacts } from '@/pages/clients/ClientContactsFields';

describe('customer details', () => {
  it('keeps custom fields apart from the searched ones and back again', () => {
    const { standard, custom } = splitContacts({ phone: '0912', Instagram: 'ali.x' });
    expect(standard).toEqual({ phone: '0912' });
    expect(custom).toEqual([{ key: 'Instagram', value: 'ali.x' }]);
    expect(joinContacts(standard, custom)).toEqual({ phone: '0912', Instagram: 'ali.x' });
  });

  it('drops empty fields and never lets a custom row stand in for a standard one', () => {
    expect(
      joinContacts({ phone: ' ', telegram: '@ali ' }, [
        { key: 'phone', value: 'spoofed' },
        { key: ' ', value: 'x' },
        { key: 'note', value: '' },
      ]),
    ).toEqual({ telegram: '@ali' });
  });
});
