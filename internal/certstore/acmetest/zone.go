package acmetest

import (
	"context"
	"strconv"
	"sync"
)

// Zone is a DNS zone in memory, a certstore.Provider for DNS-01.
type Zone struct {
	mu      sync.Mutex
	records map[string]map[string]string
	n       int
}

func (z *Zone) Present(_ context.Context, fqdn, value string) (string, error) {
	z.mu.Lock()
	defer z.mu.Unlock()
	if z.records == nil {
		z.records = map[string]map[string]string{}
	}
	if z.records[fqdn] == nil {
		z.records[fqdn] = map[string]string{}
	}
	z.n++
	id := strconv.Itoa(z.n)
	z.records[fqdn][id] = value
	return id, nil
}

func (z *Zone) CleanUp(_ context.Context, fqdn, id string) error {
	z.mu.Lock()
	defer z.mu.Unlock()
	delete(z.records[fqdn], id)
	return nil
}

// Values is what the zone holds at fqdn.
func (z *Zone) Values(fqdn string) []string {
	z.mu.Lock()
	defer z.mu.Unlock()
	var out []string
	for _, v := range z.records[fqdn] {
		out = append(out, v)
	}
	return out
}

// Left counts records still in the zone.
func (z *Zone) Left() int {
	z.mu.Lock()
	defer z.mu.Unlock()
	n := 0
	for _, r := range z.records {
		n += len(r)
	}
	return n
}
