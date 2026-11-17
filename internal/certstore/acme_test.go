package certstore

import (
	"context"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/certstore/acmetest"
)

func issueWith(t *testing.T, ca *acmetest.CA, solver Solver, domains ...string) (Result, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return Issue(ctx, Request{Directory: ca.Directory(), Email: "ops@example.com", Domains: domains, Solver: solver})
}

func TestIssueProvesEachChallengeType(t *testing.T) {
	t.Run("http-01", func(t *testing.T) {
		ca := acmetest.New(t)
		solver := &HTTP01{Addr: "127.0.0.1:0"}
		ca.Reach = func(string) string { return solver.BoundAddr().String() }
		res, err := issueWith(t, ca, solver, "panel.example.com", "sub.example.com")
		if err != nil {
			t.Fatal(err)
		}
		info, err := Inspect(res.CertPEM, res.KeyPEM)
		if err != nil {
			t.Fatalf("the chain and key do not belong together: %v", err)
		}
		if !slices.Equal(info.Domains, []string{"panel.example.com", "sub.example.com"}) || info.Issuer != "Fake CA" {
			t.Fatalf("issued %+v", info)
		}
		if strings.Count(string(res.CertPEM), "BEGIN CERTIFICATE") != 2 {
			t.Fatal("the full chain was not kept")
		}
		if _, err := (&net.Dialer{Timeout: time.Second}).Dial("tcp", solver.BoundAddr().String()); err == nil {
			t.Fatal("the HTTP-01 listener is still open after the order")
		}
	})
	t.Run("tls-alpn-01", func(t *testing.T) {
		ca := acmetest.New(t)
		solver := &TLSALPN01{Addr: "127.0.0.1:0"}
		ca.Reach = func(string) string { return solver.BoundAddr().String() }
		res, err := issueWith(t, ca, solver, "edge.example.com")
		if err != nil {
			t.Fatal(err)
		}
		if info, _ := Inspect(res.CertPEM, res.KeyPEM); !slices.Equal(info.Domains, []string{"edge.example.com"}) {
			t.Fatalf("issued %+v", info)
		}
	})
	t.Run("dns-01 with a wildcard", func(t *testing.T) {
		ca := acmetest.New(t)
		zone := &acmetest.Zone{}
		ca.TXT = zone.Values
		res, err := issueWith(t, ca, &DNS01{Provider: zone}, "*.example.com", "example.com")
		if err != nil {
			t.Fatal(err)
		}
		if info, _ := Inspect(res.CertPEM, res.KeyPEM); !slices.Equal(info.Domains, []string{"*.example.com", "example.com"}) {
			t.Fatalf("issued %+v", info)
		}
		if zone.Left() != 0 {
			t.Fatalf("challenge records were left in the zone: %d", zone.Left())
		}
		if got := ca.Accepted(); !slices.Equal(got, []string{"dns-01", "dns-01"}) {
			t.Fatalf("challenges answered: %v", got)
		}
	})
}

func TestIssueRefusesAWildcardWithoutDNSAndReportsAFailedProof(t *testing.T) {
	ca := acmetest.New(t)
	if _, err := issueWith(t, ca, &HTTP01{Addr: "127.0.0.1:0"}, "*.example.com"); err == nil || !strings.Contains(err.Error(), "only DNS-01") {
		t.Fatalf("a wildcard over HTTP-01 gave %v", err)
	}
	if ca.Orders() != 0 {
		t.Fatal("the CA was asked before the request was checked")
	}
	gone, _ := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	gone.Close()
	ca.Reach = func(string) string { return gone.Addr().String() }
	if _, err := issueWith(t, ca, &HTTP01{Addr: "127.0.0.1:0"}, "panel.example.com"); err == nil || !strings.Contains(err.Error(), "could not verify panel.example.com") {
		t.Fatalf("a challenge the CA cannot reach gave %v", err)
	}
}

// Let's Encrypt only issues an IP address under the shortlived profile, proven over
// HTTP-01, with the address as an IP SAN and never as the common name.
func TestIssueGetsAnIPAddressCertificate(t *testing.T) {
	ca := acmetest.New(t)
	solver := &HTTP01{Addr: "127.0.0.1:0"}
	ca.Reach = func(string) string { return solver.BoundAddr().String() }
	res, err := issueWith(t, ca, solver, "203.0.113.7", "2001:db8::7")
	if err != nil {
		t.Fatal(err)
	}
	info, err := Inspect(res.CertPEM, res.KeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(info.Domains, []string{"203.0.113.7", "2001:db8::7"}) {
		t.Fatalf("the certificate names %v", info.Domains)
	}
	if life := info.NotAfter.Sub(info.NotBefore); life > 7*24*time.Hour {
		t.Fatalf("an IP certificate lasts %v; the CA gave the classic profile", life)
	}
	if got := ca.Profiles(); !slices.Equal(got, []string{"shortlived"}) {
		t.Fatalf("orders asked for profiles %v", got)
	}

	for _, solver := range []Solver{&TLSALPN01{Addr: "127.0.0.1:0"}, &DNS01{Provider: &acmetest.Zone{}}} {
		if _, err := issueWith(t, ca, solver, "panel.example.com", "203.0.113.7"); err == nil || !strings.Contains(err.Error(), "only HTTP-01") {
			t.Fatalf("an IP address over %s gave %v", solver.Type(), err)
		}
	}
	if ca.Orders() != 1 {
		t.Fatal("the CA was asked before the request was checked")
	}
}
