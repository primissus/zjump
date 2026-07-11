package db

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

// zjump-native, versioned binary on-disk format (O-1, PLAN.md §4, R-DB-1/6;
// v2 extends it for typed entries, PLAN-GIT.md §4, R2-DB).
//
// Layout (little-endian throughout, independent of host byte order):
//
//	offset 0 : 4 bytes  magic "ZJDB"
//	offset 4 : u32      version (== formatVersion; v1 still decodable, R2-DB-3)
//	offset 8 : u64      entry count
//	per entry:
//	    u64  path length
//	    ...  path bytes (UTF-8, no terminator)
//	    f64  rank (IEEE-754)
//	    u64  last_accessed (epoch seconds)
//	    u8   kind          (v2 only; 0=dir, 1=repo, 2=alias)
//	    u64  name length   (v2 only; must be 0 unless kind==2)
//	    ...  name bytes    (v2 only; UTF-8, no terminator)
//
// The 4-byte magic makes the file self-identifying and guarantees it can never
// be confused with (or mistaken for) zoxide's db.zo, whose first bytes are a
// bare u32 version — deliberately distinct from a zjump-native format (D-1).
var magic = [4]byte{'Z', 'J', 'D', 'B'}

const (
	formatVersion uint32 = 2

	// maxDBSize bounds reads so a corrupt/oversized file fails fast rather than
	// triggering an unbounded allocation, mirroring zoxide's 32 MiB guard
	// (ARCHITECTURE.md §3, R-DB-6).
	maxDBSize = 32 << 20 // 32 MiB

	headerSize = 4 + 4 + 8 // magic + version + count

	// Minimum on-disk bytes per entry, used to reject an absurd entry count
	// before allocating. v1: 8 (path len) + 8 (rank) + 8 (last). v2 adds 1
	// (kind) + 8 (name len).
	minEntryV1 = 24
	minEntryV2 = minEntryV1 + 1 + 8
)

// serialize encodes dirs into the current (v2) binary format.
func serialize(dirs []Dir) ([]byte, error) {
	// Preallocate: header + fixed overhead per entry + path/name bytes.
	size := headerSize
	for i := range dirs {
		size += minEntryV2 + len(dirs[i].Path) + len(dirs[i].Name)
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
		buf.WriteByte(byte(d.Kind))
		binary.LittleEndian.PutUint64(scratch[:], uint64(len(d.Name)))
		buf.Write(scratch[:])
		buf.WriteString(d.Name)
	}

	return buf.Bytes(), nil
}

// deserialize decodes the versioned binary format, enforcing the size guard, the
// magic, and the version. It is strict: any inconsistency is a hard error so a
// truncated/corrupt file never yields a silently-wrong database (R-DB-6). v1
// files decode with every entry becoming KindDir/name="" (R2-DB-3).
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
	if version != 1 && version != 2 {
		return nil, fmt.Errorf("unsupported version (got %d, supports 1, 2)", version)
	}
	if len(data) < headerSize {
		return nil, fmt.Errorf("could not deserialize database: corrupted data")
	}

	count := binary.LittleEndian.Uint64(data[8:headerSize])
	// Guard against an absurd count before allocating: each entry costs at least
	// minEntry bytes on disk, so count can never exceed the remaining budget.
	minEntry := uint64(minEntryV2)
	if version == 1 {
		minEntry = minEntryV1
	}
	rest := uint64(len(data) - headerSize)
	if count > rest/minEntry {
		return nil, fmt.Errorf("could not deserialize database: corrupted data (entry count %d exceeds file size)", count)
	}

	dirs := make([]Dir, 0, count)
	d := decoder{data: data, off: headerSize}
	for i := uint64(0); i < count; i++ {
		path, err := d.bytesField(i)
		if err != nil {
			return nil, err
		}
		rank, err := d.f64(i)
		if err != nil {
			return nil, err
		}
		last, err := d.u64(i)
		if err != nil {
			return nil, err
		}
		entry := Dir{Path: string(path), Rank: rank, LastAccessed: last}
		if version == 2 {
			kindByte, err := d.u8(i)
			if err != nil {
				return nil, err
			}
			if kindByte > byte(KindAlias) {
				return nil, fmt.Errorf("could not deserialize database: corrupted data (bad kind %d in entry %d)", kindByte, i)
			}
			name, err := d.bytesField(i)
			if err != nil {
				return nil, err
			}
			// Name is present iff the entry is an alias (§4 strictness).
			if kindByte == byte(KindAlias) && len(name) == 0 {
				return nil, fmt.Errorf("could not deserialize database: corrupted data (alias entry %d has empty name)", i)
			}
			if kindByte != byte(KindAlias) && len(name) != 0 {
				return nil, fmt.Errorf("could not deserialize database: corrupted data (non-alias entry %d carries a name)", i)
			}
			entry.Kind = Kind(kindByte)
			entry.Name = string(name)
		}
		dirs = append(dirs, entry)
	}

	return dirs, nil
}

// decoder is a bounds-checked forward cursor over the serialized payload. Every
// read validates the remaining budget without integer overflow (avail is capped
// at maxDBSize), so a corrupt length can never bypass the check or panic.
type decoder struct {
	data []byte
	off  int
}

func (d *decoder) truncated(entry uint64) error {
	return fmt.Errorf("could not deserialize database: corrupted data (truncated entry %d)", entry)
}

func (d *decoder) u8(entry uint64) (byte, error) {
	if d.off+1 > len(d.data) {
		return 0, d.truncated(entry)
	}
	b := d.data[d.off]
	d.off++
	return b, nil
}

func (d *decoder) u64(entry uint64) (uint64, error) {
	if d.off+8 > len(d.data) {
		return 0, d.truncated(entry)
	}
	v := binary.LittleEndian.Uint64(d.data[d.off : d.off+8])
	d.off += 8
	return v, nil
}

func (d *decoder) f64(entry uint64) (float64, error) {
	v, err := d.u64(entry)
	if err != nil {
		return 0, err
	}
	return math.Float64frombits(v), nil
}

// bytesField reads a u64 length prefix followed by that many raw bytes, checking
// the length against the remaining buffer without overflow.
func (d *decoder) bytesField(entry uint64) ([]byte, error) {
	if d.off+8 > len(d.data) {
		return nil, d.truncated(entry)
	}
	n := binary.LittleEndian.Uint64(d.data[d.off : d.off+8])
	d.off += 8
	avail := uint64(len(d.data) - d.off)
	if n > avail {
		return nil, d.truncated(entry)
	}
	b := d.data[d.off : d.off+int(n)]
	d.off += int(n)
	return b, nil
}
