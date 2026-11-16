package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/tunnel"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	backupFormat       = "manifold-backup"
	backupVersion      = 1
	backupDatabaseName = "database.db"
	backupManifestName = "manifest.json"
	backupFilesDir     = "files/"
	maxBackupFileBytes = 1 << 20
	maxBackupDBBytes   = 2 << 30
)

// The panel's own address stays this machine's on a restore unless the admin
// takes the old one too: an unreachable port or path would lock them out.
var panelAccessSettingKeys = []string{"webListen", "webDomain", "webPort", "webCertFile", "webKeyFile", "webBasePath"}

// BackupManifest describes a full backup: where it came from and what it holds.
type BackupManifest struct {
	Format         string       `json:"format" example:"manifold-backup"`
	Version        int          `json:"version" example:"1"`
	ProductVersion string       `json:"productVersion" example:"0.5.0"`
	Edition        string       `json:"edition" example:"full"`
	CreatedAt      int64        `json:"createdAt" example:"1763300000000"`
	SourceIPv4     string       `json:"sourceIPv4" example:"203.0.113.10"`
	SourceIPv6     string       `json:"sourceIPv6" example:""`
	Files          []BackupFile `json:"files"`
}

// BackupFile is one certificate or key the panel's settings point at.
type BackupFile struct {
	Path   string `json:"path" example:"/root/cert/fullchain.pem"`
	Mode   uint32 `json:"mode" example:"384"`
	SHA256 string `json:"sha256" example:"9f86d0..."`
	Exists bool   `json:"exists" example:"false"`
}

// MigrationIssue is one finding of a restore check, named by code for the page to word.
type MigrationIssue struct {
	Level  string            `json:"level" example:"warn"`
	Code   string            `json:"code" example:"addressChanges"`
	Params map[string]string `json:"params"`
}

// MigrationAddress is an address users' configs carry, and what a move does to it.
type MigrationAddress struct {
	Address string   `json:"address" example:"vpn.example.com"`
	Status  string   `json:"status" example:"dnsUpdate"`
	Points  []string `json:"points,omitempty" example:"[\"203.0.113.10\"]"`
	UsedBy  []string `json:"usedBy" example:"[\"VLESS Reality\"]"`
}

// MigrationTunnel is a tunnel of the backup and what happens to its other end.
type MigrationTunnel struct {
	Name   string `json:"name" example:"uk-relay"`
	Kind   string `json:"kind" example:"gre"`
	Role   string `json:"role" example:"client"`
	Action string `json:"action" example:"resync"`
}

// MigrationReport is what a restore would do, or did, on this server.
type MigrationReport struct {
	SourceVersion  string             `json:"sourceVersion" example:"0.5.0"`
	SourceAddress  string             `json:"sourceAddress" example:"203.0.113.10"`
	ThisAddress    string             `json:"thisAddress" example:"198.51.100.20"`
	AddressChanged bool               `json:"addressChanged" example:"true"`
	CreatedAt      int64              `json:"createdAt" example:"1763300000000"`
	Inbounds       int                `json:"inbounds" example:"12"`
	Clients        int                `json:"clients" example:"340"`
	Addresses      []MigrationAddress `json:"addresses"`
	Tunnels        []MigrationTunnel  `json:"tunnels"`
	Files          []BackupFile       `json:"files"`
	Issues         []MigrationIssue   `json:"issues"`
	Restored       bool               `json:"restored" example:"false"`
}

// RestoreOptions are the choices a restore leaves to the admin.
type RestoreOptions struct {
	// KeepPanelAccess keeps this machine's panel port, path and certificate.
	KeepPanelAccess bool
	// RewriteAddress replaces the old server's IP with this one's wherever the panel stores it.
	RewriteAddress bool
}

// certificatePathKeys are the JSON keys under which settings name a file to back up.
var certificatePathKeys = []string{"certificateFile", "keyFile", "certFile", "certificate", "key", "cert", "tls_cert", "tls_key", "certPath", "keyPath"}

// collectPaths finds every absolute file path a JSON document names under a certificate key.
func collectPaths(raw string, into map[string]bool) {
	var doc any
	if json.Unmarshal([]byte(raw), &doc) != nil {
		return
	}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, child := range x {
				if s, ok := child.(string); ok && slices.Contains(certificatePathKeys, k) && filepath.IsAbs(s) {
					into[filepath.Clean(s)] = true
					continue
				}
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(doc)
}

// referencedFiles lists the certificates and keys this panel's settings and inbounds name.
func referencedFiles() ([]string, error) {
	db := database.GetDB()
	paths := map[string]bool{}
	var settings []model.Setting
	if err := db.Where("key IN ?", []string{"webCertFile", "webKeyFile", "subCertFile", "subKeyFile", "xrayTemplateConfig"}).Find(&settings).Error; err != nil {
		return nil, err
	}
	for _, row := range settings {
		if row.Key == "xrayTemplateConfig" {
			collectPaths(row.Value, paths)
		} else if v := strings.TrimSpace(row.Value); filepath.IsAbs(v) {
			paths[filepath.Clean(v)] = true
		}
	}
	var inbounds []model.Inbound
	if err := db.Select("stream_settings", "settings").Find(&inbounds).Error; err != nil {
		return nil, err
	}
	for _, ib := range inbounds {
		collectPaths(ib.StreamSettings, paths)
		collectPaths(ib.Settings, paths)
	}
	out := make([]string, 0, len(paths))
	for p := range paths {
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() && info.Size() <= maxBackupFileBytes {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}

func backupFilename(now time.Time) string {
	return "manifold-backup_" + now.Format("2006-01-02_150405") + ".tar.gz"
}

func addTarEntry(tw *tar.Writer, name string, mode int64, data []byte) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data)), ModTime: time.Now(), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

// FullBackup is everything a move to another server needs: the database, the
// certificates its settings point at, and where it was made, in one archive.
func (s *ServerService) FullBackup() ([]byte, string, error) {
	var dbBytes []byte
	var err error
	if database.IsPostgres() {
		dbBytes, _, err = s.GetMigration()
	} else {
		dbBytes, err = s.GetDb()
	}
	if err != nil {
		return nil, "", err
	}
	files, err := referencedFiles()
	if err != nil {
		return nil, "", err
	}
	ipv4, ipv6 := s.publicIPs()
	edition := "full"
	if config.IsTunnelEdition() {
		edition = "tunnel"
	}
	manifest := BackupManifest{
		Format: backupFormat, Version: backupVersion, ProductVersion: config.GetProductVersion(), Edition: edition,
		CreatedAt: time.Now().UnixMilli(), SourceIPv4: ipv4, SourceIPv6: ipv6,
	}
	contents := map[string][]byte{}
	for _, p := range files {
		data, err := os.ReadFile(p)
		if err != nil {
			logger.Warning("backup: skipping ", p, ": ", err)
			continue
		}
		info, _ := os.Stat(p)
		sum := sha256.Sum256(data)
		manifest.Files = append(manifest.Files, BackupFile{Path: p, Mode: uint32(info.Mode().Perm()), SHA256: hex.EncodeToString(sum[:])})
		contents[p] = data
	}
	rawManifest, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, "", err
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := addTarEntry(tw, backupManifestName, 0o600, rawManifest); err != nil {
		return nil, "", err
	}
	if err := addTarEntry(tw, backupDatabaseName, 0o600, dbBytes); err != nil {
		return nil, "", err
	}
	for _, f := range manifest.Files {
		if err := addTarEntry(tw, backupFilesDir+strings.TrimPrefix(f.Path, "/"), int64(f.Mode), contents[f.Path]); err != nil {
			return nil, "", err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, "", err
	}
	if err := gz.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), backupFilename(time.Now()), nil
}

// unpackedBackup is a full backup read into a directory of its own.
type unpackedBackup struct {
	dir      string
	manifest BackupManifest
}

func (u *unpackedBackup) close() { _ = os.RemoveAll(u.dir) }

func (u *unpackedBackup) dbPath() string { return filepath.Join(u.dir, backupDatabaseName) }

func (u *unpackedBackup) filePath(abs string) string {
	return filepath.Join(u.dir, "files", filepath.FromSlash(strings.TrimPrefix(abs, "/")))
}

// unpackBackup reads an archive made by FullBackup, refusing any entry it did not write.
func unpackBackup(r io.Reader) (*unpackedBackup, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, common.NewError("this is not a Manifold full backup (.tar.gz)")
	}
	defer gz.Close()
	dir, err := os.MkdirTemp(filepath.Dir(config.GetDBPath()), ".manifold-restore-")
	if err != nil {
		return nil, err
	}
	u := &unpackedBackup{dir: dir}
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			u.close()
			return nil, common.NewErrorf("the backup is damaged: %v", err)
		}
		name := path.Clean(hdr.Name)
		limit := int64(maxBackupFileBytes)
		switch {
		case name == backupDatabaseName:
			limit = maxBackupDBBytes
		case name == backupManifestName:
		case strings.HasPrefix(name, backupFilesDir) && !strings.Contains(name, ".."):
		default:
			u.close()
			return nil, common.NewErrorf("the backup holds %q, which a Manifold backup never does", hdr.Name)
		}
		if hdr.Typeflag != tar.TypeReg || hdr.Size > limit || seen[name] {
			u.close()
			return nil, common.NewErrorf("the backup entry %q is not a file this panel restores", hdr.Name)
		}
		seen[name] = true
		target := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			u.close()
			return nil, err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			u.close()
			return nil, err
		}
		_, err = io.Copy(out, io.LimitReader(tr, limit))
		_ = out.Close()
		if err != nil {
			u.close()
			return nil, err
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, backupManifestName))
	if err != nil || json.Unmarshal(raw, &u.manifest) != nil || u.manifest.Format != backupFormat {
		u.close()
		return nil, common.NewError("this is not a Manifold full backup: it has no manifest")
	}
	if u.manifest.Version > backupVersion {
		u.close()
		return nil, common.NewErrorf("this backup was made by a newer Manifold (%s); update this panel first", u.manifest.ProductVersion)
	}
	if !seen[backupDatabaseName] {
		u.close()
		return nil, common.NewError("the backup has no database")
	}
	for i, f := range u.manifest.Files {
		data, err := os.ReadFile(u.filePath(f.Path))
		sum := sha256.Sum256(data)
		if err != nil || hex.EncodeToString(sum[:]) != f.SHA256 || !filepath.IsAbs(f.Path) {
			u.close()
			return nil, common.NewErrorf("the backup's copy of %s is damaged", f.Path)
		}
		_, statErr := os.Stat(f.Path)
		u.manifest.Files[i].Exists = statErr == nil
	}
	return u, nil
}

// backupContents is what the restore check reads out of the backup's database.
type backupContents struct {
	inbounds []model.Inbound
	hosts    []model.Host
	settings map[string]string
	tunnels  []model.TunnelEnd
	clients  int64
}

func readBackupDB(path string) (*backupContents, error) {
	db, err := gorm.Open(sqlite.Open("file:"+path+"?mode=ro"), &gorm.Config{})
	if err != nil {
		return nil, common.NewErrorf("the backup's database cannot be read: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		defer sqlDB.Close()
	}
	c := &backupContents{settings: map[string]string{}}
	if err := db.Find(&c.inbounds).Error; err != nil {
		return nil, common.NewErrorf("the backup's database has no inbounds table: %v", err)
	}
	if db.Migrator().HasTable(&model.Host{}) {
		_ = db.Find(&c.hosts).Error
	}
	var settings []model.Setting
	_ = db.Find(&settings).Error
	for _, row := range settings {
		c.settings[row.Key] = row.Value
	}
	if db.Migrator().HasTable(&model.TunnelEnd{}) {
		_ = db.Find(&c.tunnels).Error
	}
	if db.Migrator().HasTable(&model.ClientRecord{}) {
		_ = db.Model(&model.ClientRecord{}).Count(&c.clients).Error
	}
	return c, nil
}

// localAddresses are the addresses configured on this machine's interfaces.
func localAddresses() map[netip.Addr]bool {
	out := map[netip.Addr]bool{}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		if p, err := netip.ParsePrefix(a.String()); err == nil {
			out[p.Addr().Unmap()] = true
		}
	}
	return out
}

func issue(level, code string, params ...string) MigrationIssue {
	m := map[string]string{}
	for i := 0; i+1 < len(params); i += 2 {
		m[params[i]] = params[i+1]
	}
	return MigrationIssue{Level: level, Code: code, Params: m}
}

// lookupHost resolves a name for the restore check; tests substitute it.
var lookupHost = func(host string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return net.DefaultResolver.LookupHost(ctx, host)
}

// addressesOf lists the addresses users' configs carry, with the inbounds that hand them out.
func addressesOf(c *backupContents, sourceIP string) map[string][]string {
	used := map[string][]string{}
	add := func(addr, inbound string) {
		addr = strings.Trim(strings.TrimSpace(addr), "[]")
		if addr == "" || slices.Contains(used[addr], inbound) {
			return
		}
		used[addr] = append(used[addr], inbound)
	}
	fallback := c.settings["subDomain"]
	if fallback == "" {
		fallback = c.settings["webDomain"]
	}
	if fallback == "" {
		fallback = sourceIP
	}
	hostsOf := map[int][]string{}
	for _, h := range c.hosts {
		if !h.IsDisabled {
			hostsOf[h.InboundId] = append(hostsOf[h.InboundId], h.Address)
		}
	}
	for _, ib := range c.inbounds {
		if !ib.Enable || ib.Protocol == model.Tunnel {
			continue
		}
		name := ib.Remark
		if name == "" {
			name = ib.Tag
		}
		explicit := false
		for _, a := range hostsOf[ib.Id] {
			add(a, name)
			explicit = true
		}
		var stream map[string]any
		_ = json.Unmarshal([]byte(ib.StreamSettings), &stream)
		if proxies, ok := stream["externalProxy"].([]any); ok {
			for _, raw := range proxies {
				if ep, ok := raw.(map[string]any); ok {
					if dest, ok := ep["dest"].(string); ok {
						add(dest, name)
						explicit = true
					}
				}
			}
		}
		if explicit {
			continue
		}
		switch {
		case ib.ShareAddrStrategy == "custom" && strings.TrimSpace(ib.ShareAddr) != "":
			add(ib.ShareAddr, name)
		case ib.Listen != "" && isAddressLike(ib.Listen):
			add(ib.Listen, name)
		default:
			add(fallback, name)
		}
	}
	return used
}

func isAddressLike(listen string) bool {
	addr, err := netip.ParseAddr(listen)
	return err != nil || (!addr.IsUnspecified() && !addr.IsLoopback())
}

// classify says what moving to thisIP does to users who connect to addr.
func classify(addr, sourceIP, thisIP string, relays map[string]bool) (string, []string) {
	if relays[addr] {
		return "relay", nil
	}
	if ip, err := netip.ParseAddr(addr); err == nil {
		switch {
		case thisIP != "" && ip.String() == thisIP:
			return "same", nil
		case ip.String() == sourceIP && sourceIP != thisIP:
			return "changes", nil
		default:
			return "elsewhere", nil
		}
	}
	ips, err := lookupHost(addr)
	switch {
	case err != nil || len(ips) == 0:
		return "unresolved", nil
	case slices.Contains(ips, thisIP):
		return "dnsHere", ips
	case slices.Contains(ips, sourceIP):
		return "dnsUpdate", ips
	default:
		return "dnsElsewhere", ips
	}
}

// listening reports whether a port of proto cannot be bound here now.
func portTaken(proto string, port int) bool {
	addr := net.JoinHostPort("", strconv.Itoa(port))
	var lc net.ListenConfig
	if proto == "udp" {
		c, err := lc.ListenPacket(context.Background(), "udp", addr)
		if err != nil {
			return true
		}
		_ = c.Close()
		return false
	}
	l, err := lc.Listen(context.Background(), "tcp", addr)
	if err != nil {
		return true
	}
	_ = l.Close()
	return false
}

// engineCommands are the programs each native protocol needs on this machine.
var engineCommands = map[model.Protocol][]string{
	model.OpenVPN:   {"openvpn"},
	model.WireGuard: {"wg-quick", "wg"},
	model.L2TP:      {"xl2tpd", "pppd"},
}

var commandExists = func(name string) bool {
	for _, dir := range []string{"/usr/sbin", "/usr/bin", "/sbin", "/bin", "/usr/local/sbin", "/usr/local/bin"} {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

// inspect checks a backup against this machine without changing anything.
func (s *ServerService) inspect(u *unpackedBackup) (*MigrationReport, *backupContents, error) {
	c, err := readBackupDB(u.dbPath())
	if err != nil {
		return nil, nil, err
	}
	thisIPv4, _ := s.publicIPs()
	m := u.manifest
	r := &MigrationReport{
		SourceVersion: m.ProductVersion, SourceAddress: m.SourceIPv4, ThisAddress: thisIPv4,
		AddressChanged: m.SourceIPv4 != "" && thisIPv4 != "" && m.SourceIPv4 != thisIPv4,
		CreatedAt:      m.CreatedAt, Inbounds: len(c.inbounds), Clients: int(c.clients), Files: m.Files,
		Addresses: []MigrationAddress{}, Tunnels: []MigrationTunnel{}, Issues: []MigrationIssue{},
	}
	if newerVersion(m.ProductVersion, config.GetProductVersion()) {
		r.Issues = append(r.Issues, issue("error", "newerVersion", "version", m.ProductVersion, "current", config.GetProductVersion()))
	}
	thisEdition := "full"
	if config.IsTunnelEdition() {
		thisEdition = "tunnel"
	}
	if m.Edition != "" && m.Edition != thisEdition {
		r.Issues = append(r.Issues, issue("warn", "edition", "backup", m.Edition, "current", thisEdition))
	}

	relays := map[string]bool{}
	for _, row := range c.tunnels {
		t, err := tunnelFromRow(&row)
		if err != nil {
			continue
		}
		if t.Role == tunnel.RoleClient && t.Settings.RemoteAddr != "" {
			relays[t.Settings.RemoteAddr] = true
		}
		mt := MigrationTunnel{Name: t.Name, Kind: t.Kind, Role: t.Role, Action: "none"}
		switch {
		case row.RelayId != 0:
			mt.Action = "resync"
		case !r.AddressChanged:
		case tunnel.IsKernel(t.Kind), t.Kind == tunnel.KindL3 && t.Settings.Dialer != t.Role,
			t.Kind == tunnel.KindReverse && t.Role == tunnel.RoleServer:
			mt.Action = "repair"
		}
		r.Tunnels = append(r.Tunnels, mt)
		if t.Settings.LocalAddr != "" {
			if a, err := netip.ParseAddr(t.Settings.LocalAddr); err == nil && !localAddresses()[a] {
				r.Issues = append(r.Issues, issue("info", "tunnelLocalAddr", "tunnel", t.Name, "address", t.Settings.LocalAddr))
			}
		}
		if (t.Kind == tunnel.KindReverse || t.Kind == tunnel.KindL3) && !fileExists(tunnel.BackPackPath()) {
			r.Issues = append(r.Issues, issue("error", "backpackMissing", "tunnel", t.Name))
		}
	}

	used := addressesOf(c, m.SourceIPv4)
	keys := make([]string, 0, len(used))
	for a := range used {
		keys = append(keys, a)
	}
	sort.Strings(keys)
	for _, a := range keys {
		status, points := classify(a, m.SourceIPv4, thisIPv4, relays)
		r.Addresses = append(r.Addresses, MigrationAddress{Address: a, Status: status, Points: points, UsedBy: used[a]})
	}

	var current []model.Inbound
	_ = database.GetDB().Select("port").Find(&current).Error
	ownPorts := map[int]bool{}
	for _, ib := range current {
		ownPorts[ib.Port] = true
	}
	local := localAddresses()
	missing := map[string][]string{}
	for _, ib := range c.inbounds {
		if !ib.Enable {
			continue
		}
		if a, err := netip.ParseAddr(ib.Listen); err == nil && !a.IsUnspecified() && !a.IsLoopback() && !local[a.Unmap()] {
			r.Issues = append(r.Issues, issue("info", "listenMissing", "inbound", ib.Remark, "address", ib.Listen))
		}
		if !ownPorts[ib.Port] && ib.Port > 0 {
			proto := "tcp"
			if ib.Protocol == model.WireGuard || ib.Protocol == model.L2TP || ib.Protocol == model.Hysteria || ib.Protocol == model.TUIC {
				proto = "udp"
			}
			if portTaken(proto, ib.Port) {
				r.Issues = append(r.Issues, issue("error", "portBusy", "port", strconv.Itoa(ib.Port), "inbound", ib.Remark))
			}
		}
		for _, cmd := range engineCommands[ib.Protocol] {
			if !commandExists(cmd) && !slices.Contains(missing[cmd], ib.Remark) {
				missing[cmd] = append(missing[cmd], ib.Remark)
			}
		}
	}
	for cmd, inbounds := range missing {
		r.Issues = append(r.Issues, issue("error", "engineMissing", "command", cmd, "inbounds", strings.Join(inbounds, ", ")))
	}
	sort.SliceStable(r.Issues, func(i, j int) bool { return levelRank(r.Issues[i].Level) < levelRank(r.Issues[j].Level) })
	return r, c, nil
}

func levelRank(level string) int {
	switch level {
	case "error":
		return 0
	case "warn":
		return 1
	default:
		return 2
	}
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// newerVersion reports whether version a is later than b, both as x.y.z.
func newerVersion(a, b string) bool {
	parse := func(v string) [3]int {
		var out [3]int
		for i, part := range strings.SplitN(strings.TrimPrefix(v, "v"), ".", 3) {
			n, _ := strconv.Atoi(strings.TrimFunc(part, func(r rune) bool { return r < '0' || r > '9' }))
			out[i] = n
		}
		return out
	}
	pa, pb := parse(a), parse(b)
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

// InspectBackup checks a full backup against this machine and changes nothing.
func (s *ServerService) InspectBackup(r io.Reader) (*MigrationReport, error) {
	u, err := unpackBackup(r)
	if err != nil {
		return nil, err
	}
	defer u.close()
	report, _, err := s.inspect(u)
	return report, err
}

// RestoreBackup moves a full backup onto this machine: its certificates, its
// database, and the fixes this machine needs, then resends relayed tunnels.
func (s *ServerService) RestoreBackup(r io.Reader, opts RestoreOptions) (*MigrationReport, error) {
	u, err := unpackBackup(r)
	if err != nil {
		return nil, err
	}
	defer u.close()
	report, _, err := s.inspect(u)
	if err != nil {
		return nil, err
	}
	for _, i := range report.Issues {
		if i.Code == "newerVersion" {
			return report, common.NewErrorf("this backup was made by Manifold %s, newer than this panel; update first", report.SourceVersion)
		}
	}
	for _, f := range u.manifest.Files {
		if err := restoreFile(u.filePath(f.Path), f); err != nil {
			return report, common.NewErrorf("restoring %s: %v", f.Path, err)
		}
	}
	db, err := os.Open(u.dbPath())
	if err != nil {
		return report, err
	}
	defer db.Close()
	var keep []string
	if opts.KeepPanelAccess {
		keep = panelAccessSettingKeys
	}
	if err := s.importDBKeeping(db, keep); err != nil {
		return report, err
	}
	if err := adaptToThisMachine(report, opts); err != nil {
		return report, err
	}
	report.Restored = true
	go func() {
		ts := TunnelService{}
		ts.Reconcile()
		for _, err := range ts.Resync() {
			logger.Warning("restore: resending a tunnel to its relay failed: ", err)
		}
	}()
	return report, nil
}

// restoreFile writes one backed-up file, keeping a different file already there aside.
func restoreFile(from string, f BackupFile) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	if existing, err := os.ReadFile(f.Path); err == nil {
		if bytes.Equal(existing, data) {
			return nil
		}
		if err := os.WriteFile(f.Path+".before-restore", existing, 0o600); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(f.Path, data, os.FileMode(f.Mode&0o777)|0o400)
}

// adaptToThisMachine makes the restored data start here: inbounds and tunnels
// bound to the old server's addresses listen on this one's, and optionally the
// old IP users were given becomes this one wherever the panel stores it.
func adaptToThisMachine(report *MigrationReport, opts RestoreOptions) error {
	db := database.GetDB()
	local := localAddresses()
	var inbounds []model.Inbound
	if err := db.Find(&inbounds).Error; err != nil {
		return err
	}
	rewrite := opts.RewriteAddress && report.AddressChanged
	for i := range inbounds {
		ib := &inbounds[i]
		changed := false
		if a, err := netip.ParseAddr(ib.Listen); err == nil && !a.IsUnspecified() && !a.IsLoopback() && !local[a.Unmap()] {
			ib.Listen, changed = "", true
		}
		if rewrite {
			if ib.ShareAddr == report.SourceAddress {
				ib.ShareAddr, changed = report.ThisAddress, true
			}
			if s := replaceDest(ib.StreamSettings, report.SourceAddress, report.ThisAddress); s != ib.StreamSettings {
				ib.StreamSettings, changed = s, true
			}
		}
		if changed {
			if err := db.Model(ib).Select("listen", "share_addr", "stream_settings").Updates(ib).Error; err != nil {
				return err
			}
		}
	}
	if rewrite {
		if err := db.Model(&model.Host{}).Where("address = ?", report.SourceAddress).Update("address", report.ThisAddress).Error; err != nil {
			return err
		}
		for _, key := range []string{"subURI", "subJsonURI", "subDomain", "webDomain"} {
			var row model.Setting
			if db.Where("key = ?", key).First(&row).Error == nil && strings.Contains(row.Value, report.SourceAddress) {
				row.Value = strings.ReplaceAll(row.Value, report.SourceAddress, report.ThisAddress)
				if err := db.Save(&row).Error; err != nil {
					return err
				}
			}
		}
	}
	var ends []model.TunnelEnd
	if err := db.Find(&ends).Error; err != nil {
		return err
	}
	for i := range ends {
		t, err := tunnelFromRow(&ends[i])
		if err != nil {
			continue
		}
		dirty := false
		if a, err := netip.ParseAddr(t.Settings.LocalAddr); err == nil && !local[a.Unmap()] {
			t.Settings.LocalAddr, dirty = "", true
		}
		if t.Settings.PublicAddr == report.SourceAddress && report.AddressChanged {
			t.Settings.PublicAddr, dirty = "", true
		}
		if dirty {
			raw, _ := json.Marshal(t.Settings)
			if err := db.Model(&ends[i]).Update("settings", string(raw)).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// replaceDest rewrites the old IP in the external proxies of a stream setting.
func replaceDest(stream, oldIP, newIP string) string {
	var doc map[string]any
	if oldIP == "" || newIP == "" || json.Unmarshal([]byte(stream), &doc) != nil {
		return stream
	}
	proxies, ok := doc["externalProxy"].([]any)
	if !ok {
		return stream
	}
	changed := false
	for _, raw := range proxies {
		if ep, ok := raw.(map[string]any); ok && ep["dest"] == oldIP {
			ep["dest"], changed = newIP, true
		}
	}
	if !changed {
		return stream
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return stream
	}
	return string(out)
}
