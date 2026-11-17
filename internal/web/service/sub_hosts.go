package service

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/access"
	"github.com/mhsanaei/3x-ui/v3/internal/web/entity"
)

// subscriberLabelLength keeps a per-subscriber host short enough to read out yet unguessable.
const subscriberLabelLength = 10

// SubHostPlan is where subscription links point: the panel's subscription domains in
// order (the first for new links, a "*." one giving every subscriber a host of its own)
// and the domain of each reseller whose customers' links carry it.
type SubHostPlan struct {
	Domains  []string
	Reseller map[int]string
	secret   []byte
}

var subHostSecretMu sync.Mutex

// subHostSecret is the panel's own key for per-subscriber host names, made on first use.
func subHostSecret() []byte {
	subHostSecretMu.Lock()
	defer subHostSecretMu.Unlock()
	var row model.Setting
	if err := database.GetDB().Where("key = ?", "subHostSecret").First(&row).Error; err == nil && row.Value != "" {
		if b, err := hex.DecodeString(row.Value); err == nil {
			return b
		}
	}
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	_ = (&SettingService{}).saveSetting("subHostSecret", hex.EncodeToString(b))
	return b
}

// LoadSubHostPlan reads the domains and every reseller's own domain.
func LoadSubHostPlan() SubHostPlan {
	raw, _ := (&SettingService{}).GetSubDomains()
	plan := SubHostPlan{Domains: entity.ParseSubDomains(raw), Reseller: map[int]string{}}
	var sellers []model.User
	if err := database.GetDB().Select("id", "sub_domain").
		Where("tier = ? AND sub_domain <> ''", access.TierReseller).Find(&sellers).Error; err == nil {
		for _, u := range sellers {
			if host := strings.ToLower(strings.TrimSpace(u.SubDomain)); entity.ValidSubDomain(host) {
				plan.Reseller[u.Id] = host
			}
		}
	}
	if plan.hasWildcard() {
		plan.secret = subHostSecret()
	}
	return plan
}

func (p SubHostPlan) hasWildcard() bool {
	for _, d := range p.Domains {
		if strings.HasPrefix(d, "*.") {
			return true
		}
	}
	for _, d := range p.Reseller {
		if strings.HasPrefix(d, "*.") {
			return true
		}
	}
	return false
}

// SubscriberLabel is a user's own name under a wildcard domain: stable across refreshes,
// different under each domain, and not traceable back to the subscription.
func SubscriberLabel(secret []byte, recordID int, domain string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("sub-host:" + strconv.Itoa(recordID) + ":" + domain))
	n := new(big.Int).SetBytes(mac.Sum(nil))
	label := n.Text(36)
	for len(label) < subscriberLabelLength {
		label = "0" + label
	}
	// A label must start with a letter for the strictest resolvers.
	return "k" + label[:subscriberLabelLength-1]
}

// HostFor is the host a user's links are built on, "" when no domain is configured.
func (p SubHostPlan) HostFor(recordID, ownerID int) string {
	domain := p.Reseller[ownerID]
	if domain == "" && len(p.Domains) > 0 {
		domain = p.Domains[0]
	}
	return p.expand(domain, recordID)
}

func (p SubHostPlan) expand(domain string, recordID int) string {
	if rest, ok := strings.CutPrefix(domain, "*."); ok {
		if len(p.secret) == 0 {
			return rest
		}
		return SubscriberLabel(p.secret, recordID, domain) + "." + rest
	}
	return domain
}

// Accepts reports a host the subscription server answers on: a configured domain, a reseller's,
// or one label under one of their wildcards. Earlier domains keep serving the links handed out.
func (p SubHostPlan) Accepts(host string) bool {
	host = strings.ToLower(host)
	check := func(domain string) bool {
		if rest, ok := strings.CutPrefix(domain, "*."); ok {
			label, parent, found := strings.Cut(host, ".")
			return (found && parent == rest && label != "") || host == rest
		}
		return host == domain
	}
	for _, d := range p.Domains {
		if check(d) {
			return true
		}
	}
	for _, d := range p.Reseller {
		if check(d) {
			return true
		}
	}
	return false
}

// Configured reports whether any domain restricts where subscriptions are served.
func (p SubHostPlan) Configured() bool {
	return len(p.Domains) > 0 || len(p.Reseller) > 0
}

// withHost replaces the host of a link base, keeping its scheme, port and path.
func withHost(base, host string) string {
	if host == "" {
		return base
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return base
	}
	if port := u.Port(); port != "" {
		u.Host = net.JoinHostPort(host, port)
	} else {
		u.Host = host
	}
	return u.String()
}

var acceptedSubHosts = struct {
	sync.Mutex
	plan   SubHostPlan
	legacy string
	read   time.Time
}{}

// SubHostAccepted is the subscription server's host check, with the plan reread at most
// every 30 seconds so a reseller's new domain works without a restart.
func SubHostAccepted(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	acceptedSubHosts.Lock()
	if time.Since(acceptedSubHosts.read) > 30*time.Second {
		acceptedSubHosts.plan = LoadSubHostPlan()
		acceptedSubHosts.legacy, _ = (&SettingService{}).GetSubDomain()
		acceptedSubHosts.read = time.Now()
	}
	plan, legacy := acceptedSubHosts.plan, acceptedSubHosts.legacy
	acceptedSubHosts.Unlock()
	if !plan.Configured() && legacy == "" {
		return true
	}
	return strings.EqualFold(host, legacy) || plan.Accepts(host)
}
