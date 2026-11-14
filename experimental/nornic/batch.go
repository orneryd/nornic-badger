// Copyright 2026 NornicDB contributors. Apache-2.0.
package nornic

import "encoding/binary"

// DecodeInto decodes a bounded column, dispatching once per batch.
func (c Int64Column) DecodeInto(dst []int64) error {
	if len(dst) != c.count {
		return ErrFormat
	}
	switch c.mode {
	case rawInt64:
		for i := range dst {
			dst[i] = int64(binary.LittleEndian.Uint64(c.data[i*8:]))
		}
	case constantInt64:
		for i := range dst {
			dst[i] = int64(c.base)
		}
	default:
		mask := (uint64(1) << c.width) - 1
		for i := range dst {
			bit := i * c.width
			off, shift := bit/8, uint(bit%8)
			var word uint64
			if off+8 <= len(c.data) {
				word = binary.LittleEndian.Uint64(c.data[off:])
			} else {
				for j := off; j < len(c.data); j++ {
					word |= uint64(c.data[j]) << uint((j-off)*8)
				}
			}
			value := word >> shift
			if shift+uint(c.width) > 64 {
				value |= uint64(c.data[off+8]) << (64 - shift)
			}
			dst[i] = int64(c.base + (value & mask))
		}
	}
	return nil
}
