package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// A backup made with the panel down is the archive the panel's own restore reads.
func TestBackupCreateWritesASealedFullBackupThatRestoreReads(t *testing.T) {
	newTokenCLIEnv(t)
	database.GetDB().Create(&model.Inbound{UserId: 1, Enable: true, Port: 8443, Tag: "in-8443", Protocol: model.VLESS, Settings: `{"clients":[]}`})
	out := t.TempDir()
	if code := runBackupCLI([]string{"create", "-out", out, "-passphrase", "s3cret"}); code != 0 {
		t.Fatalf("create exited %d", code)
	}
	files, _ := filepath.Glob(filepath.Join(out, "manifold-backup_*.tar.gz.enc"))
	if len(files) != 1 {
		t.Fatalf("wrote %v", files)
	}
	sealed, _ := os.ReadFile(files[0])
	if _, err := service.OpenBackup(sealed, ""); err == nil || !strings.Contains(err.Error(), "passphrase") {
		t.Fatalf("the backup opened without its passphrase: %v", err)
	}
	archive, err := service.OpenBackup(sealed, "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	report, err := (&service.ServerService{}).InspectBackup(bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("restore cannot read the backup: %v", err)
	}
	if report.Inbounds != 1 || report.SourceVersion != config.GetProductVersion() {
		t.Fatalf("the backup holds %d inbounds from version %q", report.Inbounds, report.SourceVersion)
	}
}
