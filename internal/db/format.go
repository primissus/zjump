package db

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

// zjump-native, versioned binary on-disk format (O-1, PLAN.md §4, R-DB-1/6).
//
// Layout (little-endian throughout, independent of host byte order):
//
//	offset 0 : 4 bytes  magic "ZJDB"
//	offset 4 : u32      version (== formatVersion; reject anything else)
//	offset 8 : u64      entry count
//	per entry:
//	    u64  path length
//	    ...  path bytes (UTF-8, no terminator)
//	    f64  rank (IEEE-754)
//	    u64  last_accessed (epoch seconds)
//
// The 4-byte magic makes the file self-identifying and guarantees it can never
// be confused with (or mistaken for) zoxide's db.zo, whose first bytes are a
// bare u32 version — deliberately distinct from a zjump-native format (D-1).
var magic = [4]byte{'Z', 'J', 'D', 'B'}

const (
	formatVersion uint32 = 1

	// maxDBSize bounds reads so a corrupt/oversized file fails fast rather than
	// triggering an unbounded allocation, mirroring zoxide's 32 MiB guard
	// (ARCHITECTURE.md §3, R-DB-6).
	maxDBSize = 32 << 20 // 32 MiB

	headerSize = 4 + 4 + 8 // magic + version + count
)

// serialize encodes dirs into the versioned binary format.
func serialize(dirs []Dir) ([]byte, error) {
	// Preallocate: header + 24 bytes fixed overhead per entry + path bytes.
	size := headerSize
	for i := range dirs {
		size += 8 + len(dirs[i].Path) + 8 + 8
	}

	buf := bytes.NewBuffer(make([]byte, 0, size))
	buf.Write(magic[:])
	var scratch [8]byte
	binary.LittleEndian.PutUint32(scratch[:4], formatVersion)
	buf.Write(scratch[:4])
	binary.LittleEndian.PutUint64(scratch[:], uint64(len(dirs)))
	buf.Write(scratch[:])

	for i := range dirs {
		d := &dirs[i]
		binary.LittleEndian.PutUint64(scratch[:], uint64(len(d.Path)))
		buf.Write(scratch[:])
		buf.WriteString(d.Path)
		binary.LittleEndian.PutUint64(scratch[:], math.Float64bits(d.Rank))
		buf.Write(scratch[:])
		binary.LittleEndian.PutUint64(scratch[:], d.LastAccessed)
		buf.Write(scratch[:])
	}

	return buf.Bytes(), nil
}

// deserialize decodes the versioned binary format, enforcing the size guard, the
// magic, and the version. It is strict: any inconsistency is a hard error so a
// truncated/corrupt file never yields a silently-wrong database (R-DB-6).
func deserialize(data []byte) ([]Dir, error) {
	if len(data) > maxDBSize {
		return nil, fmt.Errorf("database is too large (%d bytes, max %d): possibly corrupt", len(data), maxDBSize)
	}
	if len(data) < 8 {
		return nil, fmt.Errorf("could not deserialize database: corrupted data")
	}
	if !bytes.Equal(data[:4], magic[:]) {
		return nil, fmt.Errorf("not a zjump database: bad magic header")
	}
	version := binary.LittleEndian.Uint32(data[4:8])
	if version != formatVersion {
		return nil, fmt.Errorf("unsupported version (got %d, supports %d)", version, formatVersion)
	}
	if len(data) < headerSize {
		return nil, fmt.Errorf("could not deserialize database: corrupted data")
	}

	count := binary.LittleEndian.Uint64(data[8:headerSize])
	// Guard against an absurd count before allocating: each entry costs at least
	// 24 bytes on disk, so count can never exceed the remaining byte budget.
	rest := uint64(len(data) - headerSize)
	if count > rest/24 {
		return nil, fmt.Errorf("could not deserialize database: corrupted data (entry count %d exceeds file size)", count)
	}

	dirs := make([]Dir, 0, count)
	off := headerSize
	for i := uint64(0); i < count; i++ {
		if off+8 > len(data) {
			return nil, fmt.Errorf("could not deserialize database: corrupted data (truncated entry %d)", i)
		}
		pathLen := binary.LittleEndian.Uint64(data[off : off+8])
		off += 8
		// Guard against a corrupt/huge pathLen WITHOUT overflowing: check the
		// bound as a subtraction (avail is capped at maxDBSize, so once we know
		// pathLen <= avail, int(pathLen) is always in range).
		avail := uint64(len(data) - off)
		if pathLen > avail || avail-pathLen < 16 {
			return nil, fmt.Errorf("could not deserialize database: corrupted data (truncated entry %d)", i)
		}
		path := string(data[off : off+int(pathLen)])
		off += int(pathLen)
		rank := math.Float64frombits(binary.LittleEndian.Uint64(data[off : off+8]))
		off += 8
		last := binary.LittleEndian.Uint64(data[off : off+8])
		off += 8
		dirs = append(dirs, Dir{Path: path, Rank: rank, LastAccessed: last})
	}

	return dirs, nil
}
