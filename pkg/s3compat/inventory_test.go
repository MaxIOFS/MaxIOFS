package s3compat

import (
	"testing"

	"github.com/maxiofs/maxiofs/internal/inventory"
	"github.com/stretchr/testify/assert"
)

func TestInternalToXML_EmptyFrequencyDoesNotPanic(t *testing.T) {
	cfg := &inventory.InventoryConfig{
		ID:                "inv-1",
		BucketName:        "source-bucket",
		TenantID:          "tenant-1",
		Enabled:           true,
		Frequency:         "",
		Format:            "csv",
		DestinationBucket: "dest-bucket",
		DestinationPrefix: "reports/",
		IncludedFields:    inventory.DefaultIncludedFields(),
	}

	xmlCfg := internalToXML(cfg)

	assert.Equal(t, "", xmlCfg.Schedule.Frequency)
	assert.Equal(t, "inv-1", xmlCfg.ID)
	assert.Equal(t, "arn:aws:s3:::dest-bucket", xmlCfg.Destination.S3BucketDestination.Bucket)
}
