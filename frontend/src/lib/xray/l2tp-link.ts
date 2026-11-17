// The panel renders an L2TP account as a link so it can travel in the same
// lists as the proxy links; no app imports this scheme.
const SCHEME = 'l2tp://';

export interface L2tpAccount {
  host: string;
  username: string;
  password: string;
  /** The IPsec key; the server offers IPsec whether or not it requires it. */
  psk: string;
  requiresIpsec: boolean;
  remark: string;
}

export function isL2tpLink(link: string): boolean {
  return link.trim().startsWith(SCHEME);
}

/** The account a link carries, or null when it is not a readable L2TP link. */
export function l2tpAccountFromLink(link: string): L2tpAccount | null {
  const trimmed = link.trim();
  if (!trimmed.startsWith(SCHEME)) return null;
  let url: URL;
  try {
    // The scheme is unknown to URL parsers, which then leave the host unparsed.
    url = new URL(`http://${trimmed.slice(SCHEME.length)}`);
  } catch {
    return null;
  }
  if (!url.username || !url.hostname) return null;
  return {
    host: url.hostname.replace(/^\[|\]$/g, ''),
    username: decodeURIComponent(url.username),
    password: decodeURIComponent(url.password),
    psk: url.searchParams.get('psk') ?? '',
    requiresIpsec: url.searchParams.get('ipsec') !== '0',
    remark: decodeURIComponent(url.hash.replace(/^#/, '')),
  };
}

export interface L2tpAccountEntry {
  key: string;
  index: number;
  account: L2tpAccount;
}

/** The readable L2TP accounts among a client's links, in link order. */
export function l2tpAccountsFromLinks(links: string[]): L2tpAccountEntry[] {
  const out: L2tpAccountEntry[] = [];
  links.forEach((link, index) => {
    const account = l2tpAccountFromLink(link);
    if (account) out.push({ key: `l2tp-${index}`, index, account });
  });
  return out;
}

export interface L2tpAccountLabels {
  protocol: string;
  server: string;
  username: string;
  password: string;
  psk: string;
}

/** The lines a person copies from: what to choose and type in the VPN settings of a device. */
export function l2tpAccountText(account: L2tpAccount, labels: L2tpAccountLabels): string {
  return [
    `${labels.protocol}: L2TP/IPsec PSK`,
    `${labels.server}: ${account.host}`,
    `${labels.username}: ${account.username}`,
    `${labels.password}: ${account.password}`,
    `${labels.psk}: ${account.psk}`,
    '',
  ].join('\n');
}

/** A download name for an account's text, safe on every file system. */
export function l2tpAccountFileName(remark: string, fallback: string): string {
  const base = (remark || fallback).replace(/[^A-Za-z0-9._-]+/g, '_').replace(/^_+|_+$/g, '');
  return `${base || 'l2tp'}.txt`;
}
