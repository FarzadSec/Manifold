import type { TunnelView } from '@/generated/types';

export const KERNEL_KINDS = ['gre', 'gretap', 'ip6gre', 'ip6gretap', 'ipip', 'sit'] as const;
export const REVERSE_TRANSPORTS = [
  'tcp',
  'tcpmux',
  'stealth',
  'ws',
  'wsmux',
  'wss',
  'wssmux',
  'kcp',
  'quic',
  'udp',
  'pck',
  'xdi',
] as const;
export const L3_CARRIERS = ['udp', 'quic', 'pck', 'sni', 'xdi'] as const;

export function isKernel(kind: string): boolean {
  return (KERNEL_KINDS as readonly string[]).includes(kind);
}

export function kindLabel(kind: string): string {
  switch (kind) {
    case 'reverse':
      return 'BackPack reverse';
    case 'l3':
      return 'BackPack L3';
    default:
      return kind.toUpperCase();
  }
}

export const STATE_COLORS: Record<string, string> = {
  up: '#52c41a',
  degraded: '#faad14',
  connecting: '#1677ff',
  down: '#ff4d4f',
  error: '#ff4d4f',
  disabled: '#bfbfbf',
};

export function stateOf(t: TunnelView): string {
  if (!t.enable) return 'disabled';
  return t.status?.state ?? 'connecting';
}

// The address the other server is reached at, as the list shows it.
export function endpointOf(t: TunnelView): string {
  const s = t.settings;
  if (t.kind === 'reverse') {
    return t.role === 'server' ? `:${s.port ?? ''}` : `${s.remoteAddr ?? ''}:${s.port ?? ''}`;
  }
  if (t.kind === 'l3') {
    return s.dialer === t.role ? `${s.remoteAddr ?? ''}:${s.port ?? ''}` : `:${s.port ?? ''}`;
  }
  return s.remoteAddr ?? '';
}
