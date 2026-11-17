package service

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/backupstore"
	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/eventbus"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/nodessh"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

const backupRunTimeout = 30 * time.Minute

// BackupService runs scheduled full backups to their targets and keeps the newest few.
type BackupService struct{}

// BackupTargetInput saves a target; a nil secret keeps the stored one, "" clears it.
type BackupTargetInput struct {
	Id         int     `json:"id" example:"0"`
	Name       string  `json:"name" example:"offsite"`
	Enable     bool    `json:"enable" example:"true"`
	Kind       string  `json:"kind" example:"s3"`
	EveryHours int     `json:"everyHours" example:"24"`
	Keep       int     `json:"keep" example:"7"`
	Dir        string  `json:"dir" example:""`
	Endpoint   string  `json:"endpoint" example:"https://s3.example.com"`
	Region     string  `json:"region" example:"us-east-1"`
	Bucket     string  `json:"bucket" example:"manifold"`
	Prefix     string  `json:"prefix" example:"fra-1"`
	AccessKey  string  `json:"accessKey" example:"AKIA..."`
	Host       string  `json:"host" example:""`
	Port       int     `json:"port" example:"22"`
	User       string  `json:"user" example:""`
	HostKey    string  `json:"hostKey" example:""`
	Passphrase *string `json:"passphrase,omitempty"`
	SecretKey  *string `json:"secretKey,omitempty"`
	Password   *string `json:"password,omitempty"`
	PrivateKey *string `json:"privateKey,omitempty"`
}

// BackupTargetView is a target without its secrets, saying which are set.
type BackupTargetView struct {
	model.BackupTarget
	HasPassphrase bool `json:"hasPassphrase" example:"true"`
	HasSecretKey  bool `json:"hasSecretKey" example:"true"`
	HasPassword   bool `json:"hasPassword" example:"false"`
	HasPrivateKey bool `json:"hasPrivateKey" example:"false"`
}

func backupView(t model.BackupTarget) BackupTargetView {
	return BackupTargetView{
		BackupTarget:  t,
		HasPassphrase: t.Passphrase != "",
		HasSecretKey:  t.SecretKey != "",
		HasPassword:   t.Password != "",
		HasPrivateKey: t.PrivateKey != "",
	}
}

// DefaultBackupDir is where a local target keeps backups when it names no directory.
func DefaultBackupDir() string {
	return filepath.Join(config.GetDBFolderPath(), "backups")
}

func (s *BackupService) List() ([]BackupTargetView, error) {
	var rows []model.BackupTarget
	if err := database.GetDB().Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]BackupTargetView, 0, len(rows))
	for _, r := range rows {
		out = append(out, backupView(r))
	}
	return out, nil
}

func (s *BackupService) get(id int) (model.BackupTarget, error) {
	var t model.BackupTarget
	err := database.GetDB().First(&t, id).Error
	if database.IsNotFound(err) {
		return t, common.NewError("no such backup target")
	}
	return t, err
}

func keepSecret(stored string, in *string) string {
	if in == nil {
		return stored
	}
	return *in
}

// Save validates and stores a target.
func (s *BackupService) Save(in BackupTargetInput) (BackupTargetView, error) {
	var t model.BackupTarget
	if in.Id > 0 {
		stored, err := s.get(in.Id)
		if err != nil {
			return BackupTargetView{}, err
		}
		t = stored
	}
	t.Name, t.Enable, t.Kind = strings.TrimSpace(in.Name), in.Enable, in.Kind
	t.EveryHours, t.Keep = in.EveryHours, in.Keep
	t.Dir, t.Endpoint, t.Region = strings.TrimSpace(in.Dir), strings.TrimSpace(in.Endpoint), strings.TrimSpace(in.Region)
	t.Bucket, t.Prefix, t.AccessKey = strings.TrimSpace(in.Bucket), strings.Trim(strings.TrimSpace(in.Prefix), "/"), strings.TrimSpace(in.AccessKey)
	t.Host, t.Port, t.User, t.HostKey = strings.TrimSpace(in.Host), in.Port, strings.TrimSpace(in.User), strings.TrimSpace(in.HostKey)
	t.Passphrase = keepSecret(t.Passphrase, in.Passphrase)
	t.SecretKey = keepSecret(t.SecretKey, in.SecretKey)
	t.Password = keepSecret(t.Password, in.Password)
	t.PrivateKey = keepSecret(t.PrivateKey, in.PrivateKey)
	if err := validateBackupTarget(&t); err != nil {
		return BackupTargetView{}, err
	}
	if err := database.GetDB().Save(&t).Error; err != nil {
		return BackupTargetView{}, err
	}
	return backupView(t), nil
}

func validateBackupTarget(t *model.BackupTarget) error {
	if t.Name == "" {
		return common.NewError("a backup target needs a name")
	}
	if t.EveryHours < 0 || t.EveryHours > 24*30 {
		return common.NewError("back up every 1 to 720 hours, or 0 for by hand only")
	}
	if t.Keep < 0 || t.Keep > 1000 {
		return common.NewError("keep 1 to 1000 backups, or 0 to keep them all")
	}
	switch t.Kind {
	case "local":
		if t.Dir != "" && !filepath.IsAbs(t.Dir) {
			return common.NewError("the directory must be an absolute path")
		}
	case "s3":
		if t.Endpoint == "" || t.Bucket == "" || t.AccessKey == "" || t.SecretKey == "" {
			return common.NewError("an S3 target needs an endpoint, a bucket, an access key and a secret key")
		}
		if !strings.HasPrefix(t.Endpoint, "https://") && !strings.HasPrefix(t.Endpoint, "http://") {
			return common.NewError("the S3 endpoint must start with https://")
		}
	case "ssh":
		if t.Host == "" || t.User == "" || (t.Password == "" && t.PrivateKey == "") {
			return common.NewError("an SSH target needs a host, a user and a password or private key")
		}
		if t.HostKey == "" {
			return common.NewError("read and accept the SSH host key first")
		}
	default:
		return common.NewError("a backup target is local, s3 or ssh")
	}
	return nil
}

func (s *BackupService) Delete(id int) error {
	return database.GetDB().Delete(&model.BackupTarget{}, id).Error
}

func backupStoreFor(t model.BackupTarget) backupstore.Store {
	switch t.Kind {
	case "s3":
		return &backupstore.S3{
			Endpoint: t.Endpoint, Region: t.Region, Bucket: t.Bucket, Prefix: t.Prefix,
			AccessKey: t.AccessKey, SecretKey: t.SecretKey,
			Client: (&SettingService{}).NewProxiedHTTPClient(backupRunTimeout),
		}
	case "ssh":
		return backupstore.SSH{Dir: t.Dir, Target: nodessh.Target{
			Host: t.Host, Port: t.Port, User: t.User, Password: t.Password, PrivateKey: t.PrivateKey, HostKey: t.HostKey,
		}}
	}
	dir := t.Dir
	if dir == "" {
		dir = DefaultBackupDir()
	}
	return backupstore.Local{Dir: dir}
}

// Files lists what a target holds, newest first by name.
func (s *BackupService) Files(id int) ([]backupstore.File, error) {
	t, err := s.get(id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	files, err := backupStoreFor(t).List(ctx)
	sort.Slice(files, func(i, j int) bool { return files[i].Name > files[j].Name })
	return files, err
}

var backupRuns sync.Mutex

// Run makes a full backup now, seals it when the target has a passphrase, stores it and
// prunes; the outcome is recorded on the target either way.
func (s *BackupService) Run(id int, now time.Time) (BackupTargetView, error) {
	backupRuns.Lock()
	defer backupRuns.Unlock()
	t, err := s.get(id)
	if err != nil {
		return BackupTargetView{}, err
	}
	name, size, runErr := runBackup(t, now)
	t.LastRun = now.UnixMilli()
	t.LastError = ""
	evType := eventbus.EventBackupSucceeded
	if runErr != nil {
		t.LastError = runErr.Error()
		evType = eventbus.EventBackupFailed
		logger.Warning("backup to", t.Name, "failed:", runErr)
	} else {
		t.LastFile, t.LastSize = name, size
	}
	if eventBus != nil {
		eventBus.Publish(eventbus.Event{
			Type: evType, Source: t.Name, Timestamp: now,
			Data: map[string]any{"target": t.Name, "file": name, "size": size, "error": t.LastError},
		})
	}
	if err := database.GetDB().Model(&model.BackupTarget{}).Where("id = ?", t.Id).Updates(map[string]any{
		"last_run": t.LastRun, "last_file": t.LastFile, "last_size": t.LastSize, "last_error": t.LastError,
	}).Error; err != nil {
		return backupView(t), err
	}
	return backupView(t), runErr
}

func runBackup(t model.BackupTarget, now time.Time) (string, int64, error) {
	server := &ServerService{}
	data, _, err := server.FullBackup()
	if err != nil {
		return "", 0, err
	}
	encrypted := t.Passphrase != ""
	if encrypted {
		if data, err = backupstore.Encrypt(data, t.Passphrase); err != nil {
			return "", 0, err
		}
	}
	name := backupstore.Name(server.backupHost(""), now, encrypted)
	ctx, cancel := context.WithTimeout(context.Background(), backupRunTimeout)
	defer cancel()
	store := backupStoreFor(t)
	if err := store.Put(ctx, name, data); err != nil {
		return "", 0, err
	}
	if _, err := backupstore.Prune(ctx, store, t.Keep); err != nil {
		return name, int64(len(data)), errors.New("stored, but pruning old backups failed: " + err.Error())
	}
	return name, int64(len(data)), nil
}

// RunDue runs every enabled target whose interval has passed since its last run.
func (s *BackupService) RunDue(now time.Time) {
	var rows []model.BackupTarget
	if err := database.GetDB().Where("enable = ? AND every_hours > 0", true).Find(&rows).Error; err != nil {
		logger.Warning("backups:", err)
		return
	}
	for _, t := range rows {
		if now.Sub(time.UnixMilli(t.LastRun)) >= time.Duration(t.EveryHours)*time.Hour {
			_, _ = s.Run(t.Id, now)
		}
	}
}

// OpenBackup turns an uploaded backup into the archive restore reads, opening a sealed one.
func OpenBackup(data []byte, passphrase string) ([]byte, error) {
	if !backupstore.IsEncrypted(data) {
		return data, nil
	}
	if passphrase == "" {
		return nil, common.NewError("this backup is encrypted: enter its passphrase")
	}
	return backupstore.Decrypt(data, passphrase)
}
