package backupstore

import (
	"bytes"
	"errors"
	"testing"
)

// An encrypted backup opens with its passphrase only, and a changed byte is caught, not restored.
func TestEncryptedBackupsOpenOnlyWithTheirPassphrase(t *testing.T) {
	plain := bytes.Repeat([]byte("manifold backup "), 4096)
	sealed, err := Encrypt(plain, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !IsEncrypted(sealed) || IsEncrypted(plain) {
		t.Fatal("encrypted and plain backups are not told apart")
	}
	if bytes.Contains(sealed, []byte("manifold backup")) {
		t.Fatal("the sealed backup carries plain text")
	}
	got, err := Decrypt(sealed, "correct horse")
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("decrypt = %d bytes, %v", len(got), err)
	}
	if _, err := Decrypt(sealed, "wrong"); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("wrong passphrase = %v", err)
	}
	tampered := append([]byte{}, sealed...)
	tampered[len(tampered)-1] ^= 1
	if _, err := Decrypt(tampered, "correct horse"); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("tampered backup = %v", err)
	}
	again, _ := Encrypt(plain, "correct horse")
	if bytes.Equal(again, sealed) {
		t.Fatal("two encryptions of one backup are identical: no fresh salt or nonce")
	}
}
