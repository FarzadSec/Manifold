import type {
  BenchCandidate,
  BenchResult,
  TunnelBenchPick,
  TunnelRequest,
} from '@/generated/types';
import { kindLabel } from './tunnelInfo';

export type BenchFamily = 'kernel' | 'reverse' | 'l3';

export const BENCH_FAMILIES: BenchFamily[] = ['kernel', 'reverse', 'l3'];

export function benchFamily(kind: string): BenchFamily {
  if (kind === 'reverse') return 'reverse';
  if (kind === 'l3') return 'l3';
  return 'kernel';
}

// What tells one candidate from another of its kind: the transport, or the carrier.
export function benchName(c: Pick<BenchCandidate, 'kind' | 'transport' | 'carrier'>): string {
  const variant = c.kind === 'reverse' ? c.transport : c.kind === 'l3' ? c.carrier : '';
  return variant ? `${kindLabel(c.kind)} · ${variant}` : kindLabel(c.kind);
}

// A candidate as its key names it, for a candidate under way that has no result yet.
export function candidateFromKey(key: string): BenchCandidate {
  const [kind, variant, dialer] = key.split(':');
  if (kind === 'reverse') return { key, kind, transport: variant };
  if (kind === 'l3') return { key, kind, carrier: variant, dialer };
  return { key, kind };
}

const STATE_ORDER: Record<string, number> = { ok: 0, failed: 1, skipped: 2, pending: 3 };

// Results as the table lists them: what worked by score, then failed, skipped, not yet tested.
export function sortBenchResults(results: BenchResult[]): BenchResult[] {
  return [...results].sort(
    (a, b) =>
      (STATE_ORDER[a.state] ?? 3) - (STATE_ORDER[b.state] ?? 3) ||
      b.score - a.score ||
      a.key.localeCompare(b.key),
  );
}

// The client end of a recommended tunnel, as the tunnel form opens it; the form then fills
// the tunnel addresses, a port and a secret, and the forwarded ports are the admin's.
export function pickToRequest(pick: TunnelBenchPick, name: string): TunnelRequest {
  const settings: TunnelRequest['settings'] = { forwards: [], nat: 'masquerade' };
  if (pick.kind === 'reverse') settings.transport = pick.transport;
  if (pick.kind === 'l3') {
    settings.carrier = pick.carrier;
    settings.dialer = pick.dialer;
  }
  if (pick.mtu) settings.mtu = pick.mtu;
  if (pick.remoteAddr) settings.remoteAddr = pick.remoteAddr;
  return { name, kind: pick.kind, role: 'client', enable: true, relayId: 0, settings };
}

// The address the other server reaches this panel at: the browser's own, unless the
// browser came through a tunnel to the loopback, which the other server cannot use.
export function defaultPanelUrl(location: Pick<Location, 'origin' | 'hostname'>, basePath = '/') {
  const host = location.hostname.replace(/^\[|\]$/g, '');
  if (host === 'localhost' || host === '::1' || /^127\./.test(host)) return '';
  const path = basePath.startsWith('/') ? basePath : `/${basePath}`;
  return location.origin + (path.endsWith('/') ? path : `${path}/`);
}

export function mbps(v: number): string {
  if (!v) return '-';
  return v >= 100 ? v.toFixed(0) : v.toFixed(1);
}

// isolate keeps a value such as "590.49 MB" or an address whole inside right-to-left text.
export function isolate(value: string | number): string {
  return `\u2068${value}\u2069`;
}
