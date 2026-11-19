package tunnel

import (
	"errors"
	"fmt"
	"math"
	"net"
	"net/netip"
	"slices"
	"strconv"
)

// BenchCandidate is one way the tunnel test links the two servers.
type BenchCandidate struct {
	Key       string `json:"key" example:"reverse:tcpmux"`
	Kind      string `json:"kind" example:"reverse"`
	Transport string `json:"transport,omitempty" example:"tcpmux"`
	Carrier   string `json:"carrier,omitempty" example:""`
	Dialer    string `json:"dialer,omitempty" example:""`
	MTU       int    `json:"mtu,omitempty" example:"0"`
}

// BenchCatalogue is every candidate a test can try, in the order it tries them: the
// kernel links, BackPack's reverse transports, then its layer-3 carriers dialled each way.
func BenchCatalogue() []BenchCandidate {
	var out []BenchCandidate
	for _, k := range kernelKinds {
		out = append(out, BenchCandidate{Key: k, Kind: k})
	}
	for _, tr := range reverseTransports {
		out = append(out, BenchCandidate{Key: KindReverse + ":" + tr, Kind: KindReverse, Transport: tr})
	}
	for _, dialer := range []string{RoleServer, RoleClient} {
		for _, c := range l3Carriers {
			out = append(out, BenchCandidate{Key: KindL3 + ":" + c + ":" + dialer, Kind: KindL3, Carrier: c, Dialer: dialer})
		}
	}
	return out
}

// BenchCandidateByKey finds a candidate of the catalogue.
func BenchCandidateByKey(key string) (BenchCandidate, bool) {
	i := slices.IndexFunc(BenchCatalogue(), func(c BenchCandidate) bool { return c.Key == key })
	if i < 0 {
		return BenchCandidate{}, false
	}
	return BenchCatalogue()[i], true
}

// NeedsIPv6 reports whether the candidate's outer packets are IPv6.
func (c BenchCandidate) NeedsIPv6() bool { return ipv6Outer(c.Kind) }

// HasInterface reports whether the candidate's ends get a tunnel address, and so an MTU.
func (c BenchCandidate) HasInterface() bool { return IsKernel(c.Kind) || c.Kind == KindL3 }

// MTUVariants are the smaller MTUs worth trying when large packets do not cross a path.
func (c BenchCandidate) MTUVariants() []int {
	ceiling := 1500
	if IsKernel(c.Kind) {
		ceiling = defaultMTU(c.Kind)
	}
	var out []int
	for _, mtu := range []int{1400, 1360, 1280} {
		if mtu < ceiling {
			out = append(out, mtu)
		}
	}
	return out
}

// BenchPortSpan is how many ports a test reserves on each server, from its first port.
const BenchPortSpan = 80

// BenchSlots is how many candidates fit a test's ports and its /24 of tunnel addresses.
const BenchSlots = (BenchPortSpan - 1) / 2

// BenchSlot is where one candidate lives: its interface, ports and tunnel addresses.
type BenchSlot struct {
	N           int
	BenchPort   int
	TunnelPort  int
	ForwardPort int
	Inner4      netip.Prefix
	Inner6      netip.Prefix
}

// BenchSlotAt places the n-th candidate of a test whose ports start at base and whose
// tunnel addresses come from net4 (a /24) and net6 (a /64).
func BenchSlotAt(n, base int, net4, net6 netip.Prefix) (BenchSlot, error) {
	if n < 0 || n >= BenchSlots {
		return BenchSlot{}, fmt.Errorf("tunnel: a test holds at most %d candidates", BenchSlots)
	}
	if !net4.Addr().Is4() || net4.Bits() != 24 || !net6.Addr().Is6() || net6.Bits() != 64 {
		return BenchSlot{}, errors.New("tunnel: a test's tunnel addresses are an IPv4 /24 and an IPv6 /64")
	}
	a4 := net4.Masked().Addr().As4()
	a4[3] = byte(n * 4)
	a6 := net6.Masked().Addr().As16()
	a6[15] = byte(n * 4)
	return BenchSlot{
		N: n, BenchPort: base, TunnelPort: base + 1 + 2*n, ForwardPort: base + 2 + 2*n,
		Inner4: netip.PrefixFrom(netip.AddrFrom4(a4), 30), Inner6: netip.PrefixFrom(netip.AddrFrom16(a6), 126),
	}, nil
}

// BenchSide is one server of a test: its role and how the other reaches it.
type BenchSide struct {
	Role  string
	Addr4 string
	Addr6 string
}

// ErrBenchNoIPv6 skips a candidate whose outer packets need IPv6 on both servers.
var ErrBenchNoIPv6 = errors.New("tunnel: both servers need a public IPv6 address for this kind")

// innerPair gives the origin the second address of a /30 or /126 and the relay the first.
func innerPair(inner netip.Prefix, role string) (local, peer string) {
	first := inner.Masked().Addr().Next()
	second := first.Next()
	if role == RoleServer {
		first, second = second, first
	}
	return netip.PrefixFrom(second, inner.Bits()).String(), first.String()
}

// BuildBenchPair makes this server's end of a candidate and the other's as its mirror;
// the other server builds the same end from the same arguments, so neither dictates one.
func BuildBenchPair(c BenchCandidate, slot BenchSlot, self, peer BenchSide, pairID, token string) (mine, theirs Tunnel, err error) {
	if self.Role != RoleClient && self.Role != RoleServer || peer.Role != Opposite(self.Role) {
		return Tunnel{}, Tunnel{}, errors.New("tunnel: a test needs one origin and one relay")
	}
	if !pairIDPattern.MatchString(pairID) {
		return Tunnel{}, Tunnel{}, errors.New("tunnel: a test end needs a pair id")
	}
	mine = Tunnel{PairID: pairID, Name: "test " + c.Key, Kind: c.Kind, Role: self.Role, Enabled: true, DevName: BenchDevice(slot.N)}
	s := &mine.Settings
	selfAddr, peerAddr := self.Addr4, peer.Addr4
	if c.NeedsIPv6() {
		selfAddr, peerAddr = self.Addr6, peer.Addr6
		if selfAddr == "" || peerAddr == "" {
			return Tunnel{}, Tunnel{}, ErrBenchNoIPv6
		}
	}
	if selfAddr == "" || peerAddr == "" {
		return Tunnel{}, Tunnel{}, errors.New("tunnel: both servers need a public IPv4 address for this kind")
	}
	switch {
	case IsKernel(c.Kind):
		inner := slot.Inner4
		if c.Kind == KindSIT {
			inner = slot.Inner6
		}
		s.RemoteAddr, s.MTU = peerAddr, c.MTU
		s.LocalIP, s.PeerIP = innerPair(inner, self.Role)
		if c.Kind == KindGRE || c.Kind == KindGRETAP || c.Kind == KindIP6GRE || c.Kind == KindIP6GRETAP {
			// A key of its own keeps the test's link apart from a real GRE link between the same servers.
			s.Key = 0x4d540000 | uint32(slot.N)
		}
	case c.Kind == KindReverse:
		s.Transport, s.Port, s.Token = c.Transport, slot.TunnelPort, token
		s.Forwards = []PortForward{{Listen: strconv.Itoa(slot.ForwardPort), Target: strconv.Itoa(slot.BenchPort), Proto: ProtoBoth}}
		if self.Role == RoleClient {
			s.RemoteAddr = peerAddr
		}
	case c.Kind == KindL3:
		s.Carrier, s.Dialer, s.Port, s.Token, s.MTU = c.Carrier, c.Dialer, slot.TunnelPort, token, c.MTU
		s.LocalIP, s.PeerIP = innerPair(slot.Inner4, self.Role)
		if c.Dialer == self.Role {
			s.RemoteAddr = peerAddr
		}
	default:
		return Tunnel{}, Tunnel{}, fmt.Errorf("tunnel: %q is not a kind the test knows", c.Kind)
	}
	if mine, err = Normalize(mine); err != nil {
		return Tunnel{}, Tunnel{}, err
	}
	theirs = Mirror(mine, selfAddr)
	theirs.DevName = mine.DevName
	if theirs, err = Normalize(theirs); err != nil {
		return Tunnel{}, Tunnel{}, err
	}
	return mine, theirs, nil
}

// BenchTarget is where the relay's end reaches the origin's test port: through the reverse
// tunnel's forwarded port, or at the origin's tunnel address, which is also pinged.
func BenchTarget(relayEnd Tunnel, benchPort int) (dial, echo string) {
	if relayEnd.Kind == KindReverse && len(relayEnd.Settings.Forwards) > 0 {
		return net.JoinHostPort("127.0.0.1", relayEnd.Settings.Forwards[0].Listen), ""
	}
	return net.JoinHostPort(relayEnd.Settings.PeerIP, strconv.Itoa(benchPort)), relayEnd.Settings.PeerIP
}

// States of a candidate's result; a pending one has not been measured yet.
const (
	BenchOK      = "ok"
	BenchFailed  = "failed"
	BenchSkipped = "skipped"
	BenchPending = "pending"
)

// Rounds of a test: every candidate is screened, the best are measured in full, and the
// best of those with an interface are tried again at smaller MTUs.
const (
	BenchStageScreen = "screen"
	BenchStageFull   = "full"
	BenchStageTune   = "tune"
)

// BenchResult is how one candidate did: TCPScore and UDPScore weigh what it carried of
// each against the best of the run, out of 100, and Score weighs the two together.
type BenchResult struct {
	BenchCandidate
	BenchMetrics
	State string `json:"state" example:"ok"`
	// Stage is the round that measured it: a short screening, the full test, or tuning.
	Stage    string  `json:"stage" example:"full"`
	Score    float64 `json:"score" example:"87.4"`
	TCPScore float64 `json:"tcpScore" example:"91.2"`
	UDPScore float64 `json:"udpScore" example:"80.5"`
}

// Throughput weighs one stream each way and the loaded streams into one figure in Mbps.
func (r BenchResult) Throughput() float64 {
	return 0.3*r.DownMbps + 0.3*r.UpMbps + 0.2*r.LoadDownMbps + 0.2*r.LoadUpMbps
}

// UDPRate is the rate UDP held, the mean of both ways, in Mbps.
func (r BenchResult) UDPRate() float64 { return 0.5*r.UDPDownMbps + 0.5*r.UDPUpMbps }

// BenchPicks are indexes into the results, or -1: the best tunnel for every inbound, and
// the best for TCP inbounds and for UDP inbounds alone.
type BenchPicks struct {
	Best int
	TCP  int
	UDP  int
}

// Weights of the overall score: TCP serves most inbounds, UDP serves WireGuard, L2TP and
// OpenVPN over UDP, and a tunnel that carries only one loses the other's share.
const (
	tcpShare = 0.6
	udpShare = 0.4
)

// RankBench scores every result and picks the best. Throughput weighs most; loss, broken
// streams and queueing under load take away what a fast but unstable path would gain.
func RankBench(results []BenchResult) BenchPicks {
	var maxT, maxU float64
	minLat, minLoad, minUDP := math.Inf(1), math.Inf(1), math.Inf(1)
	for _, r := range results {
		if r.State != BenchOK {
			continue
		}
		if r.TCP {
			maxT = max(maxT, r.Throughput())
			minLat = min(minLat, max(r.LatencyMs, 0.1))
			minLoad = min(minLoad, max(loadedLatency(r), 0.1))
		}
		if r.UDP {
			maxU = max(maxU, r.UDPRate())
			minUDP = min(minUDP, max(r.UDPLatencyMs, 0.1))
		}
	}
	picks := BenchPicks{Best: -1, TCP: -1, UDP: -1}
	better := func(pick *int, i int, score func(BenchResult) float64) {
		if score(results[i]) > 0 && (*pick < 0 || score(results[i]) > score(results[*pick])) {
			*pick = i
		}
	}
	for i := range results {
		r := &results[i]
		r.Score, r.TCPScore, r.UDPScore = 0, 0, 0
		if r.State != BenchOK {
			continue
		}
		if r.TCP {
			var tRel float64
			if maxT > 0 {
				tRel = r.Throughput() / maxT
			}
			lRel := minLat / max(r.LatencyMs, 0.1)
			qRel := minLoad / max(loadedLatency(*r), 0.1)
			keep := 1 - min(r.Loss, 100)/100
			broken := 0.0
			if streams := r.Streams + 2; r.StreamErrors > 0 {
				broken = min(float64(r.StreamErrors)/float64(streams), 1)
			}
			r.TCPScore = round1(100 * (0.50*tRel + 0.15*lRel + 0.10*qRel + 0.25*keep*keep*(1-broken)))
		}
		if r.UDP {
			var uRel float64
			if maxU > 0 {
				uRel = r.UDPRate() / maxU
			}
			lRel := minUDP / max(r.UDPLatencyMs, 0.1)
			loss := max(r.UDPLoss, (r.UDPDownLoss+r.UDPUpLoss)/2)
			keep := 1 - min(loss, 100)/100
			r.UDPScore = round1(100 * (0.50*uRel + 0.15*lRel + 0.35*keep*keep))
		}
		r.Score = round1(tcpShare*r.TCPScore + udpShare*r.UDPScore)
		better(&picks.Best, i, func(r BenchResult) float64 { return r.Score })
		better(&picks.TCP, i, func(r BenchResult) float64 { return r.TCPScore })
		better(&picks.UDP, i, func(r BenchResult) float64 { return r.UDPScore })
	}
	return picks
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

func loadedLatency(r BenchResult) float64 {
	if r.LoadLatencyMs > 0 {
		return r.LoadLatencyMs
	}
	return r.LatencyMs
}
