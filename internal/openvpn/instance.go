package openvpn

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// User is one account an OpenVPN server accepts.
type User struct {
	Name     string
	Password string
	// MaxSessions caps the account's simultaneous connections; zero means no cap.
	MaxSessions int
}

// Instance is the desired runtime configuration of one OpenVPN inbound.
type Instance struct {
	Id     int
	Tag    string
	Listen string
	Port   int
	Proto  string
	Subnet netip.Prefix
	DNS    []string

	CACert      string
	ServerCert  string
	ServerKey   string
	TLSCryptKey string

	Users []User
}

// InstanceFromInbound derives the server to run from an OpenVPN inbound, with
// one user per enabled client. It reports false when there is nothing to run:
// another protocol, unusable settings, or no client that can log in.
func InstanceFromInbound(ib *model.Inbound) (Instance, bool) {
	if ib == nil || ib.Protocol != model.OpenVPN {
		return Instance{}, false
	}
	var parsed InboundSettings
	if err := json.Unmarshal([]byte(ib.Settings), &parsed); err != nil || parsed.Server == nil {
		return Instance{}, false
	}
	server := parsed.Server
	subnet, err := parseSubnet(server.Subnet)
	if err != nil || !server.hasPKI() || server.TLSCryptKey == "" {
		return Instance{}, false
	}
	if server.Proto != ProtoUDP && server.Proto != ProtoTCP {
		return Instance{}, false
	}
	dns, err := parseDNS(server.DNS)
	if err != nil {
		return Instance{}, false
	}
	listen := strings.TrimSpace(ib.Listen)
	if _, ok := listenAddress(listen); !ok {
		return Instance{}, false
	}
	users := make([]User, 0, len(parsed.Clients))
	for _, c := range parsed.Clients {
		if !c.Enable || ValidateUsername(c.Email) != nil || ValidatePassword(c.Password) != nil {
			continue
		}
		users = append(users, User{Name: c.Email, Password: c.Password, MaxSessions: max(c.LimitIP, 0)})
	}
	if len(users) == 0 {
		return Instance{}, false
	}
	return Instance{
		Id:          ib.Id,
		Tag:         ib.Tag,
		Listen:      listen,
		Port:        ib.Port,
		Proto:       server.Proto,
		Subnet:      subnet,
		DNS:         dns,
		CACert:      server.CACert,
		ServerCert:  server.ServerCert,
		ServerKey:   server.ServerKey,
		TLSCryptKey: server.TLSCryptKey,
		Users:       users,
	}, true
}

// deviceName is the tun interface of an inbound; Linux caps it at 15 bytes.
func deviceName(id int) string { return fmt.Sprintf("mfovpn%d", id) }

func egressOwner(id int) string { return fmt.Sprintf("ovpn%d", id) }

// structuralFingerprint moves with everything only a restart of openvpn can apply.
func (inst Instance) structuralFingerprint() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		inst.Listen,
		fmt.Sprint(inst.Port),
		inst.Proto,
		inst.Subnet.String(),
		strings.Join(inst.DNS, ","),
		inst.CACert,
		inst.ServerCert,
		inst.ServerKey,
		inst.TLSCryptKey,
	}, "\x00")))
	return hex.EncodeToString(sum[:])
}

// usersFingerprint moves with the accounts, which the panel applies without
// touching the process; client order in the stored settings does not matter.
func (inst Instance) usersFingerprint() string {
	rows := make([]string, 0, len(inst.Users))
	for _, u := range inst.Users {
		rows = append(rows, fmt.Sprintf("%s\x00%s\x00%d", u.Name, u.Password, u.MaxSessions))
	}
	slices.Sort(rows)
	sum := sha256.Sum256([]byte(strings.Join(rows, "\x01")))
	return hex.EncodeToString(sum[:])
}
