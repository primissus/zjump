// Package e2e drives the built zjump binary end-to-end. These tests are
// dependency-free (they exec only zjump itself, not bash/zsh/fzf), so they run
// in the default `go test ./...` path (A-8). Interactive fzf/shell behavior is
// covered separately behind the `shelltests` build tag.
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var zjumpBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "zjump-e2e-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	zjumpBin = filepath.Join(dir, "zjump")
	build := exec.Command("go", "build", "-o", zjumpBin, "./cmd/zjump")
	build.Dir = ".." // module root
	if out, err := build.CombinedOutput(); err != nil {
		panic("build failed: " + err.Error() + "\n" + string(out))
	}
	os.Exit(m.Run())
}

type result struct {
	stdout string
	stderr string
	code   int
}

// run executes zjump with args under a fresh data dir (unless one is supplied
// via extraEnv). extraEnv entries are "KEY=VALUE".
func run(t *testing.T, dataDir string, extraEnv []string, args ...string) result {
	t.Helper()
	cmd := exec.Command(zjumpBin, args...)
	cmd.Env = append(os.Environ(), "_ZJUMP_DATA_DIR="+dataDir)
	cmd.Env = append(cmd.Env, extraEnv...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run(%v): %v", args, err)
		}
	}
	return result{stdout.String(), stderr.String(), code}
}

func TestAddQueryRemove(t *testing.T) {
	data := t.TempDir()
	root := t.TempDir()
	be := filepath.Join(root, "proj", "backend")
	fe := filepath.Join(root, "proj", "frontend")
	os.MkdirAll(be, 0o755)
	os.MkdirAll(fe, 0o755)

	run(t, data, nil, "add", be)
	run(t, data, nil, "add", be) // rank now 2
	run(t, data, nil, "add", fe)

	// Best match: backend outranks frontend.
	if got := strings.TrimSpace(run(t, data, nil, "query", "backend").stdout); got != be {
		t.Errorf("query backend = %q, want %q", got, be)
	}
	// Two keywords, ordered.
	if got := strings.TrimSpace(run(t, data, nil, "query", "proj", "back").stdout); got != be {
		t.Errorf("query proj back = %q, want %q", got, be)
	}
	// --list shows both.
	list := run(t, data, nil, "query", "--list").stdout
	if !strings.Contains(list, be) || !strings.Contains(list, fe) {
		t.Errorf("query --list missing entries:\n%s", list)
	}

	// remove, then it's gone.
	if r := run(t, data, nil, "remove", be); r.code != 0 {
		t.Errorf("remove failed: %s", r.stderr)
	}
	if strings.Contains(run(t, data, nil, "query", "--list").stdout, be) {
		t.Error("backend still present after remove")
	}
}

func TestScoreFormat(t *testing.T) {
	data := t.TempDir()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "x"), 0o755)
	run(t, data, nil, "add", filepath.Join(root, "x"))
	out := run(t, data, nil, "query", "--list", "--score").stdout
	// rank 1.0, just-added => <1h bucket => score 4.0, 6-char field.
	if !strings.HasPrefix(out, "   4.0 ") {
		t.Errorf("score line = %q, want prefix '   4.0 '", out)
	}
}

func TestNoMatch(t *testing.T) {
	data := t.TempDir()
	r := run(t, data, nil, "query", "definitelynotpresent")
	if r.code == 0 {
		t.Error("expected nonzero exit for no match")
	}
	if !strings.Contains(r.stderr, "no match found") {
		t.Errorf("stderr = %q, want 'no match found'", r.stderr)
	}
}

func TestNotADirectory(t *testing.T) {
	data := t.TempDir()
	f := filepath.Join(t.TempDir(), "afile")
	os.WriteFile(f, []byte("x"), 0o644)
	r := run(t, data, nil, "add", f)
	if r.code == 0 || !strings.Contains(r.stderr, "not a directory") {
		t.Errorf("want 'not a directory' error, got code=%d stderr=%q", r.code, r.stderr)
	}
}

func TestRemoveNotFound(t *testing.T) {
	data := t.TempDir()
	r := run(t, data, nil, "remove", "/no/such/entry")
	if r.code == 0 || !strings.Contains(r.stderr, "path not found in database") {
		t.Errorf("want 'path not found' error, got code=%d stderr=%q", r.code, r.stderr)
	}
}

func TestUnrecognizedSubcommand(t *testing.T) {
	data := t.TempDir()
	r := run(t, data, nil, "frobnicate")
	if r.code == 0 || !strings.Contains(r.stderr, "unrecognized subcommand") {
		t.Errorf("want 'unrecognized subcommand', got code=%d stderr=%q", r.code, r.stderr)
	}
}

func TestRelativeDataDirRejected(t *testing.T) {
	// Override the data dir with a relative path (R-ENV-1).
	cmd := exec.Command(zjumpBin, "query", "x")
	cmd.Env = append(os.Environ(), "_ZJUMP_DATA_DIR=relative/dir")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected error for relative _ZJUMP_DATA_DIR")
	}
	if !strings.Contains(stderr.String(), "must be an absolute path") {
		t.Errorf("stderr = %q, want 'must be an absolute path'", stderr.String())
	}
}

func TestExcludeDirsSkipsAdd(t *testing.T) {
	data := t.TempDir()
	root := t.TempDir()
	skip := filepath.Join(root, "skipme")
	keep := filepath.Join(root, "keepme")
	os.MkdirAll(skip, 0o755)
	os.MkdirAll(keep, 0o755)

	env := []string{"_ZJUMP_EXCLUDE_DIRS=" + filepath.Join(root, "skip*")}
	run(t, data, env, "add", skip)
	run(t, data, env, "add", keep)

	list := run(t, data, nil, "query", "--list", "--all").stdout
	if strings.Contains(list, skip) {
		t.Errorf("excluded dir was added:\n%s", list)
	}
	if !strings.Contains(list, keep) {
		t.Errorf("non-excluded dir missing:\n%s", list)
	}
}

func TestExcludeDirsLazyDeleteOnQuery(t *testing.T) {
	data := t.TempDir()
	root := t.TempDir()
	a := filepath.Join(root, "aaa")
	b := filepath.Join(root, "bbb")
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)
	run(t, data, nil, "add", a)
	run(t, data, nil, "add", b)

	// Query with an exclude glob matching /aaa: it should be purged from the DB.
	run(t, data, []string{"_ZJUMP_EXCLUDE_DIRS=" + a}, "query", "--list")
	// Now without the exclude, /aaa must be gone permanently.
	list := run(t, data, nil, "query", "--list", "--all").stdout
	if strings.Contains(list, a) {
		t.Errorf("excluded entry not lazily deleted:\n%s", list)
	}
	if !strings.Contains(list, b) {
		t.Errorf("other entry wrongly removed:\n%s", list)
	}
}

func TestBaseDir(t *testing.T) {
	data := t.TempDir()
	root := t.TempDir()
	in := filepath.Join(root, "sub", "target")
	out := filepath.Join(t.TempDir(), "elsewhere")
	os.MkdirAll(in, 0o755)
	os.MkdirAll(out, 0o755)
	run(t, data, nil, "add", in)
	run(t, data, nil, "add", out)

	list := run(t, data, nil, "query", "--list", "--base-dir", root).stdout
	if !strings.Contains(list, in) || strings.Contains(list, out) {
		t.Errorf("--base-dir %s did not restrict correctly:\n%s", root, list)
	}
}

func TestAllShowsNonexistent(t *testing.T) {
	data := t.TempDir()
	gone := filepath.Join(t.TempDir(), "willvanish")
	os.MkdirAll(gone, 0o755)
	run(t, data, nil, "add", gone)
	os.RemoveAll(gone)

	// Without --all, a nonexistent dir is filtered out.
	if strings.Contains(run(t, data, nil, "query", "--list").stdout, gone) {
		t.Error("nonexistent dir shown without --all")
	}
	// With --all, it's included.
	if !strings.Contains(run(t, data, nil, "query", "--list", "--all").stdout, gone) {
		t.Error("nonexistent dir hidden with --all")
	}
}

func TestInitProducesScript(t *testing.T) {
	data := t.TempDir()
	for _, sh := range []string{"bash", "zsh"} {
		out := run(t, data, nil, "init", sh).stdout
		if !strings.Contains(out, "__zjump_z") || !strings.Contains(out, "function zz()") {
			t.Errorf("init %s missing expected content", sh)
		}
	}
	// Unsupported shell is rejected.
	if r := run(t, data, nil, "init", "fish"); r.code == 0 {
		t.Error("init fish should be rejected (out of scope)")
	}
	// --no-cmd suppresses the z command.
	out := run(t, data, nil, "init", "bash", "--no-cmd").stdout
	if strings.Contains(out, "function zz()") {
		t.Error("--no-cmd should not define zz()")
	}
	// --cmd renames.
	out = run(t, data, nil, "init", "bash", "--cmd", "j").stdout
	if !strings.Contains(out, "function j()") || !strings.Contains(out, "function ji()") {
		t.Error("--cmd j should define j() and ji()")
	}
}

// TestBrokenPipe checks silent-exit-0 on a broken stdout pipe (A-7, R-ERR-1).
// Uses /bin/sh only for the pipeline; not the gated shell-integration suite.
func TestBrokenPipe(t *testing.T) {
	data := t.TempDir()
	root := t.TempDir()
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		d := filepath.Join(root, n)
		os.MkdirAll(d, 0o755)
		run(t, data, nil, "add", d)
	}
	cmd := exec.Command("/bin/sh", "-c", zjumpBin+" query --list | head -1")
	cmd.Env = append(os.Environ(), "_ZJUMP_DATA_DIR="+data)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("pipeline errored: %v (stderr=%q)", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("broken pipe produced stderr: %q", stderr.String())
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		t.Error("expected at least one line from head")
	}
}

func TestAliasCreateListDelete(t *testing.T) {
	data := t.TempDir()
	target := filepath.Join(t.TempDir(), "myproj")
	os.MkdirAll(target, 0o755)

	// Create.
	r := run(t, data, nil, "alias", "proj", target)
	if r.code != 0 {
		t.Fatalf("alias create failed: %s", r.stderr)
	}

	// List.
	list := run(t, data, nil, "alias").stdout
	if !strings.Contains(list, "proj\t") || !strings.Contains(list, target) {
		t.Errorf("alias list missing entry:\n%s", list)
	}

	// Delete.
	r = run(t, data, nil, "alias", "-d", "proj")
	if r.code != 0 {
		t.Fatalf("alias delete failed: %s", r.stderr)
	}

	// List should now be empty.
	list = run(t, data, nil, "alias").stdout
	if strings.Contains(list, target) {
		t.Errorf("alias list still shows deleted entry:\n%s", list)
	}

	// Delete nonexistent.
	r = run(t, data, nil, "alias", "-d", "proj")
	if r.code == 0 || !strings.Contains(r.stderr, "alias not found") {
		t.Errorf("delete nonexistent: code=%d stderr=%q", r.code, r.stderr)
	}
}

func TestAliasRejectsInvalidName(t *testing.T) {
	data := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	os.MkdirAll(target, 0o755)

	for _, name := range []string{"", ".", "..", "-bad", "x/y", "a\nb"} {
		r := run(t, data, nil, "alias", name, target)
		if r.code == 0 {
			t.Errorf("alias %q should be rejected", name)
		}
	}
}

func TestAliasRejectsNotADirectory(t *testing.T) {
	data := t.TempDir()
	f := filepath.Join(t.TempDir(), "afile")
	os.WriteFile(f, []byte("x"), 0o644)

	r := run(t, data, nil, "alias", "key", f)
	if r.code == 0 || !strings.Contains(r.stderr, "not a directory") {
		t.Errorf("alias with file target: code=%d stderr=%q", r.code, r.stderr)
	}
}

func TestAliasQueryResolution(t *testing.T) {
	data := t.TempDir()
	aliasTarget := filepath.Join(t.TempDir(), "aliastarget")
	frecTarget := filepath.Join(t.TempDir(), "frecdir")
	os.MkdirAll(aliasTarget, 0o755)
	os.MkdirAll(frecTarget, 0o755)

	// Set up: alias "foo" -> aliastarget, frecency entry "foo/bar" -> /frecdir/foo/bar
	run(t, data, nil, "alias", "foo", aliasTarget)
	frecPath := filepath.Join(frecTarget, "foo", "bar")
	os.MkdirAll(frecPath, 0o755)
	run(t, data, nil, "add", frecPath)

	// Query "foo" alone: alias wins.
	out := strings.TrimSpace(run(t, data, nil, "query", "foo").stdout)
	if out != aliasTarget {
		t.Errorf("alias query = %q, want %q", out, aliasTarget)
	}

	// Query "foo" "bar" (2 keywords): no alias match, falls through to frecency.
	out = strings.TrimSpace(run(t, data, nil, "query", "foo", "bar").stdout)
	if out != frecPath {
		t.Errorf("multi-keyword query = %q, want %q", out, frecPath)
	}
}

func TestAliasDanglingTargetError(t *testing.T) {
	data := t.TempDir()
	target := filepath.Join(t.TempDir(), "willvanish")
	os.MkdirAll(target, 0o755)
	run(t, data, nil, "alias", "lost", target)
	os.RemoveAll(target)

	r := run(t, data, nil, "query", "lost")
	if r.code == 0 {
		t.Error("dangling alias should error")
	}
	if !strings.Contains(r.stderr, "no longer exists") {
		t.Errorf("dangling alias error = %q", r.stderr)
	}
}

func TestAliasAlreadyInOnlyMatch(t *testing.T) {
	data := t.TempDir()
	target := filepath.Join(t.TempDir(), "here")
	os.MkdirAll(target, 0o755)
	run(t, data, nil, "alias", "here", target)

	// Simulate z pass --exclude with the alias target.
	r := run(t, data, nil, "query", "--exclude", target, "here")
	if r.code == 0 || !strings.Contains(r.stderr, "you are already in the only match") {
		t.Errorf("excluded alias: code=%d stderr=%q", r.code, r.stderr)
	}
}

func TestAliasOverwrite(t *testing.T) {
	data := t.TempDir()
	old := filepath.Join(t.TempDir(), "old")
	new := filepath.Join(t.TempDir(), "new")
	os.MkdirAll(old, 0o755)
	os.MkdirAll(new, 0o755)

	run(t, data, nil, "alias", "home", old)
	run(t, data, nil, "alias", "home", new)

	out := strings.TrimSpace(run(t, data, nil, "query", "home").stdout)
	if out != new {
		t.Errorf("alias overwrite: query = %q, want %q", out, new)
	}
}

func TestAliasPrefixMatch(t *testing.T) {
	data := t.TempDir()
	target1 := filepath.Join(t.TempDir(), "docs-web-app")
	target2 := filepath.Join(t.TempDir(), "proj-dir")
	os.MkdirAll(target1, 0o755)
	os.MkdirAll(target2, 0o755)

	// Create aliases.
	run(t, data, nil, "alias", "punk-records", target1)
	run(t, data, nil, "alias", "proj", target2)

	// Unique prefix match.
	out := strings.TrimSpace(run(t, data, nil, "query", "punk-").stdout)
	if out != target1 {
		t.Errorf("prefix match 'punk-' = %q, want %q", out, target1)
	}

	// Exact match still works.
	out = strings.TrimSpace(run(t, data, nil, "query", "proj").stdout)
	if out != target2 {
		t.Errorf("exact match 'proj' = %q, want %q", out, target2)
	}

	// Hyphenated alias name with period prefix.
	run(t, data, nil, "alias", ".my-test", target1)
	out = strings.TrimSpace(run(t, data, nil, "query", ".my-").stdout)
	if out != target1 {
		t.Errorf("prefix match '.my-' = %q, want %q", out, target1)
	}

	// Ambiguous prefix falls through to frecency (no DB entries → no match).
	r := run(t, data, nil, "query", "p")
	if r.code == 0 || !strings.Contains(r.stderr, "no match found") {
		t.Errorf("ambiguous prefix 'p': expected 'no match found', got code=%d stderr=%q", r.code, r.stderr)
	}
}

func TestAliasRejectsLeadingDash(t *testing.T) {
	data := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	os.MkdirAll(target, 0o755)

	r := run(t, data, nil, "alias", "-badname", target)
	if r.code == 0 {
		t.Fatal("alias with leading dash should be rejected")
	}
	if !strings.Contains(r.stderr, "flag provided but not defined") {
		t.Errorf("error = %q, want flag parsing rejection", r.stderr)
	}
}

func TestAliasSpecialCharacters(t *testing.T) {
	data := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	os.MkdirAll(target, 0o755)

	tests := []struct {
		name   string
		prefix string
	}{
		{"_test", "_"},
		{".hidden", ".hi"},
		{"123abc", "123"},
		{"{braces}", "{b"},
		{"[test]", "[t"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			run(t, data, nil, "alias", tc.name, target)

			// Exact match.
			out := strings.TrimSpace(run(t, data, nil, "query", tc.name).stdout)
			if out != target {
				t.Errorf("exact %q = %q, want %q", tc.name, out, target)
			}

			// Prefix match.
			out = strings.TrimSpace(run(t, data, nil, "query", tc.prefix).stdout)
			if out != target {
				t.Errorf("prefix %q -> %q = %q, want %q", tc.prefix, tc.name, out, target)
			}
		})
	}
}
