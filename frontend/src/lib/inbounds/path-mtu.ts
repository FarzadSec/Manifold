// Mirrors internal/pathmtu: what the panel gives an inbound that names no MTU, on the
// server and in every config, so it fits a 1460-byte path (a GRE, IPIP or SIT relay's included).
export const WIREGUARD_DEFAULT_MTU = 1380;
export const OPENVPN_UDP_DEFAULT_MTU = 1388;
const OPENVPN_OVERHEAD = 24;
const OPENVPN_MAX_PAYLOAD = 1412;

export function openvpnDefaultMssfix(mtu: number | undefined): number {
  return Math.min(
    (mtu && mtu > 0 ? mtu : OPENVPN_UDP_DEFAULT_MTU) + OPENVPN_OVERHEAD,
    OPENVPN_MAX_PAYLOAD,
  );
}
