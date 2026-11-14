// Copyright 2026 NornicDB contributors. Apache-2.0.
package nornic

import "encoding/binary"

// CountEqual evaluates an equality count without an expanded output array.
// The validated column's immutable input remains borrowed. No result cache or
// approximate comparison is used. Work is bounded by MaxColumnValues.
func (c Int64Column) CountEqual(target int64) int {
	switch c.mode {
	case constantInt64:
		if target == int64(c.base) {
			return c.count
		}
		return 0
	case rawInt64:
		count := 0
		for i := 0; i < c.count; i++ {
			if int64(binary.LittleEndian.Uint64(c.data[i*8:])) == target {
				count++
			}
		}
		return count
	default:
		if target < int64(c.base) {
			return 0
		}
		delta := uint64(target) - c.base
		if delta > (uint64(1)<<c.width)-1 {
			return 0
		}
		count := 0
		for i := 0; i < c.count; i++ {
			if readPacked(c.data, i*c.width, c.width) == delta {
				count++
			}
		}
		return count
	}
}
