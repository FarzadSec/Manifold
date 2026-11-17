package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/backupstore"
)

func strPtr(s string) *string { return &s }

// A due target gets a sealed full backup that opens with its passphrase, a target not due
// gets nothing, and the oldest backups go once there are more than it keeps.
func TestScheduledBackupsAreSealedStoredAndPruned(t *testing.T) {
	setupBulkDB(t)
	setSubSettings(t, map[string]string{"webDomain": "panel.test"})
	dir := t.TempDir()
	svc := &BackupService{}
	target, err := svc.Save(BackupTargetInput{
		Name: "here", Enable: true, Kind: "local", Dir: dir, EveryHours: 24, Keep: 2, Passphrase: strPtr("pass"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 11, 17, 3, 0, 0, 0, time.UTC)
	svc.RunDue(t0)
	svc.RunDue(t0.Add(time.Hour))
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("an hour later there are %d backups, want the one run that was due", len(entries))
	}
	for _, at := range []time.Time{t0.Add(25 * time.Hour), t0.Add(49 * time.Hour)} {
		svc.RunDue(at)
	}
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if len(names) != 2 || !strings.Contains(names[0], "panel.test-20261118-040000") || !strings.HasSuffix(names[1], "20261119-040000.tar.gz.enc") {
		t.Fatalf("stored %v, want the two newest of three runs, sealed", names)
	}
	sealed, _ := os.ReadFile(dir + "/" + names[1])
	archive, err := backupstore.Decrypt(sealed, "pass")
	if err != nil || !bytes.HasPrefix(archive, []byte{0x1f, 0x8b}) {
		t.Fatalf("the sealed file does not open to a gzip archive: %v", err)
	}
	if _, err := (&ServerService{}).InspectBackup(bytes.NewReader(archive)); err != nil {
		t.Fatalf("the opened backup is not a full backup the restore reads: %v", err)
	}
	list, _ := svc.List()
	if list[0].LastError != "" || list[0].LastFile != names[1] || target.Id != list[0].Id {
		t.Fatalf("target after runs = %+v", list[0])
	}
}

// Secrets are written, kept when a save leaves them out, cleared when sent empty, and
// never come back out.
func TestBackupSecretsAreWriteOnly(t *testing.T) {
	setupBulkDB(t)
	svc := &BackupService{}
	saved, err := svc.Save(BackupTargetInput{
		Name: "s3", Enable: true, Kind: "s3", Endpoint: "https://s3.example.com", Bucket: "b", AccessKey: "AK",
		SecretKey: strPtr("very-secret"), Passphrase: strPtr("pass"),
	})
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.Save(BackupTargetInput{Id: saved.Id, Name: "s3", Enable: true, Kind: "s3", Endpoint: "https://s3.example.com", Bucket: "b", AccessKey: "AK", Passphrase: strPtr("")})
	if err != nil {
		t.Fatal(err)
	}
	if !again.HasSecretKey || again.HasPassphrase {
		t.Fatalf("after a save without the secret and with an empty passphrase: %+v", again)
	}
	list, _ := svc.List()
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), "very-secret") {
		t.Fatalf("a secret left the panel: %s", raw)
	}
}

// An encrypted upload needs its passphrase before restore reads it; a plain one passes through.
func TestRestoreOpensSealedBackups(t *testing.T) {
	plain := []byte{0x1f, 0x8b, 1, 2, 3}
	if got, err := OpenBackup(plain, ""); err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("plain = %v, %v", got, err)
	}
	sealed, _ := backupstore.Encrypt(plain, "pass")
	if _, err := OpenBackup(sealed, ""); err == nil || !strings.Contains(err.Error(), "passphrase") {
		t.Fatalf("sealed without passphrase = %v", err)
	}
	if _, err := OpenBackup(sealed, "nope"); !errors.Is(err, backupstore.ErrWrongPassphrase) {
		t.Fatalf("wrong passphrase = %v", err)
	}
	if got, err := OpenBackup(sealed, "pass"); err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("right passphrase = %v, %v", got, err)
	}
}
