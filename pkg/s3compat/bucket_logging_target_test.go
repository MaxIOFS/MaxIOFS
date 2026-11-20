package s3compat

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/inventory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// storedInventories keeps the inventory configurations stored.
type storedInventories struct{ configs []*inventory.InventoryConfig }

func (s *storedInventories) GetConfigByID(ctx context.Context, id, tenantID string) (*inventory.InventoryConfig, error) {
	return nil, sql.ErrNoRows
}
func (s *storedInventories) ListConfigsByBucket(ctx context.Context, bucketName, tenantID string) ([]*inventory.InventoryConfig, error) {
	return s.configs, nil
}
func (s *storedInventories) UpsertConfigByID(ctx context.Context, config *inventory.InventoryConfig) error {
	s.configs = append(s.configs, config)
	return nil
}
func (s *storedInventories) DeleteConfigByID(ctx context.Context, id, tenantID string) error {
	return nil
}

// A bucket's access logs and inventory reports go to a bucket of its own
// tenant that the caller may write to: not to a global bucket or another
// tenant's, even one its policy names, nor to one that does not exist or that
// the caller may not write to.
func TestAccessLogsAndInventoriesGoOnlyWhereTheCallerMayWrite(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()
	require.NoError(t, env.authManager.CreateTenant(ctx, &auth.Tenant{ID: "other", Name: "other", Status: "active"}))
	for _, b := range []struct{ tenant, name string }{
		{env.tenantID, "src"}, {env.tenantID, "logs"}, {env.tenantID, "closed"}, {"", "global-logs"}, {"other", "others-logs"},
	} {
		require.NoError(t, env.bucketManager.CreateBucket(ctx, b.tenant, b.name, ""))
	}
	writer, ok := env.authManager.(interface {
		PutIAMInlinePolicy(ctx context.Context, targetType, targetID, name, document string) error
	})
	require.True(t, ok)
	require.NoError(t, writer.PutIAMInlinePolicy(ctx, auth.IAMTargetUser, env.userID, "logging", `{"Version":"2012-10-17","Statement":[
		{"Effect":"Allow","Action":["s3:PutBucketLogging","s3:PutInventoryConfiguration"],"Resource":["arn:aws:s3:::src"]},
		{"Effect":"Allow","Action":["s3:PutObject"],"Resource":["arn:aws:s3:::logs/*","arn:aws:s3:::global-logs/*","arn:aws:s3:::others-logs/*"]}]}`))
	user, err := env.authManager.GetUser(ctx, env.userID)
	require.NoError(t, err)

	put := func(target string) int {
		body := `<BucketLoggingStatus xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><LoggingEnabled>` +
			`<TargetBucket>` + target + `</TargetBucket><TargetPrefix>access/</TargetPrefix></LoggingEnabled></BucketLoggingStatus>`
		req := httptest.NewRequest(http.MethodPut, "/src?logging", strings.NewReader(body))
		req = mux.SetURLVars(req.WithContext(setUserInContext(req.Context(), user)), map[string]string{"bucket": "src"})
		rec := httptest.NewRecorder()
		env.handler.PutBucketLogging(rec, req)
		return rec.Code
	}
	inventories := &storedInventories{}
	env.handler.SetInventoryManager(inventories)
	putInventory := func(target string) int {
		body := `<InventoryConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Id>inv</Id><IsEnabled>true</IsEnabled>` +
			`<Destination><S3BucketDestination><Bucket>arn:aws:s3:::` + target + `</Bucket><Format>CSV</Format><Prefix>reports/</Prefix></S3BucketDestination></Destination>` +
			`<Schedule><Frequency>Daily</Frequency></Schedule><IncludedObjectVersions>Current</IncludedObjectVersions></InventoryConfiguration>`
		req := httptest.NewRequest(http.MethodPut, "/src?inventory&id=inv", strings.NewReader(body))
		req = mux.SetURLVars(req.WithContext(setUserInContext(req.Context(), user)), map[string]string{"bucket": "src"})
		rec := httptest.NewRecorder()
		env.handler.PutBucketInventoryConfiguration(rec, req)
		return rec.Code
	}

	for name, put := range map[string]func(string) int{"access logs": put, "inventory": putInventory} {
		assert.Equal(t, http.StatusOK, put("logs"), name+": a bucket of its tenant it may write to")
		assert.Equal(t, http.StatusBadRequest, put("closed"), name+": a bucket of its tenant it may not write to")
		assert.Equal(t, http.StatusBadRequest, put("global-logs"), name+": a global bucket")
		assert.Equal(t, http.StatusBadRequest, put("others-logs"), name+": a bucket of another tenant")
		assert.Equal(t, http.StatusBadRequest, put("missing"), name+": a bucket that does not exist")
	}
	assert.Len(t, inventories.configs, 1, "only the inventory into a bucket it may write to is stored")
}
