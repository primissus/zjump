package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePathAbsolute(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/foo/bar", "/foo/bar"},
		{"/foo/../bar", "/bar"},
		{"/foo/./bar", "/foo/bar"},
		{"/foo//bar", "/foo/bar"},
		{"/foo/bar/..", "/foo"},
		{"/..", "/"},           // cannot pop past root
		{"/", "/"},             //
		{"/a/b/../../c", "/c"}, //
	}
	for _, tc := range cases {
		got, err := ResolvePath(tc.in)
		if err != nil {
			t.Errorf("ResolvePath(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ResolvePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolvePathRelative(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolvePath("foo/bar")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cwd, "foo", "bar")
	if got != want {
		t.Errorf("ResolvePath(relative) = %q, want %q", got, want)
	}

	// "." resolves to the cwd itself.
	got, _ = ResolvePath(".")
	if got != cwd {
		t.Errorf("ResolvePath(\".\") = %q, want %q", got, cwd)
	}
}

func TestCurrentTime(t *testing.T) {
	now, err := CurrentTime()
	if err != nil {
		t.Fatal(err)
	}
	if now == 0 {
		t.Error("CurrentTime returned 0")
	}
}
