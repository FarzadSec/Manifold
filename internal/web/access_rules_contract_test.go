package web

import (
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/web/access"
)

// A route without a rule is refused to everyone, and a rule without a route hides a typo
// that leaves the real route unguarded, so both directions must match.
func TestEveryAPIRouteHasAnAccessRule(t *testing.T) {
	registered := map[string]bool{}
	for route := range registeredContractRoutes(t) {
		method, path, _ := strings.Cut(route, " ")
		if rel, ok := strings.CutPrefix(path, "/panel/api/"); ok {
			registered[method+" /"+rel] = true
		}
	}
	for route := range registered {
		method, path, _ := strings.Cut(route, " ")
		if _, ok := access.RouteRule(method, path); !ok {
			t.Errorf("%s has no access rule in internal/web/access/routes.go", route)
		}
	}
	for _, route := range access.RuledRoutes() {
		if !registered[route] {
			t.Errorf("access rule %s names no registered route", route)
		}
	}
}
