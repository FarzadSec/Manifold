package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/tunnel"
)

const (
	oldServerIP = "203.0.113.10"
	newServerIP = "198.51.100.20"
)

func stubDNS(t *testing.T, answers map[string][]string) {
	t.Helper()
	prev := lookupHost
	lookupHost = func(host string) ([]string, error) { return answers[host], nil }
	t.Cleanup(func() { lookupHost = prev })
}

// seedServer is a panel as users see it before a move: one inbound reached by
// the server's bare IP, one by a domain, a certificate on disk, and two tunnels.
func seedServer(t *testing.T) (cert string) {
	t.Helper()
	initTunnelDB(t)
	cert = filepath.Join(t.TempDir(), "fullchain.pem")
	if err := os.WriteFile(cert, []byte("CERT"), 0o600); err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	stream := `{"security":"tls","tlsSettings":{"certificates":[{"certificateFile":"` + cert + `","keyFile":"/nonexistent/key.pem"}]}}`
	rows := []model.Inbound{
		{Remark: "by-ip", Port: 40001, Enable: true, Protocol: model.VLESS, Tag: "a", StreamSettings: stream},
		{Remark: "by-domain", Port: 40002, Enable: true, Protocol: model.VLESS, Tag: "b", ShareAddrStrategy: "custom", ShareAddr: "vpn.example.com"},
		{Remark: "bound", Port: 40003, Enable: true, Protocol: model.VLESS, Tag: "c", Listen: "192.0.2.77", ShareAddrStrategy: "custom", ShareAddr: "relay.example.com"},
	}
	for i := range rows {
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&model.Host{InboundId: rows[0].Id, Remark: "ip", Address: oldServerIP}).Error; err != nil {
		t.Fatal(err)
	}
	ends := []model.TunnelEnd{
		{PairId: "aaaaaaaaaaaaaaaaaaaaaaaa", Name: "relayed", Kind: tunnel.KindReverse, Role: tunnel.RoleClient, Enable: true, RelayId: 1,
			Settings: `{"transport":"tcpmux","port":8443,"remoteAddr":"relay.example.com"}`},
		{PairId: "bbbbbbbbbbbbbbbbbbbbbbbb", Name: "paired", Kind: tunnel.KindGRE, Role: tunnel.RoleClient, Enable: true,
			Settings: `{"remoteAddr":"198.51.100.7","localAddr":"192.0.2.77","localIp":"10.80.0.2/30","peerIp":"10.80.0.1","ttl":255}`},
	}
	for i := range ends {
		if err := db.Create(&ends[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	return cert
}

// A move breaks a user exactly when the address in their config stops leading
// to the server; the check must say so for each address, before anything changes.
func TestInspectBackupSaysWhatAMoveDoesToEachAddress(t *testing.T) {
	cert := seedServer(t)
	stubDNS(t, map[string][]string{"vpn.example.com": {oldServerIP}, "relay.example.com": {"198.51.100.7"}})
	s := &ServerService{cachedIPv4: oldServerIP}
	archive, name, err := s.FullBackup()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(name, "manifold-backup_") || !strings.HasSuffix(name, ".tar.gz") {
		t.Fatalf("file name %q", name)
	}

	moved := &ServerService{cachedIPv4: newServerIP}
	report, err := moved.InspectBackup(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	if !report.AddressChanged || report.SourceAddress != oldServerIP || report.ThisAddress != newServerIP || report.Inbounds != 3 {
		t.Fatalf("report header = %+v", report)
	}
	status := map[string]string{}
	for _, a := range report.Addresses {
		status[a.Address] = a.Status
	}
	want := map[string]string{oldServerIP: "changes", "vpn.example.com": "dnsUpdate", "relay.example.com": "relay"}
	for addr, st := range want {
		if status[addr] != st {
			t.Errorf("address %s: status %q, want %q (all: %v)", addr, status[addr], st, status)
		}
	}
	actions := map[string]string{}
	for _, tun := range report.Tunnels {
		actions[tun.Name] = tun.Action
	}
	if actions["relayed"] != "resync" || actions["paired"] != "repair" {
		t.Fatalf("tunnel actions = %v", actions)
	}
	if len(report.Files) != 1 || report.Files[0].Path != cert || !report.Files[0].Exists {
		t.Fatalf("files = %+v, want only the certificate that exists", report.Files)
	}
	codes := map[string]bool{}
	for _, i := range report.Issues {
		codes[i.Code] = true
	}
	if !codes["listenMissing"] || !codes["tunnelLocalAddr"] {
		t.Fatalf("issues = %+v, want the old listen and tunnel addresses named", report.Issues)
	}
}

func TestAdaptingTheRestoredDataToThisMachine(t *testing.T) {
	seedServer(t)
	report := &MigrationReport{SourceAddress: oldServerIP, ThisAddress: newServerIP, AddressChanged: true}
	if err := adaptToThisMachine(report, RestoreOptions{RewriteAddress: true}); err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	var bound model.Inbound
	db.Where("tag = ?", "c").First(&bound)
	if bound.Listen != "" {
		t.Fatalf("an inbound kept listening on the old server's address %q", bound.Listen)
	}
	var host model.Host
	db.First(&host)
	if host.Address != newServerIP {
		t.Fatalf("host address = %q, want this server's", host.Address)
	}
	var end model.TunnelEnd
	db.Where("name = ?", "paired").First(&end)
	if strings.Contains(end.Settings, "192.0.2.77") {
		t.Fatalf("a tunnel stayed bound to the old address: %s", end.Settings)
	}
}

func TestAdaptingWithoutRewriteKeepsTheOldIPUsersHave(t *testing.T) {
	seedServer(t)
	report := &MigrationReport{SourceAddress: oldServerIP, ThisAddress: newServerIP, AddressChanged: true}
	if err := adaptToThisMachine(report, RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	var host model.Host
	database.GetDB().First(&host)
	if host.Address != oldServerIP {
		t.Fatalf("host address = %q, want the old IP kept for an admin who moves it", host.Address)
	}
}

func tarGz(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func TestUnpackBackupRefusesWhatItDidNotWrite(t *testing.T) {
	initTunnelDB(t)
	manifest, _ := json.Marshal(BackupManifest{Format: backupFormat, Version: backupVersion})
	damaged, _ := json.Marshal(BackupManifest{Format: backupFormat, Version: backupVersion, Files: []BackupFile{{Path: "/etc/x.pem", SHA256: "00"}}})
	tests := []struct {
		name    string
		archive []byte
		wantErr string
	}{
		{"plain database file", []byte("SQLite format 3"), "not a Manifold full backup"},
		{"path outside the backup", tarGz(t, map[string]string{"manifest.json": string(manifest), "../../etc/cron.d/x": "x"}), "which a Manifold backup never does"},
		{"no manifest", tarGz(t, map[string]string{"database.db": "x"}), "it has no manifest"},
		{"no database", tarGz(t, map[string]string{"manifest.json": string(manifest)}), "has no database"},
		{"file that does not match its checksum", tarGz(t, map[string]string{"manifest.json": string(damaged), "database.db": "x", "files/etc/x.pem": "y"}), "copy of /etc/x.pem is damaged"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := unpackBackup(bytes.NewReader(tt.archive))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestRestoreFileKeepsADifferentFileAside(t *testing.T) {
	dir := t.TempDir()
	from, to := filepath.Join(dir, "from"), filepath.Join(dir, "cert.pem")
	_ = os.WriteFile(from, []byte("NEW"), 0o600)
	_ = os.WriteFile(to, []byte("OLD"), 0o600)
	if err := restoreFile(from, BackupFile{Path: to, Mode: 0o600}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(to)
	aside, _ := os.ReadFile(to + ".before-restore")
	if string(got) != "NEW" || string(aside) != "OLD" {
		t.Fatalf("file %q, aside %q", got, aside)
	}
}
