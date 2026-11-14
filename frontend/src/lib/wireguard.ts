// An inbound that names no interface runs on mfwg<id> (wgquick.DefaultInterface in Go).
export function wireguardInterfaceName(named: unknown, inboundId: number): string {
  const name = typeof named === 'string' ? named.trim() : '';
  return name || `mfwg${inboundId}`;
}
