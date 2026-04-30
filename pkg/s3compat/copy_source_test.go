package s3compat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCopySourceHeader_SeparatesVersionBeforeDecodingKey(t *testing.T) {
	bucket, key, versionID, err := parseCopySourceHeader("/source-bucket/reports/a%20file.txt?versionId=v1")

	require.NoError(t, err)
	assert.Equal(t, "source-bucket", bucket)
	assert.Equal(t, "reports/a file.txt", key)
	assert.Equal(t, "v1", versionID)
}

func TestParseCopySourceHeader_DoesNotTreatEncodedQuestionMarkAsVersionQuery(t *testing.T) {
	bucket, key, versionID, err := parseCopySourceHeader("/source-bucket/reports/a%3FversionId=v1.txt")

	require.NoError(t, err)
	assert.Equal(t, "source-bucket", bucket)
	assert.Equal(t, "reports/a?versionId=v1.txt", key)
	assert.Empty(t, versionID)
}

func TestParseCopySourceHeader_InvalidEncoding(t *testing.T) {
	_, _, _, err := parseCopySourceHeader("/source-bucket/bad%zzkey")

	assert.Error(t, err)
}
