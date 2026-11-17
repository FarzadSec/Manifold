// Package certstore issues certificates over ACME and keeps them as PEM files that
// inbounds and the panel read by path.
package certstore

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	CertFile = "fullchain.pem"
	KeyFile  = "privkey.pem"
)

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,62}$`)

// ValidName reports whether name can be a certificate's folder.
func ValidName(name string) bool {
	return validName.MatchString(name) && !strings.Contains(name, "..")
}

// Paths are the files a certificate is kept in under root.
func Paths(root, name string) (cert, key string) {
	dir := filepath.Join(root, name)
	return filepath.Join(dir, CertFile), filepath.Join(dir, KeyFile)
}

// Write stores a certificate and key; each file is replaced whole, so a reader never
// sees half of one.
func Write(root, name string, certPEM, keyPEM []byte) error {
	if !ValidName(name) {
		return errors.New("invalid certificate name")
	}
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	certPath, keyPath := Paths(root, name)
	if err := replace(keyPath, keyPEM, 0o600); err != nil {
		return err
	}
	return replace(certPath, certPEM, 0o644)
}

func replace(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".part"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Remove deletes a certificate's folder.
func Remove(root, name string) error {
	if !ValidName(name) {
		return errors.New("invalid certificate name")
	}
	return os.RemoveAll(filepath.Join(root, name))
}

// Info is what a certificate says about itself.
type Info struct {
	Domains   []string
	NotBefore time.Time
	NotAfter  time.Time
	Issuer    string
}

// Inspect parses a chain and its key and checks that they belong together.
func Inspect(certPEM, keyPEM []byte) (Info, error) {
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return Info{}, err
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return Info{}, err
	}
	domains := leaf.DNSNames
	if len(domains) == 0 && leaf.Subject.CommonName != "" {
		domains = []string{leaf.Subject.CommonName}
	}
	issuer := leaf.Issuer.CommonName
	if issuer == "" && len(leaf.Issuer.Organization) > 0 {
		issuer = leaf.Issuer.Organization[0]
	}
	return Info{Domains: domains, NotBefore: leaf.NotBefore, NotAfter: leaf.NotAfter, Issuer: issuer}, nil
}

// pemBlocks encodes DER blocks of one type.
func pemBlocks(kind string, ders ...[]byte) []byte {
	var out []byte
	for _, d := range ders {
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: d})...)
	}
	return out
}
