package fbhttp

import (
	"path/filepath"
	"testing"
)

func TestSlashCleanUsesHostSeparator(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		sep  string
		want string
	}{
		// Windows: a backslash is a separator and must be resolved before the
		// path is matched against a rule.
		{"windows traversal", `/allow\..\Secret.txt`, `\`, "/Secret.txt"},
		{"windows leading backslash", `\Secret.txt`, `\`, "/Secret.txt"},
		{"windows mixed separators", `/a/b\..\..\c`, `\`, "/c"},
		{"windows already canonical", "/Secret.txt", `\`, "/Secret.txt"},
		{"windows relative", `allow\..\Secret.txt`, `\`, "/Secret.txt"},
		{"windows empty", "", `\`, "/"},

		// POSIX: a backslash is a legal filename character and must survive, or
		// files named with one become unreachable.
		{"posix backslash is a filename character", `/a\b.txt`, "/", `/a\b.txt`},
		{"posix traversal", "/allow/../Secret.txt", "/", "/Secret.txt"},
		{"posix relative", "Secret.txt", "/", "/Secret.txt"},
		{"posix multiple slashes", "//a///b", "/", "/a/b"},
		{"posix empty", "", "/", "/"},
	}

	for _, tc := range cases {
		if tc.sep != string(filepath.Separator) {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := slashClean(tc.in); got != tc.want {
				t.Errorf("slashClean(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}
