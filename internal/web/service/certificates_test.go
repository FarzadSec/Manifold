package service

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/certstore"
	"github.com/mhsanaei/3x-ui/v3/internal/certstore/acmetest"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func useFakeCA(t *testing.T) (*acmetest.CA, *acmetest.Zone) {
	t.Helper()
	ca := acmetest.New(t)
	zone := &acmetest.Zone{}
	ca.TXT = zone.Values
	prevDir, prevDNS, prevWait := ACMEDirectory, DNSProviderFor, dnsWait
	ACMEDirectory = func(bool) string { return ca.Directory() }
	DNSProviderFor = func(kind, _ string) (certstore.Provider, error) { return zone, nil }
	dnsWait = nil
	t.Cleanup(func() { ACMEDirectory, DNSProviderFor, dnsWait = prevDir, prevDNS, prevWait })
	return ca, zone
}

func waitIssued(t *testing.T, id int) model.Certificate {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var c model.Certificate
		database.GetDB().First(&c, id)
		if !c.Issuing && !issuing(id) && c.LastRun > 0 {
			return c
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the order did not finish")
	return model.Certificate{}
}

func TestIssueStoresTheFilesAndRestartsXrayForInboundsThatReadThem(t *testing.T) {
	setupBulkDB(t)
	ca, zone := useFakeCA(t)
	s := &CertificateService{}
	token := "cf-token"
	view, err := s.Save(CertificateInput{
		Domains: []string{"*.Example.com", "example.com"}, Challenge: "dns-01", DNSProvider: "cloudflare",
		DNSToken: &token, AutoRenew: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Name != "example.com" || !view.HasDNSToken {
		t.Fatalf("saved %+v", view)
	}
	database.GetDB().Create(&model.Inbound{
		UserId: 1, Enable: true, Port: 8443, Tag: "in-8443", Protocol: model.VLESS,
		Settings: `{"clients":[]}`, StreamSettings: `{"security":"tls","tlsSettings":{"certificates":[{"certificateFile":"` + view.CertFile + `","keyFile":"` + view.KeyFile + `"}]}}`,
	})
	(&XrayService{}).IsNeedRestartAndSetFalse()

	if err := s.Issue(view.Id); err != nil {
		t.Fatal(err)
	}
	c := waitIssued(t, view.Id)
	if c.LastError != "" {
		t.Fatalf("issue failed: %s", c.LastError)
	}
	certPEM, _ := os.ReadFile(view.CertFile)
	keyPEM, _ := os.ReadFile(view.KeyFile)
	info, err := certstore.Inspect(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("the files on disk are not a pair: %v", err)
	}
	if strings.Join(info.Domains, ",") != "*.example.com,example.com" || c.NotAfter != info.NotAfter.UnixMilli() {
		t.Fatalf("on disk %+v, recorded notAfter %d", info, c.NotAfter)
	}
	if !(&XrayService{}).IsNeedRestartAndSetFalse() {
		t.Fatal("an inbound reads the certificate but Xray was not asked to restart")
	}
	if zone.Left() != 0 || ca.Orders() != 1 {
		t.Fatalf("records left %d, orders %d", zone.Left(), ca.Orders())
	}
	if st, _ := os.Stat(view.KeyFile); st.Mode().Perm() != 0o600 {
		t.Fatalf("the key is readable by others: %v", st.Mode())
	}
}

func TestRenewDueRenewsNearExpiryAndBacksOffAfterAFailure(t *testing.T) {
	setupBulkDB(t)
	ca, _ := useFakeCA(t)
	s := &CertificateService{}
	token := "t"
	view, err := s.Save(CertificateInput{Domains: []string{"vpn.example.com"}, Challenge: "dns-01", DNSProvider: "arvancloud", DNSToken: &token, AutoRenew: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Issue(view.Id); err != nil {
		t.Fatal(err)
	}
	first := waitIssued(t, view.Id)
	now := time.Now()
	s.RenewDue(now)
	if ca.Orders() != 1 {
		t.Fatalf("a certificate with %s left was renewed", time.UnixMilli(first.NotAfter).Sub(now))
	}
	database.GetDB().Model(&model.Certificate{}).Where("id = ?", view.Id).Updates(map[string]any{
		"not_before": now.Add(-80 * 24 * time.Hour).UnixMilli(), "not_after": now.Add(10 * 24 * time.Hour).UnixMilli(),
	})
	s.RenewDue(now)
	if ca.Orders() != 2 {
		t.Fatalf("orders after a due renewal: %d", ca.Orders())
	}
	var renewed model.Certificate
	database.GetDB().First(&renewed, view.Id)
	if renewed.NotAfter <= now.Add(10*24*time.Hour).UnixMilli() || renewed.CertPEM == first.CertPEM {
		t.Fatal("the renewal did not replace the certificate")
	}

	database.GetDB().Model(&model.Certificate{}).Where("id = ?", view.Id).Updates(map[string]any{
		"not_after": now.Add(time.Hour).UnixMilli(), "last_error": "rate limited", "last_run": now.Add(-time.Hour).UnixMilli(),
	})
	s.RenewDue(now)
	if ca.Orders() != 2 {
		t.Fatal("a renewal that failed an hour ago was tried again at once")
	}
	s.RenewDue(now.Add(retryFailedEach))
	if ca.Orders() != 3 {
		t.Fatal("a failed renewal was never tried again")
	}
}

func selfSigned(t *testing.T, cn string) (certPEM, keyPEM []byte) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: cn}, DNSNames: []string{cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(48 * time.Hour),
	}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	kder, _ := x509.MarshalPKCS8PrivateKey(key)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kder})
}

func TestSaveRefusesAWildcardWithoutDNSAndUploadChecksThePair(t *testing.T) {
	setupBulkDB(t)
	s := &CertificateService{}
	if _, err := s.Save(CertificateInput{Domains: []string{"*.example.com"}, Challenge: "http-01"}); err == nil || !strings.Contains(err.Error(), "only DNS-01") {
		t.Fatalf("a wildcard over HTTP-01 was saved: %v", err)
	}
	if _, err := s.Save(CertificateInput{Domains: []string{"example.com"}, Challenge: "dns-01", DNSProvider: "cloudflare"}); err == nil {
		t.Fatal("DNS-01 was saved without a token")
	}
	certA, keyA := selfSigned(t, "own.example.com")
	_, keyB := selfSigned(t, "own.example.com")
	if _, err := s.Upload(CertificateUpload{Cert: string(certA), Key: string(keyB)}); err == nil || !strings.Contains(err.Error(), "do not make a pair") {
		t.Fatalf("a certificate with another key was taken: %v", err)
	}
	view, err := s.Upload(CertificateUpload{Cert: string(certA), Key: string(keyA)})
	if err != nil {
		t.Fatal(err)
	}
	if view.Name != "own.example.com" || view.Source != "upload" || strings.Join(view.Domains, ",") != "own.example.com" {
		t.Fatalf("uploaded %+v", view)
	}
	if onDisk, _ := os.ReadFile(view.CertFile); string(onDisk) != string(certA) {
		t.Fatal("the uploaded certificate is not where inbounds read it")
	}
	if _, err := s.Save(CertificateInput{Id: view.Id, Domains: []string{"own.example.com"}, Challenge: "http-01"}); err == nil {
		t.Fatal("an uploaded certificate was turned into an ACME one in place")
	}
}

// The CLI's way back into a panel without a domain: an IP certificate it serves at once,
// renewed by the job within the 160 hours it lasts rather than on the 90-day schedule.
func TestIssueForPanelServesAnIPCertificateAndRenewsItWithinItsShortLife(t *testing.T) {
	setupBulkDB(t)
	ca, _ := useFakeCA(t)
	port := freeTCPPort(t)
	ca.Reach = func(string) string { return net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) }
	s := &CertificateService{}
	in := CertificateInput{Domains: []string{"203.0.113.9"}, Challenge: "http-01", Port: port, AutoRenew: true}
	view, err := s.IssueForPanel(in)
	if err != nil {
		t.Fatal(err)
	}
	settings := &SettingService{}
	certFile, _ := settings.GetCertFile()
	keyFile, _ := settings.GetKeyFile()
	if certFile != view.CertFile || keyFile != view.KeyFile {
		t.Fatalf("the panel serves %s / %s, the store keeps %s / %s", certFile, keyFile, view.CertFile, view.KeyFile)
	}
	certPEM, _ := os.ReadFile(certFile)
	keyPEM, _ := os.ReadFile(keyFile)
	info, err := certstore.Inspect(certPEM, keyPEM)
	if err != nil || strings.Join(info.Domains, ",") != "203.0.113.9" {
		t.Fatalf("the panel's files hold %+v (%v)", info, err)
	}

	again, err := s.IssueForPanel(in)
	if err != nil || again.Id != view.Id || ca.Orders() != 2 {
		t.Fatalf("issuing again for the same address gave id %d (want %d), %d orders, %v", again.Id, view.Id, ca.Orders(), err)
	}

	issued := time.UnixMilli(again.NotBefore)
	s.RenewDue(issued.Add(time.Hour))
	var c model.Certificate
	database.GetDB().First(&c, view.Id)
	if ca.Orders() != 2 || c.WarnedAt != 0 {
		t.Fatalf("a 160-hour certificate an hour old was renewed (%d orders) or warned about (%d)", ca.Orders(), c.WarnedAt)
	}
	s.RenewDue(issued.Add(110 * time.Hour))
	if ca.Orders() != 3 {
		t.Fatal("a 160-hour certificate with 50 hours left was not renewed")
	}
}

func TestSaveTakesAnIPAddressOverHTTP01Only(t *testing.T) {
	setupBulkDB(t)
	s := &CertificateService{}
	if _, err := s.Save(CertificateInput{Domains: []string{"203.0.113.9"}, Challenge: "tls-alpn-01"}); err == nil || !strings.Contains(err.Error(), "only HTTP-01") {
		t.Fatalf("an IP address over TLS-ALPN-01 was saved: %v", err)
	}
	view, err := s.Save(CertificateInput{Domains: []string{"2001:DB8:0::9"}, Challenge: "http-01"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Name != "2001-db8--9" || strings.Join(view.Domains, ",") != "2001:db8::9" {
		t.Fatalf("saved %q for %v", view.Name, view.Domains)
	}
}
