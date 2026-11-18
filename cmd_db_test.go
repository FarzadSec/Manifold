package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// Switching back to SQLite starts from this copy, so it must hold the panel's rows.
func TestCopyPostgresToSQLiteCarriesThePanel(t *testing.T) {
	baseDSN := strings.TrimSpace(os.Getenv("XUI_TEST_PG_DSN"))
	if baseDSN == "" {
		t.Skip("set XUI_TEST_PG_DSN to copy a real PostgreSQL database")
	}
	admin, err := gorm.Open(postgres.Open(baseDSN), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := admin.DB()
	t.Cleanup(func() { _ = pool.Close() })
	schema := fmt.Sprintf("to_sqlite_%d", time.Now().UnixNano())
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = database.CloseDB()
		_ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error
	})
	t.Setenv("XUI_DB_TYPE", "postgres")
	t.Setenv("XUI_DB_DSN", baseDSN+" search_path="+schema)
	if err := database.InitDB(""); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Create(&model.ClientRecord{Email: "moved", SubID: "moved-sub", Enable: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.CloseDB(); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "manifold.db")
	if err := copyPostgresToSQLite(dst); err != nil {
		t.Fatalf("copyPostgresToSQLite: %v", err)
	}
	copied, err := gorm.Open(sqlite.Open(dst), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	copiedPool, _ := copied.DB()
	defer copiedPool.Close()
	var emails []string
	if err := copied.Model(&model.ClientRecord{}).Pluck("email", &emails).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Join(emails, ",") != "moved" {
		t.Errorf("accounts in the SQLite copy = %v, want [moved]", emails)
	}
}

// The file to switch back to is often the panel's old SQLite database; a copy
// must never overwrite it.
func TestCopyPostgresToSQLiteKeepsAnExistingFile(t *testing.T) {
	t.Setenv("XUI_DB_TYPE", "postgres")
	t.Setenv("XUI_DB_DSN", "postgres://nobody@127.0.0.1:1/none?sslmode=disable")
	dst := filepath.Join(t.TempDir(), "manifold.db")
	if err := os.WriteFile(dst, []byte("old panel"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := copyPostgresToSQLite(dst)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("copyPostgresToSQLite error = %v, want a refusal to overwrite", err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "old panel" {
		t.Errorf("existing file now holds %q", got)
	}
}
