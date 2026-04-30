package cluster

import "testing"

func TestEscapeHAObjectKeyPreservesPrefixesAndEscapesControls(t *testing.T) {
	tests := map[string]string{
		"plain.txt":              "plain.txt",
		"dir/file name.txt":      "dir/file%20name.txt",
		"dir/a?b#c%25.txt":       "dir/a%3Fb%23c%2525.txt",
		"dir//nested/trailing/":  "dir//nested/trailing/",
		"unicode/cafe\u0301.txt": "unicode/cafe%CC%81.txt",
	}

	for in, want := range tests {
		if got := escapeHAObjectKey(in); got != want {
			t.Fatalf("escapeHAObjectKey(%q) = %q, want %q", in, got, want)
		}
	}
}
