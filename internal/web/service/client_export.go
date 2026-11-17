package service

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/openvpn"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

// SubURLBuilder builds subscription addresses from settings read once, so a list of
// thousands of users costs no query per row.
type SubURLBuilder struct {
	sub, json, clash, singbox string
	plan                      SubHostPlan
}

// NewSubURLBuilder reads where subscription links point. A configured URI (the public
// address behind a CDN or proxy) wins; else the subscription domain, port and path.
func (s *SettingService) NewSubURLBuilder() SubURLBuilder {
	subURI, _ := s.GetSubURI()
	subJSONURI, _ := s.GetSubJsonURI()
	subClashURI, _ := s.GetSubClashURI()
	subDomain, _ := s.GetSubDomain()
	subPort, _ := s.GetSubPort()
	subPath, _ := s.GetSubPath()
	subJSONPath, _ := s.GetSubJsonPath()
	subClashPath, _ := s.GetSubClashPath()
	jsonEnabled, _ := s.GetSubJsonEnable()
	clashEnabled, _ := s.GetSubClashEnable()
	keyFile, _ := s.GetSubKeyFile()
	certFile, _ := s.GetSubCertFile()
	tls := keyFile != "" && certFile != ""
	scheme := "http"
	if tls {
		scheme = "https"
	}
	if subDomain == "" {
		if d, err := s.GetWebDomain(); err == nil && d != "" {
			subDomain = d
		} else if ip := KnownPublicIPv4(); ip != "" {
			subDomain = ip
		} else if h, err := os.Hostname(); err == nil && h != "" {
			subDomain = h
		} else {
			subDomain = "localhost"
		}
	}
	host := subDomain
	if standardPort := (subPort == 443 && tls) || (subPort == 80 && !tls); !standardPort {
		host = fmt.Sprintf("%s:%d", subDomain, subPort)
	}
	base := func(uri, path string) string {
		if uri != "" {
			return strings.TrimSuffix(uri, "/") + "/"
		}
		return fmt.Sprintf("%s://%s/%s/", scheme, host, strings.Trim(path, "/"))
	}
	b := SubURLBuilder{sub: base(subURI, subPath), plan: LoadSubHostPlan()}
	// A wildcard in the public address names each user too, so it needs the same key.
	singboxURI, _ := s.GetSubSingboxURI()
	for _, uri := range []string{subURI, subJSONURI, subClashURI, singboxURI} {
		if u, err := url.Parse(uri); err == nil && strings.HasPrefix(u.Hostname(), "*.") && b.plan.secret == nil {
			b.plan.secret = subHostSecret()
		}
	}
	if jsonEnabled {
		b.json = base(subJSONURI, subJSONPath)
	}
	if clashEnabled {
		b.clash = base(subClashURI, subClashPath)
	}
	if on, _ := s.GetSubSingboxEnable(); on {
		uri, _ := s.GetSubSingboxURI()
		path, _ := s.GetSubSingboxPath()
		b.singbox = base(uri, path)
	}
	return b
}

// For returns a user's subscription addresses, each "" when that format is off. The host is
// its reseller's domain, else the first subscription domain, a wildcard naming it alone.
func (b SubURLBuilder) For(recordID, ownerID int, subID string) (sub, json, clash string) {
	if subID == "" {
		return "", "", ""
	}
	host := b.plan.HostFor(recordID, ownerID)
	build := func(base string) string {
		if base == "" {
			return ""
		}
		base = withHost(base, host)
		if u, err := url.Parse(base); err == nil {
			if h := u.Hostname(); strings.HasPrefix(h, "*.") {
				base = withHost(base, b.plan.expand(h, recordID))
			}
		}
		return base + subID
	}
	return build(b.sub), build(b.json), build(b.clash)
}

// SingboxFor is the user's sing-box profile address, "" while that format is off.
func (b SubURLBuilder) SingboxFor(recordID, ownerID int, subID string) string {
	if b.singbox == "" || subID == "" {
		return ""
	}
	only := b
	only.sub, only.json, only.clash = b.singbox, "", ""
	link, _, _ := only.For(recordID, ownerID, subID)
	return link
}

// SubscriptionURLs is For for the user holding subID, looked up by it.
func (s *SettingService) SubscriptionURLs(subID string) (string, string) {
	var rec model.ClientRecord
	_ = database.GetDB().Select("id", "owner_id").Where("sub_id = ?", subID).First(&rec).Error
	sub, json, _ := s.NewSubURLBuilder().For(rec.Id, rec.OwnerId, subID)
	return sub, json
}

type exportRow struct {
	model.ClientRecord
	Up, Down     int64
	LastOnline   int64
	LastSubFetch int64
	OwnerName    string
}

// exportRows reads the users to export, all of them or only these emails; owner >= 0 keeps one owner's.
func exportRows(emails []string, owner int) ([]exportRow, error) {
	q := database.GetDB().Model(&model.ClientRecord{})
	if emails != nil {
		q = q.Where("email IN ?", emails)
	}
	if owner >= 0 {
		q = q.Where("owner_id = ?", owner)
	}
	var recs []model.ClientRecord
	if err := q.Order("email").Find(&recs).Error; err != nil {
		return nil, err
	}
	all := make([]string, len(recs))
	for i, r := range recs {
		all[i] = r.Email
	}
	traffic := map[string]xray.ClientTraffic{}
	var rows []xray.ClientTraffic
	for _, batch := range chunkStrings(all, sqlInChunk) {
		if err := database.GetDB().Where("email IN ?", batch).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, t := range rows {
			traffic[t.Email] = t
		}
	}
	var users []model.User
	if err := database.GetDB().Select("id", "username").Find(&users).Error; err != nil {
		return nil, err
	}
	names := map[int]string{}
	for _, u := range users {
		names[u.Id] = u.Username
	}
	out := make([]exportRow, len(recs))
	for i, r := range recs {
		t := traffic[r.Email]
		out[i] = exportRow{ClientRecord: r, Up: t.Up, Down: t.Down, LastOnline: t.LastOnline, LastSubFetch: t.LastSubFetch, OwnerName: names[r.OwnerId]}
	}
	return out, nil
}

func exportTime(ms int64, loc *time.Location) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).In(loc).Format("2006-01-02 15:04")
}

// ExportCSV is one row per user with its subscription link: the list to hand to someone.
// A spreadsheet reads the leading byte-order mark as UTF-8.
func (s *ClientService) ExportCSV(emails []string, owner int, now time.Time) (string, error) {
	rows, err := exportRows(emails, owner)
	if err != nil {
		return "", err
	}
	urls := (&SettingService{}).NewSubURLBuilder()
	loc := usageLocation(database.GetDB(), now)
	var b bytes.Buffer
	b.WriteString("\uFEFF")
	w := csv.NewWriter(&b)
	_ = w.Write([]string{
		"name", "enabled", "group", "owner", "used_bytes", "total_bytes", "expiry", "speed_limit_mbps",
		"ip_limit", "device_limit", "last_online", "last_fetch", "subscription", "comment",
		"telegram", "email", "phone", "bale", "soroush", "rubika",
	})
	for _, r := range rows {
		subURL, _, _ := urls.For(r.Id, r.OwnerId, r.SubID)
		expiry := exportTime(r.ExpiryTime, loc)
		if r.ExpiryTime < 0 {
			expiry = fmt.Sprintf("%d days from first use", -r.ExpiryTime/int64(24*time.Hour/time.Millisecond))
		}
		row := []string{
			r.Email, strconv.FormatBool(r.Enable), r.Group, r.OwnerName,
			strconv.FormatInt(r.Up+r.Down, 10), strconv.FormatInt(r.TotalGB, 10), expiry,
			strconv.Itoa(r.SpeedLimit), strconv.Itoa(r.LimitIP), strconv.Itoa(r.LimitHwid),
			exportTime(r.LastOnline, loc), exportTime(r.LastSubFetch, loc), subURL, r.Comment,
		}
		for _, f := range model.ContactFields {
			row = append(row, r.Contacts[f])
		}
		_ = w.Write(row)
	}
	w.Flush()
	return b.String(), w.Error()
}

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9._@-]+`)

func packName(s string) string {
	s = strings.Trim(unsafeFileChars.ReplaceAllString(s, "_"), "_.")
	if s == "" {
		return "config"
	}
	return s
}

// clientAppFiles turns the links a dedicated app cannot import into the files it opens:
// an OpenVPN link carries its profile, a WireGuard link becomes a wg-quick config.
func clientAppFiles(links []string) map[string]string {
	files := map[string]string{}
	add := func(base, ext, body string) {
		name := base + ext
		for i := 2; files[name] != ""; i++ {
			name = fmt.Sprintf("%s-%d%s", base, i, ext)
		}
		files[name] = body
	}
	for _, link := range links {
		switch {
		case strings.HasPrefix(link, openvpn.ShareLinkScheme):
			raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(link, openvpn.ShareLinkScheme))
			if err == nil {
				add("openvpn", ".ovpn", string(raw))
			}
		case strings.HasPrefix(link, "wireguard://"):
			if conf, remark, ok := wireguardConf(link); ok {
				add(packName(remark), ".conf", conf)
			}
		}
	}
	return files
}

func wireguardConf(link string) (string, string, bool) {
	u, err := url.Parse(link)
	if err != nil || u.User == nil || u.Host == "" {
		return "", "", false
	}
	q := u.Query()
	var b strings.Builder
	b.WriteString("[Interface]\n")
	fmt.Fprintf(&b, "PrivateKey = %s\n", u.User.Username())
	if v := q.Get("address"); v != "" {
		fmt.Fprintf(&b, "Address = %s\n", v)
	}
	if v := q.Get("dns"); v != "" {
		fmt.Fprintf(&b, "DNS = %s\n", v)
	}
	if v := q.Get("mtu"); v != "" {
		fmt.Fprintf(&b, "MTU = %s\n", v)
	}
	b.WriteString("\n[Peer]\n")
	fmt.Fprintf(&b, "PublicKey = %s\n", q.Get("publickey"))
	if v := q.Get("presharedkey"); v != "" {
		fmt.Fprintf(&b, "PresharedKey = %s\n", v)
	}
	fmt.Fprintf(&b, "Endpoint = %s\n", u.Host)
	b.WriteString("AllowedIPs = 0.0.0.0/0, ::/0\n")
	if v := q.Get("keepalive"); v != "" {
		fmt.Fprintf(&b, "PersistentKeepalive = %s\n", v)
	}
	return b.String(), u.Fragment, true
}

// ConfigPack is a zip with a folder per user: its subscription link, every share link,
// and the files dedicated apps open (an OpenVPN profile, a WireGuard config).
func (s *ClientService) ConfigPack(inboundSvc *InboundService, host string, emails []string, owner int) ([]byte, error) {
	rows, err := exportRows(emails, owner)
	if err != nil {
		return nil, err
	}
	urls := (&SettingService{}).NewSubURLBuilder()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name, body string) error {
		f, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = f.Write([]byte(body))
		return err
	}
	for _, r := range rows {
		dir := packName(r.Email) + "/"
		if r.SubID != "" {
			subURL, jsonURL, _ := urls.For(r.Id, r.OwnerId, r.SubID)
			text := subURL + "\n"
			if jsonURL != "" {
				text += jsonURL + "\n"
			}
			if err := write(dir+"subscription.txt", text); err != nil {
				return nil, err
			}
		}
		links, err := inboundSvc.GetAllClientLinks(host, r.Email)
		if err != nil {
			continue
		}
		var shareable []string
		for _, l := range links {
			if !strings.HasPrefix(l, openvpn.ShareLinkScheme) {
				shareable = append(shareable, l)
			}
		}
		if len(shareable) > 0 {
			if err := write(dir+"links.txt", strings.Join(shareable, "\n")+"\n"); err != nil {
				return nil, err
			}
		}
		for name, body := range clientAppFiles(links) {
			if err := write(dir+name, body); err != nil {
				return nil, err
			}
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
