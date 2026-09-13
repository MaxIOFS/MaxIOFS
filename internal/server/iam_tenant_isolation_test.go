package server

// Every IAM entity belongs to a tenant, or to the deployment. These tests pin
// what a tenant may reach of what it does not own.

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// iamTenantAdmin returns an administrator confined to one tenant: it holds
// iam:manage, like any admin, but inside its own namespace.
func iamTenantAdmin(username, tenantID string) *auth.User {
	user := iamAdmin(username)
	user.TenantID = tenantID
	return user
}

const trustAnyone = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow",` +
	`"Principal":{"AWS":"*"},"Action":"sts:AssumeRole"}]}`

// createIAMRoleAs creates a role owned by the caller's tenant and removes it
// when the test ends.
func createIAMRoleAs(t *testing.T, caller *auth.User, roleName string) {
	t.Helper()
	rec := postIAMForm(t, url.Values{
		"Action": {"CreateRole"}, "RoleName": {roleName},
		"AssumeRolePolicyDocument": {trustAnyone},
	}, caller)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	t.Cleanup(func() {
		postIAMForm(t, url.Values{"Action": {"DeleteRole"}, "RoleName": {roleName}}, iamAdmin("cleanup"))
	})
}

func TestIAMXML_TenantCannotReachAnotherTenantsRole(t *testing.T) {
	owner := iamTenantAdmin("iam-tenant-a-admin", "tenant-iso-a")
	stranger := iamTenantAdmin("iam-tenant-b-admin", "tenant-iso-b")
	roleName := "TestTenantOwnedRole"

	createIAMRoleAs(t, owner, roleName)

	// The owner reaches it.
	rec := postIAMForm(t, url.Values{"Action": {"GetRole"}, "RoleName": {roleName}}, owner)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	for _, form := range []url.Values{
		{"Action": {"GetRole"}, "RoleName": {roleName}},
		{"Action": {"DeleteRole"}, "RoleName": {roleName}},
		{"Action": {"UpdateAssumeRolePolicy"}, "RoleName": {roleName}, "PolicyDocument": {trustAnyone}},
		{"Action": {"PutRolePolicy"}, "RoleName": {roleName}, "PolicyName": {"p"},
			"PolicyDocument": {`{"Statement":[{"Effect":"Allow","Action":"s3:*","Resource":"*"}]}`}},
		{"Action": {"ListRolePolicies"}, "RoleName": {roleName}},
		{"Action": {"AttachRolePolicy"}, "RoleName": {roleName},
			"PolicyArn": {auth.IAMPolicyARN("ReadOnlyAccess")}},
	} {
		rec := postIAMForm(t, form, stranger)
		assert.Equal(t, http.StatusNotFound, rec.Code,
			"%s reached another tenant's role: %s", form.Get("Action"), rec.Body.String())
		assert.Equal(t, "NoSuchEntity", iamErrorCodeOf(t, rec.Body.String()),
			"a role of another tenant must answer as if it did not exist")
	}

	// And it is not in the stranger's listing.
	rec = postIAMForm(t, url.Values{"Action": {"ListRoles"}}, stranger)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "<RoleName>"+roleName+"</RoleName>")

	rec = postIAMForm(t, url.Values{"Action": {"ListRoles"}}, owner)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "<RoleName>"+roleName+"</RoleName>")
}

func TestIAMXML_GlobalAdminReachesEveryTenantsRole(t *testing.T) {
	owner := iamTenantAdmin("iam-tenant-c-admin", "tenant-iso-c")
	roleName := "TestGlobalAdminSeesIt"

	createIAMRoleAs(t, owner, roleName)

	global := iamAdmin("iam-global-admin")
	rec := postIAMForm(t, url.Values{"Action": {"GetRole"}, "RoleName": {roleName}}, global)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = postIAMForm(t, url.Values{"Action": {"ListRoles"}}, global)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "<RoleName>"+roleName+"</RoleName>")
}

// A role with no tenant belongs to the deployment: any tenant can assume it, so
// any tenant can read it, and none may change what it grants.
func TestIAMXML_TenantReadsGlobalRolesButCannotChangeThem(t *testing.T) {
	global := iamAdmin("iam-global-role-owner")
	tenant := iamTenantAdmin("iam-tenant-d-admin", "tenant-iso-d")
	roleName := "TestDeploymentWideRole"

	createIAMRoleAs(t, global, roleName)

	rec := postIAMForm(t, url.Values{"Action": {"GetRole"}, "RoleName": {roleName}}, tenant)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = postIAMForm(t, url.Values{"Action": {"ListRoles"}}, tenant)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "<RoleName>"+roleName+"</RoleName>")

	for _, form := range []url.Values{
		{"Action": {"DeleteRole"}, "RoleName": {roleName}},
		{"Action": {"UpdateAssumeRolePolicy"}, "RoleName": {roleName}, "PolicyDocument": {trustAnyone}},
		{"Action": {"PutRolePolicy"}, "RoleName": {roleName}, "PolicyName": {"escalate"},
			"PolicyDocument": {`{"Statement":[{"Effect":"Allow","Action":"s3:*","Resource":"*"}]}`}},
	} {
		rec := postIAMForm(t, form, tenant)
		assert.Equal(t, http.StatusForbidden, rec.Code,
			"%s changed a deployment-wide role: %s", form.Get("Action"), rec.Body.String())
		assert.Equal(t, "AccessDenied", iamErrorCodeOf(t, rec.Body.String()))
	}
}

const readsAnything = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow",` +
	`"Action":["s3:GetObject"],"Resource":["*"]}]}`

// createIAMPolicyAs creates a managed policy owned by the caller's tenant and
// removes it when the test ends.
func createIAMPolicyAs(t *testing.T, caller *auth.User, policyName string) {
	t.Helper()
	rec := postIAMForm(t, url.Values{
		"Action": {"CreatePolicy"}, "PolicyName": {policyName},
		"PolicyDocument": {readsAnything},
	}, caller)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	t.Cleanup(func() {
		postIAMForm(t, url.Values{
			"Action": {"DeletePolicy"}, "PolicyArn": {auth.IAMPolicyARN(policyName)},
		}, iamAdmin("cleanup"))
	})
}

func TestIAMXML_TenantCannotReachAnotherTenantsPolicy(t *testing.T) {
	owner := iamTenantAdmin("iam-tenant-g-admin", "tenant-iso-g")
	stranger := iamTenantAdmin("iam-tenant-h-admin", "tenant-iso-h")
	policyName := "TestTenantOwnedPolicy"
	arn := auth.IAMPolicyARN(policyName)

	createIAMPolicyAs(t, owner, policyName)

	rec := postIAMForm(t, url.Values{"Action": {"GetPolicy"}, "PolicyArn": {arn}}, owner)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	for _, form := range []url.Values{
		{"Action": {"GetPolicy"}, "PolicyArn": {arn}},
		{"Action": {"DeletePolicy"}, "PolicyArn": {arn}},
		{"Action": {"ListPolicyVersions"}, "PolicyArn": {arn}},
		{"Action": {"CreatePolicyVersion"}, "PolicyArn": {arn}, "PolicyDocument": {readsAnything}},
		{"Action": {"SetDefaultPolicyVersion"}, "PolicyArn": {arn}, "VersionId": {"v1"}},
	} {
		rec := postIAMForm(t, form, stranger)
		assert.Equal(t, http.StatusNotFound, rec.Code,
			"%s reached another tenant's policy: %s", form.Get("Action"), rec.Body.String())
		assert.Equal(t, "NoSuchEntity", iamErrorCodeOf(t, rec.Body.String()))
	}

	rec = postIAMForm(t, url.Values{"Action": {"ListPolicies"}}, stranger)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "<PolicyName>"+policyName+"</PolicyName>")

	rec = postIAMForm(t, url.Values{"Action": {"ListPolicies"}}, owner)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "<PolicyName>"+policyName+"</PolicyName>")
}

// Attaching is the escalation path: the target check alone would still let a
// tenant hand its own identities a policy belonging to another tenant.
func TestIAMXML_TenantCannotAttachAnotherTenantsPolicy(t *testing.T) {
	owner := iamTenantAdmin("iam-tenant-i-admin", "tenant-iso-i")
	stranger := iamTenantAdmin("iam-tenant-j-admin", "tenant-iso-j")
	policyName := "TestNotYoursToAttach"
	roleName := "TestStrangerRole"

	createIAMPolicyAs(t, owner, policyName)
	createIAMRoleAs(t, stranger, roleName)

	rec := postIAMForm(t, url.Values{
		"Action": {"AttachRolePolicy"}, "RoleName": {roleName},
		"PolicyArn": {auth.IAMPolicyARN(policyName)},
	}, stranger)
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	assert.Equal(t, "NoSuchEntity", iamErrorCodeOf(t, rec.Body.String()))
}

// The built-in policies have no tenant: every tenant attaches them, and none
// rewrites them.
func TestIAMXML_TenantAttachesBuiltinPolicyButCannotChangeIt(t *testing.T) {
	tenant := iamTenantAdmin("iam-tenant-k-admin", "tenant-iso-k")
	roleName := "TestBuiltinAttachRole"
	arn := auth.IAMPolicyARN("ReadOnlyAccess")

	createIAMRoleAs(t, tenant, roleName)

	rec := postIAMForm(t, url.Values{"Action": {"ListPolicies"}}, tenant)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "<PolicyName>ReadOnlyAccess</PolicyName>")

	rec = postIAMForm(t, url.Values{
		"Action": {"AttachRolePolicy"}, "RoleName": {roleName}, "PolicyArn": {arn},
	}, tenant)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	for _, form := range []url.Values{
		{"Action": {"CreatePolicyVersion"}, "PolicyArn": {arn}, "PolicyDocument": {readsAnything}},
		{"Action": {"DeletePolicy"}, "PolicyArn": {arn}},
	} {
		rec := postIAMForm(t, form, tenant)
		assert.Equal(t, http.StatusForbidden, rec.Code,
			"%s changed a deployment-wide policy: %s", form.Get("Action"), rec.Body.String())
		assert.Equal(t, "AccessDenied", iamErrorCodeOf(t, rec.Body.String()))
	}
}

// createGroup registers a group and removes it when the test ends.
func createGroup(t *testing.T, name, tenantID string) *auth.Group {
	t.Helper()
	server := getSharedServer()
	group := &auth.Group{
		ID:        "iam-iso-group-" + name + "-" + tenantID,
		Name:      name,
		TenantID:  tenantID,
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, server.authManager.CreateGroup(t.Context(), group))
	t.Cleanup(func() { _ = server.authManager.DeleteGroup(t.Context(), group.ID) })
	return group
}

// A group name means the caller's own group. Before this, every group action
// resolved against the deployment-wide groups whoever asked.
func TestIAMXML_GroupPolicyLandsOnTheCallersOwnGroup(t *testing.T) {
	tenant := iamTenantAdmin("iam-tenant-e-admin", "tenant-iso-e")
	groupName := "operators"

	deploymentWide := createGroup(t, groupName, "")
	owned := createGroup(t, groupName, tenant.TenantID)

	server := getSharedServer()
	im, ok := server.authManager.(auth.IAMManager)
	require.True(t, ok)

	rec := postIAMForm(t, url.Values{
		"Action": {"PutGroupPolicy"}, "GroupName": {groupName}, "PolicyName": {"reads"},
		"PolicyDocument": {`{"Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"*"}]}`},
	}, tenant)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	_, err := im.GetIAMInlinePolicy(t.Context(), auth.IAMTargetGroup, owned.ID, "reads")
	require.NoError(t, err, "the policy should sit on the tenant's own group")

	_, err = im.GetIAMInlinePolicy(t.Context(), auth.IAMTargetGroup, deploymentWide.ID, "reads")
	require.Error(t, err, "a tenant must not write onto a deployment-wide group")
}

func TestIAMXML_TenantCannotTargetADeploymentWideGroup(t *testing.T) {
	tenant := iamTenantAdmin("iam-tenant-f-admin", "tenant-iso-f")
	createGroup(t, "auditors", "")

	rec := postIAMForm(t, url.Values{
		"Action": {"PutGroupPolicy"}, "GroupName": {"auditors"}, "PolicyName": {"reads"},
		"PolicyDocument": {`{"Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"*"}]}`},
	}, tenant)
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	assert.Equal(t, "NoSuchEntity", iamErrorCodeOf(t, rec.Body.String()))
}
