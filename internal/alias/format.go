package alias

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Versioned binary format for the aliases file — same discipline as db/format.go
// and the same atomic writer. The file is self-identifying with a distinct magic
// so it can never be confused with the DB file even if placed in the same
// directory.
//
// Layout (little-endian):
//
//	offset 0 : 4 bytes  magic "ZJAL"
//	offset 4 : u32      version
//	offset 8 : u64      entry count
//	per entry:
//	    u64  name length
//	    ...  name bytes (UTF-8)
//	    u64  path length
//	    ...  path bytes (UTF-8)

var magic = [4]byte{'Z', 'J', 'A', 'L'}

const (
	formatVersion uint32 = 1
	maxAliasSize         = 32 << 20 // 32 MiB guard, same as DB
	headerSize           = 4 + 4 + 8
)

// Alias is a single name→path mapping.
type Alias struct {
	Name string
	Path string
}

// serialize encodes aliases into the versioned binary format.
func serialize(entries []Alias) ([]byte, error) {
	size := headerSize
	for i := range entries {
		size += 8 + len(entries[i].Name) + 8 + len(entries[i].Path)
	}

	buf := make([]byte, 0, size)
	buf = append(buf, magic[:]...)
	var scratch [8]byte
	binary.LittleEndian.PutUint32(scratch[:4], formatVersion)
	buf = append(buf, scratch[:4]...)
	binary.LittleEndian.PutUint64(scratch[:], uint64(len(entries)))
	buf = append(buf, scratch[:]...)

	for i := range entries {
		e := &entries[i]
		binary.LittleEndian.PutUint64(scratch[:], uint64(len(e.Name)))
		buf = append(buf, scratch[:]...)
		buf = append(buf, e.Name...)
		binary.LittleEndian.PutUint64(scratch[:], uint64(len(e.Path)))
		buf = append(buf, scratch[:]...)
		buf = append(buf, e.Path...)
	}

	return buf, nil
}

// deserialize decodes the versioned binary format, enforcing the size guard,
// magic, and version.
func deserialize(data []byte) ([]Alias, error) {
	if len(data) > maxAliasSize {
		return nil, fmt.Errorf("alias file is too large (%d bytes, max %d): possibly corrupt", len(data), maxAliasSize)
	}
	if len(data) < 8 {
		return nil, fmt.Errorf("could not deserialize aliases: corrupted data")
	}
	if data[0] != magic[0] || data[1] != magic[1] || data[2] != magic[2] || data[3] != magic[3] {
		return nil, fmt.Errorf("not a zjump aliases file: bad magic header")
	}
	version := binary.LittleEndian.Uint32(data[4:8])
	if version != formatVersion {
		return nil, fmt.Errorf("unsupported alias version (got %d, supports %d)", version, formatVersion)
	}
	if len(data) < headerSize {
		return nil, fmt.Errorf("could not deserialize aliases: corrupted data")
	}

	count := binary.LittleEndian.Uint64(data[8:headerSize])
	rest := uint64(len(data) - headerSize)
	// Each entry costs at least 16 bytes (8 name + 8 path) — conservative guard.
	if count > rest/16 {
		return nil, fmt.Errorf("could not deserialize aliases: corrupted data (entry count %d exceeds file size)", count)
	}

	entries := make([]Alias, 0, count)
	off := headerSize
	for i := uint64(0); i < count; i++ {
		if off+8 > len(data) {
			return nil, fmt.Errorf("could not deserialize aliases: corrupted data (truncated entry %d)", i)
		}
		nameLen := binary.LittleEndian.Uint64(data[off : off+8])
		off += 8
		avail := uint64(len(data) - off)
		if nameLen > avail || avail-nameLen < 8 {
			return nil, fmt.Errorf("could not deserialize aliases: corrupted data (truncated name in entry %d)", i)
		}
		name := string(data[off : off+int(nameLen)])
		off += int(nameLen)

		pathLen := binary.LittleEndian.Uint64(data[off : off+8])
		off += 8
		avail = uint64(len(data) - off)
		if pathLen > avail {
			return nil, fmt.Errorf("could not deserialize aliases: corrupted data (truncated path in entry %d)", i)
		}
		path := string(data[off : off+int(pathLen)])
		off += int(pathLen)

		entries = append(entries, Alias{Name: name, Path: path})
	}

	return entries, nil
}

func init() {
	// Ensure compile-time constant is non-zero.
	if formatVersion == 0 {
		panic("formatVersion must be non-zero")
	}
	_ = math.MaxFloat64 // keep import
}
