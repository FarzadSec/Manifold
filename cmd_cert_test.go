package main

import (
	"context"
	"net"
	"slices"
	"strconv"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/certstore/acmetest"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

func panelCert(t *testing.T) (string, string) {
	t.Helper()
	s := &service.SettingService{}
	certFile, err := s.GetCertFile()
	if err != nil {
		t.Fatal(err)
	}
	keyFile, _ := s.GetKeyFile()
	return certFile, keyFile
}

// `manifold cert` with no flags used to set both files to "", dropping the panel to HTTP.
func TestCertCommandWithoutFlagsLeavesThePanelCertificateAlone(t *testing.T) {
	newTokenCLIEnv(t)
	if code := runCertCLI([]string{"-webCert", "/root/cert/fullchain.pem", "-webCertKey", "/root/cert/privkey.pem"}); code != 0 {
		t.Fatalf("setting the files exited %d", code)
	}
	if code := runCertCLI(nil); code != 2 {
		t.Fatalf("no flags exited %d, want the usage (2)", code)
	}
	if certFile, keyFile := panelCert(t); certFile != "/root/cert/fullchain.pem" || keyFile != "/root/cert/privkey.pem" {
		t.Fatalf("no flags changed the panel's certificate to %q / %q", certFile, keyFile)
	}
	if code := runCertCLI([]string{"-reset"}); code != 0 {
		t.Fatalf("-reset exited %d", code)
	}
	if certFile, keyFile := panelCert(t); certFile != "" || keyFile != "" {
		t.Fatalf("-reset left %q / %q", certFile, keyFile)
	}
}

func TestCertIssueGetsAnHTTP01CertificateThatRenewsAndServesThePanel(t *testing.T) {
	newTokenCLIEnv(t)
	ca := acmetest.New(t)
	prev := service.ACMEDirectory
	service.ACMEDirectory = func(bool) string { return ca.Directory() }
	t.Cleanup(func() { service.ACMEDirectory = prev })
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	ca.Reach = func(string) string { return net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) }

	code := runCertCLI([]string{"issue", "-domain", "panel.example.com, 203.0.113.9", "-port", strconv.Itoa(port)})
	if code != 0 {
		t.Fatalf("cert issue exited %d", code)
	}
	var c model.Certificate
	if err := database.GetDB().Where("name = ?", "panel.example.com").First(&c).Error; err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.Domains, []string{"panel.example.com", "203.0.113.9"}) || c.Challenge != "http-01" || !c.AutoRenew || c.CertPEM == "" {
		t.Fatalf("stored %v over %s, autoRenew %v, issued %v", c.Domains, c.Challenge, c.AutoRenew, c.CertPEM != "")
	}
	if certFile, _ := panelCert(t); certFile == "" {
		t.Fatal("the panel does not serve the issued certificate")
	}
}
