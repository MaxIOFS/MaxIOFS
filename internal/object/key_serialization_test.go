package object

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
)

// Every Manager method that changes an object identified by (bucket, key) must
// hold the per-key lock. These are all read-modify-write on the same document,
// so one that skips the lock overwrites whatever another had just committed.
//
// notObjectMutations is the only thing taken on trust: a new mutator that is
// not listed in keyMutators fails the completeness check rather than passing
// silently.
var notObjectMutations = map[string]bool{
	"GetObject":             true,
	"GetObjectMetadata":     true,
	"GetObjectRetention":    true,
	"GetObjectLegalHold":    true,
	"GetObjectTagging":      true,
	"GetObjectACL":          true,
	"GetObjectAttributes":   true,
	"HeadObject":            true,
	"ObjectExists":          true,
	"CheckWriteConditions":  true,
	"GetObjectVersions":     true,
	"ListObjectVersions":    true,
	"VerifyObjectIntegrity": true,
	// Takes (bucket, key) but writes an upload record, not the object.
	"CreateMultipartUpload": true,
	"VerifyBucketIntegrity": true,
	"SearchObjects":         true,
	"ListObjects":           true,
}

func keyMutators() map[string]func(om *objectManager, ctx context.Context, bucket, key string) {
	return map[string]func(om *objectManager, ctx context.Context, bucket, key string){
		"PutObject": func(om *objectManager, ctx context.Context, b, k string) {
			_, _ = om.PutObject(ctx, b, k, strings.NewReader("x"), http.Header{})
		},
		"DeleteObject": func(om *objectManager, ctx context.Context, b, k string) {
			_, _ = om.DeleteObject(ctx, b, k, false)
		},
		"DeleteObjectVersion": func(om *objectManager, ctx context.Context, b, k string) {
			_ = om.DeleteObjectVersion(ctx, b, k, "v1")
		},
		"SetObjectRetention": func(om *objectManager, ctx context.Context, b, k string) {
			_ = om.SetObjectRetention(ctx, b, k, &RetentionConfig{})
		},
		"SetObjectLegalHold": func(om *objectManager, ctx context.Context, b, k string) {
			_ = om.SetObjectLegalHold(ctx, b, k, &LegalHoldConfig{})
		},
		"SetRestoreStatus": func(om *objectManager, ctx context.Context, b, k string) {
			_ = om.SetRestoreStatus(ctx, b, k, "ongoing", nil)
		},
		"SetObjectTagging": func(om *objectManager, ctx context.Context, b, k string) {
			_ = om.SetObjectTagging(ctx, b, k, &TagSet{})
		},
		"DeleteObjectTagging": func(om *objectManager, ctx context.Context, b, k string) {
			_ = om.DeleteObjectTagging(ctx, b, k)
		},
		"UpdateObjectMetadata": func(om *objectManager, ctx context.Context, b, k string) {
			_ = om.UpdateObjectMetadata(ctx, b, k, map[string]string{"x": "y"})
		},
		"SetObjectACL": func(om *objectManager, ctx context.Context, b, k string) {
			_ = om.SetObjectACL(ctx, b, k, &ACL{})
		},
	}
}

// takesBucketKey reports whether a method's first two arguments after the
// context are the bucket and key — the shape this rule applies to.
func takesBucketKey(m reflect.Method) bool {
	t := m.Type
	if t.NumIn() < 3 {
		return false
	}
	return t.In(1).Kind() == reflect.String && t.In(2).Kind() == reflect.String
}

func TestKeyMutatorsAreAllCovered(t *testing.T) {
	covered := keyMutators()
	iface := reflect.TypeOf((*Manager)(nil)).Elem()

	for i := 0; i < iface.NumMethod(); i++ {
		m := iface.Method(i)
		if !takesBucketKey(m) || notObjectMutations[m.Name] {
			continue
		}
		if _, ok := covered[m.Name]; !ok {
			t.Fatalf("Manager.%s changes an object but is not covered by the key-serialization test; "+
				"add it to keyMutators, or to notObjectMutations if it does not write that object", m.Name)
		}
	}
}

func TestKeyMutatorsSerializeOnTheKeyLock(t *testing.T) {
	om, _, metaStore := setupManagerWithConfigKey(t)
	ctx := context.Background()

	const bucket, key = "lock-bucket", "lock-key.txt"
	if err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{Name: bucket, OwnerID: "user-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := om.PutObject(ctx, bucket, key, strings.NewReader("seed"), http.Header{}); err != nil {
		t.Fatal(err)
	}

	for name, call := range keyMutators() {
		t.Run(name, func(t *testing.T) {
			unlock := om.lockKey(bucket, key)

			done := make(chan struct{})
			go func() {
				call(om, ctx, bucket, key)
				close(done)
			}()

			select {
			case <-done:
				unlock()
				t.Fatalf("%s completed while the key lock was held — it does not serialize", name)
			case <-time.After(150 * time.Millisecond):
			}

			unlock()

			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatalf("%s did not complete after the key lock was released", name)
			}
		})
	}
}

// A PUT takes the key's lock once its data is in, so a client still sending
// holds up no read.
func TestAReadIsNotHeldUpByAWriteStillReceiving(t *testing.T) {
	om, _, metaStore := setupManagerWithConfigKey(t)
	ctx := context.Background()
	const bucket, key = "busy", "k"
	if err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{Name: bucket, OwnerID: "user-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := om.PutObject(ctx, bucket, key, strings.NewReader("first"), http.Header{}); err != nil {
		t.Fatal(err)
	}

	body, sending := io.Pipe()
	var wg sync.WaitGroup
	defer wg.Wait()
	defer sending.Close()
	wg.Go(func() {
		if _, err := om.PutObject(ctx, bucket, key, body, http.Header{}); err != nil {
			t.Error(err)
		}
	})
	if _, err := sending.Write([]byte("sec")); err != nil {
		t.Fatal(err)
	}

	read := make(chan string, 1)
	wg.Go(func() {
		_, r, err := om.GetObject(ctx, bucket, key)
		if err != nil {
			read <- err.Error()
			return
		}
		data, _ := io.ReadAll(r)
		r.Close()
		read <- string(data)
	})
	select {
	case data := <-read:
		if data != "first" {
			t.Fatalf("read %q while the write was receiving", data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the read waited for the write to receive its data")
	}

	if _, err := sending.Write([]byte("ond")); err != nil {
		t.Fatal(err)
	}
	sending.Close()
	wg.Wait()
	_, r, err := om.GetObject(ctx, bucket, key)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if data, _ := io.ReadAll(r); string(data) != "second" {
		t.Fatalf("read %q after the write", data)
	}
}
