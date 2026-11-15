// Package l2tp runs L2TP inbounds: xl2tpd and pppd carry the sessions, the
// system's strongSwan protects them with IPsec, and the panel decides every
// login and counts every session's traffic.
package l2tp

import (
	"crypto/des" //nolint:gosec // MS-CHAPv2 is defined in terms of DES; the clients offer nothing else.
	"crypto/subtle"
	"unicode/utf16"

	"golang.org/x/crypto/md4" //nolint:gosec,staticcheck // The NT hash is MD4 by definition.
)

const (
	challengeSize  = 8
	ntResponseSize = 24
)

// ntHash is the NT password hash: MD4 over the password in UTF-16LE.
func ntHash(password string) []byte {
	units := utf16.Encode([]rune(password))
	raw := make([]byte, 0, len(units)*2)
	for _, unit := range units {
		raw = append(raw, byte(unit), byte(unit>>8))
	}
	sum := md4.New()
	sum.Write(raw)
	return sum.Sum(nil)
}

// desKey spreads 56 key bits over the 8 bytes DES takes; the parity bits are not checked.
func desKey(seven []byte) []byte {
	return []byte{
		seven[0],
		seven[0]<<7 | seven[1]>>1,
		seven[1]<<6 | seven[2]>>2,
		seven[2]<<5 | seven[3]>>3,
		seven[3]<<4 | seven[4]>>4,
		seven[4]<<3 | seven[5]>>5,
		seven[5]<<2 | seven[6]>>6,
		seven[6] << 1,
	}
}

// challengeResponse is RFC 2759's ChallengeResponse: the challenge encrypted
// under three DES keys cut from the zero-padded password hash.
func challengeResponse(challenge, passwordHash []byte) []byte {
	padded := make([]byte, 21)
	copy(padded, passwordHash)
	out := make([]byte, ntResponseSize)
	for i := 0; i < 3; i++ {
		block, err := des.NewCipher(desKey(padded[i*7 : i*7+7])) //nolint:gosec // See the import.
		if err != nil {
			return nil
		}
		block.Encrypt(out[i*8:i*8+8], challenge)
	}
	return out
}

// verifyMSCHAPv2 checks the response a peer gave to a challenge hash against
// the account's password. On success it returns the key pppd derives its own
// answer to the peer from: the MD4 of the password hash.
func verifyMSCHAPv2(password string, challenge, response []byte) ([]byte, bool) {
	if len(challenge) != challengeSize || len(response) != ntResponseSize {
		return nil, false
	}
	hash := ntHash(password)
	if subtle.ConstantTimeCompare(challengeResponse(challenge, hash), response) != 1 {
		return nil, false
	}
	sum := md4.New()
	sum.Write(hash)
	return sum.Sum(nil), true
}
