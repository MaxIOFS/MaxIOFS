package auth

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSTSBucketSubresources(t *testing.T) {
	cases := []struct{ query, get, put, del string }{
		{"encryption", ActionGetBucketEncryption, ActionPutBucketEncryption, ActionPutBucketEncryption},
		{"object-lock", ActionGetBucketObjectLockConfiguration, ActionPutBucketObjectLockConfiguration, ""},
		{"notification", ActionGetBucketNotification, ActionPutBucketNotification, ActionPutBucketNotification},
		{"website", ActionGetBucketWebsite, ActionPutBucketWebsite, ActionDeleteBucketWebsite},
		{"replication", ActionGetBucketReplication, ActionPutBucketReplication, ActionPutBucketReplication},
		{"logging", ActionGetBucketLogging, ActionPutBucketLogging, ""},
		{"publicAccessBlock", ActionGetBucketPublicAccess, ActionPutBucketPublicAccess, ActionPutBucketPublicAccess},
		{"ownershipControls", ActionGetBucketOwnership, ActionPutBucketOwnership, ActionPutBucketOwnership},
		{"inventory&id=report", ActionGetBucketInventory, ActionPutBucketInventory, ActionPutBucketInventory},
		{"accelerate", ActionGetAccelerate, ActionPutAccelerate, ""},
		{"requestPayment", ActionGetRequestPayment, ActionPutRequestPayment, ""},
	}
	for _, tc := range cases {
		for method, action := range map[string]string{"GET": tc.get, "PUT": tc.put, "DELETE": tc.del} {
			if action == "" {
				continue
			}
			t.Run(method+"-"+tc.query, func(t *testing.T) {
				am, user, cleanup := setupSTSTest(t)
				defer cleanup()
				doc := fmt.Sprintf("{\"Statement\":[{\"Effect\":\"Allow\",\"Action\":%q,\"Resource\":\"arn:aws:s3:::bucket\"}]}", action)
				sess, err := am.IssueSTSSession(t.Context(), user.ID, 3600, doc)
				require.NoError(t, err)
				for _, path := range []string{"/bucket?", "/bucket/?"} {
					req := httptest.NewRequest(method, path+tc.query, nil)
					require.Equal(t, action, S3ActionForRequest(req))
					signRequestV4(am, req, sess.TempAccessKeyID, sess.SecretAccessKey, sess.SessionToken, true)
					_, err := am.ValidateS3SignatureV4(t.Context(), req)
					require.NoError(t, err)
				}
			})
		}
	}
}

func TestS3ActionPrecedenceAndTrailingSlashes(t *testing.T) {
	for _, tc := range []struct{ method, path, action, resource string }{
		{"GET", "/bucket/?encryption", ActionGetBucketEncryption, "arn:aws:s3:::bucket"},
		{"GET", "/bucket/folder/", ActionGetObject, "arn:aws:s3:::bucket/folder/"},
		{"GET", "/bucket//", ActionGetObject, "arn:aws:s3:::bucket//"},
		{"GET", "/bucket?acl&tagging", ActionGetBucketTagging, "arn:aws:s3:::bucket"},
		{"GET", "/bucket/key?acl&retention", ActionGetObjectRetention, "arn:aws:s3:::bucket/key"},
		{"PUT", "/bucket/key?tagging&legal-hold", ActionPutObjectLegalHold, "arn:aws:s3:::bucket/key"},
		{"DELETE", "/bucket/key?tagging&versionId=v", ActionDeleteObjectTagging, "arn:aws:s3:::bucket/key"},
		{"GET", "/bucket/key?uploadId=u&tagging", ActionListMultipartUploadParts, "arn:aws:s3:::bucket/key"},
		{"POST", "/bucket/key?select", ActionGetObject, "arn:aws:s3:::bucket/key"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			require.Equal(t, tc.action, S3ActionForRequest(req))
			require.Equal(t, tc.resource, ResourceARNForRequest(req))
		})
	}
}
