package auth

import (
	"context"
	"maps"
	"slices"
	"strings"
	"sync"
)

// authCache keeps what signed requests read from the auth tables: each access
// key with its user and whether the user's tenant is active, and each user's
// permissions. Triggers count every change to those tables in
// auth_generation, whoever makes it: this node, another one through the
// cluster, or SQL. A caller reads the count before the data, so nothing kept
// is older than the count it is kept under; a newer count empties the cache.
type authCache struct {
	mu         sync.RWMutex
	generation int64
	signers    map[string]cachedSigner
	sets       map[string]*PolicySet
}

type cachedSigner struct {
	key          AccessKey // its secret as stored, encrypted
	user         *User
	tenantActive bool
}

// at reports whether what the cache keeps is of generation, emptying it for a
// newer one. A caller holding an older generation reads the database and
// keeps nothing.
func (c *authCache) at(generation int64) bool {
	c.mu.RLock()
	current := c.signers != nil && c.generation == generation
	c.mu.RUnlock()
	if current {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.signers == nil || generation > c.generation {
		c.generation = generation
		c.signers = make(map[string]cachedSigner)
		c.sets = make(map[string]*PolicySet)
	}
	return c.generation == generation
}

func (c *authCache) signer(generation int64, accessKeyID string) (cachedSigner, bool) {
	if !c.at(generation) {
		return cachedSigner{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.signers[accessKeyID]
	return s, ok && c.generation == generation
}

func (c *authCache) keepSigner(generation int64, accessKeyID string, s cachedSigner) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.signers != nil && c.generation == generation {
		c.signers[accessKeyID] = s
	}
}

func (c *authCache) set(generation int64, key string) (*PolicySet, bool) {
	if !c.at(generation) {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.sets[key]
	return s, ok && c.generation == generation
}

func (c *authCache) keepSet(generation int64, key string, s *PolicySet) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sets != nil && c.generation == generation {
		c.sets[key] = s
	}
}

// requestGeneration is the count a request reads once, for its signature and
// its permissions: what it is authorized by is the auth tables as they were
// when it started.
type requestGeneration struct {
	once sync.Once
	n    int64
	ok   bool
}

type requestGenerationKey struct{}

func withRequestGeneration(ctx context.Context) context.Context {
	return context.WithValue(ctx, requestGenerationKey{}, &requestGeneration{})
}

// authGeneration is the count of changes to the auth tables, read once per
// request; false when it cannot be read, and nothing is then kept.
func (am *authManager) authGeneration(ctx context.Context) (int64, bool) {
	read := func() (int64, bool) {
		n, err := am.store.AuthGeneration()
		return n, err == nil
	}
	g, _ := ctx.Value(requestGenerationKey{}).(*requestGeneration)
	if g == nil {
		return read()
	}
	g.once.Do(func() { g.n, g.ok = read() })
	return g.n, g.ok
}

// signerOf reads an access key, the user it belongs to and whether that
// user's tenant is active. ErrInvalidCredentials is a key that does not
// exist, ErrUserNotFound a key whose user does not. What it returns is the
// caller's to change.
func (am *authManager) signerOf(ctx context.Context, accessKeyID string) (*AccessKey, *User, bool, error) {
	generation, cached := am.authGeneration(ctx)
	if cached {
		if s, ok := am.cache.signer(generation, accessKeyID); ok {
			key := s.key
			return &key, s.user.clone(), s.tenantActive, nil
		}
	}
	key, err := am.store.GetAccessKey(accessKeyID)
	if err != nil {
		return nil, nil, false, ErrInvalidCredentials
	}
	user, err := am.store.GetUserByID(key.UserID)
	if err != nil {
		return key, nil, false, ErrUserNotFound
	}
	// A tenant that could not be read is not active for this request, and is
	// not kept: only a status read from the database is.
	tenantActive, tenantRead := true, true
	if user.TenantID != "" {
		active, err := am.store.TenantActive(user.TenantID)
		tenantActive, tenantRead = err == nil && active, err == nil
	}
	if cached && tenantRead {
		am.cache.keepSigner(generation, accessKeyID, cachedSigner{key: *key, user: user.clone(), tenantActive: tenantActive})
	}
	return key, user, tenantActive, nil
}

// clone is a copy of u that shares nothing a caller may change.
func (u *User) clone() *User {
	c := *u
	c.Roles = slices.Clone(u.Roles)
	c.Policies = slices.Clone(u.Policies)
	c.Metadata = maps.Clone(u.Metadata)
	c.BackupCodes = slices.Clone(u.BackupCodes)
	c.BackupCodesUsed = slices.Clone(u.BackupCodesUsed)
	return &c
}

// policySetKey names what a user's permissions are resolved from.
func policySetKey(user *User) string {
	return user.ID + "\x00" + user.TenantID + "\x00" + strings.Join(user.Roles, "\x00")
}

// copy is a PolicySet of its own, whose session policy a caller may set; its
// lists are shared and grow apart on an append.
func (p *PolicySet) copy() *PolicySet {
	c := *p
	c.Documents = slices.Clip(p.Documents)
	c.Actions = slices.Clip(p.Actions)
	return &c
}
