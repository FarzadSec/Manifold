package access

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// ErrAccountExpired refuses a reseller whose account is past its expiry, at login and per request.
var ErrAccountExpired = errors.New("this account has expired")

const actorKey = "access_actor"

// Actor is the account a request acts as, with what it may reach.
type Actor struct {
	UserId   int
	Username string
	Tier     string
	Owner    bool
	Perms    Set
	Account  model.User
	Role     *model.Role
	ViaToken bool
	// ReadScope and WriteScope say which users it reads and changes: ScopeAll or ScopeOwn.
	ReadScope  string
	WriteScope string
}

// Which users a role reaches.
const (
	ScopeAll = "all"
	ScopeOwn = "own"
)

// ValidScopes reports whether a role may hold this pair; a role cannot change users it cannot see.
func ValidScopes(read, write string) bool {
	valid := func(s string) bool { return s == "" || s == ScopeAll || s == ScopeOwn }
	return valid(read) && valid(write) && !(read == ScopeOwn && write != ScopeOwn)
}

// Allows reports whether the actor may call a route with this rule. A staff route acts on
// every user at once, so it stays shut to an actor kept to its own users for that access.
func (a *Actor) Allows(r Rule) bool {
	if a == nil {
		return false
	}
	if r.Staff {
		switch {
		case a.Tier == TierReseller:
			return false
		case r.Perm == UsersRead && a.ReadsOwnOnly(), r.Perm == UsersWrite && a.WritesOwnOnly():
			return false
		}
	}
	return a.Perms.Has(r.Perm)
}

// ReadsOwnOnly is true when the actor sees only the users it owns.
func (a *Actor) ReadsOwnOnly() bool {
	return a != nil && a.ReadScope == ScopeOwn
}

// WritesOwnOnly is true when the actor changes only the users it owns.
func (a *Actor) WritesOwnOnly() bool {
	return a != nil && a.WriteScope == ScopeOwn
}

// IsReseller reports whether the actor sells from an allowance of its own.
func (a *Actor) IsReseller() bool {
	return a != nil && a.Tier == TierReseller
}

// IsMainAdmin reports whether the actor is on the main admin tier.
func (a *Actor) IsMainAdmin() bool {
	return a != nil && a.Tier == TierAdmin
}

// SetActor stores the resolved actor on the request.
func SetActor(c *gin.Context, a *Actor) {
	c.Set(actorKey, a)
}

// ActorFrom returns the request's actor, or nil before authentication.
func ActorFrom(c *gin.Context) *Actor {
	v, ok := c.Get(actorKey)
	if !ok {
		return nil
	}
	a, _ := v.(*Actor)
	return a
}

// Resolve turns an account into an actor: its tier's ceiling, narrowed by its custom role.
func Resolve(user *model.User) (*Actor, error) {
	if user == nil {
		return nil, errors.New("no account")
	}
	if user.Tier == TierReseller && user.Suspended == SuspendedExpired {
		return nil, ErrAccountExpired
	}
	tier := user.Tier
	if !ValidTier(tier) {
		tier = TierAdmin
	}
	actor := &Actor{UserId: user.Id, Username: user.Username, Tier: tier, Owner: user.IsOwner, Account: *user, ReadScope: ScopeAll, WriteScope: ScopeAll}
	if tier == TierReseller {
		actor.ReadScope, actor.WriteScope = ScopeOwn, ScopeOwn
	}
	if user.RoleId > 0 {
		var role model.Role
		if err := database.GetDB().First(&role, user.RoleId).Error; err != nil {
			return nil, err
		}
		if role.Tier != tier {
			return nil, errors.New("the account's role belongs to another tier")
		}
		actor.Role = &role
		actor.Perms = NewSet(tier, role.Permissions)
		if tier != TierReseller && !user.IsOwner {
			if role.UsersReadScope == ScopeOwn {
				actor.ReadScope = ScopeOwn
			}
			if role.UsersWriteScope == ScopeOwn {
				actor.WriteScope = ScopeOwn
			}
		}
		return actor, nil
	}
	actor.Perms = NewSet(tier, Ceiling(tier))
	return actor, nil
}

// ResolveToken narrows the token's account by the token's own tier and permissions.
func ResolveToken(token *model.ApiToken) (*Actor, error) {
	db := database.GetDB()
	var user model.User
	q := db.Model(&model.User{})
	if token.UserId > 0 {
		q = q.Where("id = ?", token.UserId)
	} else {
		q = q.Where("is_owner = ?", true)
	}
	if err := q.First(&user).Error; err != nil {
		return nil, err
	}
	actor, err := Resolve(&user)
	if err != nil {
		return nil, err
	}
	actor.ViaToken = true
	if ValidTier(token.Tier) && TierRank(token.Tier) < TierRank(actor.Tier) {
		actor.Tier = token.Tier
		actor.Perms = actor.Perms.Intersect(NewSet(token.Tier, Ceiling(token.Tier)))
	}
	if len(token.Permissions) > 0 {
		actor.Perms = actor.Perms.Intersect(NewSet(actor.Tier, token.Permissions))
	}
	return actor, nil
}

// Suspension reasons for a reseller and its users.
const (
	SuspendedVolume  = "volume"
	SuspendedExpired = "expired"
)
