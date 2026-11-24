package auth

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// parsedDocuments keeps each policy document parsed once, by its text: every
// request evaluates the same few. What it gives is shared and never changed.
// Full, it is emptied.
var parsedDocuments = struct {
	sync.RWMutex
	docs map[string]*parsedDocument
}{docs: make(map[string]*parsedDocument)}

const maxParsedDocuments = 4096

type parsedDocument struct {
	policy *Policy // the document read as JSON; nil when it is not JSON
	valid  error   // what ParseIAMPolicy says of it, at the managed policy budget
}

// parsed is raw parsed, from parsedDocuments when it was parsed before.
func parsed(raw string) *parsedDocument {
	parsedDocuments.RLock()
	d, ok := parsedDocuments.docs[raw]
	parsedDocuments.RUnlock()
	if ok {
		return d
	}
	d = &parsedDocument{}
	var policy Policy
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &policy) == nil {
		d.policy = &policy
	}
	_, d.valid = ParseIAMPolicy(raw, IAMMaxManagedPolicyBytes)
	parsedDocuments.Lock()
	if len(parsedDocuments.docs) >= maxParsedDocuments {
		clear(parsedDocuments.docs)
	}
	parsedDocuments.docs[raw] = d
	parsedDocuments.Unlock()
	return d
}

// ParseIAMPolicy validates a policy document and returns its parsed form.
func ParseIAMPolicy(raw string, maxBytes int) (*Policy, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("%w: policy document is empty", ErrIAMInvalidInput)
	}
	if len(trimmed) > maxBytes {
		return nil, fmt.Errorf("%w: policy exceeds %d bytes", ErrIAMInvalidInput, maxBytes)
	}

	var policy Policy
	if err := json.Unmarshal([]byte(trimmed), &policy); err != nil {
		return nil, fmt.Errorf("%w: not valid JSON: %v", ErrIAMInvalidInput, err)
	}
	if len(policy.Statement) == 0 {
		return nil, fmt.Errorf("%w: policy has no statements", ErrIAMInvalidInput)
	}
	if len(policy.Statement) > IAMMaxPolicyStatements {
		return nil, fmt.Errorf("%w: policy has more than %d statements", ErrIAMInvalidInput, IAMMaxPolicyStatements)
	}

	for i, st := range policy.Statement {
		if st.Effect != EffectAllow && st.Effect != EffectDeny {
			return nil, fmt.Errorf("%w: statement %d has Effect %q (must be Allow or Deny)", ErrIAMInvalidInput, i, st.Effect)
		}
		if len(st.Principal) > 0 {
			return nil, fmt.Errorf("%w: statement %d must not set Principal (identity policies are attached, not addressed)", ErrIAMInvalidInput, i)
		}
		if len(st.Condition) > 0 {
			return nil, fmt.Errorf("%w: statement %d uses Condition, which is not supported yet", ErrIAMInvalidInput, i)
		}
		if len(policyStringValues(st.Action)) == 0 {
			return nil, fmt.Errorf("%w: statement %d has no Action", ErrIAMInvalidInput, i)
		}
		if len(policyStringValues(st.Resource)) == 0 {
			return nil, fmt.Errorf("%w: statement %d has no Resource", ErrIAMInvalidInput, i)
		}
	}

	return &policy, nil
}

// EvaluateIAMDocuments applies a set of policy documents to one
func EvaluateIAMDocuments(documents []string, action, resource string) bool {
	allowed, denied := evaluateIAMDocuments(documents, action, resource)
	return allowed && !denied
}

// namesOnEveryResource reports whether policy documents allow an action by
// naming it, on every resource, with no Deny taking it back. A wildcard does
// not name it, though it does deny it: it is how administration is decided, and
// "*" is everything in the holder's own namespace, never administration.
func namesOnEveryResource(documents []string, action string) bool {
	named := false
	for _, raw := range documents {
		d := parsed(raw)
		if d.valid != nil {
			return false // A document that cannot be read denies.
		}
		for _, st := range d.policy.Statement {
			if !stsResourceMatches(st.Resource, "*") {
				continue
			}
			switch st.Effect {
			case EffectDeny:
				if stsActionMatches(st.Action, action) {
					return false
				}
			case EffectAllow:
				for _, a := range policyStringValues(st.Action) {
					if strings.EqualFold(a, action) {
						named = true
					}
				}
			}
		}
	}
	return named
}

// evaluateIAMDocuments reports whether the set of documents allows the request
// and whether it denies it explicitly. A document that cannot be read denies.
func evaluateIAMDocuments(documents []string, action, resource string) (allowed, denied bool) {
	if action == "" {
		return false, true
	}
	for _, raw := range documents {
		d := parsed(raw)
		if d.policy == nil {
			return false, true
		}
		for _, st := range d.policy.Statement {
			if !stsActionMatches(st.Action, action) || !stsResourceMatches(st.Resource, resource) {
				continue
			}
			if st.Effect == EffectDeny {
				return false, true
			}
			if st.Effect == EffectAllow {
				allowed = true
			}
		}
	}
	return allowed, false
}
