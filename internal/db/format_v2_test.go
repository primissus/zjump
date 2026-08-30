package db

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// TestFormatV2RoundTrip: every kind, including multi-byte UTF-8 in both path and
// alias name, survives an encode/decode cycle (R2-DB-2).
func TestFormatV2RoundTrip(t *testing.T) {
	in := []Dir{
		{Path: "/plain/dir", Rank: 1.5, LastAccessed: 100, Kind: KindDir},
		{Path: "/repos/λ-project", Rank: 42.0, LastAccessed: 999, Kind: KindRepo},
		{Path: "/tmp/中文/target", Rank: 7.0, LastAccessed: 5, Kind: KindAlias, Name: "工作"},
		{Path: "", Rank: 0.0, LastAccessed: 0, Kind: KindDir},
	}
	data, err := serialize(in)
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(data[4:8]); got != 2 {
		t.Fatalf("serialized version = %d, want 2", got)
	}
	out, err := deserialize(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(in) {
		t.Fatalf("len = %d, want %d", len(out), len(in))
	}
	for i := range in {
		if out[i] != in[i] {
			t.Errorf("entry %d = %+v, want %+v", i, out[i], in[i])
		}
	}
}

// buildV2Entry hand-assembles a single-entry v2 file so tests can inject fields
// (bad kind, mismatched name) that serialize would never produce.
func buildV2Entry(path string, rank float64, last uint64, kind byte, name string) []byte {
	var b []byte
	b = append(b, magic[:]...)
	var u4 [4]byte
	binary.LittleEndian.PutUint32(u4[:], 2)
	b = append(b, u4[:]...)
	var u8 [8]byte
	binary.LittleEndian.PutUint64(u8[:], 1) // count
	b = append(b, u8[:]...)
	binary.LittleEndian.PutUint64(u8[:], uint64(len(path)))
	b = append(b, u8[:]...)
	b = append(b, path...)
	binary.LittleEndian.PutUint64(u8[:], math.Float64bits(rank))
	b = append(b, u8[:]...)
	binary.LittleEndian.PutUint64(u8[:], last)
	b = append(b, u8[:]...)
	b = append(b, kind)
	binary.LittleEndian.PutUint64(u8[:], uint64(len(name)))
	b = append(b, u8[:]...)
	b = append(b, name...)
	return b
}

func TestFormatV2RejectsBadKind(t *testing.T) {
	if _, err := deserialize(buildV2Entry("/x", 1, 1, 3, "")); err == nil {
		t.Error("expected error on kind byte > 2")
	}
}

func TestFormatV2RejectsNameOnDir(t *testing.T) {
	if _, err := deserialize(buildV2Entry("/x", 1, 1, byte(KindDir), "oops")); err == nil {
		t.Error("expected error on non-alias entry carrying a name")
	}
}

func TestFormatV2RejectsAliasWithoutName(t *testing.T) {
	if _, err := deserialize(buildV2Entry("/x", 1, 1, byte(KindAlias), "")); err == nil {
		t.Error("expected error on alias entry with empty name")
	}
}

// TestFormatV2Truncation: chopping any trailing bytes of a valid v2 file (into
// the kind/name fields) is a clean error, never a panic (R2-DB-2, R-ERR-3).
func TestFormatV2Truncation(t *testing.T) {
	full := buildV2Entry("/some/path", 3.5, 42, byte(KindAlias), "alias")
	for cut := 1; cut < 18; cut++ { // walk back through name, name-len, kind, last...
		data := full[:len(full)-cut]
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("deserialize panicked at cut=%d: %v", cut, r)
				}
			}()
			if _, err := deserialize(data); err == nil {
				t.Errorf("expected error at cut=%d", cut)
			}
		}()
	}
}

// dbFileVersion reads the on-disk format version of a saved database file.
func dbFileVersion(t *testing.T, path string) uint32 {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 8 {
		t.Fatalf("file too short: %d bytes", len(raw))
	}
	return binary.LittleEndian.Uint32(raw[4:8])
}

// TestV1FixtureUpgrade decodes the committed v1 fixture: every entry becomes
// KindDir with an empty name, and the first dirty save rewrites the file as v2
// (R2-DB-3).
func TestV1FixtureUpgrade(t *testing.T) {
	raw, err := os.ReadFile("testdata/v1.db")
	if err != nil {
		t.Fatal(err)
	}
	if v := binary.LittleEndian.Uint32(raw[4:8]); v != 1 {
		t.Fatalf("fixture is version %d, expected a v1 file", v)
	}

	dirs, err := deserialize(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 4 {
		t.Fatalf("want 4 entries, got %d", len(dirs))
	}
	sawMultibyte := false
	for _, d := range dirs {
		if d.Kind != KindDir {
			t.Errorf("v1 entry %q decoded as kind %d, want KindDir", d.Path, d.Kind)
		}
		if d.Name != "" {
			t.Errorf("v1 entry %q decoded with name %q, want empty", d.Path, d.Name)
		}
		if d.Path == "/tmp/λ/data" {
			sawMultibyte = true
		}
	}
	if !sawMultibyte {
		t.Error("expected the multi-byte fixture path to survive decoding")
	}

	// Load the fixture as a live DB, dirty it, and confirm the save rewrites v2.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, Filename)
	if err := os.WriteFile(dbPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	database, err := OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	database.AddUpdate("/new/entry", 1.0, testEpoch, KindDir) // dirties
	if err := database.Save(); err != nil {
		t.Fatal(err)
	}
	if v := dbFileVersion(t, dbPath); v != 2 {
		t.Errorf("after dirty save the file is version %d, want 2", v)
	}
	// Re-read: the v2 file must decode with the same entry count + 1 new one.
	reopened, err := OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.Dirs()) != 5 {
		t.Errorf("reopened v2 db has %d entries, want 5", len(reopened.Dirs()))
	}
}

// TestLoadV1WithoutDirtyDoesNotRewrite: merely opening a v1 file and saving
// (without mutating) leaves it byte-for-byte v1 — D-4 preserved (R2-DB-3).
func TestLoadV1WithoutDirtyDoesNotRewrite(t *testing.T) {
	raw, err := os.ReadFile("testdata/v1.db")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	dbPath := filepath.Join(dir, Filename)
	if err := os.WriteFile(dbPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	database, err := OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if database.Dirty() {
		t.Fatal("freshly loaded db should not be dirty")
	}
	if err := database.Save(); err != nil { // no-op under D-4
		t.Fatal(err)
	}
	if v := dbFileVersion(t, dbPath); v != 1 {
		t.Errorf("save without mutation rewrote the file to version %d, want it left at v1", v)
	}
}
