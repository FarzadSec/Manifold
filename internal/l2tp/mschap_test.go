package l2tp

import (
	"encoding/hex"
	"strings"
	"testing"
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The vectors are RFC 2759 section 9.2: user "User", password "clientPass".
const (
	rfcChallengeHash    = "D02E4386BCE91226"
	rfcNTResponse       = "82309ECD8D708B5EA08FAA3981CD83544233114A3D85D6DF"
	rfcPasswordHashHash = "41C00C584BD2D91C4017A2A12FA59F3F"
)

func TestMSCHAPv2ResponseOfTheRightPasswordYieldsTheSessionKey(t *testing.T) {
	key, ok := verifyMSCHAPv2("clientPass", unhex(t, rfcChallengeHash), unhex(t, rfcNTResponse))
	if !ok {
		t.Fatal("the RFC 2759 response was refused")
	}
	if got := strings.ToUpper(hex.EncodeToString(key)); got != rfcPasswordHashHash {
		t.Fatalf("session key = %s, want %s", got, rfcPasswordHashHash)
	}
}

func TestMSCHAPv2ResponseIsRefusedWhenAnythingIsOff(t *testing.T) {
	flipped := unhex(t, rfcNTResponse)
	flipped[23] ^= 1
	tests := map[string]struct {
		password            string
		challenge, response []byte
	}{
		"another password":        {"clientpass", unhex(t, rfcChallengeHash), unhex(t, rfcNTResponse)},
		"one bit of the response": {"clientPass", unhex(t, rfcChallengeHash), flipped},
		"another challenge":       {"clientPass", unhex(t, "D02E4386BCE91227"), unhex(t, rfcNTResponse)},
		"short challenge":         {"clientPass", unhex(t, "D02E4386BCE912"), unhex(t, rfcNTResponse)},
		"short response":          {"clientPass", unhex(t, rfcChallengeHash), unhex(t, rfcNTResponse)[:23]},
		"empty response":          {"clientPass", unhex(t, rfcChallengeHash), nil},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if key, ok := verifyMSCHAPv2(tt.password, tt.challenge, tt.response); ok || key != nil {
				t.Fatalf("accepted with key %x", key)
			}
		})
	}
}
