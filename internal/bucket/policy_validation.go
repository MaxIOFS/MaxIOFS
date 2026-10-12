package bucket

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrMalformedPolicy is a bucket policy that cannot be enforced as written.
var ErrMalformedPolicy = errors.New("malformed bucket policy")

// Statement elements, condition operators and condition keys the evaluator
// applies. A policy using anything else is refused instead of being read
// differently from what it says: a Deny ignored is access granted.
var (
	policyStatementElements = map[string]bool{
		"Sid": true, "Effect": true, "Principal": true, "Action": true, "Resource": true, "Condition": true,
	}
	policyConditionOperators = map[string]bool{
		"stringequals": true, "stringnotequals": true, "stringequalsignorecase": true, "stringnotequalsignorecase": true,
		"stringlike": true, "stringnotlike": true, "bool": true, "ipaddress": true, "notipaddress": true,
		"arnequals": true, "arnlike": true, "arnnotequals": true, "arnnotlike": true,
		"numericequals": true, "numericnotequals": true,
	}
	policyConditionKeys = map[string]bool{"aws:sourceip": true, "aws:securetransport": true}
)

// ValidatePolicy parses a bucket policy and refuses what the evaluator would
// not enforce as written: elements other than Sid, Effect, Principal, Action,
// Resource and Condition; principals other than "*", user ARNs
// (arn:aws:iam:::user/<name>) and user IDs; actions outside S3; resources
// outside the bucket; condition keys other than aws:SourceIp and
// aws:SecureTransport.
func ValidatePolicy(raw []byte, bucketName string) (*Policy, error) {
	// A UTF-8 byte order mark, as PowerShell writes it, or encoded twice.
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	raw = bytes.TrimPrefix(raw, []byte{0xC3, 0xAF, 0xC2, 0xBB, 0xC2, 0xBF})

	var doc struct {
		Version   string          `json:"Version"`
		Statement json.RawMessage `json:"Statement"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%w: the policy is not valid JSON", ErrMalformedPolicy)
	}
	if doc.Version != "2012-10-17" && doc.Version != "2008-10-17" {
		return nil, fmt.Errorf("%w: Version must be 2012-10-17 or 2008-10-17", ErrMalformedPolicy)
	}

	// A single statement may be given as an object.
	statements := bytes.TrimSpace(doc.Statement)
	if len(statements) > 0 && statements[0] == '{' {
		statements = append(append([]byte{'['}, statements...), ']')
	}
	var rawStatements []map[string]json.RawMessage
	var parsed []Statement
	if err := json.Unmarshal(statements, &rawStatements); err != nil {
		return nil, fmt.Errorf("%w: Statement must be a statement or a list of statements", ErrMalformedPolicy)
	}
	if err := json.Unmarshal(statements, &parsed); err != nil {
		return nil, fmt.Errorf("%w: Statement must be a statement or a list of statements", ErrMalformedPolicy)
	}
	if len(parsed) == 0 {
		return nil, fmt.Errorf("%w: the policy has no Statement", ErrMalformedPolicy)
	}

	for i, st := range parsed {
		for element := range rawStatements[i] {
			if !policyStatementElements[element] {
				return nil, fmt.Errorf("%w: statement %d: %s is not supported", ErrMalformedPolicy, i+1, element)
			}
		}
		if err := validateStatement(st, bucketName); err != nil {
			return nil, fmt.Errorf("%w: statement %d: %s", ErrMalformedPolicy, i+1, err)
		}
	}
	return &Policy{Version: doc.Version, Statement: parsed}, nil
}

func validateStatement(st Statement, bucketName string) error {
	if st.Effect != "Allow" && st.Effect != "Deny" {
		return errors.New("Effect must be Allow or Deny")
	}
	if err := validatePrincipal(st.Principal); err != nil {
		return err
	}

	actions := toStringSlice(st.Action)
	if len(actions) == 0 {
		return errors.New("Action is required")
	}
	for _, action := range actions {
		if action != "*" && !strings.HasPrefix(strings.ToLower(action), "s3:") {
			return fmt.Errorf("action %q is not an S3 action", action)
		}
	}

	resources := toStringSlice(st.Resource)
	if len(resources) == 0 {
		return errors.New("Resource is required")
	}
	bucketARN := "arn:aws:s3:::" + bucketName
	for _, resource := range resources {
		if resource != bucketARN && !strings.HasPrefix(resource, bucketARN+"/") {
			return fmt.Errorf("resource %q is not this bucket or its objects", resource)
		}
	}

	for operator, block := range st.Condition {
		if !policyConditionOperators[strings.ToLower(operator)] {
			return fmt.Errorf("condition operator %s is not supported", operator)
		}
		keys, ok := block.(map[string]interface{})
		if !ok || len(keys) == 0 {
			return fmt.Errorf("condition %s is malformed", operator)
		}
		for key, value := range keys {
			if !policyConditionKeys[strings.ToLower(key)] {
				return fmt.Errorf("condition key %s is not supported; aws:SourceIp and aws:SecureTransport are", key)
			}
			if len(toStringSlice(value)) == 0 {
				return fmt.Errorf("condition %s %s has no value", operator, key)
			}
		}
	}
	return nil
}

func validatePrincipal(principal interface{}) error {
	switch p := principal.(type) {
	case string:
		if p == "*" {
			return nil
		}
		return errors.New(`Principal must be "*" or {"AWS": ...}`)
	case map[string]interface{}:
		if len(p) == 0 {
			return errors.New("Principal is empty")
		}
		for kind, value := range p {
			if kind != "AWS" && kind != "CanonicalUser" {
				return fmt.Errorf("principal type %s is not supported", kind)
			}
			names := toStringSlice(value)
			if len(names) == 0 {
				return fmt.Errorf("principal %s names no one", kind)
			}
			for _, name := range names {
				if err := validatePrincipalName(name); err != nil {
					return err
				}
			}
		}
		return nil
	default:
		return errors.New("Principal is required")
	}
}

// validatePrincipalName accepts "*", a user ARN and a user ID. Another ARN
// would never match a caller, and a Deny naming it would deny no one.
func validatePrincipalName(name string) error {
	if name == "" {
		return errors.New("a principal is empty")
	}
	if !strings.HasPrefix(name, "arn:") {
		return nil
	}
	if user, ok := strings.CutPrefix(name, "arn:aws:iam:::user/"); ok && user != "" && !strings.Contains(user, "*") {
		return nil
	}
	return fmt.Errorf("principal %q is not supported; name users as arn:aws:iam:::user/<name>", name)
}
