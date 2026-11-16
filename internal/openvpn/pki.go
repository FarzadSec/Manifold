package openvpn

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"
)

const (
	// Profiles pin the server's name; certificates issued before 0.5.0 all carry this one.
	serverCommonName = "manifold-openvpn-server"
	caCommonName     = "Manifold OpenVPN CA"

	defaultCertificateDays = 3650
	maxCertificateDays     = 36500

	staticKeyHeader = "-----BEGIN OpenVPN Static key V1-----"
	staticKeyFooter = "-----END OpenVPN Static key V1-----"
	staticKeyBytes  = 256
)

// Key types a new certificate set can use; RSA is for clients that know no ECDSA.
const (
	KeyECDSAP256 = "ecdsa-p256"
	KeyECDSAP384 = "ecdsa-p384"
	KeyRSA2048   = "rsa-2048"
	KeyRSA4096   = "rsa-4096"
)

// verify-x509-name takes the name unquoted, so it is held to a hostname's characters.
var serverNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// PKIOptions describes a new certificate set; zero values take the defaults.
type PKIOptions struct {
	KeyType    string `json:"keyType" example:"ecdsa-p256"`
	Days       int    `json:"days" example:"3650"`
	CAName     string `json:"caName" example:"Manifold OpenVPN CA"`
	ServerName string `json:"serverName" example:"manifold-openvpn-server"`
}

// Keys is a complete key set for one OpenVPN inbound.
type Keys struct {
	CACert      string `json:"caCert"`
	CAKey       string `json:"caKey"`
	ServerCert  string `json:"serverCert"`
	ServerKey   string `json:"serverKey"`
	TLSCryptKey string `json:"tlsCryptKey"`
}

type pkiMaterial struct {
	caCert, caKey, serverCert, serverKey string
}

func (o PKIOptions) normalized() (PKIOptions, error) {
	o.KeyType = strings.ToLower(strings.TrimSpace(o.KeyType))
	if o.KeyType == "" {
		o.KeyType = KeyECDSAP256
	}
	switch o.KeyType {
	case KeyECDSAP256, KeyECDSAP384, KeyRSA2048, KeyRSA4096:
	default:
		return o, fmt.Errorf("openvpn: key type %q must be %s, %s, %s or %s", o.KeyType, KeyECDSAP256, KeyECDSAP384, KeyRSA2048, KeyRSA4096)
	}
	if o.Days == 0 {
		o.Days = defaultCertificateDays
	}
	if o.Days < 1 || o.Days > maxCertificateDays {
		return o, fmt.Errorf("openvpn: a certificate lasts 1 to %d days, not %d", maxCertificateDays, o.Days)
	}
	o.CAName = strings.TrimSpace(o.CAName)
	if o.CAName == "" {
		o.CAName = caCommonName
	}
	if len(o.CAName) > 64 || strings.ContainsFunc(o.CAName, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return o, errors.New("openvpn: the CA name is at most 64 printable characters")
	}
	o.ServerName = strings.TrimSpace(o.ServerName)
	if o.ServerName == "" {
		o.ServerName = serverCommonName
	}
	if !serverNamePattern.MatchString(o.ServerName) {
		return o, fmt.Errorf("openvpn: server name %q must be letters, digits, '.', '_' or '-', at most 64", o.ServerName)
	}
	return o, nil
}

func newPrivateKey(keyType string) (crypto.Signer, error) {
	switch keyType {
	case KeyECDSAP384:
		return ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	case KeyRSA2048:
		return rsa.GenerateKey(rand.Reader, 2048)
	case KeyRSA4096:
		return rsa.GenerateKey(rand.Reader, 4096)
	default:
		return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	}
}

func randomSerial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
}

func generatePKI() (pkiMaterial, error) {
	return generatePKIWith(PKIOptions{})
}

func generatePKIWith(raw PKIOptions) (pkiMaterial, error) {
	opts, err := raw.normalized()
	if err != nil {
		return pkiMaterial{}, err
	}
	now := time.Now()
	lifetime := time.Duration(opts.Days) * 24 * time.Hour
	caKey, err := newPrivateKey(opts.KeyType)
	if err != nil {
		return pkiMaterial{}, err
	}
	caSerial, err := randomSerial()
	if err != nil {
		return pkiMaterial{}, err
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          caSerial,
		Subject:               pkix.Name{CommonName: opts.CAName},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(lifetime),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, caKey.Public(), caKey)
	if err != nil {
		return pkiMaterial{}, err
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return pkiMaterial{}, err
	}

	serverKey, err := newPrivateKey(opts.KeyType)
	if err != nil {
		return pkiMaterial{}, err
	}
	serverSerial, err := randomSerial()
	if err != nil {
		return pkiMaterial{}, err
	}
	usage := x509.KeyUsageDigitalSignature
	if _, isRSA := serverKey.(*rsa.PrivateKey); isRSA {
		usage |= x509.KeyUsageKeyEncipherment
	}
	serverTemplate := &x509.Certificate{
		SerialNumber:          serverSerial,
		Subject:               pkix.Name{CommonName: opts.ServerName},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(lifetime),
		BasicConstraintsValid: true,
		KeyUsage:              usage,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCert, serverKey.Public(), caKey)
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

// GenerateKeys issues a new CA, server certificate and control channel key.
func GenerateKeys(opts PKIOptions) (Keys, error) {
	pki, err := generatePKIWith(opts)
	if err != nil {
		return Keys{}, err
	}
	static, err := generateStaticKey()
	if err != nil {
		return Keys{}, err
	}
	return Keys{CACert: pki.caCert, CAKey: pki.caKey, ServerCert: pki.serverCert, ServerKey: pki.serverKey, TLSCryptKey: static}, nil
}

// CertInfo is what the panel shows of a certificate.
type CertInfo struct {
	Subject   string    `json:"subject" example:"manifold-openvpn-server"`
	Issuer    string    `json:"issuer" example:"Manifold OpenVPN CA"`
	KeyType   string    `json:"keyType" example:"ecdsa-p256"`
	NotBefore time.Time `json:"notBefore"`
	NotAfter  time.Time `json:"notAfter"`
	SHA256    string    `json:"sha256" example:"5f:0a:..."`
}

// DescribeCertificate reads a PEM certificate for display.
func DescribeCertificate(pemText string) (CertInfo, error) {
	cert, err := parseCertificate("certificate", pemText)
	if err != nil {
		return CertInfo{}, err
	}
	sum := sha256.Sum256(cert.Raw)
	pairs := make([]string, len(sum))
	for i, b := range sum {
		pairs[i] = hex.EncodeToString([]byte{b})
	}
	return CertInfo{
		Subject:   cert.Subject.CommonName,
		Issuer:    cert.Issuer.CommonName,
		KeyType:   describeKey(cert.PublicKey),
		NotBefore: cert.NotBefore,
		NotAfter:  cert.NotAfter,
		SHA256:    strings.Join(pairs, ":"),
	}, nil
}

func describeKey(key any) string {
	switch k := key.(type) {
	case *ecdsa.PublicKey:
		return "ecdsa-" + strings.ToLower(strings.ReplaceAll(k.Curve.Params().Name, "-", ""))
	case *rsa.PublicKey:
		return fmt.Sprintf("rsa-%d", k.N.BitLen())
	default:
		return "unknown"
	}
}

func encodeCertificate(der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func encodeKey(key crypto.Signer) (string, error) {
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
// client would then reject, and returns the server name profiles verify.
func checkPKI(s ServerSettings) (string, error) {
	ca, err := parseCertificate("caCert", s.CACert)
	if err != nil {
		return "", err
	}
	server, err := parseCertificate("serverCert", s.ServerCert)
	if err != nil {
		return "", err
	}
	if err := server.CheckSignatureFrom(ca); err != nil {
		return "", fmt.Errorf("openvpn: serverCert is not signed by caCert: %w", err)
	}
	name := server.Subject.CommonName
	if !serverNamePattern.MatchString(name) {
		return "", fmt.Errorf("openvpn: serverCert is named %q; profiles verify the name, so use letters, digits, '.', '_' or '-'", name)
	}
	block, _ := pem.Decode([]byte(s.ServerKey))
	if block == nil {
		return "", errors.New("openvpn: serverKey is not a PEM private key")
	}
	key, err := parsePrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("openvpn: serverKey: %w", err)
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return "", errors.New("openvpn: serverKey is not a signing key")
	}
	public, ok := signer.Public().(interface{ Equal(x crypto.PublicKey) bool })
	if !ok || !public.Equal(server.PublicKey) {
		return "", errors.New("openvpn: serverKey does not belong to serverCert")
	}
	return name, nil
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
