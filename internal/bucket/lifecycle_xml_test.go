package bucket

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseLifecycle(t *testing.T, body string) (*LifecycleConfig, error) {
	t.Helper()
	var doc LifecycleConfigurationXML
	require.NoError(t, xml.Unmarshal([]byte(body), &doc))
	return LifecycleFromXML(&doc)
}

// A lifecycle keeps every condition and action of its rules, stored and read
// back: filters by prefix, tags and size, expirations by date, transitions and
// the noncurrent versions kept.
func TestALifecycleKeepsWhatItsRulesSay(t *testing.T) {
	manager, cleanup := setupBucketTest(t)
	defer cleanup()
	ctx := context.Background()
	require.NoError(t, manager.CreateBucket(ctx, "", "rules-kept", ""))

	config, err := parseLifecycle(t, `<LifecycleConfiguration>
		<Rule>
			<ID>archive</ID>
			<Status>Enabled</Status>
			<Filter><And>
				<Prefix>logs/</Prefix>
				<Tag><Key>kind</Key><Value>audit</Value></Tag>
				<Tag><Key>team</Key><Value>ops</Value></Tag>
				<ObjectSizeGreaterThan>10</ObjectSizeGreaterThan>
				<ObjectSizeLessThan>5000</ObjectSizeLessThan>
			</And></Filter>
			<Transition><Days>30</Days><StorageClass>STANDARD_IA</StorageClass></Transition>
			<Transition><Days>0</Days><StorageClass>GLACIER</StorageClass></Transition>
			<NoncurrentVersionTransition>
				<NoncurrentDays>10</NoncurrentDays><NewerNoncurrentVersions>2</NewerNoncurrentVersions><StorageClass>DEEP_ARCHIVE</StorageClass>
			</NoncurrentVersionTransition>
			<NoncurrentVersionExpiration>
				<NoncurrentDays>60</NoncurrentDays><NewerNoncurrentVersions>3</NewerNoncurrentVersions>
			</NoncurrentVersionExpiration>
		</Rule>
		<Rule>
			<ID>until</ID>
			<Status>Disabled</Status>
			<Filter><Tag><Key>temp</Key><Value>yes</Value></Tag></Filter>
			<Expiration><Date>2027-01-01T00:00:00.000Z</Date></Expiration>
			<Transition><Date>2026-12-01</Date><StorageClass>GLACIER_IR</StorageClass></Transition>
		</Rule>
		<Rule>
			<ID>markers</ID>
			<Status>Enabled</Status>
			<Prefix>tmp/</Prefix>
			<Expiration><ExpiredObjectDeleteMarker>true</ExpiredObjectDeleteMarker></Expiration>
			<AbortIncompleteMultipartUpload><DaysAfterInitiation>7</DaysAfterInitiation></AbortIncompleteMultipartUpload>
		</Rule>
		<Rule>
			<ID>large</ID>
			<Status>Enabled</Status>
			<Filter><ObjectSizeGreaterThan>1048576</ObjectSizeGreaterThan></Filter>
			<Expiration><Days>400</Days></Expiration>
		</Rule>
	</LifecycleConfiguration>`)
	require.NoError(t, err)
	require.NoError(t, manager.SetLifecycle(ctx, "", "rules-kept", config))
	stored, err := manager.GetLifecycle(ctx, "", "rules-kept")
	require.NoError(t, err)

	thirty, zero, fourHundred := 30, 0, 400
	ten, fiveThousand, mebibyte := int64(10), int64(5000), int64(1048576)
	yes := true
	until := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	from := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	want := &LifecycleConfig{Rules: []LifecycleRule{{
		ID: "archive", Status: "Enabled",
		Filter: LifecycleFilter{
			Prefix:                "logs/",
			Tags:                  []Tag{{Key: "kind", Value: "audit"}, {Key: "team", Value: "ops"}},
			ObjectSizeGreaterThan: &ten,
			ObjectSizeLessThan:    &fiveThousand,
		},
		Transitions: []LifecycleTransition{
			{Days: &thirty, StorageClass: "STANDARD_IA"},
			{Days: &zero, StorageClass: "GLACIER"},
		},
		NoncurrentVersionTransitions: []NoncurrentVersionTransition{
			{NoncurrentDays: 10, NewerNoncurrentVersions: 2, StorageClass: "DEEP_ARCHIVE"},
		},
		NoncurrentVersionExpiration: &NoncurrentVersionExpiration{NoncurrentDays: 60, NewerNoncurrentVersions: 3},
	}, {
		ID: "until", Status: "Disabled",
		Filter:      LifecycleFilter{Tags: []Tag{{Key: "temp", Value: "yes"}}},
		Expiration:  &LifecycleExpiration{Date: &until},
		Transitions: []LifecycleTransition{{Date: &from, StorageClass: "GLACIER_IR"}},
	}, {
		ID: "markers", Status: "Enabled",
		Filter:                         LifecycleFilter{Prefix: "tmp/"},
		Expiration:                     &LifecycleExpiration{ExpiredObjectDeleteMarker: &yes},
		AbortIncompleteMultipartUpload: &LifecycleAbortIncompleteMultipartUpload{DaysAfterInitiation: 7},
	}, {
		ID: "large", Status: "Enabled",
		Filter:     LifecycleFilter{ObjectSizeGreaterThan: &mebibyte},
		Expiration: &LifecycleExpiration{Days: &fourHundred},
	}}}
	assert.Equal(t, want, stored)

	// The rules read back as XML say the same.
	doc := LifecycleToXML(stored)
	body, err := xml.Marshal(doc)
	require.NoError(t, err)
	again, err := parseLifecycle(t, string(body))
	require.NoError(t, err)
	assert.Equal(t, want, again)
}

// A lifecycle whose rules S3 would not take is refused, with the error S3
// gives.
func TestALifecycleS3WouldNotTakeIsRefused(t *testing.T) {
	rule := func(body string) string {
		return `<LifecycleConfiguration><Rule><ID>r</ID><Status>Enabled</Status>` + body + `</Rule></LifecycleConfiguration>`
	}
	for _, c := range []struct {
		name, body, code string
	}{
		{"no rule", `<LifecycleConfiguration></LifecycleConfiguration>`, "MalformedXML"},
		{"no action", rule(`<Prefix>a/</Prefix>`), "InvalidArgument"},
		{"an unknown status", `<LifecycleConfiguration><Rule><Status>On</Status><Expiration><Days>1</Days></Expiration></Rule></LifecycleConfiguration>`, "MalformedXML"},
		{"two rules of one ID", `<LifecycleConfiguration>` +
			`<Rule><ID>r</ID><Status>Enabled</Status><Expiration><Days>1</Days></Expiration></Rule>` +
			`<Rule><ID>r</ID><Status>Enabled</Status><Expiration><Days>2</Days></Expiration></Rule>` +
			`</LifecycleConfiguration>`, "InvalidArgument"},
		{"an empty expiration", rule(`<Expiration></Expiration>`), "MalformedXML"},
		{"an expiration of 0 days", rule(`<Expiration><Days>0</Days></Expiration>`), "InvalidArgument"},
		{"an expiration by days and date", rule(`<Expiration><Days>1</Days><Date>2027-01-01T00:00:00Z</Date></Expiration>`), "MalformedXML"},
		{"an expiration on no date", rule(`<Expiration><Date>next year</Date></Expiration>`), "InvalidArgument"},
		{"delete markers with days", rule(`<Expiration><Days>1</Days><ExpiredObjectDeleteMarker>true</ExpiredObjectDeleteMarker></Expiration>`), "InvalidArgument"},
		{"delete markers by tag", rule(`<Filter><Tag><Key>a</Key><Value>b</Value></Tag></Filter><Expiration><ExpiredObjectDeleteMarker>true</ExpiredObjectDeleteMarker></Expiration>`), "InvalidArgument"},
		{"a prefix twice", rule(`<Prefix>a/</Prefix><Filter><Prefix>b/</Prefix></Filter><Expiration><Days>1</Days></Expiration>`), "MalformedXML"},
		{"a filter of two conditions without And", rule(`<Filter><Prefix>a/</Prefix><Tag><Key>a</Key><Value>b</Value></Tag></Filter><Expiration><Days>1</Days></Expiration>`), "MalformedXML"},
		{"a tag of no key", rule(`<Filter><Tag><Key></Key><Value>b</Value></Tag></Filter><Expiration><Days>1</Days></Expiration>`), "InvalidArgument"},
		{"a tag twice", rule(`<Filter><And><Tag><Key>a</Key><Value>b</Value></Tag><Tag><Key>a</Key><Value>c</Value></Tag></And></Filter><Expiration><Days>1</Days></Expiration>`), "InvalidArgument"},
		{"sizes that leave nothing", rule(`<Filter><And><ObjectSizeGreaterThan>10</ObjectSizeGreaterThan><ObjectSizeLessThan>10</ObjectSizeLessThan></And></Filter><Expiration><Days>1</Days></Expiration>`), "InvalidArgument"},
		{"a negative size", rule(`<Filter><ObjectSizeGreaterThan>-1</ObjectSizeGreaterThan></Filter><Expiration><Days>1</Days></Expiration>`), "InvalidArgument"},
		{"a size below 0", rule(`<Filter><ObjectSizeLessThan>0</ObjectSizeLessThan></Filter><Expiration><Days>1</Days></Expiration>`), "InvalidArgument"},
		{"a transition to STANDARD", rule(`<Transition><Days>1</Days><StorageClass>STANDARD</StorageClass></Transition>`), "InvalidStorageClass"},
		{"a transition to no class", rule(`<Transition><Days>1</Days><StorageClass>COLD</StorageClass></Transition>`), "InvalidStorageClass"},
		{"a transition of negative days", rule(`<Transition><Days>-1</Days><StorageClass>GLACIER</StorageClass></Transition>`), "InvalidArgument"},
		{"a transition by days and date", rule(`<Transition><Days>1</Days><Date>2027-01-01T00:00:00Z</Date><StorageClass>GLACIER</StorageClass></Transition>`), "MalformedXML"},
		{"a transition of no time", rule(`<Transition><StorageClass>GLACIER</StorageClass></Transition>`), "MalformedXML"},
		{"two transitions to one class", rule(`<Transition><Days>1</Days><StorageClass>GLACIER</StorageClass></Transition><Transition><Days>2</Days><StorageClass>GLACIER</StorageClass></Transition>`), "InvalidArgument"},
		{"transitions by days and by date", rule(`<Transition><Days>1</Days><StorageClass>GLACIER</StorageClass></Transition><Transition><Date>2027-01-01T00:00:00Z</Date><StorageClass>DEEP_ARCHIVE</StorageClass></Transition>`), "InvalidRequest"},
		{"noncurrent versions expired at 0 days", rule(`<NoncurrentVersionExpiration><NoncurrentDays>0</NoncurrentDays></NoncurrentVersionExpiration>`), "InvalidArgument"},
		{"negative noncurrent versions kept", rule(`<NoncurrentVersionExpiration><NoncurrentDays>1</NoncurrentDays><NewerNoncurrentVersions>-1</NewerNoncurrentVersions></NoncurrentVersionExpiration>`), "InvalidArgument"},
		{"a noncurrent transition to no class", rule(`<NoncurrentVersionTransition><NoncurrentDays>1</NoncurrentDays><StorageClass>STANDARD</StorageClass></NoncurrentVersionTransition>`), "InvalidStorageClass"},
		{"a noncurrent transition of negative days", rule(`<NoncurrentVersionTransition><NoncurrentDays>-1</NoncurrentDays><StorageClass>GLACIER</StorageClass></NoncurrentVersionTransition>`), "InvalidArgument"},
		{"an abort at 0 days", rule(`<AbortIncompleteMultipartUpload><DaysAfterInitiation>0</DaysAfterInitiation></AbortIncompleteMultipartUpload>`), "InvalidArgument"},
		{"an abort by tag", rule(`<Filter><Tag><Key>a</Key><Value>b</Value></Tag></Filter><AbortIncompleteMultipartUpload><DaysAfterInitiation>1</DaysAfterInitiation></AbortIncompleteMultipartUpload>`), "InvalidArgument"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := parseLifecycle(t, c.body)
			var refused *LifecycleError
			require.True(t, errors.As(err, &refused), "refused: %v", err)
			assert.Equal(t, c.code, refused.Code, refused.Message)
		})
	}
}

// The rules sent replace the stored rules of their IDs, in place; the stored
// rules of other IDs, or of none, stay; a new rule goes last. More rules than
// S3 keeps are refused.
func TestTheRulesSentAreSetAmongTheStoredOnes(t *testing.T) {
	r := func(id, prefix string) LifecycleRule {
		return LifecycleRule{ID: id, Status: "Enabled", Filter: LifecycleFilter{Prefix: prefix}}
	}
	stored := &LifecycleConfig{Rules: []LifecycleRule{r("a", "a/"), r("", "x/"), r("console", "old/"), r("", "y/")}}
	sent := &LifecycleConfig{Rules: []LifecycleRule{r("console", ""), r("new", "n/")}}
	merged, err := WithStoredRules(sent, stored)
	require.NoError(t, err)
	assert.Equal(t, []LifecycleRule{r("a", "a/"), r("", "x/"), r("console", ""), r("", "y/"), r("new", "n/")}, merged.Rules)

	merged, err = WithStoredRules(&LifecycleConfig{Rules: []LifecycleRule{r("", "s/")}}, stored)
	require.NoError(t, err)
	assert.Equal(t, append(slices.Clone(stored.Rules), r("", "s/")), merged.Rules, "a rule of no ID replaces none")

	merged, err = WithStoredRules(sent, nil)
	require.NoError(t, err)
	assert.Equal(t, sent.Rules, merged.Rules)

	many := &LifecycleConfig{}
	for i := 0; i < maxLifecycleRules; i++ {
		many.Rules = append(many.Rules, r(fmt.Sprint(i), ""))
	}
	_, err = WithStoredRules(&LifecycleConfig{Rules: []LifecycleRule{r("0", "z/")}}, many)
	require.NoError(t, err, "a stored rule replaced")
	_, err = WithStoredRules(sent, many)
	var refused *LifecycleError
	require.True(t, errors.As(err, &refused), "refused: %v", err)
	assert.Equal(t, "InvalidRequest", refused.Code)
}
