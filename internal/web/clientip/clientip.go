// Package clientip tells who sent a request, honouring forwarding headers only from trusted proxies.
package clientip

import (
	"fmt"
	"net"
	"net/netip"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"

	"github.com/gin-gonic/gin"
)

// Of returns the client's address, read from forwarding headers only behind a trusted proxy.
func Of(c *gin.Context) string {
	remoteIP, ok := extractTrustedIP(c.Request.RemoteAddr)
	if !ok {
		return "unknown"
	}
	if !isTrustedProxy(remoteIP) {
		return remoteIP
	}

	realIP, hasRealIP := extractTrustedIP(c.GetHeader("X-Real-IP"))
	forwardedIP, hasForwarded := forwardedForClient(c.GetHeader("X-Forwarded-For"))
	switch {
	case hasRealIP && hasForwarded && realIP != forwardedIP:
		// A proxy rewrites at most one of the two, so the other is the client's
		// own claim; neither may pick the identity the login limiter keys on.
		return remoteIP
	case hasForwarded:
		return forwardedIP
	case hasRealIP:
		return realIP
	}
	return remoteIP
}

// forwardedForClient walks X-Forwarded-For from the proxy end: entries a client sent
// come first, so the first hop that is not a trusted proxy is the address one saw.
func forwardedForClient(xff string) (string, bool) {
	parts := strings.Split(xff, ",")
	client := ""
	for i := len(parts) - 1; i >= 0; i-- {
		ip, ok := extractTrustedIP(parts[i])
		if !ok {
			break
		}
		client = ip
		if !isTrustedProxy(ip) {
			break
		}
	}
	return client, client != ""
}

// ViaTrustedProxy reports whether the request arrived from a trusted proxy.
func ViaTrustedProxy(c *gin.Context) bool {
	remoteIP, ok := extractTrustedIP(c.Request.RemoteAddr)
	return ok && isTrustedProxy(remoteIP)
}

func isTrustedProxy(ip string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}

	trusted := trustedProxyCIDRs()
	for value := range strings.SplitSeq(trusted, ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(value); err == nil {
			if prefix.Contains(addr) {
				return true
			}
			continue
		}
		if proxyIP, err := netip.ParseAddr(value); err == nil && proxyIP.Unmap() == addr.Unmap() {
			return true
		}
	}
	return false
}

func trustedProxyCIDRs() (trusted string) {
	trusted = service.DefaultTrustedProxyCIDRs
	defer func() {
		_ = recover()
	}()
	settingService := service.SettingService{}
	if value, err := settingService.GetTrustedProxyCIDRs(); err == nil && strings.TrimSpace(value) != "" {
		trusted = value
	}
	return trusted
}

func extractTrustedIP(value string) (string, bool) {
	candidate := strings.TrimSpace(value)
	if candidate == "" {
		return "", false
	}

	if ip, ok := parseIPCandidate(candidate); ok {
		return ip.String(), true
	}

	if host, _, err := net.SplitHostPort(candidate); err == nil {
		if ip, ok := parseIPCandidate(host); ok {
			return ip.String(), true
		}
	}

	if strings.Count(candidate, ":") == 1 {
		if host, _, err := net.SplitHostPort(fmt.Sprintf("[%s]", candidate)); err == nil {
			if ip, ok := parseIPCandidate(host); ok {
				return ip.String(), true
			}
		}
	}

	return "", false
}

func parseIPCandidate(value string) (netip.Addr, bool) {
	ip, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return netip.Addr{}, false
	}
	return ip.Unmap(), true
}
