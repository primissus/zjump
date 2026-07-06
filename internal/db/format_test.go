package db

import (
	"encoding/binary"
	"testing"
)

// TestSerializeRoundTrip: entries survive an encode/decode cycle byte-for-byte
// (values), including a path with multibyte UTF-8 (R-DB-1).
func TestSerializeRoundTrip(t *testing.T) {
	in := []Dir{
		{Path: "/foo/bar", Rank: 1.5, LastAccessed: 100},
		{Path: "/tmp/λ/qux", Rank: 42.0, LastAccessed: 999999},
		{Path: "", Rank: 0.0, LastAccessed: 0},
	}
	data, err := serialize(in)
	if err != nil {
		t.Fatal(err)
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

func TestDeserializeEmpty(t *testing.T) {
	data, _ := serialize(nil)
	out, err := deserialize(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("want empty, got %d", len(out))
	}
}

func TestDeserializeBadMagic(t *testing.T) {
	data, _ := serialize([]Dir{{Path: "/x", Rank: 1, LastAccessed: 1}})
	data[0] = 'X' // corrupt magic
	if _, err := deserialize(data); err == nil {
		t.Error("expected error on bad magic")
	}
}

func TestDeserializeBadVersion(t *testing.T) {
	data, _ := serialize(nil)
	binary.LittleEndian.PutUint32(data[4:8], 999) // bogus version
	if _, err := deserialize(data); err == nil {
		t.Error("expected error on unsupported version")
	}
}

func TestDeserializeTooShort(t *testing.T) {
	if _, err := deserialize([]byte{1, 2, 3}); err == nil {
		t.Error("expected error on too-short input")
	}
}

func TestDeserializeTruncated(t *testing.T) {
	data, _ := serialize([]Dir{{Path: "/foo/bar", Rank: 1, LastAccessed: 1}})
	if _, err := deserialize(data[:len(data)-4]); err == nil {
		t.Error("expected error on truncated entry")
	}
}

func TestDeserializeAbsurdCount(t *testing.T) {
	data, _ := serialize(nil)
	binary.LittleEndian.PutUint64(data[8:16], 1<<40) // implausible entry count
	if _, err := deserialize(data); err == nil {
		t.Error("expected error on absurd entry count")
	}
}

// TestDeserializeOverflowPathLen: a crafted near-max pathLen must fail cleanly,
// never panic via an integer-overflow-bypassed bounds check (R-DB-6, R-ERR-3).
func TestDeserializeOverflowPathLen(t *testing.T) {
	data := make([]byte, 0, 40)
	data = append(data, magic[:]...)
	var u4 [4]byte
	binary.LittleEndian.PutUint32(u4[:], formatVersion)
	data = append(data, u4[:]...)
	var u8 [8]byte
	binary.LittleEndian.PutUint64(u8[:], 1) // count = 1
	data = append(data, u8[:]...)
	binary.LittleEndian.PutUint64(u8[:], ^uint64(0)) // pathLen = 0xFFFF...FF
	data = append(data, u8[:]...)
	data = append(data, make([]byte, 16)...) // padding to survive the count guard

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("deserialize panicked on crafted pathLen: %v", r)
		}
	}()
	if _, err := deserialize(data); err == nil {
		t.Error("expected a clean error on overflowing pathLen")
	}
}

// A zoxide db.zo starts with a bare u32 version (3) — no "ZJDB" magic — so it can
// never be misread as a zjump database (D-1).
func TestRejectsZoxideFormat(t *testing.T) {
	zoxideLike := make([]byte, 12)
	binary.LittleEndian.PutUint32(zoxideLike[0:4], 3) // zoxide VERSION
	binary.LittleEndian.PutUint64(zoxideLike[4:12], 0)
	if _, err := deserialize(zoxideLike); err == nil {
		t.Error("zjump must not accept a zoxide db.zo layout")
	}
}
