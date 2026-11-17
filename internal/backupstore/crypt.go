// Package backupstore seals backups with a passphrase and stores them on this server, an
// S3-compatible bucket or another server over SSH, keeping the newest few.
package backupstore

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"errors"
)

var (
	magic              = []byte("MANIFOLD-BACKUP-1\n")
	ErrWrongPassphrase = errors.New("the passphrase does not open this backup, or the file was changed")
	ErrNotEncrypted    = errors.New("this backup is not encrypted")
)

const (
	saltSize = 16
	// OWASP's floor for PBKDF2-HMAC-SHA256; a backup is sealed once and opened rarely.
	kdfIterations = 600000
)

// IsEncrypted reports a backup sealed by Encrypt.
func IsEncrypted(data []byte) bool { return bytes.HasPrefix(data, magic) }

func key(passphrase string, salt []byte) ([]byte, error) {
	return pbkdf2.Key(sha256.New, passphrase, salt, kdfIterations, 32)
}

// Encrypt seals data with AES-256-GCM under a key derived from passphrase and a fresh salt.
func Encrypt(data []byte, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return nil, errors.New("a passphrase is needed")
	}
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	k, err := key(passphrase, salt)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	head := append(append(append([]byte{}, magic...), salt...), nonce...)
	return gcm.Seal(head, nonce, data, head), nil
}

// Decrypt opens a backup sealed by Encrypt; the header is authenticated with the data.
func Decrypt(data []byte, passphrase string) ([]byte, error) {
	if !IsEncrypted(data) {
		return nil, ErrNotEncrypted
	}
	rest := data[len(magic):]
	if len(rest) < saltSize+12+16 {
		return nil, ErrWrongPassphrase
	}
	salt := rest[:saltSize]
	k, err := key(passphrase, salt)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := rest[saltSize : saltSize+gcm.NonceSize()]
	head := data[:len(magic)+saltSize+gcm.NonceSize()]
	plain, err := gcm.Open(nil, nonce, data[len(head):], head)
	if err != nil {
		return nil, ErrWrongPassphrase
	}
	return plain, nil
}
