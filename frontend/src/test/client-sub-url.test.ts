import { describe, expect, it } from 'vitest';

import { clientSubUrls } from '@/lib/clients/subscription-url';

const settings = {
  enable: true,
  subURI: 'https://sub.example.com/sub/',
  subJsonEnable: true,
  subJsonURI: 'https://sub.example.com/json/',
};

describe('client subscription address', () => {
  it("uses the server's own address, which carries a reseller's or a subscriber's host", () => {
    expect(
      clientSubUrls(
        {
          subId: 'abc',
          subUrl: 'https://r.example.net/sub/abc',
          subJsonUrl: 'https://r.example.net/json/abc',
        },
        settings,
      ),
    ).toEqual({ sub: 'https://r.example.net/sub/abc', json: 'https://r.example.net/json/abc' });
  });

  it('falls back to the panel base, and to nothing while subscriptions are off', () => {
    expect(clientSubUrls({ subId: 'abc' }, settings).sub).toBe('https://sub.example.com/sub/abc');
    expect(clientSubUrls({ subId: 'abc', subUrl: 'x' }, { ...settings, enable: false })).toEqual({
      sub: '',
      json: '',
    });
    expect(clientSubUrls({ subId: 'abc' }, { ...settings, subJsonEnable: false }).json).toBe('');
  });
});
