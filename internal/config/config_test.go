package config

import (
	"os"
	"testing"
)

// unsetenv clears key for the duration of the test, restoring it afterward.
func unsetenv(t *testing.T, key string) {
	t.Helper()
	prev, had := os.LookupEnv(key)
	_ = os.Unsetenv(key)
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, prev)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func TestMaxageDefault(t *testing.T) {
	unsetenv(t, "_ZJUMP_MAXAGE")
	v, err := Maxage()
	if err != nil {
		t.Fatal(err)
	}
	if v != 10000.0 {
		t.Errorf("default maxage = %v, want 10000", v)
	}
}

func TestMaxageParse(t *testing.T) {
	t.Setenv("_ZJUMP_MAXAGE", "500")
	v, err := Maxage()
	if err != nil || v != 500.0 {
		t.Errorf("Maxage(500) = %v, %v", v, err)
	}
}

func TestMaxageInvalid(t *testing.T) {
	t.Setenv("_ZJUMP_MAXAGE", "notanumber")
	if _, err := Maxage(); err == nil {
		t.Error("expected error parsing non-integer maxage")
	}
	t.Setenv("_ZJUMP_MAXAGE", "-5")
	if _, err := Maxage(); err == nil {
		t.Error("expected error parsing negative maxage (u32)")
	}
}

func TestMaxageLeadingPlus(t *testing.T) {
	// Rust's u32::from_str accepts a leading '+'; zjump must too (R-ENV-5).
	t.Setenv("_ZJUMP_MAXAGE", "+5000")
	v, err := Maxage()
	if err != nil || v != 5000.0 {
		t.Errorf("Maxage(+5000) = %v, %v; want 5000, nil", v, err)
	}
	// But a doubled sign is still invalid (matches Rust).
	t.Setenv("_ZJUMP_MAXAGE", "++5")
	if _, err := Maxage(); err == nil {
		t.Error("expected error parsing ++5")
	}
}

func TestDataDirRelativeRejected(t *testing.T) {
	t.Setenv("_ZJUMP_DATA_DIR", "relative/path")
	if _, err := DataDir(); err == nil {
		t.Error("relative _ZJUMP_DATA_DIR must be rejected")
	}
}

func TestDataDirAbsolute(t *testing.T) {
	t.Setenv("_ZJUMP_DATA_DIR", "/abs/zjump")
	dir, err := DataDir()
	if err != nil {
		t.Fatal(err)
	}
	if dir != "/abs/zjump" {
		t.Errorf("DataDir = %q, want /abs/zjump", dir)
	}
}

func TestEchoAndResolve(t *testing.T) {
	t.Setenv("_ZJUMP_ECHO", "1")
	if !Echo() {
		t.Error("_ZJUMP_ECHO=1 should be true")
	}
	t.Setenv("_ZJUMP_ECHO", "true")
	if Echo() {
		t.Error("_ZJUMP_ECHO=true should be false (only exact \"1\")")
	}
	t.Setenv("_ZJUMP_RESOLVE_SYMLINKS", "1")
	if !ResolveSymlinks() {
		t.Error("_ZJUMP_RESOLVE_SYMLINKS=1 should be true")
	}
	t.Setenv("_ZJUMP_RESOLVE_SYMLINKS", "0")
	if ResolveSymlinks() {
		t.Error("_ZJUMP_RESOLVE_SYMLINKS=0 should be false")
	}
}

func TestAutoIndexDirectory(t *testing.T) {
	unsetenv(t, "_ZJUMP_AUTO_INDEX_DIRECTORY")
	if AutoIndexDirectory() {
		t.Error("unset _ZJUMP_AUTO_INDEX_DIRECTORY should be false")
	}
	t.Setenv("_ZJUMP_AUTO_INDEX_DIRECTORY", "1")
	if !AutoIndexDirectory() {
		t.Error("_ZJUMP_AUTO_INDEX_DIRECTORY=1 should be true")
	}
	t.Setenv("_ZJUMP_AUTO_INDEX_DIRECTORY", "0")
	if AutoIndexDirectory() {
		t.Error("_ZJUMP_AUTO_INDEX_DIRECTORY=0 should be false (only exact \"1\")")
	}
	t.Setenv("_ZJUMP_AUTO_INDEX_DIRECTORY", "yes")
	if AutoIndexDirectory() {
		t.Error("_ZJUMP_AUTO_INDEX_DIRECTORY=yes should be false (only exact \"1\")")
	}
}

func TestExcludeDirsCustom(t *testing.T) {
	t.Setenv("_ZJUMP_EXCLUDE_DIRS", "/tmp/*:/secret")
	globs, err := ExcludeDirs()
	if err != nil {
		t.Fatal(err)
	}
	if len(globs) != 2 {
		t.Fatalf("want 2 globs, got %d", len(globs))
	}
	if !globs[0].Match("/tmp/anything") {
		t.Error("/tmp/* should match /tmp/anything")
	}
	if !globs[1].Match("/secret") {
		t.Error("/secret should match itself")
	}
}

func TestExcludeDirsInvalid(t *testing.T) {
	t.Setenv("_ZJUMP_EXCLUDE_DIRS", "/a[bc")
	if _, err := ExcludeDirs(); err == nil {
		t.Error("expected error on invalid glob in _ZJUMP_EXCLUDE_DIRS")
	}
}

func TestPickTopDefault(t *testing.T) {
	unsetenv(t, "_ZJUMP_PICK_TOP")
	v, err := PickTop()
	if err != nil {
		t.Fatal(err)
	}
	if v != 10 {
		t.Errorf("default picktop = %d, want 10", v)
	}
}

func TestPickTopParse(t *testing.T) {
	t.Setenv("_ZJUMP_PICK_TOP", "5")
	v, err := PickTop()
	if err != nil || v != 5 {
		t.Errorf("PickTop(5) = %d, %v", v, err)
	}
}

func TestPickTopInvalid(t *testing.T) {
	t.Setenv("_ZJUMP_PICK_TOP", "notanumber")
	if _, err := PickTop(); err == nil {
		t.Error("expected error parsing non-integer picktop")
	}
	t.Setenv("_ZJUMP_PICK_TOP", "0")
	if _, err := PickTop(); err == nil {
		t.Error("expected error for zero picktop")
	}
	t.Setenv("_ZJUMP_PICK_TOP", "-3")
	if _, err := PickTop(); err == nil {
		t.Error("expected error for negative picktop")
	}
}
