package object

import (
	"fmt"
	"unicode/utf8"
)

// tagRefusal is a set of tags refused, with what S3 says of it.
type tagRefusal struct {
	kind    error
	message string
}

func (e *tagRefusal) Error() string { return e.message }
func (e *tagRefusal) Unwrap() error { return e.kind }

// ValidateTags refuses tags S3 refuses on an object: more than 10, a key
// empty, of more than 128 characters or given twice, a value of more than
// 256. The error is ErrTooManyTags or ErrInvalidTag.
func ValidateTags(tags []Tag) error {
	if len(tags) > 10 {
		return &tagRefusal{ErrTooManyTags, "object tags cannot be greater than 10"}
	}
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		switch {
		case tag.Key == "":
			return &tagRefusal{ErrInvalidTag, "tag key cannot be empty"}
		case utf8.RuneCountInString(tag.Key) > 128:
			return &tagRefusal{ErrInvalidTag, fmt.Sprintf("tag key %q is longer than 128 characters", tag.Key)}
		case utf8.RuneCountInString(tag.Value) > 256:
			return &tagRefusal{ErrInvalidTag, fmt.Sprintf("the value of tag %q is longer than 256 characters", tag.Key)}
		case seen[tag.Key]:
			return &tagRefusal{ErrInvalidTag, fmt.Sprintf("tag key %q is given more than once", tag.Key)}
		}
		seen[tag.Key] = true
	}
	return nil
}
