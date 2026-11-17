import type { InboundOption } from '@/schemas/client';

// inboundsFor is the inbounds an imported protocol can go to; Hysteria 2 users go to
// hysteria inbounds.
export function inboundsFor(protocol: string, inbounds: InboundOption[]): InboundOption[] {
  const want = protocol === 'hysteria2' ? 'hysteria' : protocol;
  return inbounds.filter((ib) => (ib.protocol ?? '').startsWith(want));
}

// defaultMapping picks, for each protocol the source has, the one inbound of that
// protocol when there is exactly one, so the common case needs no choosing.
export function defaultMapping(
  protocols: Record<string, number>,
  inbounds: InboundOption[],
): Record<string, number[]> {
  const out: Record<string, number[]> = {};
  for (const p of Object.keys(protocols)) {
    const candidates = inboundsFor(p, inbounds);
    out[p] = candidates.length === 1 ? [candidates[0].id] : [];
  }
  return out;
}
