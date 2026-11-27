// Package pathmtu holds the packet sizes the VPN engines fall back to, so a server and
// every config it hands out agree on one size that fits the path to any user.
package pathmtu

// Path is the largest outer packet assumed to reach any user: 1500 less one 40-byte layer
// (IPv6, DS-Lite, 464XLAT), which the panel's GRE, IPIP and SIT tunnels also carry.
const Path = 1460

// WireGuard is what the official WireGuard apps use. Path's 1380 put 1440 bytes on the wire, which
// some mobile networks split and then lost the second half of: big pages and uploads stalled.
const WireGuard = 1280

// OpenVPN leaves room for IPv6, UDP and a data packet's 24 bytes (opcode and peer id, packet
// id, AEAD tag); OpenVPNPayload is mssfix's unit, the UDP payload of the largest packet.
const (
	OpenVPNOverhead = 24
	OpenVPNPayload  = Path - 40 - 8
	OpenVPN         = OpenVPNPayload - OpenVPNOverhead
)

// WireGuardMTU is an inbound's MTU, or WireGuard when it names none.
func WireGuardMTU(named int) int {
	if named > 0 {
		return named
	}
	return WireGuard
}
