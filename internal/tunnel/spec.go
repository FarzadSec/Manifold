// Package tunnel links this server to another one: kernel GRE, IPIP and SIT
// links, and BackPack's reverse and layer-3 tunnels, with the ports the relay
// side forwards to this side through them.
package tunnel

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Kinds of tunnel. The first six are kernel links, the last two run BackPack.
const (
	KindGRE       = "gre"
	KindGRETAP    = "gretap"
	KindIP6GRE    = "ip6gre"
	KindIP6GRETAP = "ip6gretap"
	KindIPIP      = "ipip"
	KindSIT       = "sit"
	KindReverse   = "reverse"
	KindL3        = "l3"
)

// The two ends: the client is the panel that serves the accounts, the server
// is the relay users connect to, which forwards its ports to the client.
const (
	RoleClient = "client"
	RoleServer = "server"
)

const (
	NATMasquerade = "masquerade"
	NATPreserve   = "preserve"
)

const (
	ProtoTCP  = "tcp"
	ProtoUDP  = "udp"
	ProtoBoth = "both"
	// ProtoESP is IPsec's IP protocol 50: no port, and only a kernel link carries it.
	ProtoESP = "esp"
)

var (
	kernelKinds = []string{KindGRE, KindGRETAP, KindIP6GRE, KindIP6GRETAP, KindIPIP, KindSIT}
	// Reverse transports and layer-3 carriers of BackPack v1.8; spoof needs forged-source settings it does not take here.
	reverseTransports = []string{"tcp", "tcpmux", "stealth", "ws", "wsmux", "wss", "wssmux", "kcp", "quic", "udp", "pck", "xdi"}
	l3Carriers        = []string{"udp", "quic", "pck", "sni", "xdi"}
)

// TunnelSettings is the JSON a tunnel row keeps; each kind reads the fields it needs.
type TunnelSettings struct {
	// LocalAddr binds the outer packets to one of this server's addresses; empty lets the route choose.
	LocalAddr string `json:"localAddr,omitempty" example:"203.0.113.10"`
	// RemoteAddr is the other server's public address, or the relay's host for a reverse client.
	RemoteAddr string `json:"remoteAddr,omitempty" example:"198.51.100.7"`
	// PublicAddr is how the other side reaches this server; empty means the detected public address.
	PublicAddr string `json:"publicAddr,omitempty" example:"203.0.113.10"`
	LocalIP    string `json:"localIp,omitempty" example:"10.80.0.2/30"`
	PeerIP     string `json:"peerIp,omitempty" example:"10.80.0.1"`
	MTU        int    `json:"mtu,omitempty" example:"1476"`
	TTL        int    `json:"ttl,omitempty" example:"255"`
	Key        uint32 `json:"key,omitempty" example:"0"`

	Transport string `json:"transport,omitempty" example:"tcpmux"`
	Carrier   string `json:"carrier,omitempty" example:"udp"`
	// Port is where the listening side of a BackPack tunnel waits for the other.
	Port  int    `json:"port,omitempty" example:"8443"`
	Token string `json:"token,omitempty" example:"3f9c..."`
	// Dialer says which role opens a layer-3 tunnel; the other one listens on Port.
	Dialer        string `json:"dialer,omitempty" example:"server"`
	ProxyProtocol bool   `json:"proxyProtocol,omitempty" example:"false"`

	Forwards []PortForward `json:"forwards,omitempty"`
	NAT      string        `json:"nat,omitempty" example:"masquerade"`
}

// PortForward is one port, or range, the server side exposes and sends to the client side.
type PortForward struct {
	Listen string `json:"listen" example:"443"`
	Target string `json:"target,omitempty" example:"2096"`
	Proto  string `json:"proto,omitempty" example:"tcp"`
}

// Tunnel is one end of a tunnel as the engines run it.
type Tunnel struct {
	ID       int
	PairID   string
	Name     string
	Kind     string
	Role     string
	Enabled  bool
	Settings TunnelSettings
	// DevName replaces the interface name the id gives, for the tunnel test's ends.
	DevName string
}

// IsKernel reports whether the kind is a kernel link.
func IsKernel(kind string) bool { return slices.Contains(kernelKinds, kind) }

func isBackPack(kind string) bool { return kind == KindReverse || kind == KindL3 }

// HasInterface reports whether this end has an interface with an address to probe.
func (t Tunnel) HasInterface() bool { return IsKernel(t.Kind) || t.Kind == KindL3 }

// Device is the interface of this end; Linux allows 15 bytes.
func (t Tunnel) Device() string {
	if t.DevName != "" {
		return t.DevName
	}
	if t.Kind == KindL3 {
		return fmt.Sprintf("mfbp%d", t.ID)
	}
	return fmt.Sprintf("mft%d", t.ID)
}

func ipv6Outer(kind string) bool { return kind == KindIP6GRE || kind == KindIP6GRETAP }

// Opposite is the other end's role.
func Opposite(role string) string {
	if role == RoleServer {
		return RoleClient
	}
	return RoleServer
}

// NewToken returns a random secret for a BackPack tunnel.
func NewToken() string {
	raw := make([]byte, 24)
	_, _ = rand.Read(raw)
	return hex.EncodeToString(raw)
}

// NewPairID names both ends of one tunnel.
func NewPairID() string {
	raw := make([]byte, 12)
	_, _ = rand.Read(raw)
	return hex.EncodeToString(raw)
}

var (
	pairIDPattern = regexp.MustCompile(`^[a-f0-9]{8,64}$`)
	tokenPattern  = regexp.MustCompile(`^[A-Za-z0-9._~-]{16,128}$`)
	hostPattern   = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,62}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,62}[A-Za-z0-9])?)*$`)
)

// Normalize fills defaults and checks one end before it is saved or run.
func Normalize(t Tunnel) (Tunnel, error) {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" || len(t.Name) > 64 || strings.ContainsFunc(t.Name, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return t, errors.New("tunnel: a name is 1 to 64 printable characters")
	}
	if t.PairID == "" {
		t.PairID = NewPairID()
	}
	if !pairIDPattern.MatchString(t.PairID) {
		return t, fmt.Errorf("tunnel: pair id %q is not hex", t.PairID)
	}
	t.Kind = strings.ToLower(strings.TrimSpace(t.Kind))
	if !IsKernel(t.Kind) && !isBackPack(t.Kind) {
		return t, fmt.Errorf("tunnel: kind %q is not one of %s", t.Kind, strings.Join(append(slices.Clone(kernelKinds), KindReverse, KindL3), ", "))
	}
	if t.Role != RoleClient && t.Role != RoleServer {
		return t, fmt.Errorf("tunnel: role %q must be %s or %s", t.Role, RoleClient, RoleServer)
	}
	s := &t.Settings
	var err error
	if s.PublicAddr = strings.TrimSpace(s.PublicAddr); s.PublicAddr != "" && !validHost(s.PublicAddr) {
		return t, fmt.Errorf("tunnel: public address %q is not an address or host name", s.PublicAddr)
	}
	switch {
	case IsKernel(t.Kind):
		err = normalizeKernel(t.Kind, s)
	case t.Kind == KindReverse:
		err = normalizeReverse(t.Role, s)
	default:
		err = normalizeL3(t.Role, s)
	}
	if err != nil {
		return t, err
	}
	if s.Forwards, err = normalizeForwards(t.Kind, s.Forwards); err != nil {
		return t, err
	}
	return t, nil
}

func validHost(host string) bool {
	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}
	return len(host) <= 253 && hostPattern.MatchString(host)
}

func normalizeKernel(kind string, s *TunnelSettings) error {
	s.RemoteAddr = strings.TrimSpace(s.RemoteAddr)
	remote, err := netip.ParseAddr(s.RemoteAddr)
	if err != nil {
		return fmt.Errorf("tunnel: the other server's address %q must be an IP address", s.RemoteAddr)
	}
	if remote.Is6() != ipv6Outer(kind) || remote.Is4In6() {
		return fmt.Errorf("tunnel: a %s link needs an IPv%s address for the other server", kind, map[bool]string{true: "6", false: "4"}[ipv6Outer(kind)])
	}
	if s.LocalAddr = strings.TrimSpace(s.LocalAddr); s.LocalAddr != "" {
		local, err := netip.ParseAddr(s.LocalAddr)
		if err != nil || local.Is6() != remote.Is6() {
			return fmt.Errorf("tunnel: this server's address %q must be an IP address of the same family as the other's", s.LocalAddr)
		}
	}
	if err := normalizeInner(kind, s); err != nil {
		return err
	}
	if s.MTU != 0 && (s.MTU < 576 || s.MTU > 9000) {
		return fmt.Errorf("tunnel: MTU %d must be 0 for the default or between 576 and 9000", s.MTU)
	}
	if s.TTL == 0 {
		s.TTL = 255
	}
	if s.TTL < 1 || s.TTL > 255 {
		return fmt.Errorf("tunnel: TTL %d must be between 1 and 255", s.TTL)
	}
	if s.Key != 0 && kind != KindGRE && kind != KindGRETAP && kind != KindIP6GRE && kind != KindIP6GRETAP {
		return fmt.Errorf("tunnel: only GRE links carry a key, not %s", kind)
	}
	switch s.NAT {
	case "":
		s.NAT = NATMasquerade
	case NATMasquerade, NATPreserve:
	default:
		return fmt.Errorf("tunnel: NAT mode %q must be %s or %s", s.NAT, NATMasquerade, NATPreserve)
	}
	s.Transport, s.Carrier, s.Port, s.Token, s.Dialer, s.ProxyProtocol = "", "", 0, "", "", false
	return nil
}

// normalizeInner checks the two tunnel addresses: this end's prefix and the peer inside it.
func normalizeInner(kind string, s *TunnelSettings) error {
	local, err := netip.ParsePrefix(strings.TrimSpace(s.LocalIP))
	if err != nil {
		return fmt.Errorf("tunnel: this end's tunnel address %q must be an address with a prefix, such as 10.80.0.2/30", s.LocalIP)
	}
	peer, err := netip.ParseAddr(strings.TrimSpace(s.PeerIP))
	if err != nil {
		return fmt.Errorf("tunnel: the other end's tunnel address %q is not an IP address", s.PeerIP)
	}
	switch {
	case kind == KindSIT && !local.Addr().Is6():
		return errors.New("tunnel: a SIT link carries IPv6, so its tunnel addresses must be IPv6")
	case kind == KindIPIP && !local.Addr().Is4():
		return errors.New("tunnel: an IPIP link carries IPv4, so its tunnel addresses must be IPv4")
	case local.Addr().Is4() != peer.Is4():
		return errors.New("tunnel: both tunnel addresses must be of one family")
	case local.Addr().Is4() && local.Bits() > 30, local.Addr().Is6() && local.Bits() > 126:
		return fmt.Errorf("tunnel: prefix %s leaves no room for the other end", local)
	case !local.Masked().Contains(peer) || peer == local.Addr():
		return fmt.Errorf("tunnel: the other end's address %s must be another address inside %s", peer, local.Masked())
	}
	s.LocalIP, s.PeerIP = local.String(), peer.String()
	return nil
}

func normalizeBackPack(s *TunnelSettings) error {
	if s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("tunnel: the tunnel port %d must be between 1 and 65535", s.Port)
	}
	if s.Token = strings.TrimSpace(s.Token); s.Token == "" {
		s.Token = NewToken()
	}
	if !tokenPattern.MatchString(s.Token) {
		return errors.New("tunnel: a token is 16 to 128 letters, digits or . _ ~ -")
	}
	return nil
}

func normalizeReverse(role string, s *TunnelSettings) error {
	if !slices.Contains(reverseTransports, s.Transport) {
		return fmt.Errorf("tunnel: transport %q must be one of %s", s.Transport, strings.Join(reverseTransports, ", "))
	}
	if err := normalizeBackPack(s); err != nil {
		return err
	}
	s.RemoteAddr = strings.TrimSpace(s.RemoteAddr)
	if role == RoleClient && !validHost(s.RemoteAddr) {
		return fmt.Errorf("tunnel: the relay's address %q must be an IP address or a host name", s.RemoteAddr)
	}
	s.LocalAddr, s.LocalIP, s.PeerIP, s.MTU, s.TTL, s.Key, s.Carrier, s.Dialer, s.NAT = "", "", "", 0, 0, 0, "", "", ""
	return nil
}

func normalizeL3(role string, s *TunnelSettings) error {
	if !slices.Contains(l3Carriers, s.Carrier) {
		return fmt.Errorf("tunnel: carrier %q must be one of %s", s.Carrier, strings.Join(l3Carriers, ", "))
	}
	if err := normalizeBackPack(s); err != nil {
		return err
	}
	if s.Dialer == "" {
		s.Dialer = RoleServer
	}
	if s.Dialer != RoleClient && s.Dialer != RoleServer {
		return fmt.Errorf("tunnel: the dialling side %q must be %s or %s", s.Dialer, RoleClient, RoleServer)
	}
	s.RemoteAddr = strings.TrimSpace(s.RemoteAddr)
	if s.Dialer == role && !validHost(s.RemoteAddr) {
		return fmt.Errorf("tunnel: the side that dials needs the other server's address, not %q", s.RemoteAddr)
	}
	if err := normalizeInner(KindL3, s); err != nil {
		return err
	}
	if s.MTU != 0 && (s.MTU < 576 || s.MTU > 9000) {
		return fmt.Errorf("tunnel: MTU %d must be 0 for the default or between 576 and 9000", s.MTU)
	}
	s.LocalAddr, s.TTL, s.Key, s.Transport, s.NAT, s.ProxyProtocol = "", 0, 0, "", "", false
	return nil
}

type portRange struct{ low, high int }

func parsePorts(raw string) (portRange, error) {
	low, high, isRange := strings.Cut(strings.TrimSpace(raw), "-")
	a, err := strconv.Atoi(strings.TrimSpace(low))
	if err != nil || a < 1 || a > 65535 {
		return portRange{}, fmt.Errorf("tunnel: %q is not a port or range of ports", raw)
	}
	b := a
	if isRange {
		if b, err = strconv.Atoi(strings.TrimSpace(high)); err != nil || b <= a || b > 65535 {
			return portRange{}, fmt.Errorf("tunnel: %q is not a port or range of ports", raw)
		}
	}
	return portRange{a, b}, nil
}

func (r portRange) String() string {
	if r.low == r.high {
		return strconv.Itoa(r.low)
	}
	return fmt.Sprintf("%d-%d", r.low, r.high)
}

func (r portRange) overlaps(o portRange) bool { return r.low <= o.high && o.low <= r.high }

func normalizeForwards(kind string, in []PortForward) ([]PortForward, error) {
	out := make([]PortForward, 0, len(in))
	type taken struct {
		r     portRange
		proto string
	}
	var seen []taken
	esp := false
	for _, f := range in {
		if strings.EqualFold(strings.TrimSpace(f.Proto), ProtoESP) {
			if !IsKernel(kind) {
				return nil, errors.New("tunnel: ESP needs a kernel link (GRE, IPIP, SIT…); BackPack carries TCP and UDP only")
			}
			if esp {
				return nil, errors.New("tunnel: ESP is forwarded twice")
			}
			esp = true
			out = append(out, PortForward{Proto: ProtoESP})
			continue
		}
		listen, err := parsePorts(f.Listen)
		if err != nil {
			return nil, err
		}
		target := listen
		if strings.TrimSpace(f.Target) != "" {
			if target, err = parsePorts(f.Target); err != nil {
				return nil, err
			}
		}
		if target.high-target.low != listen.high-listen.low {
			return nil, fmt.Errorf("tunnel: %s and %s are ranges of different sizes", listen, target)
		}
		if IsKernel(kind) && listen.low != listen.high && target != listen {
			return nil, fmt.Errorf("tunnel: a kernel link forwards a range %s to the same ports only", listen)
		}
		proto := strings.ToLower(strings.TrimSpace(f.Proto))
		if proto == "" {
			proto = ProtoTCP
		}
		if proto != ProtoTCP && proto != ProtoUDP && proto != ProtoBoth {
			return nil, fmt.Errorf("tunnel: protocol %q must be tcp, udp or both", f.Proto)
		}
		if isBackPack(kind) && proto == ProtoUDP {
			return nil, errors.New("tunnel: a BackPack tunnel forwards TCP, or TCP and UDP together, not UDP alone")
		}
		for _, s := range seen {
			if s.r.overlaps(listen) && (s.proto == proto || s.proto == ProtoBoth || proto == ProtoBoth) {
				return nil, fmt.Errorf("tunnel: port %s is forwarded twice", listen)
			}
		}
		seen = append(seen, taken{listen, proto})
		fw := PortForward{Listen: listen.String(), Proto: proto}
		if target != listen {
			fw.Target = target.String()
		}
		out = append(out, fw)
	}
	return out, nil
}

// ListenPorts returns every port this end listens on, with its protocol, so
// that no inbound is given one of them.
func (t Tunnel) ListenPorts() map[int]string {
	ports := map[int]string{}
	s := t.Settings
	switch {
	case t.Kind == KindReverse && t.Role == RoleServer:
		ports[s.Port] = ProtoTCP
		if slices.Contains([]string{"kcp", "quic", "udp"}, s.Transport) {
			ports[s.Port] = ProtoUDP
		}
	case t.Kind == KindL3 && s.Dialer != t.Role:
		ports[s.Port] = ProtoUDP
		if s.Carrier == "pck" || s.Carrier == "sni" {
			ports[s.Port] = ProtoTCP
		}
	}
	if t.Role != RoleServer {
		return ports
	}
	for _, f := range s.Forwards {
		r, err := parsePorts(f.Listen)
		if err != nil || f.Proto == ProtoESP {
			continue
		}
		for p := r.low; p <= r.high; p++ {
			ports[p] = f.Proto
		}
	}
	return ports
}

// Mirror returns the other end of t, with the sides swapped: what one end
// calls local the other calls remote. publicAddr is how that end reaches this one.
func Mirror(t Tunnel, publicAddr string) Tunnel {
	s := t.Settings
	m := TunnelSettings{
		MTU: s.MTU, TTL: s.TTL, Key: s.Key, Transport: s.Transport, Carrier: s.Carrier, Port: s.Port,
		Token: s.Token, Dialer: s.Dialer, ProxyProtocol: s.ProxyProtocol, NAT: s.NAT,
		Forwards: slices.Clone(s.Forwards),
	}
	if t.HasInterface() {
		local, _ := netip.ParsePrefix(s.LocalIP)
		m.LocalIP = netip.PrefixFrom(mustAddr(s.PeerIP), local.Bits()).String()
		m.PeerIP = local.Addr().String()
	}
	switch {
	case IsKernel(t.Kind):
		m.RemoteAddr = publicAddr
	case t.Kind == KindReverse && t.Role == RoleServer:
		m.RemoteAddr = publicAddr
	case t.Kind == KindL3 && s.Dialer != t.Role:
		m.RemoteAddr = publicAddr
	}
	return Tunnel{PairID: t.PairID, Name: t.Name, Kind: t.Kind, Role: Opposite(t.Role), Enabled: true, Settings: m}
}

func mustAddr(raw string) netip.Addr {
	addr, _ := netip.ParseAddr(raw)
	return addr
}

// PairingScheme marks a pairing code: the other end of a tunnel, ready to import.
const PairingScheme = "manifold-tunnel://"

type pairing struct {
	Version  int            `json:"v"`
	PairID   string         `json:"pairId"`
	Name     string         `json:"name"`
	Kind     string         `json:"kind"`
	Role     string         `json:"role"`
	Settings TunnelSettings `json:"settings"`
}

// PairingCode encodes the other end of t for the server that will run it.
func PairingCode(t Tunnel, publicAddr string) (string, error) {
	other := Mirror(t, publicAddr)
	if _, err := Normalize(other); err != nil {
		return "", fmt.Errorf("tunnel: the other end would not be valid: %w", err)
	}
	raw, err := json.Marshal(pairing{Version: 1, PairID: other.PairID, Name: other.Name, Kind: other.Kind, Role: other.Role, Settings: other.Settings})
	if err != nil {
		return "", err
	}
	return PairingScheme + base64URL(raw), nil
}

// ParsePairingCode reads a code another panel made into this end of the tunnel.
func ParsePairingCode(code string) (Tunnel, error) {
	body, ok := strings.CutPrefix(strings.TrimSpace(code), PairingScheme)
	if !ok {
		return Tunnel{}, fmt.Errorf("tunnel: a pairing code starts with %s", PairingScheme)
	}
	raw, err := decodeBase64URL(body)
	if err != nil {
		return Tunnel{}, errors.New("tunnel: the pairing code is damaged")
	}
	var p pairing
	if err := json.Unmarshal(raw, &p); err != nil || p.Version != 1 {
		return Tunnel{}, errors.New("tunnel: the pairing code is damaged or from a newer panel")
	}
	return Normalize(Tunnel{PairID: p.PairID, Name: p.Name, Kind: p.Kind, Role: p.Role, Enabled: true, Settings: p.Settings})
}

func base64URL(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }

func decodeBase64URL(text string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(strings.TrimSpace(text), "="))
}
