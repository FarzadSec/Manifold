package importer

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Tokens made by Marzban's own create_subscription_token and its older JWT, in Python.
func TestMarzbanTokensFromMarzbanItself(t *testing.T) {
	name, at, ok := ParseMarzbanToken("YWxpY2UsMTcwMDAwMDAwMAaWkyvFjnW4", "s3cr3t")
	if !ok || name != "alice" || at.Unix() != 1700000000 {
		t.Fatalf("short token: %q %v %v", name, at, ok)
	}
	jwt := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJib2IiLCJhY2Nlc3MiOiJzdWJzY3JpcHRpb24iLCJpYXQiOjE2OTAwMDAwMDB9.ZKJj-LVrLBwRoSAy2NaMh8juPFYhEMXSAF7N3khajmU"
	if name, at, ok := ParseMarzbanToken(jwt, "s3cr3t"); !ok || name != "bob" || at.Unix() != 1690000000 {
		t.Fatalf("JWT token: %q %v %v", name, at, ok)
	}
	for _, bad := range []struct{ token, secret string }{
		{"YWxpY2UsMTcwMDAwMDAwMAaWkyvFjnW4", "other"},
		{"Ym9iLDE3MDAwMDAwMDAaWkyvFjnW4", "s3cr3t"},
		{jwt, "other"},
		// An admin login token is signed with the same secret but is no subscription.
		{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJhZG1pbiIsImFjY2VzcyI6InN1ZG8iLCJpYXQiOjE2OTAwMDAwMDB9.FXBnEw3KCjiJtxL1bdoIW0e47I29yNw7f0fC6eSYhSg", "s3cr3t"},
	} {
		if _, _, ok := ParseMarzbanToken(bad.token, bad.secret); ok {
			t.Fatalf("token %q passed with secret %q", bad.token, bad.secret)
		}
	}
	if got := MarzbanToken("alice", "s3cr3t", time.Unix(1700000000, 0)); got != "YWxpY2UsMTcwMDAwMDAwMAaWkyvFjnW4" {
		t.Fatalf("MarzbanToken = %s", got)
	}
}

func sqliteFixture(t *testing.T, stmts ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "panel.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	return path
}

func byName(r *Result) map[string]User {
	out := map[string]User{}
	for _, u := range r.Users {
		out[u.Username] = u
	}
	return out
}

func TestReadSQLiteKnowsEachPanel(t *testing.T) {
	ctx := context.Background()
	t.Run("marzban", func(t *testing.T) {
		path := sqliteFixture(t,
			`CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT, status TEXT, used_traffic INTEGER, data_limit INTEGER, expire INTEGER, note TEXT, data_limit_reset_strategy TEXT, sub_revoked_at DATETIME)`,
			`CREATE TABLE proxies (id INTEGER PRIMARY KEY, user_id INTEGER, type TEXT, settings TEXT)`,
			`CREATE TABLE jwt (id INTEGER PRIMARY KEY, secret_key TEXT)`,
			`INSERT INTO jwt VALUES (1, 'sekret')`,
			`INSERT INTO users VALUES (1, 'alice', 'active', 5000, 10000, 1800000000, 'vip', 'month', '2025-01-02 03:04:05')`,
			`INSERT INTO users VALUES (2, 'bob', 'disabled', 0, NULL, NULL, NULL, 'no_reset', NULL)`,
			`INSERT INTO proxies VALUES (1, 1, 'VLESS', '{"id":"11111111-1111-1111-1111-111111111111","flow":"xtls-rprx-vision"}')`,
			`INSERT INTO proxies VALUES (2, 1, 'Trojan', '{"password":"tro"}')`,
			`INSERT INTO proxies VALUES (3, 2, 'VMess', '{"id":"22222222-2222-2222-2222-222222222222"}')`,
		)
		res, err := ReadSQLite(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		u := byName(res)
		a := u["alice"]
		if res.Source != "marzban" || res.TokenSecret != "sekret" || !a.Enabled || a.DataLimit != 10000 || a.Down != 5000 ||
			a.Expire != 1800000000000 || a.Reset != "monthly" || a.Creds[VLESS].Flow != "xtls-rprx-vision" || a.Creds[Trojan].Password != "tro" ||
			a.RevokedAt != time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC).UnixMilli() {
			t.Fatalf("alice = %+v (source %s, secret %q)", a, res.Source, res.TokenSecret)
		}
		if b := u["bob"]; b.Enabled || b.DataLimit != 0 || b.Expire != 0 || b.Creds[VMess].ID == "" {
			t.Fatalf("bob = %+v", b)
		}
	})
	t.Run("pasarguard", func(t *testing.T) {
		path := sqliteFixture(t,
			`CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT, status TEXT, used_traffic INTEGER, data_limit INTEGER, expire DATETIME, proxy_settings TEXT, data_limit_reset_strategy TEXT)`,
			`INSERT INTO users VALUES (1, 'carol', 'on_hold', 1, 2, '2027-06-01 00:00:00', '{"vless":{"id":"33333333-3333-3333-3333-333333333333"},"shadowsocks":{"password":"ss","method":"aes-256-gcm"}}', 'week')`,
		)
		res, err := ReadSQLite(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		c := byName(res)["carol"]
		if res.Source != "pasarguard" || !c.Enabled || c.Expire != time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC).UnixMilli() ||
			c.Creds[Shadowsocks].Method != "aes-256-gcm" || c.Reset != "weekly" || len(res.Warnings) != 2 {
			t.Fatalf("carol = %+v, warnings %v", c, res.Warnings)
		}
	})
	t.Run("marzneshin", func(t *testing.T) {
		path := sqliteFixture(t,
			`CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT, key TEXT, enabled BOOLEAN, expired BOOLEAN, data_limit_reached BOOLEAN, expire_strategy TEXT, expire_date DATETIME, data_limit INTEGER, used_traffic INTEGER, note TEXT, removed BOOLEAN, data_limit_reset_strategy TEXT)`,
			`INSERT INTO users VALUES (1, 'dave', '0123456789abcdef0123456789abcdef', 1, 0, 0, 'fixed_date', '2027-01-01T00:00:00', 0, 7, '', 0, 'no_reset')`,
			`INSERT INTO users VALUES (2, 'gone', 'ffffffffffffffffffffffffffffffff', 1, 0, 0, 'never', NULL, 0, 0, '', 1, 'no_reset')`,
		)
		res, err := ReadSQLite(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		d := byName(res)["dave"]
		if len(res.Users) != 1 || d.Creds[VLESS].ID != "01234567-89ab-cdef-0123-456789abcdef" || d.Creds[Trojan].Password != "0123456789abcdef0123456789abcdef" ||
			strings.Join(d.Aliases, ",") != "dave/0123456789abcdef0123456789abcdef" {
			t.Fatalf("marzneshin = %+v", res.Users)
		}
	})
	t.Run("3x-ui", func(t *testing.T) {
		path := sqliteFixture(t,
			`CREATE TABLE inbounds (id INTEGER PRIMARY KEY, protocol TEXT, settings TEXT)`,
			`CREATE TABLE client_traffics (id INTEGER PRIMARY KEY, email TEXT, up INTEGER, down INTEGER, enable BOOLEAN)`,
			`INSERT INTO inbounds VALUES (1, 'vless', '{"clients":[{"id":"44444444-4444-4444-4444-444444444444","email":"erin","subId":"erinsub","totalGB":1073741824,"expiryTime":0,"enable":true,"tgId":"","limitIp":2}]}')`,
			`INSERT INTO inbounds VALUES (2, 'shadowsocks', '{"method":"2022-blake3-aes-128-gcm","clients":[{"password":"c3M=","email":"erin"}]}')`,
			`INSERT INTO client_traffics VALUES (1, 'erin', 10, 20, 0)`,
		)
		res, err := ReadSQLite(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		e := byName(res)["erin"]
		if res.Source != "3x-ui" || e.SubID != "erinsub" || e.Enabled || e.Up != 10 || e.Down != 20 || e.LimitIP != 2 ||
			e.Creds[Shadowsocks].Method != "2022-blake3-aes-128-gcm" || e.Creds[VLESS].ID == "" {
			t.Fatalf("erin = %+v", e)
		}
	})
	t.Run("s-ui", func(t *testing.T) {
		path := sqliteFixture(t,
			`CREATE TABLE clients (id INTEGER PRIMARY KEY, enable BOOLEAN, name TEXT, config TEXT, volume INTEGER, expiry INTEGER, up INTEGER, down INTEGER, desc TEXT)`,
			`INSERT INTO clients VALUES (1, 1, 'frank', '{"vless":{"name":"frank","uuid":"55555555-5555-5555-5555-555555555555"},"hysteria2":{"name":"frank","password":"hy"}}', 100, 1800000000, 1, 2, 'x')`,
		)
		res, err := ReadSQLite(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		f := byName(res)["frank"]
		if res.Source != "s-ui" || f.SubID != "frank" || f.Creds[Hysteria2].Password != "hy" || f.Expire != 1800000000000 {
			t.Fatalf("frank = %+v", f)
		}
	})
	t.Run("unknown", func(t *testing.T) {
		path := sqliteFixture(t, `CREATE TABLE things (id INTEGER)`)
		if _, err := ReadSQLite(ctx, path); err == nil || !strings.Contains(err.Error(), "not one the importer knows") {
			t.Fatalf("an unknown database gave %v", err)
		}
	})
}

func TestReadMarzbanAPIPagesAndKeepsTheCurrentLink(t *testing.T) {
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/admin/token":
			_ = r.ParseForm()
			if r.PostForm.Get("username") != "admin" || r.PostForm.Get("password") != "pw" {
				w.WriteHeader(401)
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"bearer"}`))
		case "/api/users":
			auth = append(auth, r.Header.Get("Authorization"))
			offset := r.URL.Query().Get("offset")
			users := []map[string]any{}
			if offset == "0" {
				for i := 0; i < pageSize; i++ {
					users = append(users, map[string]any{"username": "u" + string(rune('a'+i%26)) + "x", "status": "active", "proxies": map[string]any{"vless": map[string]any{"id": "id"}}})
				}
			} else {
				users = append(users, map[string]any{
					"username": "last", "status": "limited", "data_limit": 10, "used_traffic": 10,
					"expire": 1800000000, "proxies": map[string]any{"trojan": map[string]any{"password": "p"}},
					"subscription_url": "https://old.example.com/sub/bGFzdCwxNzAwMDAwMDAwabcdefghij",
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"users": users, "total": pageSize + 1})
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	res, err := Remote{URL: srv.URL, Username: "admin", Password: "pw"}.ReadMarzbanAPI(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Users) != pageSize+1 || len(auth) != 2 || auth[1] != "Bearer tok" {
		t.Fatalf("read %d users over %v", len(res.Users), auth)
	}
	last := res.Users[pageSize]
	if last.Enabled || last.Expire != 1800000000000 || strings.Join(last.Aliases, ",") != "bGFzdCwxNzAwMDAwMDAwabcdefghij" {
		t.Fatalf("last = %+v", last)
	}
	if _, err := (Remote{URL: srv.URL, Username: "admin", Password: "no"}).ReadMarzbanAPI(context.Background(), ""); err == nil {
		t.Fatal("a refused login was taken")
	}
}

func TestReadHiddifyCountsDaysFromTheStart(t *testing.T) {
	res, err := ReadHiddify([]byte(`{"users":[{"uuid":"66666666-6666-6666-6666-666666666666","name":"gina","usage_limit_GB":2.5,"current_usage_GB":0.5,"package_days":30,"start_date":"2026-11-01","mode":"monthly","enable":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	g := res.Users[0]
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if g.DataLimit != 5<<29 || g.Down != 1<<29 || g.Expire != start.Add(30*24*time.Hour).UnixMilli() || g.SubID != g.Creds[VLESS].ID || g.Reset != "monthly" {
		t.Fatalf("gina = %+v", g)
	}
	if _, err := ReadHiddify([]byte(`{"nope":1}`)); err == nil {
		t.Fatal("a file without users was taken")
	}
}
