package service

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// scratchPostgresDatabase creates a database of its own, because pg_dump and
// pg_restore work on whole databases, not on the schemas other tests isolate in.
func scratchPostgresDatabase(t *testing.T, baseDSN, name string) string {
	t.Helper()
	cfg, err := pgconn.ParseConfig(baseDSN)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := gorm.Open(postgres.Open(baseDSN), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := admin.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	dbName := fmt.Sprintf("%s_%d", name, time.Now().UnixNano())
	if err := admin.Exec("CREATE DATABASE " + dbName).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = database.CloseDB()
		if err := admin.Exec("DROP DATABASE " + dbName + " WITH (FORCE)").Error; err != nil {
			t.Error(err)
		}
	})
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(cfg.User, cfg.Password),
		Host:     cfg.Host + ":" + strconv.Itoa(int(cfg.Port)),
		Path:     "/" + dbName,
		RawQuery: "sslmode=disable",
	}
	return u.String()
}

func pgDumpForTest(t *testing.T, dsn string) []byte {
	t.Helper()
	bin, err := exec.LookPath("pg_dump")
	if err != nil {
		t.Skip("pg_dump is not installed")
	}
	env, dbname, err := pgConnEnv(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(context.Background(), bin, "--format=custom", "--no-owner", "--no-privileges", "--dbname", dbname)
	cmd.Env = env
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("pg_dump: %v: %s", err, stderr.String())
	}
	return out.Bytes()
}

// Only device rows are merged: two accounts with one email are refused, and the
// restore runs in one transaction, so the panel's own data stays as it was.
func TestRestorePostgresDumpWithDuplicateAccountsChangesNothing(t *testing.T) {
	baseDSN := strings.TrimSpace(os.Getenv("XUI_TEST_PG_DSN"))
	if baseDSN == "" {
		t.Skip("set XUI_TEST_PG_DSN to run real PostgreSQL restores")
	}
	if runtime.GOOS == "windows" {
		t.Skip("the isolated core executable fixture requires a POSIX environment")
	}
	t.Setenv("XUI_DB_TYPE", "postgres")
	t.Setenv("XUI_DB_DSN", scratchPostgresDatabase(t, baseDSN, "restore_src"))
	if err := database.InitDB(""); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`UPDATE pg_index SET indisready = false WHERE indexrelid = 'idx_clients_email'::regclass`,
		`INSERT INTO clients (email, sub_id, enable) VALUES ('twin', 'sub-1', true), ('twin', 'sub-2', true)`,
		`UPDATE pg_index SET indisready = true WHERE indexrelid = 'idx_clients_email'::regclass`,
	} {
		if err := database.GetDB().Exec(sql).Error; err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	source := os.Getenv("XUI_DB_DSN")
	if err := database.CloseDB(); err != nil {
		t.Fatal(err)
	}
	dump := pgDumpForTest(t, source)

	t.Setenv("XUI_DB_DSN", scratchPostgresDatabase(t, baseDSN, "restore_dst"))
	if err := database.InitDB(""); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Create(&model.ClientRecord{Email: "kept", SubID: "kept-sub", Enable: true}).Error; err != nil {
		t.Fatal(err)
	}
	sqliteImportTestCore(t)
	uploadPath := filepath.Join(t.TempDir(), "backup.dump")
	if err := os.WriteFile(uploadPath, dump, 0o600); err != nil {
		t.Fatal(err)
	}
	upload, err := os.Open(uploadPath)
	if err != nil {
		t.Fatal(err)
	}
	defer upload.Close()
	err = (&ServerService{}).ImportDB(upload, true)
	if err == nil || !strings.Contains(err.Error(), "database left unchanged") ||
		!strings.Contains(err.Error(), `could not create unique index "idx_clients_email"`) {
		t.Fatalf("ImportDB error = %v, want the refused email index, database unchanged", err)
	}
	var emails []string
	if err := database.GetDB().Model(&model.ClientRecord{}).Order("email").Pluck("email", &emails).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Join(emails, ",") != "kept" {
		t.Errorf("accounts after a failed restore = %v, want [kept]", emails)
	}
}

// A backup whose device registry holds the same device twice (its unique index
// damaged on the source server, or missing) must still restore, merged.
func TestRestorePostgresDumpMergesDuplicateDevices(t *testing.T) {
	baseDSN := strings.TrimSpace(os.Getenv("XUI_TEST_PG_DSN"))
	if baseDSN == "" {
		t.Skip("set XUI_TEST_PG_DSN to run real PostgreSQL restores")
	}
	if runtime.GOOS == "windows" {
		t.Skip("the isolated core executable fixture requires a POSIX environment")
	}
	const sub = "cl6wzw1but20dy1c"
	insertDevices := `INSERT INTO client_hwids (sub_id, hwid_hash, first_seen, last_seen, device_os) VALUES
		('` + sub + `', 'hash-a', 100, 200, 'older'),
		('` + sub + `', 'hash-a', 50, 300, 'newer'),
		('` + sub + `', 'hash-b', 10, 20, 'other')`
	damages := map[string][]string{
		"damaged-index": {
			`UPDATE pg_index SET indisready = false WHERE indexrelid = 'idx_client_hwids_sub_hash'::regclass`,
			insertDevices,
			`UPDATE pg_index SET indisready = true WHERE indexrelid = 'idx_client_hwids_sub_hash'::regclass`,
		},
		"missing-index": {`DROP INDEX idx_client_hwids_sub_hash`, insertDevices},
	}
	for name, steps := range damages {
		t.Run(name, func(t *testing.T) {
			t.Setenv("XUI_DB_TYPE", "postgres")
			t.Setenv("XUI_DB_DSN", scratchPostgresDatabase(t, baseDSN, "restore_src"))
			if err := database.InitDB(""); err != nil {
				t.Fatal(err)
			}
			if err := database.GetDB().Create(&model.ClientRecord{Email: "dup-device", SubID: sub, Enable: true}).Error; err != nil {
				t.Fatal(err)
			}
			for _, sql := range steps {
				if err := database.GetDB().Exec(sql).Error; err != nil {
					t.Fatalf("%s: %v", sql, err)
				}
			}
			source := os.Getenv("XUI_DB_DSN")
			if err := database.CloseDB(); err != nil {
				t.Fatal(err)
			}
			dump := pgDumpForTest(t, source)

			t.Setenv("XUI_DB_DSN", scratchPostgresDatabase(t, baseDSN, "restore_dst"))
			if err := database.InitDB(""); err != nil {
				t.Fatal(err)
			}
			sqliteImportTestCore(t)
			uploadPath := filepath.Join(t.TempDir(), "backup.dump")
			if err := os.WriteFile(uploadPath, dump, 0o600); err != nil {
				t.Fatal(err)
			}
			upload, err := os.Open(uploadPath)
			if err != nil {
				t.Fatal(err)
			}
			defer upload.Close()
			if err := (&ServerService{}).ImportDB(upload, true); err != nil {
				t.Fatalf("ImportDB: %v", err)
			}

			var devices []model.ClientHwid
			if err := database.GetDB().Where("sub_id = ?", sub).Order("hwid_hash").Find(&devices).Error; err != nil {
				t.Fatal(err)
			}
			got := make([]string, 0, len(devices))
			for _, d := range devices {
				got = append(got, fmt.Sprintf("%s first=%d last=%d os=%s", d.HwidHash, d.FirstSeen, d.LastSeen, d.DeviceOS))
			}
			want := "hash-a first=50 last=300 os=newer | hash-b first=10 last=20 os=other"
			if strings.Join(got, " | ") != want {
				t.Errorf("devices = %q, want %q", strings.Join(got, " | "), want)
			}
			var indexDef string
			if err := database.GetDB().Raw(`SELECT indexdef FROM pg_indexes WHERE indexname = 'idx_client_hwids_sub_hash'`).Scan(&indexDef).Error; err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(indexDef, "CREATE UNIQUE INDEX") {
				t.Errorf("unique device index after restore = %q, want it rebuilt", indexDef)
			}
		})
	}
}
