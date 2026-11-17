package l2tp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// User is one account the L2TP server accepts.
type User struct {
	Name     string
	Password string
	// MaxSessions caps the account's simultaneous connections; zero means no cap.
	MaxSessions int
}

// Instance is the desired runtime configuration of an L2TP inbound.
type Instance struct {
	Id  int
	Tag string
	// IPsec says whether a session must arrive through IPsec to be taken.
	IPsec  bool
	PSK    string
	Subnet netip.Prefix
	DNS    []string
	MTU    int
	Users  []User
}

// InstanceFromInbound derives the server to run from an L2TP inbound, with one
// user per enabled client. An inbound without such a client is still run, so
// that a switched-off account is refused instead of meeting a dead port.
func InstanceFromInbound(ib *model.Inbound) (Instance, bool) {
	if ib == nil || ib.Protocol != model.L2TP || ib.Port != Port {
		return Instance{}, false
	}
	var parsed InboundSettings
	if err := json.Unmarshal([]byte(ib.Settings), &parsed); err != nil || parsed.Server == nil {
		return Instance{}, false
	}
	server := parsed.Server
	subnet, err := parseSubnet(server.Subnet)
	if err != nil || ValidatePSK(server.PSK) != nil || server.MTU < minMTU || server.MTU > maxMTU {
		return Instance{}, false
	}
	dns, err := parseDNS(server.DNS)
	if err != nil {
		return Instance{}, false
	}
	users := make([]User, 0, len(parsed.Clients))
	for _, c := range parsed.Clients {
		if !c.Enable || ValidateUsername(c.Email) != nil || ValidatePassword(c.Password) != nil {
			continue
		}
		users = append(users, User{Name: c.Email, Password: c.Password, MaxSessions: max(c.UserLimit, 0)})
	}
	return Instance{
		Id:     ib.Id,
		Tag:    ib.Tag,
		IPsec:  server.RequiresIPsec(),
		PSK:    server.PSK,
		Subnet: subnet,
		DNS:    dns,
		MTU:    server.MTU,
		Users:  users,
	}, true
}

// serverAddress is the server's end of every session: the first host of the subnet.
func (inst Instance) serverAddress() netip.Addr { return inst.Subnet.Addr().Next() }

// pool is what clients are given: every host after the server's.
func (inst Instance) pool() (first, last netip.Addr) {
	raw := inst.Subnet.Addr().As4()
	for bit := inst.Subnet.Bits(); bit < 32; bit++ {
		raw[bit/8] |= 0x80 >> (bit % 8)
	}
	broadcast := netip.AddrFrom4(raw)
	return inst.serverAddress().Next(), broadcast.Prev()
}

func egressOwner(id int) string { return fmt.Sprintf("l2tp%d", id) }

// guardOwner names the rule that keeps unencrypted sessions out.
func guardOwner(id int) string { return fmt.Sprintf("l2tp%dg", id) }

// structuralFingerprint moves with what only restarting the daemons can
// apply; accounts are decided per login and never need one.
func (inst Instance) structuralFingerprint() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		fmt.Sprint(inst.IPsec),
		inst.PSK,
		inst.Subnet.String(),
		strings.Join(inst.DNS, ","),
		fmt.Sprint(inst.MTU),
	}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func (inst Instance) usersFingerprint() string {
	sum := sha256.New()
	for _, u := range inst.Users {
		fmt.Fprintf(sum, "%s\x00%s\x00%d\x00", u.Name, u.Password, u.MaxSessions)
	}
	return hex.EncodeToString(sum.Sum(nil))
}
