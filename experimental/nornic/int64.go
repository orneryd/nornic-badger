// Copyright 2026 NornicDB contributors. Licensed under the Apache License, Version 2.0.
package nornic

import (
	"encoding/binary"
	"math/bits"
)

const columnHeader = 20
const MaxColumnValues = 65536
const (
	rawInt64 byte = iota
	constantInt64
	frameInt64
)

// EncodeInt64 selects raw, constant or frame-of-reference bit packing by size.
// Signed values remain exact, including the full int64 range. This scalar codec
// is a reference implementation, not a SIMD or arbitrary-precision accumulator.
func EncodeInt64(values []int64) ([]byte, error) {
	if len(values) > MaxColumnValues {
		return nil, ErrFormat
	}
	var low, high int64
	if len(values) > 0 {
		low, high = values[0], values[0]
	}
	for _, v := range values {
		if v < low {
			low = v
		}
		if v > high {
			high = v
		}
	}
	width := bits.Len64(uint64(high) - uint64(low))
	mode := frameInt64
	if len(values) == 0 || width == 64 {
		mode = rawInt64
		width = 64
	} else if width == 0 {
		mode = constantInt64
	}
	length := columnHeader + (len(values)*width+7)/8
	out := make([]byte, length)
	copy(out, "NIC1")
	out[4] = mode
	out[5] = byte(width)
	binary.LittleEndian.PutUint32(out[8:12], uint32(len(values)))
	binary.LittleEndian.PutUint64(out[12:20], uint64(low))
	for i, v := range values {
		if mode == rawInt64 {
			binary.LittleEndian.PutUint64(out[columnHeader+i*8:], uint64(v))
			continue
		}
		writePacked(out[columnHeader:], i*width, width, uint64(v)-uint64(low))
	}
	return out, nil
}

// Int64Column borrows immutable encoded bytes. At decodes one value without
// expanding its neighbors. The caller owns the byte slice's lifetime.
type Int64Column struct {
	data         []byte
	count, width int
	mode         byte
	base         uint64
}

func OpenInt64(encoded []byte) (Int64Column, error) {
	if len(encoded) < columnHeader || string(encoded[:4]) != "NIC1" || encoded[6] != 0 || encoded[7] != 0 {
		return Int64Column{}, ErrFormat
	}
	count32 := binary.LittleEndian.Uint32(encoded[8:12])
	if count32 > MaxColumnValues {
		return Int64Column{}, ErrFormat
	}
	mode, width, count := encoded[4], int(encoded[5]), int(count32)
	if count > MaxColumnValues || mode > frameInt64 || (mode == rawInt64 && width != 64) || (mode == constantInt64 && (width != 0 || count == 0)) || (mode == frameInt64 && (width < 1 || width > 63 || count == 0)) || len(encoded) != columnHeader+(count*width+7)/8 {
		return Int64Column{}, ErrFormat
	}
	return Int64Column{data: encoded[columnHeader:], count: count, width: width, mode: mode, base: binary.LittleEndian.Uint64(encoded[12:20])}, nil
}

func (c Int64Column) Len() int { return c.count }
func (c Int64Column) At(row int) (int64, error) {
	if row < 0 || row >= c.count {
		return 0, ErrFormat
	}
	if c.mode == rawInt64 {
		return int64(binary.LittleEndian.Uint64(c.data[row*8:])), nil
	}
	return int64(c.base + readPacked(c.data, row*c.width, c.width)), nil
}

func writePacked(data []byte, offset, width int, value uint64) {
	for width > 0 {
		shift := offset % 8
		take := min(8-shift, width)
		mask := uint64((1 << take) - 1)
		data[offset/8] |= byte(value&mask) << shift
		value >>= take
		offset += take
		width -= take
	}
}
func readPacked(data []byte, offset, width int) uint64 {
	var value uint64
	written := 0
	for width > 0 {
		shift := offset % 8
		take := min(8-shift, width)
		mask := byte((1 << take) - 1)
		value |= uint64((data[offset/8]>>shift)&mask) << written
		offset += take
		written += take
		width -= take
	}
	return value
}
