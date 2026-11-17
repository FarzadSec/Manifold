import { fromBase64Url } from '@/lib/xray/inbound-link';

// The panel wraps an OpenVPN profile as a link so it can travel in the same
// lists as the proxy links; no OpenVPN app reads this scheme.
const SCHEME = 'openvpn://';

export function isOpenvpnLink(link: string): boolean {
  return link.trim().startsWith(SCHEME);
}

/** The .ovpn text a link carries, or '' when it is not a readable OpenVPN link. */
export function openvpnProfileFromLink(link: string): string {
  const trimmed = link.trim();
  if (!trimmed.startsWith(SCHEME)) return '';
  try {
    return fromBase64Url(trimmed.slice(SCHEME.length));
  } catch {
    return '';
  }
}

export interface OpenvpnProfileSummary {
  remark: string;
  port: string;
  network: string;
}

export function summarizeOpenvpnProfile(profile: string): OpenvpnProfileSummary {
  const proto = /^proto\s+(\S+)\s*$/m.exec(profile)?.[1] ?? '';
  return {
    remark: /^#\s?(.*)$/m.exec(profile)?.[1]?.trim() ?? '',
    port: /^remote\s+\S+\s+(\d+)\s*$/m.exec(profile)?.[1] ?? '',
    network: proto.startsWith('tcp') ? 'tcp' : proto.startsWith('udp') ? 'udp' : '',
  };
}

/** A download name for the profile of a link, safe on every file system. */
export function openvpnProfileFileName(remark: string, fallback: string): string {
  const base = (remark || fallback).replace(/[^A-Za-z0-9._-]+/g, '_').replace(/^_+|_+$/g, '');
  return `${base || 'profile'}.ovpn`;
}

export interface OpenvpnProfileEntry {
  key: string;
  index: number;
  text: string;
  remark: string;
  fileName: string;
}

/** The readable OpenVPN profiles among a client's links, in link order. */
export function openvpnProfilesFromLinks(
  links: string[],
  fallbackName: string,
): OpenvpnProfileEntry[] {
  const out: OpenvpnProfileEntry[] = [];
  links.forEach((link, index) => {
    const text = openvpnProfileFromLink(link);
    if (!text) return;
    const { remark } = summarizeOpenvpnProfile(text);
    out.push({
      key: `openvpn-${index}`,
      index,
      text,
      remark,
      fileName: openvpnProfileFileName(remark, fallbackName),
    });
  });
  return out;
}
