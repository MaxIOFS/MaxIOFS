package server

import (
	"encoding/xml"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/sirupsen/logrus"
)

const iamXMLNamespace = "https://iam.amazonaws.com/doc/2010-05-08/"

// iamActions is the set of Action values routed to the IAM handler. It exists
// so the shared POST / dispatcher can tell an IAM call from an STS one without
// depending on the Version parameter, which clients do not always send.
var iamActions = map[string]bool{
	"CreateUser": true, "DeleteUser": true, "GetUser": true, "ListUsers": true,
	"CreateAccessKey": true, "DeleteAccessKey": true, "ListAccessKeys": true,
	"CreatePolicy": true, "DeletePolicy": true, "GetPolicy": true, "ListPolicies": true,
	"CreatePolicyVersion": true, "DeletePolicyVersion": true, "GetPolicyVersion": true,
	"ListPolicyVersions": true, "SetDefaultPolicyVersion": true,
	"PutUserPolicy": true, "GetUserPolicy": true, "DeleteUserPolicy": true, "ListUserPolicies": true,
	"AttachUserPolicy": true, "DetachUserPolicy": true, "ListAttachedUserPolicies": true,
	"PutRolePolicy": true, "GetRolePolicy": true, "DeleteRolePolicy": true, "ListRolePolicies": true,
	"AttachRolePolicy": true, "DetachRolePolicy": true, "ListAttachedRolePolicies": true,
	"PutGroupPolicy": true, "GetGroupPolicy": true, "DeleteGroupPolicy": true, "ListGroupPolicies": true,
	"AttachGroupPolicy": true, "DetachGroupPolicy": true, "ListAttachedGroupPolicies": true,
	"CreateRole": true, "DeleteRole": true, "GetRole": true, "ListRoles": true,
	"UpdateAssumeRolePolicy": true,
}

// IsIAMAction reports whether an Action belongs to the IAM protocol.
func IsIAMAction(action string) bool { return iamActions[action] }

// iamSTSEndpointForSOSAPI returns the URL to advertise to Veeam as both the IAM
func (s *Server) iamSTSEndpointForSOSAPI() string {
	if s.settingsManager == nil {
		return ""
	}
	if enabled, err := s.settingsManager.GetBool("security.iam_api_enabled"); err != nil || !enabled {
		return ""
	}
	if _, ok := s.authManager.(auth.IAMManager); !ok {
		return ""
	}

	endpoint := strings.TrimRight(s.config.PublicAPIURL, "/")
	if endpoint == "" {
		return ""
	}
	return endpoint
}

// --- response documents ---

type iamResponseMetadata struct {
	RequestID string `xml:"RequestId"`
}

// iamResponse wraps any IAM result. Both element names are set at runtime from
// the action, which is what keeps forty near-identical wrapper structs from
// existing.
type iamResponse struct {
	XMLName  xml.Name
	Xmlns    string `xml:"xmlns,attr"`
	Result   interface{}
	Metadata iamResponseMetadata `xml:"ResponseMetadata"`
}

type iamUserDoc struct {
	XMLName    xml.Name `xml:"User"`
	Path       string   `xml:"Path"`
	UserName   string   `xml:"UserName"`
	UserID     string   `xml:"UserId"`
	Arn        string   `xml:"Arn"`
	CreateDate string   `xml:"CreateDate"`
}

type iamAccessKeyDoc struct {
	XMLName         xml.Name `xml:"AccessKey"`
	UserName        string   `xml:"UserName"`
	AccessKeyID     string   `xml:"AccessKeyId"`
	Status          string   `xml:"Status"`
	SecretAccessKey string   `xml:"SecretAccessKey,omitempty"`
	CreateDate      string   `xml:"CreateDate"`
}

type iamAccessKeyMetadataDoc struct {
	UserName    string `xml:"UserName"`
	AccessKeyID string `xml:"AccessKeyId"`
	Status      string `xml:"Status"`
	CreateDate  string `xml:"CreateDate"`
}

type iamPolicyDoc struct {
	XMLName          xml.Name `xml:"Policy"`
	PolicyName       string   `xml:"PolicyName"`
	PolicyID         string   `xml:"PolicyId"`
	Arn              string   `xml:"Arn"`
	Path             string   `xml:"Path"`
	DefaultVersionID string   `xml:"DefaultVersionId"`
	AttachmentCount  int      `xml:"AttachmentCount"`
	IsAttachable     bool     `xml:"IsAttachable"`
	Description      string   `xml:"Description,omitempty"`
	CreateDate       string   `xml:"CreateDate"`
	UpdateDate       string   `xml:"UpdateDate"`
}

type iamPolicyVersionDoc struct {
	XMLName          xml.Name `xml:"PolicyVersion"`
	Document         string   `xml:"Document"`
	VersionID        string   `xml:"VersionId"`
	IsDefaultVersion bool     `xml:"IsDefaultVersion"`
	CreateDate       string   `xml:"CreateDate"`
}

type iamRoleDoc struct {
	XMLName                  xml.Name `xml:"Role"`
	Path                     string   `xml:"Path"`
	RoleName                 string   `xml:"RoleName"`
	RoleID                   string   `xml:"RoleId"`
	Arn                      string   `xml:"Arn"`
	AssumeRolePolicyDocument string   `xml:"AssumeRolePolicyDocument"`
	MaxSessionDuration       int      `xml:"MaxSessionDuration"`
	Description              string   `xml:"Description,omitempty"`
	CreateDate               string   `xml:"CreateDate"`
}

type iamAttachedPolicyDoc struct {
	PolicyName string `xml:"PolicyName"`
	PolicyArn  string `xml:"PolicyArn"`
}

// iamEmptyResult is the body of the actions that return nothing but still need
// a well-formed response envelope.
type iamEmptyResult struct {
	XMLName xml.Name
}

type iamErrorResponse struct {
	XMLName xml.Name `xml:"ErrorResponse"`
	Xmlns   string   `xml:"xmlns,attr"`
	Error   struct {
		Type    string `xml:"Type"`
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	} `xml:"Error"`
	RequestID string `xml:"RequestId"`
}

// --- dispatcher ---

// handleAWSIAMRequest serves the IAM query protocol. The form has already been
// parsed by the shared POST / dispatcher.
func (s *Server) handleAWSIAMRequest(w http.ResponseWriter, r *http.Request) {
	action := r.PostForm.Get("Action")

	iamManager, ok := s.authManager.(auth.IAMManager)
	if !ok {
		writeIAMError(w, r, http.StatusServiceUnavailable, "ServiceFailure", "IAM is not available on this server.")
		return
	}

	if enabled, err := s.settingsManager.GetBool("security.iam_api_enabled"); err != nil || !enabled {
		writeIAMError(w, r, http.StatusForbidden, "AccessDenied", "The IAM API is disabled on this server.")
		return
	}

	caller, err := s.authorizeIAMCaller(r)
	if err != nil {
		s.auditIAMDenial(r, action, err.Error())
		writeIAMError(w, r, http.StatusForbidden, "AccessDenied", "Access denied.")
		return
	}

	switch action {
	// Identities
	case "CreateUser":
		s.iamCreateUser(w, r, iamManager, caller)
	case "DeleteUser":
		s.iamDeleteUser(w, r, iamManager)
	case "GetUser":
		s.iamGetUser(w, r, iamManager, caller)
	case "ListUsers":
		s.iamListUsers(w, r, iamManager)

	// Credentials
	case "CreateAccessKey":
		s.iamCreateAccessKey(w, r, iamManager, caller)
	case "DeleteAccessKey":
		s.iamDeleteAccessKey(w, r)
	case "ListAccessKeys":
		s.iamListAccessKeys(w, r, iamManager, caller)

	// Managed policies
	case "CreatePolicy":
		s.iamCreatePolicy(w, r, iamManager)
	case "DeletePolicy":
		s.iamDeletePolicy(w, r, iamManager)
	case "GetPolicy":
		s.iamGetPolicy(w, r, iamManager)
	case "ListPolicies":
		s.iamListPolicies(w, r, iamManager)

	// Policy versions
	case "CreatePolicyVersion":
		s.iamCreatePolicyVersion(w, r, iamManager)
	case "GetPolicyVersion":
		s.iamGetPolicyVersion(w, r, iamManager)
	case "ListPolicyVersions":
		s.iamListPolicyVersions(w, r, iamManager)
	case "DeletePolicyVersion":
		s.iamDeletePolicyVersion(w, r, iamManager)
	case "SetDefaultPolicyVersion":
		s.iamSetDefaultPolicyVersion(w, r, iamManager)

	// Inline policies, one implementation for each kind of target
	case "PutUserPolicy", "PutRolePolicy", "PutGroupPolicy":
		s.iamPutInlinePolicy(w, r, iamManager, action)
	case "GetUserPolicy", "GetRolePolicy", "GetGroupPolicy":
		s.iamGetInlinePolicy(w, r, iamManager, action)
	case "DeleteUserPolicy", "DeleteRolePolicy", "DeleteGroupPolicy":
		s.iamDeleteInlinePolicy(w, r, iamManager, action)
	case "ListUserPolicies", "ListRolePolicies", "ListGroupPolicies":
		s.iamListInlinePolicies(w, r, iamManager, action)

	// Attachments, likewise
	case "AttachUserPolicy", "AttachRolePolicy", "AttachGroupPolicy":
		s.iamAttachPolicy(w, r, iamManager, action, true)
	case "DetachUserPolicy", "DetachRolePolicy", "DetachGroupPolicy":
		s.iamAttachPolicy(w, r, iamManager, action, false)
	case "ListAttachedUserPolicies", "ListAttachedRolePolicies", "ListAttachedGroupPolicies":
		s.iamListAttachedPolicies(w, r, iamManager, action)

	// Roles
	case "CreateRole":
		s.iamCreateRole(w, r, iamManager, caller)
	case "GetRole":
		s.iamGetRole(w, r, iamManager)
	case "ListRoles":
		s.iamListRoles(w, r, iamManager)
	case "DeleteRole":
		s.iamDeleteRole(w, r, iamManager)
	case "UpdateAssumeRolePolicy":
		s.iamUpdateAssumeRolePolicy(w, r, iamManager)

	default:
		writeIAMError(w, r, http.StatusBadRequest, "InvalidAction",
			"The action "+action+" is not valid for this endpoint.")
	}
}

// authorizeIAMCaller establishes who is making an IAM call and that they are
// allowed to. The signature has already been verified by the S3 middleware;
// what is checked here is the kind of credential and the capability.
func (s *Server) authorizeIAMCaller(r *http.Request) (*auth.User, error) {
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok || user == nil {
		return nil, errors.New("request was not signed with valid credentials")
	}
	if stsRequestSignedWithTemporaryCredential(r) {
		return nil, errors.New("temporary credentials cannot manage IAM entities")
	}
	if !auth.CheckCapabilityInContext(r.Context(), s.authManager, auth.CapIAMManage) {
		return nil, errors.New("caller lacks the iam:manage capability")
	}
	return user, nil
}

// --- identities ---

func (s *Server) iamCreateUser(w http.ResponseWriter, r *http.Request, im auth.IAMManager, caller *auth.User) {
	userName := r.PostForm.Get("UserName")

	user, err := im.CreateIAMUser(r.Context(), userName, r.PostForm.Get("Path"), caller.TenantID)
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}

	s.auditIAMChange(r, caller, "CreateUser", userName)
	s.afterIAMWrite(r.Context())

	writeIAMXML(w, r, "CreateUser", &iamUserDoc{
		Path:       iamPathOf(user),
		UserName:   user.Username,
		UserID:     user.ID,
		Arn:        auth.IAMUserARN(user.Username),
		CreateDate: iamTime(user.CreatedAt),
	})
}

func (s *Server) iamDeleteUser(w http.ResponseWriter, r *http.Request, im auth.IAMManager) {
	userName := r.PostForm.Get("UserName")
	// The tombstones have to name what the delete removed, so the identity is
	// resolved before it stops existing.
	userID, _ := im.ResolveIAMUserID(r.Context(), userName)
	inlineNames := s.iamInlinePolicyNames(r.Context(), im, auth.IAMTargetUser, userID)
	attachedNames := s.iamAttachedPolicyNames(r.Context(), im, auth.IAMTargetUser, userID)

	if err := im.DeleteIAMUser(r.Context(), userName); err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}

	for _, name := range inlineNames {
		s.recordIAMDeletion(r.Context(), cluster.EntityTypeIAMInlinePolicy,
			iamInlineTombstoneID(auth.IAMTargetUser, userID, name))
	}
	for _, name := range attachedNames {
		s.recordIAMDeletion(r.Context(), cluster.EntityTypeIAMAttachment,
			iamAttachmentTombstoneID(name, auth.IAMTargetUser, userID))
	}

	s.afterIAMWrite(r.Context())
	writeIAMEmpty(w, r, "DeleteUser")
}

func (s *Server) iamGetUser(w http.ResponseWriter, r *http.Request, im auth.IAMManager, caller *auth.User) {
	// AWS treats GetUser with no UserName as "describe me".
	userName := r.PostForm.Get("UserName")
	if userName == "" {
		userName = caller.Username
	}

	user, err := im.GetIAMUser(r.Context(), userName)
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}
	writeIAMXML(w, r, "GetUser", &iamUserDoc{
		Path:       iamPathOf(user),
		UserName:   user.Username,
		UserID:     user.ID,
		Arn:        auth.IAMUserARN(user.Username),
		CreateDate: iamTime(user.CreatedAt),
	})
}

func (s *Server) iamListUsers(w http.ResponseWriter, r *http.Request, im auth.IAMManager) {
	users, err := im.ListIAMUsers(r.Context())
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}

	type result struct {
		XMLName     xml.Name
		Users       []iamUserDoc `xml:"Users>member"`
		IsTruncated bool         `xml:"IsTruncated"`
	}
	out := &result{XMLName: xml.Name{Local: "ListUsersResult"}}
	for _, u := range users {
		out.Users = append(out.Users, iamUserDoc{
			Path:       iamPathOf(u),
			UserName:   u.Username,
			UserID:     u.ID,
			Arn:        auth.IAMUserARN(u.Username),
			CreateDate: iamTime(u.CreatedAt),
		})
	}
	writeIAMResult(w, r, "ListUsers", out)
}

// --- credentials ---

func (s *Server) iamCreateAccessKey(w http.ResponseWriter, r *http.Request, im auth.IAMManager, caller *auth.User) {
	userName := r.PostForm.Get("UserName")
	if userName == "" {
		userName = caller.Username
	}

	user, err := im.GetIAMUser(r.Context(), userName)
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}

	key, err := s.authManager.GenerateAccessKey(r.Context(), user.ID)
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}

	s.auditIAMChange(r, caller, "CreateAccessKey", userName)
	s.afterIAMWrite(r.Context())
	if s.accessKeySyncMgr != nil {
		s.accessKeySyncMgr.TriggerSync(r.Context())
	}

	writeIAMXML(w, r, "CreateAccessKey", &iamAccessKeyDoc{
		UserName:        user.Username,
		AccessKeyID:     key.AccessKeyID,
		Status:          "Active",
		SecretAccessKey: key.SecretAccessKey,
		CreateDate:      iamTime(key.CreatedAt),
	})
}

func (s *Server) iamDeleteAccessKey(w http.ResponseWriter, r *http.Request) {
	accessKeyID := r.PostForm.Get("AccessKeyId")
	if accessKeyID == "" {
		writeIAMError(w, r, http.StatusBadRequest, "ValidationError", "AccessKeyId is required.")
		return
	}
	if err := s.authManager.RevokeAccessKey(r.Context(), accessKeyID); err != nil {
		writeIAMError(w, r, http.StatusNotFound, "NoSuchEntity", "The access key does not exist.")
		return
	}
	s.afterIAMWrite(r.Context())
	if s.accessKeySyncMgr != nil {
		s.accessKeySyncMgr.TriggerSync(r.Context())
	}
	writeIAMEmpty(w, r, "DeleteAccessKey")
}

func (s *Server) iamListAccessKeys(w http.ResponseWriter, r *http.Request, im auth.IAMManager, caller *auth.User) {
	userName := r.PostForm.Get("UserName")
	if userName == "" {
		userName = caller.Username
	}

	user, err := im.GetIAMUser(r.Context(), userName)
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}
	keys, err := s.authManager.ListAccessKeys(r.Context(), user.ID)
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}

	type result struct {
		XMLName     xml.Name
		Keys        []iamAccessKeyMetadataDoc `xml:"AccessKeyMetadata>member"`
		IsTruncated bool                      `xml:"IsTruncated"`
	}
	out := &result{XMLName: xml.Name{Local: "ListAccessKeysResult"}}
	for _, k := range keys {
		status := "Active"
		if k.Status != auth.AccessKeyStatusActive {
			status = "Inactive"
		}
		out.Keys = append(out.Keys, iamAccessKeyMetadataDoc{
			UserName:    user.Username,
			AccessKeyID: k.AccessKeyID,
			Status:      status,
			CreateDate:  iamTime(k.CreatedAt),
		})
	}
	writeIAMResult(w, r, "ListAccessKeys", out)
}

// --- managed policies ---

func (s *Server) iamCreatePolicy(w http.ResponseWriter, r *http.Request, im auth.IAMManager) {
	policy, err := im.CreateIAMPolicy(r.Context(),
		r.PostForm.Get("PolicyName"), r.PostForm.Get("Path"),
		r.PostForm.Get("Description"), r.PostForm.Get("PolicyDocument"))
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}
	s.afterIAMWrite(r.Context())
	writeIAMXML(w, r, "CreatePolicy", iamPolicyDocOf(policy, 0))
}

func (s *Server) iamGetPolicy(w http.ResponseWriter, r *http.Request, im auth.IAMManager) {
	policy, err := s.iamPolicyFromRequest(r, im)
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}
	writeIAMXML(w, r, "GetPolicy", iamPolicyDocOf(policy, im.CountIAMPolicyAttachments(r.Context(), policy.Name)))
}

func (s *Server) iamListPolicies(w http.ResponseWriter, r *http.Request, im auth.IAMManager) {
	policies, err := im.ListIAMPolicies(r.Context())
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}

	type result struct {
		XMLName     xml.Name
		Policies    []iamPolicyDoc `xml:"Policies>member"`
		IsTruncated bool           `xml:"IsTruncated"`
	}
	out := &result{XMLName: xml.Name{Local: "ListPoliciesResult"}}
	for _, p := range policies {
		out.Policies = append(out.Policies, *iamPolicyDocOf(p, im.CountIAMPolicyAttachments(r.Context(), p.Name)))
	}
	writeIAMResult(w, r, "ListPolicies", out)
}

func (s *Server) iamDeletePolicy(w http.ResponseWriter, r *http.Request, im auth.IAMManager) {
	policy, err := s.iamPolicyFromRequest(r, im)
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}
	if err := im.DeleteIAMPolicy(r.Context(), policy.Name); err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}
	s.recordIAMDeletion(r.Context(), cluster.EntityTypeIAMPolicy, policy.Name)
	s.afterIAMWrite(r.Context())
	writeIAMEmpty(w, r, "DeletePolicy")
}

// --- policy versions ---

func (s *Server) iamCreatePolicyVersion(w http.ResponseWriter, r *http.Request, im auth.IAMManager) {
	policy, err := s.iamPolicyFromRequest(r, im)
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}

	setDefault := strings.EqualFold(r.PostForm.Get("SetAsDefault"), "true")
	version, err := im.CreateIAMPolicyVersion(r.Context(), policy.Name, r.PostForm.Get("PolicyDocument"), setDefault)
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}
	s.afterIAMWrite(r.Context())
	writeIAMXML(w, r, "CreatePolicyVersion", iamPolicyVersionDocOf(version))
}

func (s *Server) iamGetPolicyVersion(w http.ResponseWriter, r *http.Request, im auth.IAMManager) {
	policy, err := s.iamPolicyFromRequest(r, im)
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}
	version, err := im.GetIAMPolicyVersion(r.Context(), policy.Name, r.PostForm.Get("VersionId"))
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}
	writeIAMXML(w, r, "GetPolicyVersion", iamPolicyVersionDocOf(version))
}

func (s *Server) iamListPolicyVersions(w http.ResponseWriter, r *http.Request, im auth.IAMManager) {
	policy, err := s.iamPolicyFromRequest(r, im)
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}
	versions, err := im.ListIAMPolicyVersions(r.Context(), policy.Name)
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}

	type result struct {
		XMLName     xml.Name
		Versions    []iamPolicyVersionDoc `xml:"Versions>member"`
		IsTruncated bool                  `xml:"IsTruncated"`
	}
	out := &result{XMLName: xml.Name{Local: "ListPolicyVersionsResult"}}
	for _, v := range versions {
		out.Versions = append(out.Versions, *iamPolicyVersionDocOf(v))
	}
	writeIAMResult(w, r, "ListPolicyVersions", out)
}

func (s *Server) iamDeletePolicyVersion(w http.ResponseWriter, r *http.Request, im auth.IAMManager) {
	policy, err := s.iamPolicyFromRequest(r, im)
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}
	if err := im.DeleteIAMPolicyVersion(r.Context(), policy.Name, r.PostForm.Get("VersionId")); err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}
	s.afterIAMWrite(r.Context())
	writeIAMEmpty(w, r, "DeletePolicyVersion")
}

func (s *Server) iamSetDefaultPolicyVersion(w http.ResponseWriter, r *http.Request, im auth.IAMManager) {
	policy, err := s.iamPolicyFromRequest(r, im)
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}
	if err := im.SetDefaultIAMPolicyVersion(r.Context(), policy.Name, r.PostForm.Get("VersionId")); err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}
	s.afterIAMWrite(r.Context())
	writeIAMEmpty(w, r, "SetDefaultPolicyVersion")
}

// --- inline policies ---

func (s *Server) iamPutInlinePolicy(w http.ResponseWriter, r *http.Request, im auth.IAMManager, action string) {
	targetType, targetID, err := s.iamResolveTarget(r, im, action)
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}
	if err := im.PutIAMInlinePolicy(r.Context(), targetType, targetID,
		r.PostForm.Get("PolicyName"), r.PostForm.Get("PolicyDocument")); err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}
	s.afterIAMWrite(r.Context())
	writeIAMEmpty(w, r, action)
}

func (s *Server) iamGetInlinePolicy(w http.ResponseWriter, r *http.Request, im auth.IAMManager, action string) {
	targetType, targetID, err := s.iamResolveTarget(r, im, action)
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}
	inline, err := im.GetIAMInlinePolicy(r.Context(), targetType, targetID, r.PostForm.Get("PolicyName"))
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}

	// The document goes back as it was stored. AWS returns it URL-encoded, but
	// nothing in this ecosystem agrees on that and a client that gets raw JSON
	// where it expected raw JSON is the common case.
	type result struct {
		XMLName        xml.Name
		TargetName     string `xml:"-"`
		PolicyName     string `xml:"PolicyName"`
		PolicyDocument string `xml:"PolicyDocument"`
	}
	out := &result{
		XMLName:        xml.Name{Local: action + "Result"},
		PolicyName:     inline.Name,
		PolicyDocument: inline.Document,
	}
	writeIAMResult(w, r, action, out)
}

func (s *Server) iamDeleteInlinePolicy(w http.ResponseWriter, r *http.Request, im auth.IAMManager, action string) {
	targetType, targetID, err := s.iamResolveTarget(r, im, action)
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}
	policyName := r.PostForm.Get("PolicyName")
	if err := im.DeleteIAMInlinePolicy(r.Context(), targetType, targetID, policyName); err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}
	s.recordIAMDeletion(r.Context(), cluster.EntityTypeIAMInlinePolicy,
		iamInlineTombstoneID(targetType, targetID, policyName))
	s.afterIAMWrite(r.Context())
	writeIAMEmpty(w, r, action)
}

func (s *Server) iamListInlinePolicies(w http.ResponseWriter, r *http.Request, im auth.IAMManager, action string) {
	targetType, targetID, err := s.iamResolveTarget(r, im, action)
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}
	policies, err := im.ListIAMInlinePolicies(r.Context(), targetType, targetID)
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}

	type result struct {
		XMLName     xml.Name
		Names       []string `xml:"PolicyNames>member"`
		IsTruncated bool     `xml:"IsTruncated"`
	}
	out := &result{XMLName: xml.Name{Local: action + "Result"}}
	for _, p := range policies {
		out.Names = append(out.Names, p.Name)
	}
	writeIAMResult(w, r, action, out)
}

// --- attachments ---

func (s *Server) iamAttachPolicy(w http.ResponseWriter, r *http.Request, im auth.IAMManager, action string, attach bool) {
	targetType, targetID, err := s.iamResolveTarget(r, im, action)
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}

	policyName, err := iamPolicyNameFromARN(r.PostForm.Get("PolicyArn"))
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}

	if attach {
		err = im.AttachIAMPolicy(r.Context(), policyName, targetType, targetID)
	} else {
		err = im.DetachIAMPolicy(r.Context(), policyName, targetType, targetID)
	}
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}

	if attach {
		s.triggerIAMSync(r.Context())
	} else {
		s.recordIAMDeletion(r.Context(), cluster.EntityTypeIAMAttachment,
			iamAttachmentTombstoneID(policyName, targetType, targetID))
	}
	s.afterIAMWrite(r.Context())
	writeIAMEmpty(w, r, action)
}

func (s *Server) iamListAttachedPolicies(w http.ResponseWriter, r *http.Request, im auth.IAMManager, action string) {
	targetType, targetID, err := s.iamResolveTarget(r, im, action)
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}
	policies, err := im.ListAttachedIAMPolicies(r.Context(), targetType, targetID)
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}

	type result struct {
		XMLName     xml.Name
		Policies    []iamAttachedPolicyDoc `xml:"AttachedPolicies>member"`
		IsTruncated bool                   `xml:"IsTruncated"`
	}
	out := &result{XMLName: xml.Name{Local: action + "Result"}}
	for _, p := range policies {
		out.Policies = append(out.Policies, iamAttachedPolicyDoc{PolicyName: p.Name, PolicyArn: p.ARN})
	}
	writeIAMResult(w, r, action, out)
}

// --- roles ---

func (s *Server) iamCreateRole(w http.ResponseWriter, r *http.Request, im auth.IAMManager, caller *auth.User) {
	maxDuration := 0
	if raw := r.PostForm.Get("MaxSessionDuration"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeIAMError(w, r, http.StatusBadRequest, "ValidationError", "MaxSessionDuration must be an integer.")
			return
		}
		maxDuration = parsed
	}

	role, err := im.CreateIAMRole(r.Context(),
		r.PostForm.Get("RoleName"), r.PostForm.Get("Path"), r.PostForm.Get("Description"),
		r.PostForm.Get("AssumeRolePolicyDocument"), maxDuration, caller.TenantID)
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}

	s.auditIAMChange(r, caller, "CreateRole", role.Name)
	s.afterIAMWrite(r.Context())
	writeIAMXML(w, r, "CreateRole", iamRoleDocOf(role))
}

func (s *Server) iamGetRole(w http.ResponseWriter, r *http.Request, im auth.IAMManager) {
	role, err := im.GetIAMRole(r.Context(), r.PostForm.Get("RoleName"))
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}
	writeIAMXML(w, r, "GetRole", iamRoleDocOf(role))
}

func (s *Server) iamListRoles(w http.ResponseWriter, r *http.Request, im auth.IAMManager) {
	roles, err := im.ListIAMRoles(r.Context())
	if err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}

	type result struct {
		XMLName     xml.Name
		Roles       []iamRoleDoc `xml:"Roles>member"`
		IsTruncated bool         `xml:"IsTruncated"`
	}
	out := &result{XMLName: xml.Name{Local: "ListRolesResult"}}
	for _, role := range roles {
		out.Roles = append(out.Roles, *iamRoleDocOf(role))
	}
	writeIAMResult(w, r, "ListRoles", out)
}

func (s *Server) iamDeleteRole(w http.ResponseWriter, r *http.Request, im auth.IAMManager) {
	roleName := r.PostForm.Get("RoleName")
	inlineNames := s.iamInlinePolicyNames(r.Context(), im, auth.IAMTargetRole, roleName)
	attachedNames := s.iamAttachedPolicyNames(r.Context(), im, auth.IAMTargetRole, roleName)

	if err := im.DeleteIAMRole(r.Context(), roleName); err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}

	s.recordIAMDeletion(r.Context(), cluster.EntityTypeIAMRole, roleName)
	for _, name := range inlineNames {
		s.recordIAMDeletion(r.Context(), cluster.EntityTypeIAMInlinePolicy,
			iamInlineTombstoneID(auth.IAMTargetRole, roleName, name))
	}
	for _, name := range attachedNames {
		s.recordIAMDeletion(r.Context(), cluster.EntityTypeIAMAttachment,
			iamAttachmentTombstoneID(name, auth.IAMTargetRole, roleName))
	}

	s.afterIAMWrite(r.Context())
	writeIAMEmpty(w, r, "DeleteRole")
}

func (s *Server) iamUpdateAssumeRolePolicy(w http.ResponseWriter, r *http.Request, im auth.IAMManager) {
	if err := im.UpdateIAMRoleTrustPolicy(r.Context(),
		r.PostForm.Get("RoleName"), r.PostForm.Get("PolicyDocument")); err != nil {
		writeIAMErrorFor(w, r, err)
		return
	}
	s.afterIAMWrite(r.Context())
	writeIAMEmpty(w, r, "UpdateAssumeRolePolicy")
}

// --- helpers ---

// iamResolveTarget works out which entity a policy action applies to from the
// action name and the UserName / RoleName / GroupName parameter that goes with it.
func (s *Server) iamResolveTarget(r *http.Request, im auth.IAMManager, action string) (string, string, error) {
	switch {
	case strings.Contains(action, "User"):
		name := r.PostForm.Get("UserName")
		id, err := im.ResolveIAMUserID(r.Context(), name)
		if err != nil {
			return "", "", err
		}
		return auth.IAMTargetUser, id, nil

	case strings.Contains(action, "Role"):
		name := r.PostForm.Get("RoleName")
		if _, err := im.GetIAMRole(r.Context(), name); err != nil {
			return "", "", err
		}
		// Roles key on their name: it is their primary key and their ARN.
		return auth.IAMTargetRole, name, nil

	case strings.Contains(action, "Group"):
		name := r.PostForm.Get("GroupName")
		group, err := s.authManager.GetGroupByName(r.Context(), name, "")
		if err != nil || group == nil {
			return "", "", auth.ErrIAMNoSuchEntity
		}
		return auth.IAMTargetGroup, group.ID, nil
	}
	return "", "", auth.ErrIAMInvalidInput
}

// iamPolicyFromRequest resolves the policy an action names, accepting either
// PolicyArn (what AWS sends) or PolicyName (what a person typing by hand sends).
func (s *Server) iamPolicyFromRequest(r *http.Request, im auth.IAMManager) (*auth.IAMPolicy, error) {
	name := r.PostForm.Get("PolicyName")
	if arn := r.PostForm.Get("PolicyArn"); arn != "" {
		parsed, err := iamPolicyNameFromARN(arn)
		if err != nil {
			return nil, err
		}
		name = parsed
	}
	if name == "" {
		return nil, auth.ErrIAMInvalidInput
	}
	return im.GetIAMPolicy(r.Context(), name)
}

func iamPolicyNameFromARN(arn string) (string, error) {
	if arn == "" {
		return "", auth.ErrIAMInvalidInput
	}
	resourceType, name, err := auth.ParseIAMARN(arn)
	if err != nil {
		return "", err
	}
	if resourceType != "policy" {
		return "", auth.ErrIAMInvalidInput
	}
	return name, nil
}

func iamPolicyDocOf(p *auth.IAMPolicy, attachments int) *iamPolicyDoc {
	return &iamPolicyDoc{
		PolicyName:       p.Name,
		PolicyID:         p.Name,
		Arn:              p.ARN,
		Path:             p.Path,
		DefaultVersionID: p.DefaultVersionID,
		AttachmentCount:  attachments,
		IsAttachable:     true,
		Description:      p.Description,
		CreateDate:       iamTime(p.CreatedAt),
		UpdateDate:       iamTime(p.UpdatedAt),
	}
}

func iamPolicyVersionDocOf(v *auth.IAMPolicyVersion) *iamPolicyVersionDoc {
	return &iamPolicyVersionDoc{
		Document:         v.Document,
		VersionID:        v.VersionID,
		IsDefaultVersion: v.IsDefault,
		CreateDate:       iamTime(v.CreatedAt),
	}
}

func iamRoleDocOf(role *auth.IAMRole) *iamRoleDoc {
	return &iamRoleDoc{
		Path:                     role.Path,
		RoleName:                 role.Name,
		RoleID:                   role.Name,
		Arn:                      role.ARN,
		AssumeRolePolicyDocument: role.AssumeRolePolicy,
		MaxSessionDuration:       role.MaxSessionDuration,
		Description:              role.Description,
		CreateDate:               iamTime(role.CreatedAt),
	}
}

// iamPathOf reads back the organisational path stored when the identity was
// created. Console users have none, so they report the root.
func iamPathOf(user *auth.User) string {
	if user.Metadata != nil {
		if path := user.Metadata["iam_path"]; path != "" {
			return path
		}
	}
	return "/"
}

func iamTime(unix int64) string {
	return time.Unix(unix, 0).UTC().Format(time.RFC3339)
}

func (s *Server) auditIAMChange(r *http.Request, caller *auth.User, action, target string) {
	s.logAuditEvent(r.Context(), &audit.AuditEvent{
		TenantID:     caller.TenantID,
		UserID:       caller.ID,
		Username:     caller.Username,
		EventType:    audit.EventTypeUserCreated,
		ResourceType: audit.ResourceTypeUser,
		ResourceName: target,
		Action:       audit.ActionCreate,
		Status:       audit.StatusSuccess,
		IPAddress:    getClientIP(r, s.config.TrustedProxies),
		UserAgent:    r.Header.Get("User-Agent"),
		Details:      map[string]interface{}{"method": "aws-iam", "iam_action": action},
	})
}

func (s *Server) auditIAMDenial(r *http.Request, action, reason string) {
	logrus.WithFields(logrus.Fields{"iam_action": action, "reason": reason}).
		Warn("IAM request denied")
}

// --- response writing ---

// writeIAMXML wraps a single result document in the envelope for its action.
func writeIAMXML(w http.ResponseWriter, r *http.Request, action string, result interface{}) {
	type wrapper struct {
		XMLName xml.Name
		Value   interface{}
	}
	writeIAMResult(w, r, action, &wrapper{
		XMLName: xml.Name{Local: action + "Result"},
		Value:   result,
	})
}

func writeIAMEmpty(w http.ResponseWriter, r *http.Request, action string) {
	writeIAMResult(w, r, action, &iamEmptyResult{XMLName: xml.Name{Local: action + "Result"}})
}

// writeIAMResult emits the full <ActionResponse> envelope. The result value
// carries its own element name, so one envelope serves every action.
func writeIAMResult(w http.ResponseWriter, r *http.Request, action string, result interface{}) {
	resp := &iamResponse{
		XMLName:  xml.Name{Local: action + "Response"},
		Xmlns:    iamXMLNamespace,
		Result:   result,
		Metadata: iamResponseMetadata{RequestID: requestIDOf(r)},
	}

	body, err := xml.Marshal(resp)
	if err != nil {
		writeIAMError(w, r, http.StatusInternalServerError, "ServiceFailure", "Failed to encode the response.")
		return
	}

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(body)
}

// writeIAMErrorFor maps an internal error to the AWS IAM error code a client
func writeIAMErrorFor(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrIAMNoSuchEntity):
		writeIAMError(w, r, http.StatusNotFound, "NoSuchEntity", err.Error())
	case errors.Is(err, auth.ErrIAMEntityExists):
		writeIAMError(w, r, http.StatusConflict, "EntityAlreadyExists", err.Error())
	case errors.Is(err, auth.ErrIAMLimitExceeded):
		writeIAMError(w, r, http.StatusConflict, "LimitExceeded", err.Error())
	case errors.Is(err, auth.ErrIAMDeleteConflict):
		writeIAMError(w, r, http.StatusConflict, "DeleteConflict", err.Error())
	case errors.Is(err, auth.ErrIAMInvalidInput):
		writeIAMError(w, r, http.StatusBadRequest, "InvalidInput", err.Error())
	case errors.Is(err, auth.ErrAccessDenied):
		writeIAMError(w, r, http.StatusForbidden, "AccessDenied", "Access denied.")
	default:
		writeIAMError(w, r, http.StatusInternalServerError, "ServiceFailure", err.Error())
	}
}

func writeIAMError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	resp := &iamErrorResponse{Xmlns: iamXMLNamespace, RequestID: requestIDOf(r)}
	resp.Error.Type = "Sender"
	if status >= 500 {
		resp.Error.Type = "Receiver"
	}
	resp.Error.Code = code
	resp.Error.Message = message

	body, err := xml.Marshal(resp)
	if err != nil {
		http.Error(w, message, status)
		return
	}

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(body)
}
