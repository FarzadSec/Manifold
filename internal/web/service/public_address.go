package service

import "sync"

// lookupPublicIPv4 asks the echo services for this server's IPv4; tests replace it.
var lookupPublicIPv4 = func() string {
	if ip := firstPublicIP(publicIPv4Services); ip != "N/A" {
		return ip
	}
	return ""
}

var publicIPv4 struct {
	sync.Mutex
	ip string
}

// RefreshPublicIPv4 looks this server's public IPv4 up again; a failed lookup keeps the
// last known address. The panel runs it at start and then on a schedule.
func RefreshPublicIPv4() {
	if ip := lookupPublicIPv4(); ip != "" {
		publicIPv4.Lock()
		publicIPv4.ip = ip
		publicIPv4.Unlock()
	}
}

// KnownPublicIPv4 is the last address RefreshPublicIPv4 found, "" before it found one.
func KnownPublicIPv4() string {
	publicIPv4.Lock()
	defer publicIPv4.Unlock()
	return publicIPv4.ip
}
