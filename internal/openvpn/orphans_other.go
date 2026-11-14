//go:build !linux

package openvpn

func reapOrphans(string) int { return 0 }
