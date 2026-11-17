package certstore

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme"

	"github.com/mhsanaei/3x-ui/v3/internal/web/network"
)

const (
	LetsEncrypt        = "https://acme-v02.api.letsencrypt.org/directory"
	LetsEncryptStaging = "https://acme-staging-v02.api.letsencrypt.org/directory"
)

// Solver answers one kind of challenge for the domains of an order.
type Solver interface {
	Type() string
	Present(ctx context.Context, client *acme.Client, domain string, chal *acme.Challenge) error
	CleanUp(ctx context.Context, client *acme.Client, domain string, chal *acme.Challenge)
	Close()
}

// Request is one certificate to issue.
type Request struct {
	Directory  string
	AccountKey crypto.Signer
	Email      string
	Domains    []string
	Solver     Solver
	HTTPClient *http.Client
	Log        func(string)
}

// Result is the issued chain, its new key, and the account key to reuse next time.
type Result struct {
	CertPEM, KeyPEM, AccountKeyPEM []byte
}

// Issue runs one ACME order to the end: account, authorizations, finalize, chain.
func Issue(ctx context.Context, req Request) (Result, error) {
	if len(req.Domains) == 0 {
		return Result{}, errors.New("a certificate needs at least one domain")
	}
	if req.Solver == nil {
		return Result{}, errors.New("no challenge solver")
	}
	for _, d := range req.Domains {
		if strings.HasPrefix(d, "*.") && req.Solver.Type() != "dns-01" {
			return Result{}, fmt.Errorf("%s is a wildcard, which only DNS-01 can prove", d)
		}
	}
	defer req.Solver.Close()
	log := req.Log
	if log == nil {
		log = func(string) {}
	}
	key := req.AccountKey
	if key == nil {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return Result{}, err
		}
		key = k
	}
	accountPEM, err := marshalKey(key)
	if err != nil {
		return Result{}, err
	}
	client := &acme.Client{Key: key, DirectoryURL: req.Directory, HTTPClient: req.HTTPClient, UserAgent: "manifold"}
	acct := &acme.Account{}
	if req.Email != "" {
		acct.Contact = []string{"mailto:" + req.Email}
	}
	if _, err := client.Register(ctx, acct, acme.AcceptTOS); err != nil && !errors.Is(err, acme.ErrAccountAlreadyExists) {
		return Result{}, fmt.Errorf("register the ACME account: %w", err)
	}
	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(req.Domains...))
	if err != nil {
		return Result{}, fmt.Errorf("open the order: %w", err)
	}
	for _, u := range order.AuthzURLs {
		if err := authorize(ctx, client, u, req.Solver, log); err != nil {
			return Result{}, err
		}
	}
	if order, err = client.WaitOrder(ctx, order.URI); err != nil {
		return Result{}, fmt.Errorf("wait for the order: %w", err)
	}
	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Result{}, err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: strings.TrimPrefix(req.Domains[0], "*.")},
		DNSNames: req.Domains,
	}, certKey)
	if err != nil {
		return Result{}, err
	}
	chain, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return Result{}, fmt.Errorf("finalize the order: %w", err)
	}
	keyPEM, err := marshalKey(certKey)
	if err != nil {
		return Result{}, err
	}
	log("issued for " + strings.Join(req.Domains, ", "))
	return Result{CertPEM: pemBlocks("CERTIFICATE", chain...), KeyPEM: keyPEM, AccountKeyPEM: accountPEM}, nil
}

func authorize(ctx context.Context, client *acme.Client, authzURL string, solver Solver, log func(string)) error {
	authz, err := client.GetAuthorization(ctx, authzURL)
	if err != nil {
		return err
	}
	if authz.Status == acme.StatusValid {
		return nil
	}
	domain := authz.Identifier.Value
	if authz.Wildcard {
		domain = "*." + domain
	}
	var chal *acme.Challenge
	for _, c := range authz.Challenges {
		if c.Type == solver.Type() {
			chal = c
			break
		}
	}
	if chal == nil {
		return fmt.Errorf("the CA offers no %s challenge for %s", solver.Type(), domain)
	}
	log(fmt.Sprintf("proving %s with %s", domain, chal.Type))
	if err := solver.Present(ctx, client, domain, chal); err != nil {
		return fmt.Errorf("%s for %s: %w", chal.Type, domain, err)
	}
	defer solver.CleanUp(context.WithoutCancel(ctx), client, domain, chal)
	if _, err := client.Accept(ctx, chal); err != nil {
		return fmt.Errorf("accept %s for %s: %w", chal.Type, domain, err)
	}
	if _, err := client.WaitAuthorization(ctx, authz.URI); err != nil {
		return fmt.Errorf("the CA could not verify %s: %w", domain, err)
	}
	return nil
}

func marshalKey(key crypto.Signer) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

// ParseKey reads a PEM private key written by Issue or an older PKCS#1/SEC 1 one.
func ParseKey(raw []byte) (crypto.Signer, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("no PEM key")
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if s, ok := k.(crypto.Signer); ok {
			return s, nil
		}
	}
	if k, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	return nil, errors.New("unsupported key")
}

// HTTP01 serves /.well-known/acme-challenge/ on Addr (":80" for the real thing) while
// an order runs.
type HTTP01 struct {
	Addr string

	mu     sync.Mutex
	tokens map[string]string
	srv    *http.Server
	bound  net.Addr
}

func (s *HTTP01) Type() string { return "http-01" }

// BoundAddr is where the solver listens once a challenge is presented.
func (s *HTTP01) BoundAddr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bound
}

func (s *HTTP01) Present(ctx context.Context, client *acme.Client, _ string, chal *acme.Challenge) error {
	answer, err := client.HTTP01ChallengeResponse(chal.Token)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tokens == nil {
		s.tokens = map[string]string{}
	}
	s.tokens[client.HTTP01ChallengePath(chal.Token)] = answer
	if s.srv != nil {
		return nil
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s for HTTP-01 (is something else on it?): %w", s.Addr, err)
	}
	s.bound = ln.Addr()
	s.srv = &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		answer, ok := s.tokens[r.URL.Path]
		s.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(answer))
	})}
	go network.ServeHTTP(s.srv, ln, "ACME HTTP-01 responder")
	return nil
}

func (s *HTTP01) CleanUp(_ context.Context, client *acme.Client, _ string, chal *acme.Challenge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, client.HTTP01ChallengePath(chal.Token))
}

func (s *HTTP01) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.srv != nil {
		_ = s.srv.Close()
		s.srv = nil
	}
}

// TLSALPN01 answers acme-tls/1 handshakes on Addr (":443" for the real thing).
type TLSALPN01 struct {
	Addr string

	mu    sync.Mutex
	certs map[string]*tls.Certificate
	ln    net.Listener
	bound net.Addr
}

func (s *TLSALPN01) Type() string { return "tls-alpn-01" }

func (s *TLSALPN01) BoundAddr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bound
}

func (s *TLSALPN01) Present(ctx context.Context, client *acme.Client, domain string, chal *acme.Challenge) error {
	cert, err := client.TLSALPN01ChallengeCert(chal.Token, domain)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.certs == nil {
		s.certs = map[string]*tls.Certificate{}
	}
	s.certs[domain] = &cert
	if s.ln != nil {
		return nil
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s for TLS-ALPN-01 (is something else on it?): %w", s.Addr, err)
	}
	s.ln, s.bound = ln, ln.Addr()
	cfg := &tls.Config{
		NextProtos: []string{acme.ALPNProto},
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if c, ok := s.certs[hello.ServerName]; ok {
				return c, nil
			}
			return nil, errors.New("no challenge for " + hello.ServerName)
		},
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				tc := tls.Server(conn, cfg)
				_ = tc.HandshakeContext(ctx)
				_ = tc.Close()
			}()
		}
	}()
	return nil
}

func (s *TLSALPN01) CleanUp(_ context.Context, _ *acme.Client, domain string, _ *acme.Challenge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.certs, domain)
}

func (s *TLSALPN01) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln != nil {
		_ = s.ln.Close()
		s.ln = nil
	}
}

// DNS01 writes the challenge record through a DNS provider; Wait holds the order until
// the record can be seen, and is skipped when nil.
type DNS01 struct {
	Provider Provider
	Wait     func(ctx context.Context, fqdn, value string) error

	mu  sync.Mutex
	ids map[string]string
}

func (s *DNS01) Type() string { return "dns-01" }

func challengeName(domain string) string {
	return "_acme-challenge." + strings.TrimPrefix(domain, "*.")
}

func (s *DNS01) Present(ctx context.Context, client *acme.Client, domain string, chal *acme.Challenge) error {
	value, err := client.DNS01ChallengeRecord(chal.Token)
	if err != nil {
		return err
	}
	fqdn := challengeName(domain)
	id, err := s.Provider.Present(ctx, fqdn, value)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.ids == nil {
		s.ids = map[string]string{}
	}
	s.ids[chal.Token] = id
	s.mu.Unlock()
	if s.Wait != nil {
		return s.Wait(ctx, fqdn, value)
	}
	return nil
}

func (s *DNS01) CleanUp(ctx context.Context, _ *acme.Client, domain string, chal *acme.Challenge) {
	s.mu.Lock()
	id, ok := s.ids[chal.Token]
	delete(s.ids, chal.Token)
	s.mu.Unlock()
	if ok {
		_ = s.Provider.CleanUp(ctx, challengeName(domain), id)
	}
}

func (s *DNS01) Close() {}

// PublicResolvers are asked whether a challenge record can be seen yet.
var PublicResolvers = []string{"1.1.1.1:53", "8.8.8.8:53"}

// WaitTXT polls PublicResolvers until each sees value at fqdn, or gives up after limit
// and lets the CA try anyway.
func WaitTXT(limit time.Duration) func(ctx context.Context, fqdn, value string) error {
	return func(ctx context.Context, fqdn, value string) error {
		ctx, cancel := context.WithTimeout(ctx, limit)
		defer cancel()
		for {
			seen := 0
			for _, server := range PublicResolvers {
				r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
					return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, server)
				}}
				values, _ := r.LookupTXT(ctx, fqdn)
				for _, v := range values {
					if v == value {
						seen++
						break
					}
				}
			}
			if seen == len(PublicResolvers) {
				return nil
			}
			select {
			case <-ctx.Done():
				if errors.Is(context.Cause(ctx), context.Canceled) {
					return ctx.Err()
				}
				return nil
			case <-time.After(5 * time.Second):
			}
		}
	}
}
