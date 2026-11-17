package web

import (
	"crypto/tls"
	"fmt"
	"os"
	"sync"
	"time"
)

// certReloader serves the panel's certificate and reads its files again once they
// change, so a renewed certificate is served without a restart.
type certReloader struct {
	certFile, keyFile string
	every             time.Duration

	mu      sync.Mutex
	cert    *tls.Certificate
	stamp   string
	checked time.Time
}

func fileStamp(paths ...string) string {
	out := ""
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil {
			out += fmt.Sprintf("%d:%d;", st.ModTime().UnixNano(), st.Size())
		}
	}
	return out
}

func newCertReloader(certFile, keyFile string) (*certReloader, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	return &certReloader{
		certFile: certFile, keyFile: keyFile, every: 30 * time.Second,
		cert: &cert, stamp: fileStamp(certFile, keyFile), checked: time.Now(),
	}, nil
}

// GetCertificate keeps the last pair that loaded, so a renewal caught half written is
// picked up on a later look instead of breaking the handshake.
func (r *certReloader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Since(r.checked) >= r.every {
		r.checked = time.Now()
		if stamp := fileStamp(r.certFile, r.keyFile); stamp != r.stamp {
			if cert, err := tls.LoadX509KeyPair(r.certFile, r.keyFile); err == nil {
				r.cert, r.stamp = &cert, stamp
			}
		}
	}
	return r.cert, nil
}
