package service

import (
	"strconv"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/openvpn"
)

// OpenVPNKeySet is a new key set for an OpenVPN inbound, for its form to save.
type OpenVPNKeySet struct {
	CACert      string `json:"caCert" example:"-----BEGIN CERTIFICATE-----"`
	CAKey       string `json:"caKey" example:"-----BEGIN PRIVATE KEY-----"`
	ServerCert  string `json:"serverCert" example:"-----BEGIN CERTIFICATE-----"`
	ServerKey   string `json:"serverKey" example:"-----BEGIN PRIVATE KEY-----"`
	TLSCryptKey string `json:"tlsCryptKey" example:"-----BEGIN OpenVPN Static key V1-----"`
}

// OpenVPNCertInfo is what the OpenVPN form shows of one certificate.
type OpenVPNCertInfo struct {
	Subject   string    `json:"subject" example:"manifold-openvpn-server"`
	Issuer    string    `json:"issuer" example:"Manifold OpenVPN CA"`
	KeyType   string    `json:"keyType" example:"ecdsa-p256"`
	NotBefore time.Time `json:"notBefore" example:"2026-11-16T00:00:00Z"`
	NotAfter  time.Time `json:"notAfter" example:"2036-11-14T00:00:00Z"`
	SHA256    string    `json:"sha256" example:"5f:0a:3c"`
}

// NewOpenVPNKeys issues a CA, server certificate and control channel key from form values.
func (s *ServerService) NewOpenVPNKeys(keyType, days, caName, serverName string) (*OpenVPNKeySet, error) {
	opts := openvpn.PKIOptions{KeyType: keyType, CAName: caName, ServerName: serverName}
	if days = strings.TrimSpace(days); days != "" {
		n, err := strconv.Atoi(days)
		if err != nil {
			return nil, err
		}
		opts.Days = n
	}
	keys, err := openvpn.GenerateKeys(opts)
	if err != nil {
		return nil, err
	}
	return &OpenVPNKeySet{
		CACert: keys.CACert, CAKey: keys.CAKey, ServerCert: keys.ServerCert, ServerKey: keys.ServerKey, TLSCryptKey: keys.TLSCryptKey,
	}, nil
}

// DescribeOpenVPNCert reads a PEM certificate for the OpenVPN form.
func (s *ServerService) DescribeOpenVPNCert(pem string) (*OpenVPNCertInfo, error) {
	info, err := openvpn.DescribeCertificate(pem)
	if err != nil {
		return nil, err
	}
	out := OpenVPNCertInfo(info)
	return &out, nil
}
