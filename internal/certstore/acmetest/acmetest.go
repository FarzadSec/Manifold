// Package acmetest is a small ACME CA for tests: enough of RFC 8555 for
// golang.org/x/crypto/acme, and it really checks each challenge.
package acmetest

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme"
)

// TB is the part of testing.TB the CA reports through.
type TB interface {
	Helper()
	Errorf(format string, args ...any)
	Fatal(args ...any)
	Logf(format string, args ...any)
	Cleanup(func())
}

// CA is the fake certificate authority.
type CA struct {
	t       TB
	srv     *httptest.Server
	caCert  *x509.Certificate
	caKey   *ecdsa.PrivateKey
	account crypto.PublicKey

	// Reach answers where a domain's HTTP-01 / TLS-ALPN-01 solver listens.
	Reach func(domain string) string
	// TXT is what the zone holds, for DNS-01.
	TXT func(fqdn string) []string

	mu       sync.Mutex
	authzs   []*fakeAuthz
	ordered  []string
	csrNames []string
	accepted []string
	leafDER  []byte
	orders   int
	body     []byte
}

type fakeAuthz struct {
	domain   string
	wildcard bool
	token    string
	status   string
}

// New starts a CA that lives until the test ends.
func New(t TB) *CA {
	t.Helper()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Fake CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &caKey.PublicKey, caKey)
	caCert, _ := x509.ParseCertificate(der)
	ca := &CA{t: t, caCert: caCert, caKey: caKey}
	ca.srv = httptest.NewServer(http.HandlerFunc(ca.serve))
	t.Cleanup(ca.srv.Close)
	return ca
}

func (ca *CA) url(p string) string { return ca.srv.URL + p }

// Directory is the URL an ACME client starts from.
func (ca *CA) Directory() string { return ca.srv.URL + "/dir" }

// Orders counts the orders the CA was asked for.
func (ca *CA) Orders() int {
	ca.mu.Lock()
	defer ca.mu.Unlock()
	return ca.orders
}

// Accepted is the challenge types answered, in order.
func (ca *CA) Accepted() []string {
	ca.mu.Lock()
	defer ca.mu.Unlock()
	return append([]string{}, ca.accepted...)
}

// Ordered is every identifier ever ordered.
func (ca *CA) Ordered() []string {
	ca.mu.Lock()
	defer ca.mu.Unlock()
	return append([]string{}, ca.ordered...)
}

// LeafLife is how long the certificates this CA issues last.
var LeafLife = 90 * 24 * time.Hour

func (ca *CA) jws() (protected map[string]any, payload []byte) {
	var jws struct{ Protected, Payload string }
	if err := json.Unmarshal(ca.body, &jws); err != nil {
		ca.t.Errorf("bad JWS: %s", ca.body)
	}
	head, _ := base64.RawURLEncoding.DecodeString(jws.Protected)
	_ = json.Unmarshal(head, &protected)
	payload, _ = base64.RawURLEncoding.DecodeString(jws.Payload)
	return protected, payload
}

func (ca *CA) payload(*http.Request) []byte {
	_, p := ca.jws()
	return p
}

// jwk is the account's public key, from the jwk the registration is signed with.
func (ca *CA) jwk(*http.Request) crypto.PublicKey {
	head, _ := ca.jws()
	k, _ := head["jwk"].(map[string]any)
	x, _ := base64.RawURLEncoding.DecodeString(fmt.Sprint(k["x"]))
	y, _ := base64.RawURLEncoding.DecodeString(fmt.Sprint(k["y"]))
	if k["kty"] != "EC" || k["crv"] != "P-256" {
		ca.t.Errorf("account key %v is not P-256", k)
	}
	return &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
}

func (ca *CA) keyAuth(token string) string {
	thumb, err := acme.JWKThumbprint(ca.account)
	if err != nil {
		ca.t.Fatal(err)
	}
	return token + "." + thumb
}

func (ca *CA) writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (ca *CA) authzJSON(i int) map[string]any {
	a := ca.authzs[i]
	chals := []map[string]any{}
	types := []string{"http-01", "tls-alpn-01", "dns-01"}
	if a.wildcard {
		types = []string{"dns-01"}
	}
	for _, typ := range types {
		chals = append(chals, map[string]any{"type": typ, "url": ca.url(fmt.Sprintf("/chal/%d/%s", i, typ)), "token": a.token, "status": a.status})
	}
	return map[string]any{
		"identifier": map[string]string{"type": "dns", "value": a.domain},
		"status":     a.status, "wildcard": a.wildcard, "challenges": chals,
	}
}

func (ca *CA) orderJSON(status string) map[string]any {
	var urls []string
	for i := range ca.authzs {
		urls = append(urls, ca.url(fmt.Sprintf("/authz/%d", i)))
	}
	o := map[string]any{"status": status, "authorizations": urls, "finalize": ca.url("/finalize")}
	if status == "valid" {
		o["certificate"] = ca.url("/cert")
	}
	return o
}

func (ca *CA) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Replay-Nonce", fmt.Sprintf("n%d", time.Now().UnixNano()))
	body, _ := io.ReadAll(r.Body)
	ca.mu.Lock()
	defer ca.mu.Unlock()
	ca.body = body
	switch p := r.URL.Path; {
	case p == "/dir":
		ca.writeJSON(w, 200, map[string]any{
			"newNonce": ca.url("/nonce"), "newAccount": ca.url("/acct"), "newOrder": ca.url("/order"),
			"revokeCert": ca.url("/revoke"), "keyChange": ca.url("/kc"),
			"meta": map[string]any{"termsOfService": ca.url("/tos")},
		})
	case p == "/nonce":
		w.WriteHeader(200)
	case p == "/acct":
		ca.account = ca.jwk(r)
		w.Header().Set("Location", ca.url("/acct/1"))
		ca.writeJSON(w, 201, map[string]any{"status": "valid"})
	case p == "/order" && r.Method == http.MethodPost:
		ca.orders++
		var req struct{ Identifiers []struct{ Value string } }
		_ = json.Unmarshal(ca.payload(r), &req)
		ca.authzs = nil
		for i, id := range req.Identifiers {
			ca.ordered = append(ca.ordered, id.Value)
			wild := strings.HasPrefix(id.Value, "*.")
			ca.authzs = append(ca.authzs, &fakeAuthz{domain: strings.TrimPrefix(id.Value, "*."), wildcard: wild, token: fmt.Sprintf("tok%d", i), status: "pending"})
		}
		w.Header().Set("Location", ca.url("/order/1"))
		ca.writeJSON(w, 201, ca.orderJSON("pending"))
	case p == "/order/1":
		status := "ready"
		for _, a := range ca.authzs {
			if a.status != "valid" {
				status = "pending"
			}
		}
		ca.writeJSON(w, 200, ca.orderJSON(status))
	case strings.HasPrefix(p, "/authz/"):
		var i int
		_, _ = fmt.Sscanf(p, "/authz/%d", &i)
		ca.writeJSON(w, 200, ca.authzJSON(i))
	case strings.HasPrefix(p, "/chal/"):
		var i int
		var typ string
		_, _ = fmt.Sscanf(strings.Replace(p, "/", " ", 3), " chal %d %s", &i, &typ)
		a := ca.authzs[i]
		ca.accepted = append(ca.accepted, typ)
		ca.mu.Unlock()
		err := ca.verify(typ, a)
		ca.mu.Lock()
		a.status = "valid"
		if err != nil {
			ca.t.Logf("challenge %s for %s failed: %v", typ, a.domain, err)
			a.status = "invalid"
		}
		ca.writeJSON(w, 200, map[string]any{"type": typ, "url": ca.url(p), "token": a.token, "status": a.status})
	case p == "/finalize":
		var req struct{ CSR string }
		_ = json.Unmarshal(ca.payload(r), &req)
		der, _ := base64.RawURLEncoding.DecodeString(req.CSR)
		csr, err := x509.ParseCertificateRequest(der)
		if err != nil {
			ca.t.Errorf("bad CSR: %v", err)
			return
		}
		ca.csrNames = csr.DNSNames
		w.Header().Set("Location", ca.url("/order/1"))
		ca.writeJSON(w, 200, ca.orderJSON("valid"))
		ca.issued(csr)
	case p == "/cert":
		w.Header().Set("Content-Type", "application/pem-certificate-chain")
		for _, der := range [][]byte{ca.leafDER, ca.caCert.Raw} {
			_, _ = w.Write(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
		}
	default:
		ca.t.Errorf("fake CA: unexpected %s %s", r.Method, p)
		w.WriteHeader(404)
	}
}

func (ca *CA) issued(csr *x509.CertificateRequest) {
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: csr.Subject.CommonName},
		DNSNames: csr.DNSNames, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(LeafLife),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.caCert, csr.PublicKey, ca.caKey)
	if err != nil {
		ca.t.Fatal(err)
	}
	ca.leafDER = der
}

var idPeACMEIdentifier = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 1, 31}

func (ca *CA) verify(typ string, a *fakeAuthz) error {
	want := ca.keyAuth(a.token)
	switch typ {
	case "http-01":
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+ca.Reach(a.domain)+"/.well-known/acme-challenge/"+a.token, nil)
		req.Host = a.domain
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		got, _ := io.ReadAll(resp.Body)
		if string(got) != want {
			return fmt.Errorf("served %q, want %q", got, want)
		}
	case "tls-alpn-01":
		raw, err := (&tls.Dialer{Config: &tls.Config{
			ServerName: a.domain, NextProtos: []string{acme.ALPNProto}, InsecureSkipVerify: true,
		}}).DialContext(context.Background(), "tcp", ca.Reach(a.domain))
		if err != nil {
			return err
		}
		conn := raw.(*tls.Conn)
		defer conn.Close()
		st := conn.ConnectionState()
		if st.NegotiatedProtocol != acme.ALPNProto {
			return fmt.Errorf("negotiated %q", st.NegotiatedProtocol)
		}
		digest := sha256.Sum256([]byte(want))
		for _, ext := range st.PeerCertificates[0].Extensions {
			var got []byte
			if ext.Id.Equal(idPeACMEIdentifier) && ext.Critical {
				if _, err := asn1.Unmarshal(ext.Value, &got); err == nil && string(got) == string(digest[:]) {
					return nil
				}
			}
		}
		return fmt.Errorf("no acmeIdentifier for %s", want)
	case "dns-01":
		digest := sha256.Sum256([]byte(want))
		value := base64.RawURLEncoding.EncodeToString(digest[:])
		for _, v := range ca.TXT("_acme-challenge." + a.domain) {
			if v == value {
				return nil
			}
		}
		return fmt.Errorf("no TXT %s at _acme-challenge.%s", value, a.domain)
	}
	return nil
}
