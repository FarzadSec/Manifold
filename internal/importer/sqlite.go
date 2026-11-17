package importer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// ReadSQLite opens another panel's SQLite database read-only and reads its users.
func ReadSQLite(ctx context.Context, path string) (*Result, error) {
	db, err := sql.Open("sqlite3", "file:"+url.PathEscape(path)+"?mode=ro&_query_only=1")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tables, err := tableColumns(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("not a SQLite database: %w", err)
	}
	has := func(table string, cols ...string) bool {
		got, ok := tables[table]
		if !ok {
			return false
		}
		for _, c := range cols {
			if !slices.Contains(got, c) {
				return false
			}
		}
		return true
	}
	switch {
	case has("users", "key", "expire_strategy"):
		return readMarzneshin(ctx, db)
	case has("users", "proxy_settings"):
		return readMarzbanFamily(ctx, db, "pasarguard", tables)
	case has("users", "data_limit_reset_strategy") && has("proxies", "settings"):
		return readMarzbanFamily(ctx, db, "marzban", tables)
	case has("inbounds", "settings") && has("client_traffics", "email"):
		return readXUI(ctx, db)
	case has("clients", "config", "name"):
		return readSUI(ctx, db)
	}
	return nil, errors.New("this database is not one the importer knows (Marzban, PasarGuard, Marzneshin, 3x-ui, x-ui, s-ui)")
}

func tableColumns(ctx context.Context, db *sql.DB) (map[string][]string, error) {
	names, err := strings1(ctx, db, `SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, n := range names {
		cols, err := strings1(ctx, db, `SELECT name FROM pragma_table_info(?)`, n)
		if err != nil {
			return nil, err
		}
		out[n] = cols
	}
	return out, nil
}

// strings1 reads the first column of a query as text.
func strings1(ctx context.Context, db *sql.DB, query string, args ...any) ([]string, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// rowMaps reads a query into maps of column to value, for schemas that differ by version.
func rowMaps(ctx context.Context, db *sql.DB, query string, args ...any) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := make(map[string]any, len(cols))
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				m[c] = string(b)
			} else {
				m[c] = vals[i]
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func str(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case time.Time:
		return t.UTC().Format(time.RFC3339)
	}
	return fmt.Sprint(v)
}

func num(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case float64:
		return int64(t)
	case bool:
		if t {
			return 1
		}
		return 0
	case string:
		n, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return int64(n)
	}
	return 0
}

func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "1" || strings.EqualFold(t, "true")
	}
	return num(v) != 0
}

var timeLayouts = []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02"}

// moment reads a time stored as unix seconds, unix milliseconds or a datetime text,
// as unix milliseconds; 0 stays 0.
func moment(v any) int64 {
	switch t := v.(type) {
	case nil:
		return 0
	case time.Time:
		return t.UnixMilli()
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return 0
		}
		if _, err := strconv.ParseFloat(s, 64); err != nil {
			for _, layout := range timeLayouts {
				if at, err := time.Parse(layout, s); err == nil {
					return at.UnixMilli()
				}
			}
			return 0
		}
	}
	n := num(v)
	if n <= 0 {
		return 0
	}
	if n < 1e11 {
		return n * 1000
	}
	return n
}

func readMarzbanFamily(ctx context.Context, db *sql.DB, source string, tables map[string][]string) (*Result, error) {
	res := &Result{Source: source}
	if _, ok := tables["jwt"]; ok {
		if rows, err := rowMaps(ctx, db, `SELECT * FROM jwt LIMIT 1`); err == nil && len(rows) > 0 {
			res.TokenSecret = str(rows[0]["secret_key"])
		}
	}
	users, err := rowMaps(ctx, db, `SELECT * FROM users`)
	if err != nil {
		return nil, err
	}
	proxies := map[int64]map[string]json.RawMessage{}
	if source == "marzban" {
		rows, err := rowMaps(ctx, db, `SELECT user_id, type, settings FROM proxies`)
		if err != nil {
			return nil, err
		}
		for _, p := range rows {
			id := num(p["user_id"])
			if proxies[id] == nil {
				proxies[id] = map[string]json.RawMessage{}
			}
			proxies[id][str(p["type"])] = json.RawMessage(str(p["settings"]))
		}
	}
	for _, u := range users {
		creds := map[string]json.RawMessage{}
		if source == "marzban" {
			creds = proxies[num(u["id"])]
		} else {
			_ = json.Unmarshal([]byte(str(u["proxy_settings"])), &creds)
		}
		status := strings.ToLower(str(u["status"]))
		user := User{
			Username:  str(u["username"]),
			Enabled:   status == "active" || status == "on_hold",
			DataLimit: num(u["data_limit"]),
			Down:      num(u["used_traffic"]),
			Expire:    moment(u["expire"]),
			Note:      str(u["note"]),
			Reset:     resetName(str(u["data_limit_reset_strategy"])),
			Creds:     marzbanCreds(creds),
			TokenName: str(u["username"]),
			RevokedAt: moment(u["sub_revoked_at"]),
		}
		if status == "on_hold" {
			res.Warnings = append(res.Warnings, user.Username+" is on hold in "+source+"; it is imported active and its expiry starts now")
		}
		res.Users = append(res.Users, user)
	}
	if res.TokenSecret == "" {
		res.Warnings = append(res.Warnings, "no subscription secret was found, so old /sub/<token> links cannot be recognised")
	}
	return res, nil
}

func readMarzneshin(ctx context.Context, db *sql.DB) (*Result, error) {
	res := &Result{Source: "marzneshin"}
	users, err := rowMaps(ctx, db, `SELECT * FROM users WHERE COALESCE(removed, 0) = 0`)
	if err != nil {
		users, err = rowMaps(ctx, db, `SELECT * FROM users`)
		if err != nil {
			return nil, err
		}
	}
	for _, u := range users {
		key := strings.ReplaceAll(str(u["key"]), "-", "")
		user := User{
			Username:  str(u["username"]),
			Enabled:   truthy(u["enabled"]) && !truthy(u["expired"]) && !truthy(u["data_limit_reached"]),
			DataLimit: num(u["data_limit"]),
			Down:      num(u["used_traffic"]),
			Expire:    moment(u["expire_date"]),
			Note:      str(u["note"]),
			Reset:     resetName(str(u["data_limit_reset_strategy"])),
			Creds:     marzneshinCreds(key),
			Aliases:   []string{str(u["username"]) + "/" + str(u["key"])},
		}
		if strings.EqualFold(str(u["expire_strategy"]), "start_on_first_use") {
			res.Warnings = append(res.Warnings, user.Username+" starts its time on first use in Marzneshin; it is imported without an expiry")
		}
		res.Users = append(res.Users, user)
	}
	return res, nil
}

// marzneshinCreds derives what Marzneshin derives from a user's key: the key read as a
// UUID for VLESS and VMess, the key itself as the password of the rest.
func marzneshinCreds(key string) map[string]Cred {
	out := map[string]Cred{}
	if len(key) == 32 {
		id := key[0:8] + "-" + key[8:12] + "-" + key[12:16] + "-" + key[16:20] + "-" + key[20:32]
		out[VLESS], out[VMess] = Cred{ID: id}, Cred{ID: id}
	}
	if key != "" {
		out[Trojan], out[Shadowsocks], out[Hysteria2] = Cred{Password: key}, Cred{Password: key}, Cred{Password: key}
	}
	return out
}

func readXUI(ctx context.Context, db *sql.DB) (*Result, error) {
	res := &Result{Source: "3x-ui"}
	inbounds, err := rowMaps(ctx, db, `SELECT * FROM inbounds`)
	if err != nil {
		return nil, err
	}
	traffic := map[string]map[string]any{}
	if rows, err := rowMaps(ctx, db, `SELECT * FROM client_traffics`); err == nil {
		for _, r := range rows {
			traffic[str(r["email"])] = r
		}
	}
	byEmail := map[string]*User{}
	var order []string
	for _, ib := range inbounds {
		proto := protocolName(str(ib["protocol"]))
		var settings struct {
			Method  string            `json:"method"`
			Clients []json.RawMessage `json:"clients"`
		}
		if json.Unmarshal([]byte(str(ib["settings"])), &settings) != nil {
			continue
		}
		for _, raw := range settings.Clients {
			var c struct {
				ID, Password, Email, Flow, SubID, Comment, Method string
				TotalGB                                           int64 `json:"totalGB"`
				ExpiryTime                                        int64 `json:"expiryTime"`
				Enable                                            *bool `json:"enable"`
				TgID                                              any   `json:"tgId"`
				LimitIP                                           int   `json:"limitIp"`
			}
			if json.Unmarshal(raw, &c) != nil || c.Email == "" {
				continue
			}
			u, seen := byEmail[c.Email]
			if !seen {
				u = &User{
					Username: c.Email, Enabled: c.Enable == nil || *c.Enable, DataLimit: c.TotalGB,
					Expire: c.ExpiryTime, Note: c.Comment, SubID: c.SubID, LimitIP: c.LimitIP, Creds: map[string]Cred{},
				}
				u.TgID = num(c.TgID)
				if t, ok := traffic[c.Email]; ok {
					u.Up, u.Down = num(t["up"]), num(t["down"])
					if t["enable"] != nil && !truthy(t["enable"]) {
						u.Enabled = false
					}
				}
				byEmail[c.Email] = u
				order = append(order, c.Email)
			}
			if u.Expire < 0 {
				res.Warnings = append(res.Warnings, c.Email+" starts its time on first use in 3x-ui; it is imported with that duration from now")
				u.Expire = time.Now().UnixMilli() - u.Expire
			}
			if proto == "" {
				continue
			}
			method := c.Method
			if method == "" {
				method = settings.Method
			}
			u.Creds[proto] = Cred{ID: c.ID, Flow: c.Flow, Password: c.Password, Method: method}
		}
	}
	for _, e := range order {
		res.Users = append(res.Users, *byEmail[e])
	}
	return res, nil
}

func readSUI(ctx context.Context, db *sql.DB) (*Result, error) {
	res := &Result{Source: "s-ui"}
	rows, err := rowMaps(ctx, db, `SELECT * FROM clients`)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		var config map[string]struct {
			UUID     string `json:"uuid"`
			Flow     string `json:"flow"`
			Password string `json:"password"`
			Method   string `json:"method"`
		}
		_ = json.Unmarshal([]byte(str(r["config"])), &config)
		creds := map[string]Cred{}
		for kind, c := range config {
			if p := protocolName(kind); p != "" && (c.UUID != "" || c.Password != "") {
				creds[p] = Cred{ID: c.UUID, Flow: c.Flow, Password: c.Password, Method: c.Method}
			}
		}
		name := str(r["name"])
		res.Users = append(res.Users, User{
			Username: name, Enabled: truthy(r["enable"]), DataLimit: num(r["volume"]),
			Up: num(r["up"]), Down: num(r["down"]), Expire: moment(r["expiry"]), Note: str(r["desc"]),
			Creds: creds, SubID: name,
		})
	}
	return res, nil
}
