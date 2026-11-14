package wgquick

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	// WGDashboard keeps its peers in SQLite unless it was pointed elsewhere.
	_ "github.com/mattn/go-sqlite3"
)

const (
	dashboardUnitPath = "/etc/systemd/system/wg-dashboard.service"
	dashboardService  = "wg-dashboard"
	bytesPerGiB       = 1 << 30
)

var (
	unitWorkingDirectory = regexp.MustCompile(`(?m)^\s*WorkingDirectory\s*=\s*(\S+)\s*$`)
	// Where WGDashboard's installer and its container image put the database.
	dashboardDBCandidates = []string{
		"/root/WGDashboard/src/db/wgdashboard.db",
		"/opt/WGDashboard/src/db/wgdashboard.db",
		"/opt/wgdashboard/src/db/wgdashboard.db",
		"/data/db/wgdashboard.db",
	}
)

// DashboardPeer is what WGDashboard knows about a peer beyond the config file.
type DashboardPeer struct {
	PublicKey    string
	PrivateKey   string
	Name         string
	PresharedKey string
	DNS          string
	AllowedIPs   []string
	Keepalive    int
	MTU          int
	// Up and Down are the peer's whole usage in bytes, as the account sees it.
	Up, Down int64
	// Restricted peers were taken off the interface and exist only in the database.
	Restricted bool
}

func dashboardDBOfUnit(unit string) string {
	match := unitWorkingDirectory.FindStringSubmatch(unit)
	if match == nil {
		return ""
	}
	return filepath.Join(match[1], "db", "wgdashboard.db")
}

// FindDashboardDB returns the database of a WGDashboard installed on this host, or "".
func FindDashboardDB() string {
	candidates := dashboardDBCandidates
	if unit, err := os.ReadFile(dashboardUnitPath); err == nil {
		if path := dashboardDBOfUnit(string(unit)); path != "" {
			candidates = append([]string{path}, candidates...)
		}
	}
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

// DashboardRunning reports whether WGDashboard's service is active; two
// managers of one interface would undo each other's changes.
func DashboardRunning() bool {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", dashboardService).Run() == nil
}

// ReadDashboardPeers reads what WGDashboard stored for an interface's peers,
// the restricted ones last. An interface it does not know has none.
func ReadDashboardPeers(dbPath, iface string) ([]DashboardPeer, error) {
	if err := ValidateInterface(iface); err != nil {
		return nil, err
	}
	fail := func(err error) ([]DashboardPeer, error) {
		return nil, fmt.Errorf("wireguard: cannot read the WGDashboard database %s: %w", dbPath, err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		return fail(err)
	}
	db, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return fail(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	var out []DashboardPeer
	for _, table := range []struct {
		name       string
		restricted bool
	}{{iface, false}, {iface + "_restrict_access", true}} {
		peers, err := readDashboardTable(ctx, db, table.name, table.restricted)
		if err != nil {
			return fail(err)
		}
		out = append(out, peers...)
	}
	return out, nil
}

func readDashboardTable(ctx context.Context, db *sql.DB, table string, restricted bool) ([]DashboardPeer, error) {
	var found int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&found); err != nil || found == 0 {
		return nil, err
	}
	// The name passed ValidateInterface, which leaves no character that ends a quoted identifier.
	rows, err := db.QueryContext(ctx, `SELECT id, private_key, name, DNS, allowed_ip, keepalive, mtu, preshared_key,
		total_receive, total_sent, cumu_receive, cumu_sent FROM "`+table+`" ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DashboardPeer
	for rows.Next() {
		var (
			key, private, name, dns, allowed, psk sql.NullString
			keepalive, mtu                        sql.NullInt64
			received, sent, oldReceived, oldSent  sql.NullFloat64
		)
		if err := rows.Scan(&key, &private, &name, &dns, &allowed, &keepalive, &mtu, &psk, &received, &sent, &oldReceived, &oldSent); err != nil {
			return nil, err
		}
		peer := DashboardPeer{
			PublicKey:    strings.TrimSpace(key.String),
			PrivateKey:   strings.TrimSpace(private.String),
			Name:         strings.TrimSpace(name.String),
			PresharedKey: strings.TrimSpace(psk.String),
			DNS:          strings.TrimSpace(dns.String),
			Keepalive:    int(keepalive.Int64),
			MTU:          int(mtu.Int64),
			// WGDashboard counts in GiB: what the server received is the peer's upload.
			Up:         int64((received.Float64 + oldReceived.Float64) * bytesPerGiB),
			Down:       int64((sent.Float64 + oldSent.Float64) * bytesPerGiB),
			Restricted: restricted,
		}
		for _, entry := range strings.Split(allowed.String, ",") {
			if entry = strings.TrimSpace(entry); entry != "" {
				peer.AllowedIPs = append(peer.AllowedIPs, entry)
			}
		}
		out = append(out, peer)
	}
	return out, rows.Err()
}
