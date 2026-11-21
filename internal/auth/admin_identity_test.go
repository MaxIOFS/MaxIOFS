package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func ctxWithUser(user *User, set *PolicySet) context.Context {
	ctx := context.WithValue(context.Background(), "user", user) //nolint:staticcheck
	if set != nil {
		ctx = WithPolicySet(ctx, set)
	}
	return ctx
}

func allowing(userID, tenantID string, actions ...string) *PolicySet {
	return &PolicySet{
		UserID:    userID,
		TenantID:  tenantID,
		Documents: []string{allowDocument(actions)},
		Actions:   actions,
	}
}

func TestIsAdminUser_HeldByRoleOrByPolicy(t *testing.T) {
	t.Run("the role still counts on its own", func(t *testing.T) {
		user := &User{ID: "u1", Roles: []string{RoleAdmin}}
		assert.True(t, IsAdminUser(ctxWithUser(user, nil)),
			"an administrator by role must not need a policy set on the context")
	})

	t.Run("a policy granting SuperAdmin counts", func(t *testing.T) {
		user := &User{ID: "u2", Roles: []string{RoleUser}}
		ctx := ctxWithUser(user, allowing("u2", "", ActionSuperAdmin))
		assert.True(t, IsAdminUser(ctx),
			"administration granted by policy was being ignored")
	})

	t.Run("TenantAdmin alone does not", func(t *testing.T) {
		user := &User{ID: "u3", Roles: []string{RoleUser}}
		ctx := ctxWithUser(user, allowing("u3", "", ActionTenantAdmin))
		assert.False(t, IsAdminUser(ctx))
	})

	t.Run("a set resolved for somebody else does not", func(t *testing.T) {
		user := &User{ID: "u4", Roles: []string{RoleUser}}
		ctx := ctxWithUser(user, allowing("someone-else", "", ActionSuperAdmin))
		assert.False(t, IsAdminUser(ctx))
	})

	t.Run("an ordinary user does not", func(t *testing.T) {
		user := &User{ID: "u5", Roles: []string{RoleUser}}
		ctx := ctxWithUser(user, allowing("u5", "", ActionGetObject))
		assert.False(t, IsAdminUser(ctx))
	})

	t.Run("no user at all does not", func(t *testing.T) {
		assert.False(t, IsAdminUser(context.Background()))
	})

	t.Run("a wildcard does not", func(t *testing.T) {
		user := &User{ID: "u6", Roles: []string{"operators"}}
		assert.False(t, IsAdminUser(ctxWithUser(user, allowing("u6", "", "*"))), "everything is not administration")
		assert.False(t, IsAdminUser(ctxWithUser(user, allowing("u6", "", "maxiofs:*"))))
	})

	t.Run("SuperAdmin on a bucket does not", func(t *testing.T) {
		user := &User{ID: "u7", Roles: []string{RoleUser}}
		set := &PolicySet{UserID: "u7", Documents: []string{`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"maxiofs:SuperAdmin","Resource":"arn:aws:s3:::b"}]}`}}
		assert.False(t, IsAdminUser(ctxWithUser(user, set)))
	})

	t.Run("a Deny takes it back", func(t *testing.T) {
		user := &User{ID: "u8", Roles: []string{RoleUser}}
		set := allowing("u8", "", ActionSuperAdmin)
		set.Documents = append(set.Documents, `{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Action":"maxiofs:SuperAdmin","Resource":"*"}]}`)
		assert.False(t, IsAdminUser(ctxWithUser(user, set)))
	})
}
