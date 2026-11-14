// Copyright 2026 NornicDB contributors. Apache-2.0.
// Package columnar supplies reversible physical value-column layouts. It knows
// no graph semantics and preserves logical keys, values, versions and checksums.
package columnar

import (
	"encoding/binary"
	"errors"
	"sort"
)

var ErrFormat = errors.New("invalid experimental columnar block")

const MaxBlockBytes = 64 << 20

// Span selects one opaque value/field; gaps retain all original surrounding bytes.
// Kind and Column group corresponding fields across records without interpreting them.
type Span struct {
	Start, Length int
	Kind          byte
	Column        uint64
}
type groupKey struct {
	kind   byte
	column uint64
}
type group struct {
	key           groupKey
	rows          []int
	maxLen, total int
}

func groups(spans []Span) []group {
	positions := map[groupKey]int{}
	var out []group
	for i, s := range spans {
		key := groupKey{s.Kind, s.Column}
		at, ok := positions[key]
		if !ok {
			at = len(out)
			positions[key] = at
			out = append(out, group{key: key})
		}
		g := &out[at]
		g.rows = append(g.rows, i)
		g.maxLen = max(g.maxLen, s.Length)
		g.total += s.Length
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].key, out[j].key
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		return a.column < b.column
	})
	return out
}
func (g group) dense() bool { return int64(g.maxLen)*int64(len(g.rows)) <= 4*int64(g.total) }

// Transpose aligns bytes within each annotated field column. Skewed groups
// remain sequential, bounding transform work without changing accepted values.
func Transpose(src []byte, spans []Span) ([]byte, error) {
	if len(spans) < 4 || len(spans) > 65536 || len(src) > MaxBlockBytes {
		return nil, nil
	}
	previous, total := 0, 0
	out := make([]byte, 12, 12+len(spans)*4+len(src))
	copy(out, "BCT2")
	binary.LittleEndian.PutUint32(out[4:], uint32(len(src)))
	binary.LittleEndian.PutUint32(out[8:], uint32(len(spans)))
	for _, s := range spans {
		if s.Start < previous || s.Length < 0 || s.Start > len(src) || s.Length > len(src)-s.Start {
			return nil, ErrFormat
		}
		out = binary.AppendUvarint(out, uint64(s.Start-previous))
		out = binary.AppendUvarint(out, uint64(s.Length))
		out = append(out, s.Kind)
		out = binary.AppendUvarint(out, s.Column)
		previous = s.Start + s.Length
		total += s.Length
	}
	if total == 0 {
		return nil, nil
	}
	previous = 0
	for _, s := range spans {
		out = append(out, src[previous:s.Start]...)
		previous = s.Start + s.Length
	}
	out = append(out, src[previous:]...)
	for _, g := range groups(spans) {
		if !g.dense() {
			for _, row := range g.rows {
				s := spans[row]
				out = append(out, src[s.Start:s.Start+s.Length]...)
			}
			continue
		}
		for col := 0; col < g.maxLen; col++ {
			for _, row := range g.rows {
				s := spans[row]
				if col < s.Length {
					out = append(out, src[s.Start+col])
				}
			}
		}
	}
	return out, nil
}
func frameSpans(frame []byte) ([]Span, int, int, error) {
	if len(frame) < 12 || string(frame[:4]) != "BCT2" {
		return nil, 0, 0, ErrFormat
	}
	length := uint64(binary.LittleEndian.Uint32(frame[4:]))
	count := uint64(binary.LittleEndian.Uint32(frame[8:]))
	if length > MaxBlockBytes || count < 4 || count > 65536 || count > uint64(len(frame)-12)/4 {
		return nil, 0, 0, ErrFormat
	}
	at := 12
	read := func() (uint64, error) {
		v, n := binary.Uvarint(frame[at:])
		if n <= 0 {
			return 0, ErrFormat
		}
		at += n
		return v, nil
	}
	spans := make([]Span, int(count))
	var pos, values uint64
	for i := range spans {
		gap, e := read()
		if e != nil {
			return nil, 0, 0, e
		}
		n, e := read()
		if e != nil {
			return nil, 0, 0, e
		}
		if at >= len(frame) {
			return nil, 0, 0, ErrFormat
		}
		kind := frame[at]
		at++
		column, e := read()
		if e != nil {
			return nil, 0, 0, e
		}
		if gap > length-pos || n > length-pos-gap {
			return nil, 0, 0, ErrFormat
		}
		pos += gap
		spans[i] = Span{Start: int(pos), Length: int(n), Kind: kind, Column: column}
		pos += n
		values += n
	}
	if values == 0 || uint64(at)+length != uint64(len(frame)) {
		return nil, 0, 0, ErrFormat
	}
	return spans, at, int(length), nil
}

// Restore reverses Transpose exactly, including original key offsets and checksums.
func Restore(frame []byte) ([]byte, error) {
	return RestoreWithAllocator(frame, func(length int) []byte { return make([]byte, length) })
}

// RestoreWithAllocator reconstructs directly in caller-owned storage. The frame
// is validated before allocate is called. allocate must return exactly length
// bytes; ownership and release remain with the caller, including on errors.
func RestoreWithAllocator(frame []byte, allocate func(length int) []byte) ([]byte, error) {
	spans, at, length, e := frameSpans(frame)
	if e != nil {
		return nil, e
	}
	out := allocate(length)
	if len(out) != length {
		return nil, ErrFormat
	}
	previous := 0
	for _, s := range spans {
		gap := s.Start - previous
		copy(out[previous:s.Start], frame[at:at+gap])
		at += gap
		previous = s.Start + s.Length
	}
	copy(out[previous:], frame[at:at+length-previous])
	at += length - previous
	for _, g := range groups(spans) {
		if !g.dense() {
			for _, row := range g.rows {
				s := spans[row]
				copy(out[s.Start:s.Start+s.Length], frame[at:at+s.Length])
				at += s.Length
			}
			continue
		}
		for col := 0; col < g.maxLen; col++ {
			for _, row := range g.rows {
				s := spans[row]
				if col < s.Length {
					out[s.Start+col] = frame[at]
					at++
				}
			}
		}
	}
	return out, nil
}
