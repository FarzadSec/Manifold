package openvpn

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

const (
	// Profiles pin this name, so a certificate from the same CA with another name is refused.
	serverCommonName = "manifold-openvpn-server"
	caCommonName     = "Manifold OpenVPN CA"

	certificateLifetime = 10 * 365 * 24 * time.Hour

	staticKeyHeader = "-----BEGIN OpenVPN Static key V1-----"
	staticKeyFooter = "-----END OpenVPN Static key V1-----"
	staticKeyBytes  = 256
)

type pkiMaterial struct {
	caCert, caKey, serverCert, serverKey string
}

func generatePKI() (pkiMaterial, error) {
	now := time.Now()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return pkiMaterial{}, err
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: caCommonName},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(certificateLifetime),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return pkiMaterial{}, err
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return pkiMaterial{}, err
	}

	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return pkiMaterial{}, err
	}
	serverTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: serverCommonName},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(certificateLifetime),
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return pkiMaterial{}, err
	}

	caKeyPEM, err := encodeKey(caKey)
	if err != nil {
		return pkiMaterial{}, err
	}
	serverKeyPEM, err := encodeKey(serverKey)
	if err != nil {
		return pkiMaterial{}, err
	}
	return pkiMaterial{
		caCert:     encodeCertificate(caDER),
		caKey:      caKeyPEM,
		serverCert: encodeCertificate(serverDER),
		serverKey:  serverKeyPEM,
	}, nil
}

func encodeCertificate(der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func encodeKey(key *ecdsa.PrivateKey) (string, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
}

func parseCertificate(name, pemText string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("openvpn: %s is not a PEM certificate", name)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("openvpn: %s: %w", name, err)
	}
	return cert, nil
}

// checkPKI refuses a hand-edited key set OpenVPN would start with and every
// client would then reject: a server certificate from another CA or key.
func checkPKI(s ServerSettings) error {
	ca, err := parseCertificate("caCert", s.CACert)
	if err != nil {
		return err
	}
	server, err := parseCertificate("serverCert", s.ServerCert)
	if err != nil {
		return err
	}
	if err := server.CheckSignatureFrom(ca); err != nil {
		return fmt.Errorf("openvpn: serverCert is not signed by caCert: %w", err)
	}
	if server.Subject.CommonName != serverCommonName {
		return fmt.Errorf("openvpn: serverCert must be named %q, the name profiles check", serverCommonName)
	}
	block, _ := pem.Decode([]byte(s.ServerKey))
	if block == nil {
		return errors.New("openvpn: serverKey is not a PEM private key")
	}
	key, err := parsePrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("openvpn: serverKey: %w", err)
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return errors.New("openvpn: serverKey is not a signing key")
	}
	public, ok := signer.Public().(interface{ Equal(x crypto.PublicKey) bool })
	if !ok || !public.Equal(server.PublicKey) {
		return errors.New("openvpn: serverKey does not belong to serverCert")
	}
	return nil
}

// parsePrivateKey reads the three encodings a pasted key can come in.
func parsePrivateKey(der []byte) (any, error) {
	if key, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		return key, nil
	}
	if key, err := x509.ParseECPrivateKey(der); err == nil {
		return key, nil
	}
	return x509.ParsePKCS1PrivateKey(der)
}

// generateStaticKey returns a tls-crypt key in OpenVPN's static key file format.
func generateStaticKey() (string, error) {
	raw := make([]byte, staticKeyBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(staticKeyHeader + "\n")
	encoded := hex.EncodeToString(raw)
	for i := 0; i < len(encoded); i += 32 {
		b.WriteString(encoded[i:i+32] + "\n")
	}
	b.WriteString(staticKeyFooter + "\n")
	return b.String(), nil
}

func checkStaticKey(key string) error {
	start := strings.Index(key, staticKeyHeader)
	end := strings.Index(key, staticKeyFooter)
	if start < 0 || end < start {
		return errors.New("openvpn: tlsCryptKey is not an OpenVPN static key")
	}
	body := key[start+len(staticKeyHeader) : end]
	raw, err := hex.DecodeString(string(bytes.Join(bytes.Fields([]byte(body)), nil)))
	if err != nil || len(raw) != staticKeyBytes {
		return errors.New("openvpn: tlsCryptKey must hold 256 bytes of hex")
	}
	return nil
}
