// Package access decides what an admin account may reach: its tier, the permissions its
// role keeps inside that tier, and which API route needs which permission.
package access

import (
	"slices"
	"strings"
)

// Tiers. A role only ever narrows its tier.
const (
	TierAdmin    = "admin"
	TierOperator = "operator"
	TierReseller = "reseller"
)

// Permissions, shared by roles and API tokens. A ":write" permission includes its ":read".
const (
	UsersRead     = "users:read"
	UsersWrite    = "users:write"
	PlansWrite    = "plans:write"
	NodesRead     = "nodes:read"
	NodesWrite    = "nodes:write"
	TunnelsRead   = "tunnels:read"
	TunnelsWrite  = "tunnels:write"
	PoolRead      = "pool:read"
	PoolWrite     = "pool:write"
	SettingsRead  = "settings:read"
	SettingsWrite = "settings:write"
	StatsRead     = "stats:read"
	ToolsWrite    = "tools:write"
	AdminsWrite   = "admins:write"
	TokensWrite   = "tokens:write"
	BackupRead    = "backup:read"
	BackupWrite   = "backup:write"
	BackupDeliver = "backup:deliver"
	AuditRead     = "audit:read"
	ServicesWrite = "services:write"
	permissionAny = ""
)

const (
	writeSuffix = ":write"
	readSuffix  = ":read"
)

// AllPermissions lists every permission in the order the role and token forms show them.
var AllPermissions = []string{
	UsersRead, UsersWrite, PlansWrite,
	NodesRead, NodesWrite, TunnelsRead, TunnelsWrite,
	PoolRead, PoolWrite, SettingsRead, SettingsWrite,
	StatsRead, ToolsWrite, ServicesWrite,
	AdminsWrite, TokensWrite, BackupRead, BackupWrite, BackupDeliver, AuditRead,
}

var tierCeilings = map[string][]string{
	TierAdmin: AllPermissions,
	TierOperator: {
		UsersRead, UsersWrite, PlansWrite, NodesRead, NodesWrite, TunnelsRead, TunnelsWrite,
		PoolRead, PoolWrite, SettingsRead, SettingsWrite, StatsRead, ToolsWrite, ServicesWrite,
	},
	TierReseller: {UsersRead, UsersWrite, StatsRead, ToolsWrite},
}

// ValidTier reports whether tier is one of the three tiers.
func ValidTier(tier string) bool {
	_, ok := tierCeilings[tier]
	return ok
}

// TierRank orders tiers so a role or token can be checked against its account's.
func TierRank(tier string) int {
	switch tier {
	case TierAdmin:
		return 3
	case TierOperator:
		return 2
	case TierReseller:
		return 1
	default:
		return 0
	}
}

// Ceiling is every permission the tier's built-in role holds.
func Ceiling(tier string) []string {
	return slices.Clone(tierCeilings[tier])
}

// Known reports whether p is a permission of the catalogue.
func Known(p string) bool {
	return slices.Contains(AllPermissions, p)
}

// Set is a resolved permission set, with every write's read added.
type Set map[string]struct{}

// NewSet expands writes to their reads and keeps only what the tier allows.
func NewSet(tier string, perms []string) Set {
	ceiling := tierCeilings[tier]
	out := Set{}
	for _, p := range perms {
		if !slices.Contains(ceiling, p) {
			continue
		}
		out[p] = struct{}{}
		if base, ok := strings.CutSuffix(p, writeSuffix); ok && Known(base+readSuffix) {
			out[base+readSuffix] = struct{}{}
		}
	}
	return out
}

// Has reports whether the set grants p; the empty permission is granted to every account.
func (s Set) Has(p string) bool {
	if p == permissionAny {
		return true
	}
	_, ok := s[p]
	return ok
}

// Union is what either set grants.
func (s Set) Union(other Set) Set {
	out := Set{}
	for p := range s {
		out[p] = struct{}{}
	}
	for p := range other {
		out[p] = struct{}{}
	}
	return out
}

// Without takes perms away, and with a read the write that cannot stand without it.
func (s Set) Without(perms []string) Set {
	out := s.Union(nil)
	for _, p := range perms {
		delete(out, p)
		if base, ok := strings.CutSuffix(p, readSuffix); ok {
			delete(out, base+writeSuffix)
		}
	}
	return out
}

// Intersect keeps what both sets grant.
func (s Set) Intersect(other Set) Set {
	out := Set{}
	for p := range s {
		if _, ok := other[p]; ok {
			out[p] = struct{}{}
		}
	}
	return out
}

// List returns the set in catalogue order.
func (s Set) List() []string {
	out := []string{}
	for _, p := range AllPermissions {
		if _, ok := s[p]; ok {
			out = append(out, p)
		}
	}
	return out
}
