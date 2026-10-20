package bucket

import (
	"encoding/xml"
	"fmt"
	"time"

	"github.com/maxiofs/maxiofs/internal/storageclass"
)

// LifecycleConfigurationXML is a bucket's lifecycle as the S3 API writes it.
type LifecycleConfigurationXML struct {
	XMLName xml.Name           `xml:"LifecycleConfiguration"`
	Rules   []LifecycleRuleXML `xml:"Rule"`
}

// LifecycleRuleXML is one rule of a lifecycle; Prefix is the filter of the
// rules written before Filter was.
type LifecycleRuleXML struct {
	ID                             string                             `xml:"ID,omitempty"`
	Status                         string                             `xml:"Status"`
	Prefix                         *string                            `xml:"Prefix"`
	Filter                         *LifecycleFilterXML                `xml:"Filter"`
	Expiration                     *LifecycleExpirationXML            `xml:"Expiration"`
	Transitions                    []LifecycleTransitionXML           `xml:"Transition"`
	NoncurrentVersionExpiration    *NoncurrentVersionExpirationXML    `xml:"NoncurrentVersionExpiration"`
	NoncurrentVersionTransitions   []NoncurrentVersionTransitionXML   `xml:"NoncurrentVersionTransition"`
	AbortIncompleteMultipartUpload *AbortIncompleteMultipartUploadXML `xml:"AbortIncompleteMultipartUpload"`
}

// LifecycleFilterXML holds one condition, or And with several.
type LifecycleFilterXML struct {
	Prefix                *string          `xml:"Prefix"`
	Tag                   *LifecycleTagXML `xml:"Tag"`
	ObjectSizeGreaterThan *int64           `xml:"ObjectSizeGreaterThan"`
	ObjectSizeLessThan    *int64           `xml:"ObjectSizeLessThan"`
	And                   *LifecycleAndXML `xml:"And"`
}

type LifecycleAndXML struct {
	Prefix                string            `xml:"Prefix,omitempty"`
	Tags                  []LifecycleTagXML `xml:"Tag"`
	ObjectSizeGreaterThan *int64            `xml:"ObjectSizeGreaterThan"`
	ObjectSizeLessThan    *int64            `xml:"ObjectSizeLessThan"`
}

type LifecycleTagXML struct {
	Key   string `xml:"Key"`
	Value string `xml:"Value"`
}

type LifecycleExpirationXML struct {
	Days                      *int   `xml:"Days"`
	Date                      string `xml:"Date,omitempty"`
	ExpiredObjectDeleteMarker *bool  `xml:"ExpiredObjectDeleteMarker"`
}

type LifecycleTransitionXML struct {
	Days         *int   `xml:"Days"`
	Date         string `xml:"Date,omitempty"`
	StorageClass string `xml:"StorageClass"`
}

type NoncurrentVersionExpirationXML struct {
	NoncurrentDays          int `xml:"NoncurrentDays"`
	NewerNoncurrentVersions int `xml:"NewerNoncurrentVersions,omitempty"`
}

type NoncurrentVersionTransitionXML struct {
	NoncurrentDays          int    `xml:"NoncurrentDays"`
	NewerNoncurrentVersions int    `xml:"NewerNoncurrentVersions,omitempty"`
	StorageClass            string `xml:"StorageClass"`
}

type AbortIncompleteMultipartUploadXML struct {
	DaysAfterInitiation int `xml:"DaysAfterInitiation"`
}

// LifecycleError is a lifecycle S3 would refuse; Code is the S3 error code.
type LifecycleError struct {
	Code    string
	Message string
}

func (e *LifecycleError) Error() string { return e.Message }

func refuse(code, format string, args ...any) error {
	return &LifecycleError{Code: code, Message: fmt.Sprintf(format, args...)}
}

const (
	maxLifecycleRules       = 1000
	maxLifecycleRuleID      = 255
	maxNewerNoncurrentKept  = 100
	lifecycleMalformed      = "MalformedXML"
	lifecycleInvalid        = "InvalidArgument"
	lifecycleInvalidRequest = "InvalidRequest"
	lifecycleInvalidClass   = "InvalidStorageClass"
)

// LifecycleFromXML reads the rules of a lifecycle written in the S3 API,
// refused as S3 refuses them (see LifecycleError).
func LifecycleFromXML(doc *LifecycleConfigurationXML) (*LifecycleConfig, error) {
	switch {
	case len(doc.Rules) == 0:
		return nil, refuse(lifecycleMalformed, "The lifecycle configuration has no rule")
	case len(doc.Rules) > maxLifecycleRules:
		return nil, refuse(lifecycleInvalidRequest, "A lifecycle configuration has %d rules at most", maxLifecycleRules)
	}
	config := &LifecycleConfig{Rules: make([]LifecycleRule, 0, len(doc.Rules))}
	ids := make(map[string]bool, len(doc.Rules))
	for i := range doc.Rules {
		rule, err := lifecycleRuleFromXML(&doc.Rules[i])
		if err != nil {
			return nil, err
		}
		if rule.ID != "" {
			if ids[rule.ID] {
				return nil, refuse(lifecycleInvalid, "Rule ID must be unique. Found same ID for more than one rule: %s", rule.ID)
			}
			ids[rule.ID] = true
		}
		config.Rules = append(config.Rules, rule)
	}
	return config, nil
}

// WithStoredRules sets the rules of sent among those of stored: a rule of
// sent replaces in place the stored rule of its ID, the stored rules of other
// IDs, or of none, stay, and the others of sent go last. More rules than S3
// keeps are refused.
func WithStoredRules(sent, stored *LifecycleConfig) (*LifecycleConfig, error) {
	byID := make(map[string]LifecycleRule, len(sent.Rules))
	for _, rule := range sent.Rules {
		if rule.ID != "" {
			byID[rule.ID] = rule
		}
	}
	merged := &LifecycleConfig{}
	placed := make(map[string]bool, len(byID))
	if stored != nil {
		for _, rule := range stored.Rules {
			if replacement, ok := byID[rule.ID]; ok {
				rule = replacement
				placed[rule.ID] = true
			}
			merged.Rules = append(merged.Rules, rule)
		}
	}
	for _, rule := range sent.Rules {
		if !placed[rule.ID] {
			merged.Rules = append(merged.Rules, rule)
		}
	}
	if len(merged.Rules) > maxLifecycleRules {
		return nil, refuse(lifecycleInvalidRequest, "A lifecycle configuration has %d rules at most", maxLifecycleRules)
	}
	return merged, nil
}

func lifecycleRuleFromXML(x *LifecycleRuleXML) (LifecycleRule, error) {
	rule := LifecycleRule{ID: x.ID, Status: x.Status}
	if len(x.ID) > maxLifecycleRuleID {
		return rule, refuse(lifecycleInvalid, "ID length should not exceed allowed limit of %d", maxLifecycleRuleID)
	}
	if x.Status != "Enabled" && x.Status != "Disabled" {
		return rule, refuse(lifecycleMalformed, "The status of a rule is Enabled or Disabled")
	}
	filter, err := lifecycleFilterFromXML(x)
	if err != nil {
		return rule, err
	}
	rule.Filter = filter

	// The rule's actions are timed by days or by dates, not both.
	var byDays, byDate bool
	if e := x.Expiration; e != nil {
		exp, err := lifecycleExpirationFromXML(e, len(filter.Tags) > 0)
		if err != nil {
			return rule, err
		}
		rule.Expiration = exp
		if exp != nil {
			byDays, byDate = exp.Days != nil, exp.Date != nil
		}
	}
	classes := make(map[string]bool, len(x.Transitions))
	for _, t := range x.Transitions {
		transition, err := lifecycleTransitionFromXML(t)
		if err != nil {
			return rule, err
		}
		if classes[transition.StorageClass] {
			return rule, refuse(lifecycleInvalid, "'StorageClass' must be different for 'Transition' actions in same 'Rule'")
		}
		classes[transition.StorageClass] = true
		byDays = byDays || transition.Days != nil
		byDate = byDate || transition.Date != nil
		rule.Transitions = append(rule.Transitions, transition)
	}
	if byDays && byDate {
		return rule, refuse(lifecycleInvalidRequest, "Found mixed 'Date' and 'Days' based Expiration and Transition actions in lifecycle rule")
	}
	if n := x.NoncurrentVersionExpiration; n != nil {
		if n.NoncurrentDays <= 0 {
			return rule, refuse(lifecycleInvalid, "'NoncurrentDays' for NoncurrentVersionExpiration action must be a positive integer")
		}
		if err := checkNewerNoncurrent(n.NewerNoncurrentVersions); err != nil {
			return rule, err
		}
		rule.NoncurrentVersionExpiration = &NoncurrentVersionExpiration{
			NoncurrentDays:          n.NoncurrentDays,
			NewerNoncurrentVersions: n.NewerNoncurrentVersions,
		}
	}
	clear(classes)
	for _, t := range x.NoncurrentVersionTransitions {
		if !storageclass.Transitional(t.StorageClass) {
			return rule, refuse(lifecycleInvalidClass, "The storage class you specified is not valid: %q", t.StorageClass)
		}
		if t.NoncurrentDays < 0 {
			return rule, refuse(lifecycleInvalid, "'NoncurrentDays' for NoncurrentVersionTransition action must be a nonnegative integer")
		}
		if err := checkNewerNoncurrent(t.NewerNoncurrentVersions); err != nil {
			return rule, err
		}
		if classes[t.StorageClass] {
			return rule, refuse(lifecycleInvalid, "'StorageClass' must be different for 'NoncurrentVersionTransition' actions in same 'Rule'")
		}
		classes[t.StorageClass] = true
		rule.NoncurrentVersionTransitions = append(rule.NoncurrentVersionTransitions, NoncurrentVersionTransition{
			NoncurrentDays:          t.NoncurrentDays,
			NewerNoncurrentVersions: t.NewerNoncurrentVersions,
			StorageClass:            t.StorageClass,
		})
	}
	if a := x.AbortIncompleteMultipartUpload; a != nil {
		if a.DaysAfterInitiation <= 0 {
			return rule, refuse(lifecycleInvalid, "'DaysAfterInitiation' for AbortIncompleteMultipartUpload action must be a positive integer")
		}
		if len(filter.Tags) > 0 || filter.ObjectSizeGreaterThan != nil || filter.ObjectSizeLessThan != nil {
			return rule, refuse(lifecycleInvalid, "AbortIncompleteMultipartUpload cannot be specified with tags or object sizes")
		}
		rule.AbortIncompleteMultipartUpload = &LifecycleAbortIncompleteMultipartUpload{DaysAfterInitiation: a.DaysAfterInitiation}
	}
	if rule.Expiration == nil && len(rule.Transitions) == 0 && rule.NoncurrentVersionExpiration == nil &&
		len(rule.NoncurrentVersionTransitions) == 0 && rule.AbortIncompleteMultipartUpload == nil {
		return rule, refuse(lifecycleInvalid, "At least one action needs to be specified in a rule")
	}
	return rule, nil
}

func checkNewerNoncurrent(n int) error {
	if n < 0 || n > maxNewerNoncurrentKept {
		return refuse(lifecycleInvalid, "'NewerNoncurrentVersions' must be between 0 and %d", maxNewerNoncurrentKept)
	}
	return nil
}

// lifecycleFilterFromXML reads the filter of a rule: its Prefix, or its
// Filter of one condition or of And.
func lifecycleFilterFromXML(x *LifecycleRuleXML) (LifecycleFilter, error) {
	var filter LifecycleFilter
	f := x.Filter
	if f == nil {
		if x.Prefix != nil {
			filter.Prefix = *x.Prefix
		}
		return filter, nil
	}
	if x.Prefix != nil {
		return filter, refuse(lifecycleMalformed, "A rule has a Prefix or a Filter, not both")
	}
	set := 0
	for _, given := range []bool{f.Prefix != nil, f.Tag != nil, f.ObjectSizeGreaterThan != nil, f.ObjectSizeLessThan != nil, f.And != nil} {
		if given {
			set++
		}
	}
	if set > 1 {
		return filter, refuse(lifecycleMalformed, "A Filter has one condition; several are combined with And")
	}
	var tags []LifecycleTagXML
	switch {
	case f.Prefix != nil:
		filter.Prefix = *f.Prefix
	case f.Tag != nil:
		tags = []LifecycleTagXML{*f.Tag}
	case f.ObjectSizeGreaterThan != nil:
		filter.ObjectSizeGreaterThan = f.ObjectSizeGreaterThan
	case f.ObjectSizeLessThan != nil:
		filter.ObjectSizeLessThan = f.ObjectSizeLessThan
	case f.And != nil:
		filter.Prefix = f.And.Prefix
		tags = f.And.Tags
		filter.ObjectSizeGreaterThan, filter.ObjectSizeLessThan = f.And.ObjectSizeGreaterThan, f.And.ObjectSizeLessThan
	}
	keys := make(map[string]bool, len(tags))
	for _, tag := range tags {
		if tag.Key == "" {
			return filter, refuse(lifecycleInvalid, "A tag of a filter has a key")
		}
		if keys[tag.Key] {
			return filter, refuse(lifecycleInvalid, "Duplicate Tag Keys are not allowed")
		}
		keys[tag.Key] = true
		filter.Tags = append(filter.Tags, Tag{Key: tag.Key, Value: tag.Value})
	}
	above, below := filter.ObjectSizeGreaterThan, filter.ObjectSizeLessThan
	switch {
	case above != nil && *above < 0:
		return filter, refuse(lifecycleInvalid, "'ObjectSizeGreaterThan' must be a nonnegative integer")
	case below != nil && *below <= 0:
		return filter, refuse(lifecycleInvalid, "'ObjectSizeLessThan' must be a positive integer")
	case above != nil && below != nil && *above >= *below:
		return filter, refuse(lifecycleInvalid, "'ObjectSizeLessThan' must be greater than 'ObjectSizeGreaterThan'")
	}
	return filter, nil
}

// lifecycleExpirationFromXML reads an expiration; one that removes no delete
// marker and gives no time is none.
func lifecycleExpirationFromXML(e *LifecycleExpirationXML, tagged bool) (*LifecycleExpiration, error) {
	if e.Days == nil && e.Date == "" && e.ExpiredObjectDeleteMarker == nil {
		return nil, refuse(lifecycleMalformed, "An Expiration has Days, a Date or ExpiredObjectDeleteMarker")
	}
	if e.Days != nil && e.Date != "" {
		return nil, refuse(lifecycleMalformed, "An Expiration has Days or a Date, not both")
	}
	if e.ExpiredObjectDeleteMarker != nil && (e.Days != nil || e.Date != "") {
		return nil, refuse(lifecycleInvalid, "ExpiredObjectDeleteMarker cannot be specified with Days or Date in a Lifecycle Expiration Policy")
	}
	exp := &LifecycleExpiration{}
	switch {
	case e.Days != nil:
		if *e.Days <= 0 {
			return nil, refuse(lifecycleInvalid, "'Days' for Expiration action must be a positive integer")
		}
		days := *e.Days
		exp.Days = &days
	case e.Date != "":
		date, err := lifecycleDate(e.Date)
		if err != nil {
			return nil, err
		}
		exp.Date = &date
	case *e.ExpiredObjectDeleteMarker:
		if tagged {
			return nil, refuse(lifecycleInvalid, "ExpiredObjectDeleteMarker cannot be specified with tags")
		}
		marker := true
		exp.ExpiredObjectDeleteMarker = &marker
	default:
		return nil, nil
	}
	return exp, nil
}

func lifecycleTransitionFromXML(t LifecycleTransitionXML) (LifecycleTransition, error) {
	transition := LifecycleTransition{StorageClass: t.StorageClass}
	if !storageclass.Transitional(t.StorageClass) {
		return transition, refuse(lifecycleInvalidClass, "The storage class you specified is not valid: %q", t.StorageClass)
	}
	switch {
	case t.Days == nil && t.Date == "":
		return transition, refuse(lifecycleMalformed, "A Transition has Days or a Date")
	case t.Days != nil && t.Date != "":
		return transition, refuse(lifecycleMalformed, "A Transition has Days or a Date, not both")
	case t.Days != nil:
		if *t.Days < 0 {
			return transition, refuse(lifecycleInvalid, "'Days' for Transition action must be a nonnegative integer")
		}
		days := *t.Days
		transition.Days = &days
	default:
		date, err := lifecycleDate(t.Date)
		if err != nil {
			return transition, err
		}
		transition.Date = &date
	}
	return transition, nil
}

// lifecycleDate reads a date of a rule: an ISO 8601 time, or a day.
func lifecycleDate(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, refuse(lifecycleInvalid, "Invalid date: %q", s)
}

// LifecycleToXML writes the rules of a lifecycle in the S3 API. A rule
// filtered by its prefix alone gives it as Prefix.
func LifecycleToXML(config *LifecycleConfig) LifecycleConfigurationXML {
	doc := LifecycleConfigurationXML{Rules: make([]LifecycleRuleXML, 0, len(config.Rules))}
	for _, rule := range config.Rules {
		x := LifecycleRuleXML{ID: rule.ID, Status: rule.Status}
		writeLifecycleFilter(&x, rule.Filter)
		if e := rule.Expiration; e != nil {
			x.Expiration = &LifecycleExpirationXML{Days: e.Days, Date: xmlDate(e.Date), ExpiredObjectDeleteMarker: e.ExpiredObjectDeleteMarker}
		}
		for _, t := range rule.Transitions {
			x.Transitions = append(x.Transitions, LifecycleTransitionXML{Days: t.Days, Date: xmlDate(t.Date), StorageClass: t.StorageClass})
		}
		if n := rule.NoncurrentVersionExpiration; n != nil {
			x.NoncurrentVersionExpiration = &NoncurrentVersionExpirationXML{
				NoncurrentDays:          n.NoncurrentDays,
				NewerNoncurrentVersions: n.NewerNoncurrentVersions,
			}
		}
		for _, t := range rule.NoncurrentVersionTransitions {
			x.NoncurrentVersionTransitions = append(x.NoncurrentVersionTransitions, NoncurrentVersionTransitionXML{
				NoncurrentDays:          t.NoncurrentDays,
				NewerNoncurrentVersions: t.NewerNoncurrentVersions,
				StorageClass:            t.StorageClass,
			})
		}
		if a := rule.AbortIncompleteMultipartUpload; a != nil {
			x.AbortIncompleteMultipartUpload = &AbortIncompleteMultipartUploadXML{DaysAfterInitiation: a.DaysAfterInitiation}
		}
		doc.Rules = append(doc.Rules, x)
	}
	return doc
}

func writeLifecycleFilter(x *LifecycleRuleXML, f LifecycleFilter) {
	conditions := len(f.Tags)
	for _, given := range []bool{f.Prefix != "", f.ObjectSizeGreaterThan != nil, f.ObjectSizeLessThan != nil} {
		if given {
			conditions++
		}
	}
	switch {
	case len(f.Tags) == 0 && f.ObjectSizeGreaterThan == nil && f.ObjectSizeLessThan == nil:
		if f.Prefix != "" {
			prefix := f.Prefix
			x.Prefix = &prefix
		}
	case conditions == 1 && len(f.Tags) == 1:
		x.Filter = &LifecycleFilterXML{Tag: &LifecycleTagXML{Key: f.Tags[0].Key, Value: f.Tags[0].Value}}
	case conditions == 1:
		x.Filter = &LifecycleFilterXML{ObjectSizeGreaterThan: f.ObjectSizeGreaterThan, ObjectSizeLessThan: f.ObjectSizeLessThan}
	default:
		and := &LifecycleAndXML{Prefix: f.Prefix, ObjectSizeGreaterThan: f.ObjectSizeGreaterThan, ObjectSizeLessThan: f.ObjectSizeLessThan}
		for _, tag := range f.Tags {
			and.Tags = append(and.Tags, LifecycleTagXML{Key: tag.Key, Value: tag.Value})
		}
		x.Filter = &LifecycleFilterXML{And: and}
	}
}

func xmlDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
