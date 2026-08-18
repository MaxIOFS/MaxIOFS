package server

// Tests for the AWS IAM query surface.

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// iamAdmin returns an admin identity, which is what holds iam:manage.
func iamAdmin(username string) *auth.User {
	return &auth.User{
		ID:       "iam-test-" + username,
		Username: username,
		Status:   auth.UserStatusActive,
		Roles:    []string{auth.RoleAdmin},
	}
}

// postIAMForm sends an IAM query-protocol request through the shared POST /
// dispatcher, with user in context as the S3 auth middleware would leave it.
func postIAMForm(t *testing.T, form url.Values, user *auth.User) *httptest.ResponseRecorder {
	t.Helper()
	server := getSharedServer()

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "10.56.0.1:9999"
	if user != nil {
		req = req.WithContext(context.WithValue(req.Context(), "user", user))
	}

	rec := httptest.NewRecorder()
	server.handleAWSQueryRequest(rec, req)
	return rec
}

func iamErrorCodeOf(t *testing.T, body string) string {
	t.Helper()
	var doc iamErrorResponse
	require.NoError(t, xml.Unmarshal([]byte(body), &doc), "not an IAM error document: %s", body)
	return doc.Error.Code
}

// --- routing and access control ---

func TestIAMXML_ActionsAreRoutedToTheIAMHandler(t *testing.T) {
	// An unauthenticated IAM action must produce an IAM error document, not the
	// STS "InvalidAction" that the STS branch would return.
	rec := postIAMForm(t, url.Values{"Action": {"ListUsers"}}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "AccessDenied", iamErrorCodeOf(t, rec.Body.String()))
}

func TestIAMXML_RequiresIAMManageCapability(t *testing.T) {
	plain := &auth.User{
		ID:       "iam-test-plain",
		Username: "plain-user",
		Status:   auth.UserStatusActive,
		Roles:    []string{auth.RoleUser},
	}
	rec := postIAMForm(t, url.Values{"Action": {"ListUsers"}}, plain)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "AccessDenied", iamErrorCodeOf(t, rec.Body.String()),
		"a user without iam:manage must not be able to manage identities")
}

func TestIAMXML_TemporaryCredentialsCannotManageIAM(t *testing.T) {
	server := getSharedServer()
	form := url.Values{"Action": {"ListUsers"}}

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization",
		"AWS4-HMAC-SHA256 Credential=ASIAEXAMPLEEXAMPLE/20260801/us-east-1/s3/aws4_request, SignedHeaders=host, Signature=x")
	req.RemoteAddr = "10.56.0.2:9999"
	req = req.WithContext(context.WithValue(req.Context(), "user", iamAdmin("admin-temp")))

	rec := httptest.NewRecorder()
	server.handleAWSQueryRequest(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "AccessDenied", iamErrorCodeOf(t, rec.Body.String()),
		"a session credential must not be able to mint identities that outlive it")
}

func TestIAMXML_TenantAdminCannotManageGlobalAdminIdentity(t *testing.T) {
	tenantAdmin := iamAdmin("tenant-iam-admin")
	tenantAdmin.TenantID = "tenant-iam-a"
	server := getSharedServer()
	globalUser := &auth.User{
		ID:        "iam-test-global-key-owner",
		Username:  "global-key-owner",
		Status:    auth.UserStatusActive,
		Roles:     []string{auth.RoleAdmin},
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, server.authManager.CreateUser(t.Context(), globalUser))
	t.Cleanup(func() { _ = server.authManager.DeleteUser(t.Context(), globalUser.ID) })
	globalKey, err := server.authManager.GenerateAccessKey(t.Context(), globalUser.ID)
	require.NoError(t, err)

	rec := postIAMForm(t, url.Values{"Action": {"CreateAccessKey"}, "UserName": {"admin"}}, tenantAdmin)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Equal(t, "AccessDenied", iamErrorCodeOf(t, rec.Body.String()))

	rec = postIAMForm(t, url.Values{"Action": {"GetUser"}, "UserName": {"admin"}}, tenantAdmin)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Equal(t, "AccessDenied", iamErrorCodeOf(t, rec.Body.String()))

	rec = postIAMForm(t, url.Values{"Action": {"ListUsers"}}, tenantAdmin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "<UserName>admin</UserName>")

	rec = postIAMForm(t, url.Values{"Action": {"ListAccessKeys"}, "UserName": {globalUser.Username}}, tenantAdmin)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Equal(t, "AccessDenied", iamErrorCodeOf(t, rec.Body.String()))

	rec = postIAMForm(t, url.Values{"Action": {"DeleteAccessKey"}, "AccessKeyId": {globalKey.AccessKeyID}}, tenantAdmin)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Equal(t, "AccessDenied", iamErrorCodeOf(t, rec.Body.String()))
}

func TestIsIAMAction(t *testing.T) {
	assert.True(t, IsIAMAction("CreateUser"))
	assert.True(t, IsIAMAction("SetDefaultPolicyVersion"))
	assert.False(t, IsIAMAction("GetSessionToken"))
	assert.False(t, IsIAMAction("AssumeRole"), "AssumeRole belongs to STS, not IAM")
	assert.False(t, IsIAMAction(""))
}

// --- response shape ---

func TestIAMXML_ResponseEnvelopeMatchesTheAWSShape(t *testing.T) {
	admin := iamAdmin("admin-shape")
	rec := postIAMForm(t, url.Values{"Action": {"ListUsers"}}, admin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	body := rec.Body.String()
	assert.Contains(t, body, "<ListUsersResponse")
	assert.Contains(t, body, iamXMLNamespace)
	assert.Contains(t, body, "<ListUsersResult>")
	assert.Contains(t, body, "<ResponseMetadata>")
	assert.Contains(t, body, "<RequestId>")
}

// --- entity lifecycle over the wire ---

func TestIAMXML_UserPolicyAndKeyLifecycle(t *testing.T) {
	admin := iamAdmin("admin-lifecycle")
	userName := "svc-lifecycle"
	policyDoc := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow",` +
		`"Action":["s3:GetObject"],"Resource":["arn:aws:s3:::backups/*"]}]}`

	t.Cleanup(func() {
		postIAMForm(t, url.Values{"Action": {"DeleteUser"}, "UserName": {userName}}, admin)
	})

	rec := postIAMForm(t, url.Values{"Action": {"CreateUser"}, "UserName": {userName}}, admin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "<UserName>"+userName+"</UserName>")
	assert.Contains(t, rec.Body.String(), "arn:aws:iam:::user/"+userName)

	// Creating the same identity twice must be reported, not silently accepted.
	rec = postIAMForm(t, url.Values{"Action": {"CreateUser"}, "UserName": {userName}}, admin)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "EntityAlreadyExists", iamErrorCodeOf(t, rec.Body.String()))

	rec = postIAMForm(t, url.Values{"Action": {"GetUser"}, "UserName": {userName}}, admin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = postIAMForm(t, url.Values{
		"Action": {"PutUserPolicy"}, "UserName": {userName},
		"PolicyName": {"backups"}, "PolicyDocument": {policyDoc},
	}, admin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = postIAMForm(t, url.Values{
		"Action": {"GetUserPolicy"}, "UserName": {userName}, "PolicyName": {"backups"},
	}, admin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "s3:GetObject")

	rec = postIAMForm(t, url.Values{"Action": {"ListUserPolicies"}, "UserName": {userName}}, admin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "<member>backups</member>")

	// A credential for the service identity — this is the call Veeam makes.
	rec = postIAMForm(t, url.Values{"Action": {"CreateAccessKey"}, "UserName": {userName}}, admin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "<SecretAccessKey>")

	rec = postIAMForm(t, url.Values{"Action": {"ListAccessKeys"}, "UserName": {userName}}, admin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "<SecretAccessKey>",
		"a listing must never repeat the secret")

	rec = postIAMForm(t, url.Values{
		"Action": {"DeleteUserPolicy"}, "UserName": {userName}, "PolicyName": {"backups"},
	}, admin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = postIAMForm(t, url.Values{"Action": {"DeleteUser"}, "UserName": {userName}}, admin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = postIAMForm(t, url.Values{"Action": {"GetUser"}, "UserName": {userName}}, admin)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "NoSuchEntity", iamErrorCodeOf(t, rec.Body.String()))
}

func TestIAMXML_ManagedPolicyAndAttachment(t *testing.T) {
	admin := iamAdmin("admin-managed")
	userName := "svc-managed"
	policyName := "TestManagedPolicy"
	policyARN := auth.IAMPolicyARN(policyName)
	doc := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:*"],"Resource":["*"]}]}`

	t.Cleanup(func() {
		postIAMForm(t, url.Values{
			"Action": {"DetachUserPolicy"}, "UserName": {userName}, "PolicyArn": {policyARN},
		}, admin)
		postIAMForm(t, url.Values{"Action": {"DeleteUser"}, "UserName": {userName}}, admin)
		postIAMForm(t, url.Values{"Action": {"DeletePolicy"}, "PolicyArn": {policyARN}}, admin)
	})

	require.Equal(t, http.StatusOK,
		postIAMForm(t, url.Values{"Action": {"CreateUser"}, "UserName": {userName}}, admin).Code)

	rec := postIAMForm(t, url.Values{
		"Action": {"CreatePolicy"}, "PolicyName": {policyName}, "PolicyDocument": {doc},
	}, admin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), policyARN)

	rec = postIAMForm(t, url.Values{
		"Action": {"AttachUserPolicy"}, "UserName": {userName}, "PolicyArn": {policyARN},
	}, admin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = postIAMForm(t, url.Values{"Action": {"ListAttachedUserPolicies"}, "UserName": {userName}}, admin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "<PolicyName>"+policyName+"</PolicyName>")

	// An attached policy cannot be deleted out from under the identity using it.
	rec = postIAMForm(t, url.Values{"Action": {"DeletePolicy"}, "PolicyArn": {policyARN}}, admin)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "DeleteConflict", iamErrorCodeOf(t, rec.Body.String()))

	// Versions: Veeam switches to managed policies once its inline budget runs out.
	rec = postIAMForm(t, url.Values{
		"Action": {"CreatePolicyVersion"}, "PolicyArn": {policyARN},
		"PolicyDocument": {`{"Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"*"}]}`},
		"SetAsDefault":   {"true"},
	}, admin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "<VersionId>v2</VersionId>")

	rec = postIAMForm(t, url.Values{"Action": {"ListPolicyVersions"}, "PolicyArn": {policyARN}}, admin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "v1")
	assert.Contains(t, rec.Body.String(), "v2")

	rec = postIAMForm(t, url.Values{
		"Action": {"SetDefaultPolicyVersion"}, "PolicyArn": {policyARN}, "VersionId": {"v1"},
	}, admin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestIAMXML_MalformedPolicyIsRejectedAtWriteTime(t *testing.T) {
	admin := iamAdmin("admin-malformed")
	userName := "svc-malformed"
	t.Cleanup(func() {
		postIAMForm(t, url.Values{"Action": {"DeleteUser"}, "UserName": {userName}}, admin)
	})
	require.Equal(t, http.StatusOK,
		postIAMForm(t, url.Values{"Action": {"CreateUser"}, "UserName": {userName}}, admin).Code)

	rec := postIAMForm(t, url.Values{
		"Action": {"PutUserPolicy"}, "UserName": {userName},
		"PolicyName": {"broken"}, "PolicyDocument": {`{"Statement":[{"Effect":"Perhaps"}]}`},
	}, admin)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "InvalidInput", iamErrorCodeOf(t, rec.Body.String()),
		"a policy that cannot be enforced must be refused while its author can still fix it")
}

func TestIAMXML_BuiltinPolicyIsVisibleAndProtected(t *testing.T) {
	admin := iamAdmin("admin-builtin")

	rec := postIAMForm(t, url.Values{"Action": {"ListPolicies"}}, admin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "ReadOnlyAccess")

	rec = postIAMForm(t, url.Values{
		"Action": {"DeletePolicy"}, "PolicyArn": {auth.IAMPolicyARN("ReadOnlyAccess")},
	}, admin)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "InvalidInput", iamErrorCodeOf(t, rec.Body.String()))
}

// --- roles over the wire ---

func TestIAMXML_RoleLifecycleAndAssumeRole(t *testing.T) {
	admin := iamAdmin("admin-roles")
	roleName := "TestBackupRole"
	trust := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow",` +
		`"Principal":{"AWS":"*"},"Action":"sts:AssumeRole"}]}`

	t.Cleanup(func() {
		postIAMForm(t, url.Values{"Action": {"DeleteRole"}, "RoleName": {roleName}}, admin)
	})

	rec := postIAMForm(t, url.Values{
		"Action": {"CreateRole"}, "RoleName": {roleName},
		"AssumeRolePolicyDocument": {trust}, "MaxSessionDuration": {"3600"},
	}, admin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), auth.IAMRoleARN(roleName))

	rec = postIAMForm(t, url.Values{
		"Action": {"PutRolePolicy"}, "RoleName": {roleName}, "PolicyName": {"scope"},
		"PolicyDocument": {`{"Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"*"}]}`},
	}, admin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = postIAMForm(t, url.Values{"Action": {"GetRole"}, "RoleName": {roleName}}, admin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "<RoleName>"+roleName+"</RoleName>")

	// AssumeRole is an STS action, so it goes through the STS branch — but it
	// now resolves the role this IAM call created.
	rec = postSTSForm(t, url.Values{
		"Action": {"AssumeRole"}, "RoleArn": {auth.IAMRoleARN(roleName)},
		"RoleSessionName": {"backup-job"},
	}, admin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "<AssumeRoleResponse")
	assert.Contains(t, rec.Body.String(), "backup-job")
}

func TestSTSXML_AssumeRoleRefusedByTrustPolicy(t *testing.T) {
	admin := iamAdmin("admin-closed-role")
	roleName := "TestClosedRole"
	trust := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow",` +
		`"Principal":{"AWS":"arn:aws:iam:::user/somebody-else"},"Action":"sts:AssumeRole"}]}`

	t.Cleanup(func() {
		postIAMForm(t, url.Values{"Action": {"DeleteRole"}, "RoleName": {roleName}}, admin)
	})

	require.Equal(t, http.StatusOK, postIAMForm(t, url.Values{
		"Action": {"CreateRole"}, "RoleName": {roleName}, "AssumeRolePolicyDocument": {trust},
	}, admin).Code)

	rec := postSTSForm(t, url.Values{
		"Action": {"AssumeRole"}, "RoleArn": {auth.IAMRoleARN(roleName)},
		"RoleSessionName": {"job"},
	}, admin)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "AccessDenied", stsErrorCodeOf(t, rec.Body.String()))
}
