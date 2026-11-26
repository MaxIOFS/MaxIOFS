package bucket

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
)

// ErrMalformedPolicy is a bucket policy that cannot be enforced as written.
var ErrMalformedPolicy = errors.New("malformed bucket policy")

// Statement elements and condition keys the evaluator applies, with the
// operators of conditionOperators. A policy using anything else is refused
// instead of being read differently from what it says: a Deny ignored is
// access granted.
var (
	policyStatementElements = map[string]bool{
		"Sid": true, "Effect": true, "Principal": true, "Action": true, "Resource": true, "Condition": true,
	}
	policyConditionKeys = map[string]bool{"aws:sourceip": true, "aws:securetransport": true}
)

// ValidatePolicy parses a bucket policy and refuses what the evaluator would
// not enforce as written: elements other than Sid, Effect, Principal, Action,
// Resource and Condition; principals other than "*", user ARNs
// (arn:aws:iam:::user/<name>) and user IDs; actions outside S3; resources
// outside the bucket; condition keys other than aws:SourceIp and
// aws:SecureTransport, and an address that is not one.
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
	rawStatements, parsed, err := parseStatements(doc.Statement)
	if err != nil {
		return nil, err
	}
	for i, st := range parsed {
		err := validateStatementElements(rawStatements[i])
		if err == nil {
			err = validateStatement(st, bucketName)
		}
		if err != nil {
			return nil, fmt.Errorf("%w: statement %d: %s", ErrMalformedPolicy, i+1, err)
		}
	}
	return &Policy{Version: doc.Version, Statement: parsed, Text: string(raw)}, nil
}

// parseStatements reads a policy's statements, a list or a single one, as
// written and parsed.
func parseStatements(statement json.RawMessage) ([]map[string]json.RawMessage, []Statement, error) {
	statements := bytes.TrimSpace(statement)
	if len(statements) > 0 && statements[0] == '{' {
		statements = append(append([]byte{'['}, statements...), ']')
	}
	var rawStatements []map[string]json.RawMessage
	var parsed []Statement
	if json.Unmarshal(statements, &rawStatements) != nil || json.Unmarshal(statements, &parsed) != nil {
		return nil, nil, fmt.Errorf("%w: Statement must be a statement or a list of statements", ErrMalformedPolicy)
	}
	if len(parsed) == 0 {
		return nil, nil, fmt.Errorf("%w: the policy has no Statement", ErrMalformedPolicy)
	}
	return rawStatements, parsed, nil
}

// validateStatementElements refuses an element the evaluator does not apply.
func validateStatementElements(statement map[string]json.RawMessage) error {
	for element := range statement {
		if !policyStatementElements[element] {
			return fmt.Errorf("%s is not supported", element)
		}
	}
	return nil
}

func validateStatement(st Statement, bucketName string) error {
	if st.Effect != "Allow" && st.Effect != "Deny" {
		return errors.New("Effect must be Allow or Deny")
	}
	if err := validatePrincipal(st.Principal); err != nil {
		return err
	}

	if err := validateActions(toStringSlice(st.Action)); err != nil {
		return err
	}
	if err := validateResources(toStringSlice(st.Resource), bucketName); err != nil {
		return err
	}
	return validateConditions(st.Condition)
}

// validateActions accepts "*" and the actions of S3.
func validateActions(actions []string) error {
	if len(actions) == 0 {
		return errors.New("Action is required")
	}
	for _, action := range actions {
		if action != "*" && !strings.HasPrefix(strings.ToLower(action), "s3:") {
			return fmt.Errorf("action %q is not an S3 action", action)
		}
	}
	return nil
}

// validateResources accepts the bucket and its objects.
func validateResources(resources []string, bucketName string) error {
	if len(resources) == 0 {
		return errors.New("Resource is required")
	}
	bucketARN := "arn:aws:s3:::" + bucketName
	for _, resource := range resources {
		if resource != bucketARN && !strings.HasPrefix(resource, bucketARN+"/") {
			return fmt.Errorf("resource %q is not this bucket or its objects", resource)
		}
	}
	return nil
}

// validateConditions accepts the operators of conditionOperators on the keys
// of policyConditionKeys, each naming a value, and an address operator's
// values when they are addresses.
func validateConditions(condition map[string]interface{}) error {
	for operator, block := range condition {
		op, ok := conditionOperators[strings.ToLower(operator)]
		if !ok {
			return fmt.Errorf("condition operator %s is not supported", operator)
		}
		keys, ok := block.(map[string]interface{})
		if !ok || len(keys) == 0 {
			return fmt.Errorf("condition %s is malformed", operator)
		}
		for key, value := range keys {
			if err := validateConditionValues(operator, key, toStringSlice(value), op.address); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateConditionValues(operator, key string, values []string, address bool) error {
	if !policyConditionKeys[strings.ToLower(key)] {
		return fmt.Errorf("condition key %s is not supported; aws:SourceIp and aws:SecureTransport are", key)
	}
	if len(values) == 0 {
		return fmt.Errorf("condition %s %s has no value", operator, key)
	}
	for _, value := range values {
		if address && !isAddress(value) {
			return fmt.Errorf("condition %s %s: %q is not an address", operator, key, value)
		}
	}
	return nil
}

// isAddress reports whether value is an IP address or a CIDR range.
func isAddress(value string) bool {
	_, _, err := net.ParseCIDR(value)
	return err == nil || net.ParseIP(value) != nil
}

func validatePrincipal(principal interface{}) error {
	switch p := principal.(type) {
	case string:
		if p == "*" {
			return nil
		}
		return errors.New(`Principal must be "*" or {"AWS": ...}`)
	case map[string]interface{}:
		return validatePrincipalTypes(p)
	default:
		return errors.New("Principal is required")
	}
}

// validatePrincipalTypes accepts {"AWS": ...} and {"CanonicalUser": ...},
// each naming someone.
func validatePrincipalTypes(principals map[string]interface{}) error {
	if len(principals) == 0 {
		return errors.New("Principal is empty")
	}
	for kind, value := range principals {
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
