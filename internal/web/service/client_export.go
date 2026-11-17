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

// SubscriptionURLs builds a user's subscription addresses the way the panel hands them out:
// the configured URI, else the subscription domain, port and path; the JSON one is "" when off.
func (s *SettingService) SubscriptionURLs(subID string) (string, string) {
	subURI, _ := s.GetSubURI()
	subJSONURI, _ := s.GetSubJsonURI()
	subDomain, _ := s.GetSubDomain()
	subPort, _ := s.GetSubPort()
	subPath, _ := s.GetSubPath()
	subJSONPath, _ := s.GetSubJsonPath()
	jsonEnabled, _ := s.GetSubJsonEnable()
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
		} else if h, err := os.Hostname(); err == nil && h != "" {
			subDomain = h
		} else {
			subDomain = "localhost"
		}
	}
	host := subDomain
	if !(subPort == 443 && tls) && !(subPort == 80 && !tls) {
		host = fmt.Sprintf("%s:%d", subDomain, subPort)
	}
	slashed := func(p string) string {
		return "/" + strings.Trim(p, "/") + "/"
	}
	join := func(uri, path string) string {
		if uri != "" {
			return strings.TrimSuffix(uri, "/") + "/" + subID
		}
		return fmt.Sprintf("%s://%s%s%s", scheme, host, slashed(path), subID)
	}
	jsonURL := ""
	if jsonEnabled {
		jsonURL = join(subJSONURI, subJSONPath)
	}
	return join(subURI, subPath), jsonURL
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
	settings := &SettingService{}
	loc := usageLocation(now)
	var b bytes.Buffer
	b.WriteString("\uFEFF")
	w := csv.NewWriter(&b)
	_ = w.Write([]string{
		"name", "enabled", "group", "owner", "used_bytes", "total_bytes", "expiry", "speed_limit_mbps",
		"ip_limit", "device_limit", "last_online", "last_fetch", "subscription", "comment",
	})
	for _, r := range rows {
		subURL := ""
		if r.SubID != "" {
			subURL, _ = settings.SubscriptionURLs(r.SubID)
		}
		expiry := exportTime(r.ExpiryTime, loc)
		if r.ExpiryTime < 0 {
			expiry = fmt.Sprintf("%d days from first use", -r.ExpiryTime/int64(24*time.Hour/time.Millisecond))
		}
		_ = w.Write([]string{
			r.Email, strconv.FormatBool(r.Enable), r.Group, r.OwnerName,
			strconv.FormatInt(r.Up+r.Down, 10), strconv.FormatInt(r.TotalGB, 10), expiry,
			strconv.Itoa(r.SpeedLimit), strconv.Itoa(r.LimitIP), strconv.Itoa(r.LimitHwid),
			exportTime(r.LastOnline, loc), exportTime(r.LastSubFetch, loc), subURL, r.Comment,
		})
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
	settings := &SettingService{}
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
			subURL, jsonURL := settings.SubscriptionURLs(r.SubID)
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
