package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/certstore"
	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/eventbus"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
)

const (
	renewBefore     = 30 * 24 * time.Hour
	warnBefore      = 14 * 24 * time.Hour
	retryFailedEach = 12 * time.Hour
	issueTimeout    = 10 * time.Minute
)

// CertificateService keeps the certificate store: ACME orders, uploads, renewals and
// the copies on nodes.
type CertificateService struct{}

// CertificateView is a certificate without its secrets, with the files it is kept in.
type CertificateView struct {
	model.Certificate
	CertFile    string `json:"certFile" example:"/etc/manifold/certs/example.com/fullchain.pem"`
	KeyFile     string `json:"keyFile" example:"/etc/manifold/certs/example.com/privkey.pem"`
	HasDNSToken bool   `json:"hasDnsToken" example:"true"`
}

// CertificateInput saves an ACME certificate's settings; a nil DNSToken keeps the stored one.
type CertificateInput struct {
	Id          int      `json:"id" example:"0"`
	Name        string   `json:"name" example:"example.com"`
	Domains     []string `json:"domains" example:"[\"example.com\",\"*.example.com\"]"`
	Challenge   string   `json:"challenge" example:"dns-01"`
	Port        int      `json:"port" example:"0"`
	DNSProvider string   `json:"dnsProvider" example:"cloudflare"`
	DNSToken    *string  `json:"dnsToken,omitempty"`
	Email       string   `json:"email" example:""`
	Staging     bool     `json:"staging" example:"false"`
	AutoRenew   bool     `json:"autoRenew" example:"true"`
	NodeIds     []int    `json:"nodeIds" example:"[]"`
}

// CertificateUpload stores a certificate made elsewhere.
type CertificateUpload struct {
	Name    string `json:"name" example:"example.com"`
	Cert    string `json:"cert" example:"-----BEGIN CERTIFICATE-----…"`
	Key     string `json:"key" example:"-----BEGIN PRIVATE KEY-----…"`
	NodeIds []int  `json:"nodeIds" example:"[]"`
}

// CertRoot is where the store keeps its files.
func CertRoot() string {
	return filepath.Join(config.GetDBFolderPath(), "certs")
}

// ACMEDirectory is the CA an order goes to; tests point it at their own.
var ACMEDirectory = func(staging bool) string {
	if staging {
		return certstore.LetsEncryptStaging
	}
	return certstore.LetsEncrypt
}

// DNSProviderFor builds a DNS-01 provider; tests swap it for their own zone.
var DNSProviderFor = func(kind, token string) (certstore.Provider, error) {
	switch kind {
	case "cloudflare":
		return &certstore.Cloudflare{Token: token}, nil
	case "arvancloud":
		return &certstore.ArvanCloud{APIKey: token}, nil
	}
	return nil, fmt.Errorf("unknown DNS provider %q", kind)
}

// dnsWait is how long DNS-01 waits for public resolvers to see its record.
var dnsWait = certstore.WaitTXT(3 * time.Minute)

var hostname = regexp.MustCompile(`^(\*\.)?([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

func certView(c model.Certificate) CertificateView {
	if c.Domains == nil {
		c.Domains = []string{}
	}
	if c.NodeIds == nil {
		c.NodeIds = []int{}
	}
	certFile, keyFile := certstore.Paths(CertRoot(), c.Name)
	return CertificateView{Certificate: c, CertFile: certFile, KeyFile: keyFile, HasDNSToken: c.DNSToken != ""}
}

func (s *CertificateService) List() ([]CertificateView, error) {
	var rows []model.Certificate
	if err := database.GetDB().Order("name").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]CertificateView, 0, len(rows))
	for _, r := range rows {
		out = append(out, certView(r))
	}
	return out, nil
}

func (s *CertificateService) get(id int) (model.Certificate, error) {
	var c model.Certificate
	if err := database.GetDB().First(&c, id).Error; err != nil {
		return c, common.NewError("no such certificate")
	}
	return c, nil
}

func checkNodes(ids []int) ([]int, error) {
	out := []int{}
	for _, id := range ids {
		var n model.Node
		if err := database.GetDB().Select("id").First(&n, id).Error; err != nil {
			return nil, common.NewError("no node", id)
		}
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out, nil
}

// Save stores an ACME certificate's settings; Issue gets it.
func (s *CertificateService) Save(in CertificateInput) (CertificateView, error) {
	var c model.Certificate
	if in.Id > 0 {
		stored, err := s.get(in.Id)
		if err != nil {
			return CertificateView{}, err
		}
		if stored.Source != "acme" {
			return CertificateView{}, common.NewError("an uploaded certificate is replaced by uploading again")
		}
		c = stored
	}
	name := strings.ToLower(strings.TrimSpace(in.Name))
	domains := []string{}
	for _, d := range in.Domains {
		d = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(d), "."))
		if d == "" || slices.Contains(domains, d) {
			continue
		}
		if !hostname.MatchString(d) {
			return CertificateView{}, common.NewError("not a domain name:", d)
		}
		if strings.HasPrefix(d, "*.") && in.Challenge != "dns-01" {
			return CertificateView{}, common.NewError(d, "is a wildcard, which only DNS-01 can prove")
		}
		domains = append(domains, d)
	}
	if len(domains) == 0 {
		return CertificateView{}, common.NewError("a certificate needs at least one domain")
	}
	if name == "" {
		name = strings.TrimPrefix(domains[0], "*.")
	}
	if !certstore.ValidName(name) {
		return CertificateView{}, common.NewError("a certificate name is lowercase letters, digits, dots and dashes")
	}
	if c.Id == 0 || c.Name != name {
		var taken int64
		database.GetDB().Model(&model.Certificate{}).Where("name = ? AND id <> ?", name, c.Id).Count(&taken)
		if taken > 0 {
			return CertificateView{}, common.NewError("another certificate is called", name)
		}
	}
	switch in.Challenge {
	case "http-01", "tls-alpn-01":
		if in.Port < 0 || in.Port > 65535 {
			return CertificateView{}, common.NewError("the port is 1 to 65535, or 0 for the standard one")
		}
	case "dns-01":
		if _, err := DNSProviderFor(in.DNSProvider, ""); err != nil {
			return CertificateView{}, err
		}
	default:
		return CertificateView{}, common.NewError("the challenge is http-01, tls-alpn-01 or dns-01")
	}
	nodes, err := checkNodes(in.NodeIds)
	if err != nil {
		return CertificateView{}, err
	}
	if c.Id > 0 && c.Name != name && c.CertPEM != "" {
		_ = certstore.Remove(CertRoot(), c.Name)
	}
	c.Name, c.Domains, c.Source, c.Challenge, c.Port = name, domains, "acme", in.Challenge, in.Port
	c.DNSProvider, c.Email, c.Staging, c.AutoRenew, c.NodeIds = in.DNSProvider, strings.TrimSpace(in.Email), in.Staging, in.AutoRenew, nodes
	if in.DNSToken != nil {
		c.DNSToken = strings.TrimSpace(*in.DNSToken)
	}
	if c.Challenge == "dns-01" && c.DNSToken == "" {
		return CertificateView{}, common.NewError("DNS-01 needs the provider's API token")
	}
	if c.Challenge != "dns-01" {
		c.DNSProvider, c.DNSToken = "", ""
	}
	if err := database.GetDB().Save(&c).Error; err != nil {
		return CertificateView{}, err
	}
	if c.CertPEM != "" {
		if err := certstore.Write(CertRoot(), c.Name, []byte(c.CertPEM), []byte(c.KeyPEM)); err != nil {
			return CertificateView{}, err
		}
	}
	return certView(c), nil
}

// Upload stores a certificate and key made elsewhere, replacing one of the same name.
func (s *CertificateService) Upload(in CertificateUpload) (CertificateView, error) {
	name := strings.ToLower(strings.TrimSpace(in.Name))
	info, err := certstore.Inspect([]byte(in.Cert), []byte(in.Key))
	if err != nil {
		return CertificateView{}, common.NewError("the certificate and key do not make a pair:", err)
	}
	if name == "" && len(info.Domains) > 0 {
		name = strings.TrimPrefix(info.Domains[0], "*.")
	}
	if !certstore.ValidName(name) {
		return CertificateView{}, common.NewError("a certificate name is lowercase letters, digits, dots and dashes")
	}
	nodes, err := checkNodes(in.NodeIds)
	if err != nil {
		return CertificateView{}, err
	}
	var c model.Certificate
	database.GetDB().Where("name = ?", name).First(&c)
	if c.Id > 0 && c.Source == "acme" {
		return CertificateView{}, common.NewError(name, "is issued by the panel; delete it first to upload your own")
	}
	c.Name, c.Source, c.Domains, c.NodeIds = name, "upload", info.Domains, nodes
	c.CertPEM, c.KeyPEM, c.Issuer = in.Cert, in.Key, info.Issuer
	c.NotBefore, c.NotAfter, c.LastRun, c.LastError = info.NotBefore.UnixMilli(), info.NotAfter.UnixMilli(), time.Now().UnixMilli(), ""
	if err := database.GetDB().Save(&c).Error; err != nil {
		return CertificateView{}, err
	}
	if err := s.install(&c); err != nil {
		return certView(c), err
	}
	return certView(c), nil
}

// Delete removes a certificate and its files here; copies on nodes stay where inbounds use them.
func (s *CertificateService) Delete(id int) error {
	c, err := s.get(id)
	if err != nil {
		return err
	}
	if issuing(c.Id) {
		return common.NewError("the certificate is being issued; wait for it to finish")
	}
	if err := database.GetDB().Delete(&model.Certificate{}, c.Id).Error; err != nil {
		return err
	}
	return certstore.Remove(CertRoot(), c.Name)
}

var certJobs = struct {
	sync.Mutex
	running map[int]bool
}{running: map[int]bool{}}

func issuing(id int) bool {
	certJobs.Lock()
	defer certJobs.Unlock()
	return certJobs.running[id]
}

// Issue starts an order for a certificate and returns at once; the list shows how it went.
func (s *CertificateService) Issue(id int) error {
	c, err := s.get(id)
	if err != nil {
		return err
	}
	if c.Source != "acme" {
		return common.NewError("an uploaded certificate cannot be issued")
	}
	certJobs.Lock()
	if certJobs.running[id] {
		certJobs.Unlock()
		return common.NewError("this certificate is already being issued")
	}
	certJobs.running[id] = true
	certJobs.Unlock()
	database.GetDB().Model(&model.Certificate{}).Where("id = ?", id).Update("issuing", true)
	go func() {
		defer func() {
			certJobs.Lock()
			delete(certJobs.running, id)
			certJobs.Unlock()
		}()
		_ = s.issue(c)
	}()
	return nil
}

func solverFor(c model.Certificate) (certstore.Solver, error) {
	switch c.Challenge {
	case "http-01":
		port := c.Port
		if port == 0 {
			port = 80
		}
		return &certstore.HTTP01{Addr: net.JoinHostPort("", strconv.Itoa(port))}, nil
	case "tls-alpn-01":
		port := c.Port
		if port == 0 {
			port = 443
		}
		return &certstore.TLSALPN01{Addr: net.JoinHostPort("", strconv.Itoa(port))}, nil
	case "dns-01":
		p, err := DNSProviderFor(c.DNSProvider, c.DNSToken)
		if err != nil {
			return nil, err
		}
		return &certstore.DNS01{Provider: p, Wait: dnsWait}, nil
	}
	return nil, fmt.Errorf("unknown challenge %q", c.Challenge)
}

// issue runs one order and records the outcome; it is what Issue and the renewal job run.
func (s *CertificateService) issue(c model.Certificate) error {
	db := database.GetDB()
	now := time.Now()
	runErr := func() error {
		solver, err := solverFor(c)
		if err != nil {
			return err
		}
		req := certstore.Request{
			Directory: ACMEDirectory(c.Staging), Email: c.Email, Domains: c.Domains, Solver: solver,
			Log: func(line string) { logger.Info("certificate", c.Name+":", line) },
		}
		if c.AccountKey != "" {
			if key, err := certstore.ParseKey([]byte(c.AccountKey)); err == nil {
				req.AccountKey = key
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), issueTimeout)
		defer cancel()
		res, err := certstore.Issue(ctx, req)
		if err != nil {
			return err
		}
		info, err := certstore.Inspect(res.CertPEM, res.KeyPEM)
		if err != nil {
			return err
		}
		c.CertPEM, c.KeyPEM, c.AccountKey = string(res.CertPEM), string(res.KeyPEM), string(res.AccountKeyPEM)
		c.Issuer, c.NotBefore, c.NotAfter = info.Issuer, info.NotBefore.UnixMilli(), info.NotAfter.UnixMilli()
		return nil
	}()
	c.Issuing, c.LastRun, c.LastError = false, now.UnixMilli(), ""
	outcome := map[string]any{"issuing": false, "last_run": c.LastRun, "last_error": ""}
	if runErr != nil {
		c.LastError = runErr.Error()
		outcome["last_error"] = c.LastError
		logger.Warning("certificate", c.Name, "was not issued:", runErr)
	} else {
		outcome["cert_pem"], outcome["key_pem"], outcome["account_key"] = c.CertPEM, c.KeyPEM, c.AccountKey
		outcome["issuer"], outcome["not_before"], outcome["not_after"] = c.Issuer, c.NotBefore, c.NotAfter
	}
	if err := db.Model(&model.Certificate{}).Where("id = ?", c.Id).Updates(outcome).Error; err != nil {
		logger.Warning("certificate", c.Name, "could not be recorded:", err)
	}
	if runErr == nil {
		runErr = s.install(&c)
	}
	evType := eventbus.EventCertIssued
	if runErr != nil {
		evType = eventbus.EventCertFailed
	}
	publishCert(evType, c, runErr)
	return runErr
}

func publishCert(t eventbus.EventType, c model.Certificate, err error) {
	if eventBus == nil {
		return
	}
	data := map[string]any{"name": c.Name, "domains": c.Domains, "notAfter": c.NotAfter}
	if err != nil {
		data["error"] = err.Error()
	}
	eventBus.Publish(eventbus.Event{Type: t, Source: c.Name, Timestamp: time.Now(), Data: data})
}

// install writes a certificate's files here, restarts Xray when an inbound reads them,
// and copies it to its nodes.
func (s *CertificateService) install(c *model.Certificate) error {
	if err := InstallCertificate(c.Name, []byte(c.CertPEM), []byte(c.KeyPEM)); err != nil {
		return err
	}
	var failed []string
	for _, id := range c.NodeIds {
		if err := pushCertificate(id, c); err != nil {
			failed = append(failed, err.Error())
		}
	}
	if len(failed) > 0 {
		err := errors.New("copied here but not to every node: " + strings.Join(failed, "; "))
		database.GetDB().Model(&model.Certificate{}).Where("id = ?", c.Id).Update("last_error", err.Error())
		c.LastError = err.Error()
		return err
	}
	return nil
}

// InstallCertificate writes a certificate where this panel keeps it and restarts Xray
// when one of its inbounds reads those files; a node runs it for a pushed copy too.
func InstallCertificate(name string, certPEM, keyPEM []byte) error {
	if _, err := certstore.Inspect(certPEM, keyPEM); err != nil {
		return err
	}
	if err := certstore.Write(CertRoot(), name, certPEM, keyPEM); err != nil {
		return err
	}
	certFile, _ := certstore.Paths(CertRoot(), name)
	var users int64
	database.GetDB().Model(&model.Inbound{}).Where("stream_settings LIKE ?", "%"+filepath.Dir(certFile)+"%").Count(&users)
	if users > 0 {
		(&XrayService{}).SetToNeedRestart()
	}
	return nil
}

func pushCertificate(nodeID int, c *model.Certificate) error {
	n, err := (&NodeService{}).GetById(nodeID)
	if err != nil || n == nil {
		return fmt.Errorf("node %d is gone", nodeID)
	}
	mgr := runtime.GetManager()
	if mgr == nil {
		return errors.New("runtime manager unavailable")
	}
	remote, err := mgr.RemoteFor(n)
	if err != nil {
		return fmt.Errorf("%s: %w", n.Name, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := remote.PutCertificate(ctx, c.Name, []byte(c.CertPEM), []byte(c.KeyPEM)); err != nil {
		return fmt.Errorf("%s: %w", n.Name, err)
	}
	return nil
}

// StorePushed keeps a copy a master sent, so this node lists it and its inbounds read it.
func (s *CertificateService) StorePushed(name string, certPEM, keyPEM []byte) error {
	if !certstore.ValidName(name) {
		return common.NewError("invalid certificate name")
	}
	info, err := certstore.Inspect(certPEM, keyPEM)
	if err != nil {
		return err
	}
	var c model.Certificate
	database.GetDB().Where("name = ?", name).First(&c)
	if c.Id > 0 && c.Source != "master" {
		return common.NewError("this node has its own certificate called", name)
	}
	c.Name, c.Source, c.Domains, c.CertPEM, c.KeyPEM, c.Issuer = name, "master", info.Domains, string(certPEM), string(keyPEM), info.Issuer
	c.NotBefore, c.NotAfter, c.LastRun = info.NotBefore.UnixMilli(), info.NotAfter.UnixMilli(), time.Now().UnixMilli()
	if err := database.GetDB().Save(&c).Error; err != nil {
		return err
	}
	return InstallCertificate(name, certPEM, keyPEM)
}

// RenewDue renews what is close to expiring and warns, once a day, about what will not
// renew on its own. A failed renewal waits retryFailedEach before the next try.
func (s *CertificateService) RenewDue(now time.Time) {
	var rows []model.Certificate
	if err := database.GetDB().Find(&rows).Error; err != nil {
		return
	}
	for _, c := range rows {
		if c.CertPEM == "" {
			continue
		}
		left := time.UnixMilli(c.NotAfter).Sub(now)
		auto := c.Source == "acme" && c.AutoRenew
		if auto && left < renewBefore && !issuing(c.Id) &&
			(c.LastError == "" || now.Sub(time.UnixMilli(c.LastRun)) >= retryFailedEach) {
			certJobs.Lock()
			certJobs.running[c.Id] = true
			certJobs.Unlock()
			_ = s.issue(c)
			certJobs.Lock()
			delete(certJobs.running, c.Id)
			certJobs.Unlock()
			continue
		}
		if c.NotAfter > 0 && left < warnBefore && now.Sub(time.UnixMilli(c.WarnedAt)) >= 24*time.Hour {
			database.GetDB().Model(&model.Certificate{}).Where("id = ?", c.Id).Update("warned_at", now.UnixMilli())
			publishCert(eventbus.EventCertExpiring, c, nil)
		}
	}
}
