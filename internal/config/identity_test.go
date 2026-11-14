package config

import (
	"os"
	"testing"
)

func TestManifoldEnvNameSetsTheSetting(t *testing.T) {
	t.Setenv("XUI_PORT", "")
	if err := os.Unsetenv("XUI_PORT"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MANIFOLD_PORT", "8443")

	adoptManifoldEnv()

	port, configured, err := GetPortOverride()
	if err != nil {
		t.Fatalf("GetPortOverride: %v", err)
	}
	if !configured || port != 8443 {
		t.Fatalf("port = %d, configured = %v; want 8443, true: MANIFOLD_PORT must reach the port setting", port, configured)
	}
}

func TestManifoldEnvNameWinsOverTheOldName(t *testing.T) {
	t.Setenv("XUI_PORT", "1111")
	t.Setenv("MANIFOLD_PORT", "8443")

	adoptManifoldEnv()

	port, _, err := GetPortOverride()
	if err != nil {
		t.Fatalf("GetPortOverride: %v", err)
	}
	if port != 8443 {
		t.Fatalf("port = %d, want 8443: the documented name must not lose to a leftover XUI_PORT", port)
	}
}

func TestProductVersionIsNotTheCompatibilityVersion(t *testing.T) {
	if GetProductVersion() == "" {
		t.Fatal("product version is empty")
	}
	if GetProductVersion() == GetBaseVersion() {
		t.Fatalf("product version %q equals the base version: nodes are gated on the base version, so the two must be free to differ", GetProductVersion())
	}
}
