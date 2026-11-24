package auth

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A document parsed once keeps both verdicts: evaluation reads any JSON as
// written, the list of actions only a valid policy.
func TestAParsedDocumentKeepsEachVerdict(t *testing.T) {
	doc := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:ListBucket"],"Resource":["*"],` +
		`"Condition":{"Bool":{"aws:SecureTransport":"true"}}}]}`
	for range 2 {
		assert.Empty(t, ActionsFromDocuments([]string{doc}), "a Condition is not supported")
		assert.True(t, EvaluateIAMDocuments([]string{doc}, "s3:ListBucket", "arn:aws:s3:::b"))
		assert.False(t, EvaluateIAMDocuments([]string{"not json"}, "s3:ListBucket", "arn:aws:s3:::b"))
	}
}

// The parsed documents are kept up to a bound: a server whose policies keep
// changing does not keep every text it read.
func TestParsedDocumentsStayBounded(t *testing.T) {
	for i := range maxParsedDocuments + 10 {
		doc := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],`+
			`"Resource":["arn:aws:s3:::b%d/*"]}]}`, i)
		require.True(t, EvaluateIAMDocuments([]string{doc}, "s3:GetObject", fmt.Sprintf("arn:aws:s3:::b%d/k", i)))
	}
	parsedDocuments.RLock()
	defer parsedDocuments.RUnlock()
	assert.LessOrEqual(t, len(parsedDocuments.docs), maxParsedDocuments)
}
