export interface SubUrlSettings {
  enable?: boolean;
  subURI?: string;
  subJsonURI?: string;
  subJsonEnable?: boolean;
}

export interface SubUrlClient {
  subId?: string;
  subUrl?: string;
  subJsonUrl?: string;
}

/**
 * A client's own subscription addresses: the server's, which carry its reseller's domain or its
 * host under a wildcard, else the panel's base followed by the subscription id.
 */
export function clientSubUrls(
  client: SubUrlClient | null | undefined,
  settings: SubUrlSettings | null | undefined,
): { sub: string; json: string } {
  const subId = client?.subId ?? '';
  if (!subId || !settings?.enable) return { sub: '', json: '' };
  const sub = client?.subUrl || (settings.subURI ? settings.subURI + subId : '');
  const json = settings.subJsonEnable
    ? client?.subJsonUrl || (settings.subJsonURI ? settings.subJsonURI + subId : '')
    : '';
  return { sub, json };
}
