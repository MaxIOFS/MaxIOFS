package cluster

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SQLiteTimestampUnix normalizes TIMESTAMP values returned by SQLite drivers.
func SQLiteTimestampUnix(raw interface{}) (int64, bool) {
	switch v := raw.(type) {
	case nil:
		return 0, false
	case time.Time:
		return v.Unix(), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	case []byte:
		return parseSQLiteTimestampString(string(v))
	case string:
		return parseSQLiteTimestampString(v)
	default:
		return parseSQLiteTimestampString(fmt.Sprint(v))
	}
}

func parseSQLiteTimestampString(raw string) (int64, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, false
	}
	if unix, err := strconv.ParseInt(s, 10, 64); err == nil {
		return unix, true
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999 -07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Unix(), true
		}
	}
	return 0, false
}
