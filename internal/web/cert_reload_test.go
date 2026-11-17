package web

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writePair(t *testing.T, dir, cn string) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{cn},
	}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	kder, _ := x509.MarshalPKCS8PrivateKey(key)
	_ = os.WriteFile(filepath.Join(dir, "c.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "k.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kder}), 0o600)
}

func servedName(t *testing.T, r *certReloader) string {
	t.Helper()
	c, err := r.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(c.Certificate[0])
	return leaf.Subject.CommonName
}

func TestPanelServesARenewedCertificateWithoutARestart(t *testing.T) {
	dir := t.TempDir()
	writePair(t, dir, "old.example.com")
	r, err := newCertReloader(filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem"))
	if err != nil {
		t.Fatal(err)
	}
	r.every = 0
	time.Sleep(10 * time.Millisecond)
	writePair(t, dir, "new.example.com")
	if got := servedName(t, r); got != "new.example.com" {
		t.Fatalf("served %s after the files changed", got)
	}
	time.Sleep(10 * time.Millisecond)
	_ = os.WriteFile(filepath.Join(dir, "k.pem"), []byte("half written"), 0o600)
	if got := servedName(t, r); got != "new.example.com" {
		t.Fatalf("a broken key pair replaced the working one: served %s", got)
	}
}
