package server

import (
	"cmp"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
)

// listingPage is where a page of a bucket's objects starts and how long it is.
type listingPage struct {
	prefix, delimiter, marker string
	maxKeys                   int
}

func listingPageOf(q url.Values) listingPage {
	// One not given, or not above 0, is the object manager's default.
	maxKeys, _ := strconv.Atoi(q.Get("max_keys"))
	return listingPage{prefix: q.Get("prefix"), delimiter: q.Get("delimiter"), marker: q.Get("marker"), maxKeys: maxKeys}
}

// handleListObjects lists a page of a bucket's objects.
func (s *Server) handleListObjects(w http.ResponseWriter, r *http.Request) {
	bucketName := mux.Vars(r)["bucket"]
	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "User not authenticated", http.StatusUnauthorized)
		return
	}
	if !s.requireConsoleBucketS3Action(w, r, bucketName, auth.ActionListBucket, "You do not have permission to list objects") {
		return
	}
	page := listingPageOf(r.URL.Query())
	result, err := s.objectManager.ListObjects(r.Context(), buildBucketPath(consoleBucketTenant(r, user), bucketName),
		page.prefix, page.delimiter, page.marker, page.maxKeys)
	s.writeListing(w, result, err)
}

// handleSearchObjects lists a page of a bucket's objects that pass a search's
// filters.
func (s *Server) handleSearchObjects(w http.ResponseWriter, r *http.Request) {
	bucketName := mux.Vars(r)["bucket"]
	if !s.requireConsoleBucketS3Action(w, r, bucketName, auth.ActionListBucket,
		"You do not have permission to read this bucket") {
		return
	}
	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "User not authenticated", http.StatusUnauthorized)
		return
	}
	filter, err := searchFilterOf(r.URL.Query())
	if err != nil {
		s.writeRefusal(w, err)
		return
	}
	page := listingPageOf(r.URL.Query())
	result, err := s.objectManager.SearchObjects(r.Context(), buildBucketPath(consoleBucketTenant(r, user), bucketName),
		page.prefix, page.delimiter, page.marker, page.maxKeys, filter)
	s.writeListing(w, result, err)
}

// writeListing answers a page of a bucket's objects.
func (s *Server) writeListing(w http.ResponseWriter, result *object.ListObjectsResult, err error) {
	switch {
	case errors.Is(err, object.ErrBucketNotFound):
		s.writeError(w, "Bucket not found", http.StatusNotFound)
		return
	case err != nil:
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	objects := make([]ObjectResponse, len(result.Objects))
	for i, obj := range result.Objects {
		objects[i] = ObjectResponse{
			Key:          obj.Key,
			Size:         obj.Size,
			LastModified: obj.LastModified.Format("2006-01-02T15:04:05Z"),
			ETag:         obj.ETag,
			ContentType:  obj.ContentType,
			Metadata:     obj.Metadata,
			Retention:    obj.Retention,
			LegalHold:    obj.LegalHold,
			CustomerKey:  obj.CustomerEncrypted(),
		}
	}
	prefixes := make([]string, len(result.CommonPrefixes))
	for i, cp := range result.CommonPrefixes {
		prefixes[i] = cp.Prefix
	}
	s.writeJSON(w, map[string]interface{}{
		"objects":        objects,
		"commonPrefixes": prefixes,
		"isTruncated":    result.IsTruncated,
		"nextMarker":     result.NextMarker,
		"prefix":         result.Prefix,
		"delimiter":      result.Delimiter,
	})
}

// searchFilterOf reads a search's filters: content type prefixes, a size range
// in whole bytes, an RFC 3339 date range and key:value tags. A filter it
// cannot read is refused. One given nothing passes every object.
func searchFilterOf(q url.Values) (*metadata.ObjectFilter, error) {
	bytes := func(v string) (int64, error) { return strconv.ParseInt(v, 10, 64) }
	date := func(v string) (time.Time, error) { return time.Parse(time.RFC3339, v) }
	filter := &metadata.ObjectFilter{}
	if ct := q.Get("content_type"); ct != "" {
		filter.ContentTypes = strings.Split(ct, ",")
	}
	var errs [5]error
	filter.MinSize, errs[0] = optionalParam(q, "min_size", bytes, "a whole number of bytes")
	filter.MaxSize, errs[1] = optionalParam(q, "max_size", bytes, "a whole number of bytes")
	filter.ModifiedAfter, errs[2] = optionalParam(q, "modified_after", date, "an RFC 3339 date")
	filter.ModifiedBefore, errs[3] = optionalParam(q, "modified_before", date, "an RFC 3339 date")
	filter.Tags, errs[4] = tagsParam(q["tag"])
	if err := cmp.Or(errs[:]...); err != nil {
		return nil, err
	}
	return filter, nil
}

// optionalParam reads a query parameter, nil when it is not given.
func optionalParam[T any](q url.Values, name string, parse func(string) (T, error), want string) (*T, error) {
	v := q.Get(name)
	if v == "" {
		return nil, nil
	}
	parsed, err := parse(v)
	if err != nil {
		return nil, refused(http.StatusBadRequest, name+" must be "+want)
	}
	return &parsed, nil
}

// tagsParam reads tag parameters, each key:value; nil when none is given.
func tagsParam(params []string) (map[string]string, error) {
	if len(params) == 0 {
		return nil, nil
	}
	tags := make(map[string]string, len(params))
	for _, param := range params {
		key, value, ok := strings.Cut(param, ":")
		if !ok {
			return nil, refused(http.StatusBadRequest, "tag must be key:value")
		}
		tags[key] = value
	}
	return tags, nil
}
