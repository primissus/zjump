package alias

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFormatRoundTrip(t *testing.T) {
	entries := []Alias{
		{Name: "proj", Path: "/home/user/projects/zjump"},
		{Name: "docs", Path: "/home/user/Documents"},
	}

	data, err := serialize(entries)
	if err != nil {
		t.Fatal(err)
	}

	got, err := deserialize(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(entries) {
		t.Fatalf("len = %d, want %d", len(got), len(entries))
	}
	for i, e := range entries {
		if got[i].Name != e.Name || got[i].Path != e.Path {
			t.Errorf("entry[%d] = {%q, %q}, want {%q, %q}", i, got[i].Name, got[i].Path, e.Name, e.Path)
		}
	}
}

func TestDeserializeRejectsTruncated(t *testing.T) {
	entries := []Alias{{Name: "x", Path: "/y"}}
	data, _ := serialize(entries)

	for i := 1; i < len(data); i++ {
		_, err := deserialize(data[:i])
		if err == nil {
			t.Errorf("truncated at %d bytes: expected error, got nil", i)
		}
	}
}

func TestDeserializeRejectsBadMagic(t *testing.T) {
	entries := []Alias{{Name: "x", Path: "/y"}}
	data, _ := serialize(entries)
	data[0] = 'B' // corrupt magic
	_, err := deserialize(data)
	if err == nil {
		t.Error("expected error for bad magic")
	}
}

func TestDeserializeRejectsBadVersion(t *testing.T) {
	entries := []Alias{{Name: "x", Path: "/y"}}
	data, _ := serialize(entries)
	data[4] = 99 // corrupt version
	_, err := deserialize(data)
	if err == nil {
		t.Error("expected error for bad version")
	}
}

func TestDeserializeRejectsTooLarge(t *testing.T) {
	big := make([]byte, maxAliasSize+1)
	copy(big, magic[:])
	_, err := deserialize(big)
	if err == nil {
		t.Error("expected error for oversized file")
	}
}

func TestDeserializeRejectsAbsurdCount(t *testing.T) {
	data := make([]byte, headerSize)
	copy(data, magic[:])
	data[7] = 0xff // version MSB stays 0, count field set to absurd value
	data[8] = 0xff
	data[9] = 0xff
	data[10] = 0xff
	data[11] = 0xff
	data[12] = 0xff
	data[13] = 0xff
	data[14] = 0xff
	data[15] = 0xff // count = max u64 → must exceed available bytes
	_, err := deserialize(data)
	if err == nil {
		t.Error("expected error for absurd entry count")
	}
}

func TestStoreSetGet(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	_, ok := s.Get("foo")
	if ok {
		t.Error("Get on empty store should return false")
	}

	s.Set("foo", "/tmp/foo")
	p, ok := s.Get("foo")
	if !ok || p != "/tmp/foo" {
		t.Errorf("Get after Set = (%q, %v), want (/tmp/foo, true)", p, ok)
	}
}

func TestStoreDelete(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	s.Set("foo", "/tmp")

	if !s.Delete("foo") {
		t.Error("Delete on present name returned false")
	}
	if s.Delete("foo") {
		t.Error("Delete on missing name returned true")
	}
	_, ok := s.Get("foo")
	if ok {
		t.Error("Get after Delete returned true")
	}
}

func TestStoreSaveReload(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	s.Set("a", "/alpha")
	s.Set("b", "/beta")

	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a, ok := s2.Get("a"); !ok || a != "/alpha" {
		t.Errorf("reloaded a = (%q, %v)", a, ok)
	}
	if b, ok := s2.Get("b"); !ok || b != "/beta" {
		t.Errorf("reloaded b = (%q, %v)", b, ok)
	}
}

func TestStoreSaveLazyInit(t *testing.T) {
	dir := t.TempDir()
	// First open without saving — file shouldn't exist.
	s, _ := Open(dir)
	if s.Dirty() {
		t.Error("fresh store should not be dirty")
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	// File still shouldn't exist on disk (no changes).
	if _, err := os.Stat(filepath.Join(dir, filename)); !os.IsNotExist(err) {
		t.Error("expected alias file to not exist on no-change save")
	}
}

func TestStoreEntries(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	s.Set("z", "/z")
	s.Set("a", "/a")

	entries := s.Entries()
	if len(entries) != 2 {
		t.Fatalf("len = %d, want 2", len(entries))
	}
	if entries[0].Name != "a" || entries[1].Name != "z" {
		t.Errorf("Entries not sorted: %+v", entries)
	}
}

func TestStoreSetNoop(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	s.Set("x", "/x")
	s.Save()
	if s.Dirty() {
		t.Fatal("store should be clean after save")
	}
	s.Set("x", "/x") // same name + path → no-op
	if s.Dirty() {
		t.Error("re-setting same value should not dirty store")
	}
}

func TestValidateName(t *testing.T) {
	valid := []string{"foo", "my-proj", "api_2", "ABC"}
	for _, n := range valid {
		if err := ValidateName(n); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", n, err)
		}
	}
	invalid := map[string]string{
		"":          "empty",
		".":         "dot",
		"..":        "dotdot",
		"-a":        "leading dash",
		"foo/bar":   "slash",
		"x\ny":      "newline",
	}
	for n, desc := range invalid {
		if err := ValidateName(n); err == nil {
			t.Errorf("ValidateName(%q) (%s): expected error", n, desc)
		}
	}
}
