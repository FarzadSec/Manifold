package service

import (
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/metrics"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

// UserCounts is how many users are in each state; a user can be expired and limited at once.
type UserCounts struct {
	Total    int `json:"total" example:"120"`
	Active   int `json:"active" example:"96"`
	Online   int `json:"online" example:"41"`
	Disabled int `json:"disabled" example:"9"`
	Expired  int `json:"expired" example:"11"`
	Expiring int `json:"expiring" example:"4"`
	Limited  int `json:"limited" example:"6"`
}

// PanelSummary is the business side of the dashboard: users, today's traffic, nodes,
// webhooks and backups.
type PanelSummary struct {
	Users           UserCounts `json:"users"`
	TrafficToday    int64      `json:"trafficToday" example:"53687091200"`
	NodesTotal      int        `json:"nodesTotal" example:"3"`
	NodesOnline     int        `json:"nodesOnline" example:"3"`
	WebhooksPending int        `json:"webhooksPending" example:"0"`
	WebhooksFailed  int        `json:"webhooksFailed" example:"0"`
	BackupsFailing  int        `json:"backupsFailing" example:"0"`
	LastBackup      int64      `json:"lastBackup" example:"1763340000000"`
}

func countUsers(rows []userRow, online map[string]bool, now time.Time) UserCounts {
	var c UserCounts
	for _, r := range rows {
		st := stateOf(r, now)
		c.Total++
		switch {
		case !st.enabled:
			c.Disabled++
		case !st.expired && !st.limited:
			c.Active++
		}
		if st.expired {
			c.Expired++
		}
		if st.expiring {
			c.Expiring++
		}
		if st.limited {
			c.Limited++
		}
		if online[r.Email] {
			c.Online++
		}
	}
	return c
}

func onlineSet() map[string]bool {
	out := map[string]bool{}
	for _, e := range (&InboundService{}).GetOnlineClients() {
		out[e] = true
	}
	return out
}

// Summary counts what the dashboard tiles show.
func Summary(now time.Time) (PanelSummary, error) {
	db := database.GetDB()
	var rows []userRow
	if err := userRows().Scan(&rows).Error; err != nil {
		return PanelSummary{}, err
	}
	s := PanelSummary{Users: countUsers(rows, onlineSet(), now)}
	day := dayStart(now, usageLocation(db, now)).Unix()
	if err := db.Model(&model.ClientUsage{}).Where("daily = ? AND start = ?", true, day).
		Select("COALESCE(SUM(up + down), 0)").Scan(&s.TrafficToday).Error; err != nil {
		return s, err
	}
	var nodes []model.Node
	if err := db.Select("id, enable, status").Find(&nodes).Error; err != nil {
		return s, err
	}
	for _, n := range nodes {
		if n.Enable {
			s.NodesTotal++
			if n.Status == "online" {
				s.NodesOnline++
			}
		}
	}
	var pending, failed int64
	db.Model(&model.WebhookDelivery{}).Where("status = ?", "pending").Count(&pending)
	db.Model(&model.WebhookDelivery{}).Where("status = ? AND created_at >= ?", "failed", now.Add(-24*time.Hour).UnixMilli()).Count(&failed)
	s.WebhooksPending, s.WebhooksFailed = int(pending), int(failed)
	var targets []model.BackupTarget
	db.Where("enable = ?", true).Find(&targets)
	for _, t := range targets {
		if t.LastError != "" {
			s.BackupsFailing++
		} else if t.LastRun > s.LastBackup {
			s.LastBackup = t.LastRun
		}
	}
	return s, nil
}

// MetricsOptions picks the high-cardinality parts of a scrape.
type MetricsOptions struct {
	PerUser bool
}

// Metrics is everything a Prometheus scrape gets: this server (from st, when there is
// one), Xray, users, inbounds, nodes, webhooks and backups.
func Metrics(st *Status, opt MetricsOptions, now time.Time) ([]*metrics.Family, error) {
	db := database.GetDB()
	sum, err := Summary(now)
	if err != nil {
		return nil, err
	}
	var fams []*metrics.Family
	add := func(f *metrics.Family) { fams = append(fams, f) }

	if st != nil {
		add(metrics.Gauge("manifold_build_info", "The panel and Xray versions; always 1.").
			Add(1, "version", st.PanelVersion, "xray", st.Xray.Version))
		running := 0.0
		if st.Xray.State == Running {
			running = 1
		}
		add(metrics.Gauge("manifold_xray_running", "1 while Xray runs.").Add(running))
		add(metrics.Gauge("manifold_cpu_percent", "CPU use of this server.").Add(st.Cpu))
		add(metrics.Gauge("manifold_memory_bytes", "Memory of this server.").
			Add(float64(st.Mem.Current), "kind", "used").Add(float64(st.Mem.Total), "kind", "total"))
		add(metrics.Gauge("manifold_swap_bytes", "Swap of this server.").
			Add(float64(st.Swap.Current), "kind", "used").Add(float64(st.Swap.Total), "kind", "total"))
		add(metrics.Gauge("manifold_disk_bytes", "Disk of this server.").
			Add(float64(st.Disk.Current), "kind", "used").Add(float64(st.Disk.Total), "kind", "total"))
		load := metrics.Gauge("manifold_load", "Load average of this server.")
		for i, period := range []string{"1m", "5m", "15m"} {
			if i < len(st.Loads) {
				load.Add(st.Loads[i], "period", period)
			}
		}
		add(load)
		add(metrics.Gauge("manifold_uptime_seconds", "Uptime of this server.").Add(float64(st.Uptime)))
		add(metrics.Gauge("manifold_connections", "Open connections on this server.").
			Add(float64(st.TcpCount), "proto", "tcp").Add(float64(st.UdpCount), "proto", "udp"))
		add(metrics.Counter("manifold_network_bytes_total", "Bytes through this server's network interfaces.").
			Add(float64(st.NetTraffic.Sent), "direction", "sent").Add(float64(st.NetTraffic.Recv), "direction", "received"))
	}

	u := sum.Users
	add(metrics.Gauge("manifold_users", "Users by state; expired and limited overlap.").
		Add(float64(u.Total), "state", "total").Add(float64(u.Active), "state", "active").
		Add(float64(u.Disabled), "state", "disabled").Add(float64(u.Expired), "state", "expired").
		Add(float64(u.Expiring), "state", "expiring").Add(float64(u.Limited), "state", "limited"))
	add(metrics.Gauge("manifold_users_online", "Users connected now.").Add(float64(u.Online)))
	add(metrics.Gauge("manifold_traffic_today_bytes", "Users' traffic since midnight in the panel's time zone.").
		Add(float64(sum.TrafficToday)))

	var inbounds []model.Inbound
	if err := db.Select("id, tag, remark, protocol, node_id, enable, up, down").Find(&inbounds).Error; err != nil {
		return nil, err
	}
	nodeNames := map[int]string{0: ""}
	var nodes []model.Node
	if err := db.Find(&nodes).Error; err != nil {
		return nil, err
	}
	for _, n := range nodes {
		nodeNames[n.Id] = n.Name
	}
	traffic := metrics.Counter("manifold_inbound_traffic_bytes_total", "Traffic through an inbound since its last reset.")
	enabled := metrics.Gauge("manifold_inbound_enabled", "1 while an inbound is enabled.")
	for _, ib := range inbounds {
		node := ""
		if ib.NodeID != nil {
			node = nodeNames[*ib.NodeID]
		}
		labels := []string{"tag", ib.Tag, "protocol", string(ib.Protocol), "node", node}
		traffic.Add(float64(ib.Up), append(labels, "direction", "up")...)
		traffic.Add(float64(ib.Down), append(labels, "direction", "down")...)
		enabled.Add(boolValue(ib.Enable), labels...)
	}
	add(traffic)
	add(enabled)

	up := metrics.Gauge("manifold_node_up", "1 while a node answers its heartbeat.")
	latency := metrics.Gauge("manifold_node_latency_ms", "A node's last heartbeat round trip.")
	cpu := metrics.Gauge("manifold_node_cpu_percent", "A node's CPU use.")
	mem := metrics.Gauge("manifold_node_memory_percent", "A node's memory use.")
	for _, n := range nodes {
		if !n.Enable {
			continue
		}
		up.Add(boolValue(n.Status == "online"), "node", n.Name)
		latency.Add(float64(n.LatencyMs), "node", n.Name)
		cpu.Add(n.CpuPct, "node", n.Name)
		mem.Add(n.MemPct, "node", n.Name)
	}
	add(up)
	add(latency)
	add(cpu)
	add(mem)

	add(metrics.Gauge("manifold_webhook_deliveries_pending", "Webhook deliveries waiting to be sent or retried.").
		Add(float64(sum.WebhooksPending)))
	add(metrics.Gauge("manifold_webhook_deliveries_failed", "Webhook deliveries given up in the last 24 hours.").
		Add(float64(sum.WebhooksFailed)))

	var targets []model.BackupTarget
	if err := db.Where("enable = ?", true).Find(&targets).Error; err != nil {
		return nil, err
	}
	lastRun := metrics.Gauge("manifold_backup_last_run_timestamp_seconds", "When a backup target last ran.")
	lastOK := metrics.Gauge("manifold_backup_last_ok", "1 when a backup target's last run succeeded.")
	size := metrics.Gauge("manifold_backup_last_size_bytes", "The size of a backup target's last backup.")
	for _, t := range targets {
		if t.LastRun == 0 {
			continue
		}
		lastRun.Add(float64(t.LastRun)/1000, "target", t.Name)
		lastOK.Add(boolValue(t.LastError == ""), "target", t.Name)
		size.Add(float64(t.LastSize), "target", t.Name)
	}
	add(lastRun)
	add(lastOK)
	add(size)

	if opt.PerUser {
		var rows []xray.ClientTraffic
		if err := db.Select("email, up, down, total").Find(&rows).Error; err != nil {
			return nil, err
		}
		userTraffic := metrics.Counter("manifold_user_traffic_bytes_total", "A user's traffic in its current cycle.")
		quota := metrics.Gauge("manifold_user_quota_bytes", "A user's traffic quota; 0 is unlimited.")
		for _, r := range rows {
			userTraffic.Add(float64(r.Up), "user", r.Email, "direction", "up")
			userTraffic.Add(float64(r.Down), "user", r.Email, "direction", "down")
			quota.Add(float64(r.Total), "user", r.Email)
		}
		add(userTraffic)
		add(quota)
	}
	add(metrics.Gauge("manifold_scrape_timestamp_seconds", "When this answer was made.").
		Add(float64(now.Unix())))
	return fams, nil
}

func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
