// Copyright 2026 NornicDB contributors. Apache-2.0.
package nornic

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/bits"
	"testing"
)

// Experimental patched packing: zigzag values, packed low bits and full-value
// exceptions addressed by uint16 row. This is test-only, not a public format.
func docsPatched(b []byte) []byte {
	v := docsInts(b)
	u := make([]uint64, len(v))
	var histogram [65]int
	for i, x := range v {
		u[i] = uint64(x<<1) ^ uint64(x>>63)
		histogram[bits.Len64(u[i])]++
	}
	width, best := 64, len(v)*8
	exceptions := len(v)
	for w := 0; w <= 64; w++ {
		exceptions -= histogram[w]
		size := (len(v)*w+7)/8 + exceptions*10
		if size < best {
			width, best = w, size
		}
	}
	out := make([]byte, 5+(len(v)*width+7)/8)
	out[0] = byte(width)
	binary.LittleEndian.PutUint32(out[1:], uint32(len(v)))
	for i, x := range u {
		writePacked(out[5:], i*width, width, x)
		if bits.Len64(x) > width {
			out = binary.LittleEndian.AppendUint16(out, uint16(i))
			out = binary.LittleEndian.AppendUint64(out, x)
		}
	}
	return out
}
func docsUnpatched(b []byte) ([]byte, error) {
	if len(b) < 5 {
		return nil, ErrFormat
	}
	w, n := int(b[0]), int(binary.LittleEndian.Uint32(b[1:]))
	if w > 64 || n > MaxColumnValues {
		return nil, ErrFormat
	}
	end := 5 + (n*w+7)/8
	if end > len(b) || (len(b)-end)%10 != 0 {
		return nil, ErrFormat
	}
	v := make([]int64, n)
	for i := range v {
		x := readPacked(b[5:end], i*w, w)
		v[i] = int64(x>>1) ^ -int64(x&1)
	}
	for p := end; p < len(b); p += 10 {
		i := int(binary.LittleEndian.Uint16(b[p:]))
		if i >= n {
			return nil, ErrFormat
		}
		x := binary.LittleEndian.Uint64(b[p+2:])
		v[i] = int64(x>>1) ^ -int64(x&1)
	}
	return docsRaw(v), nil
}
func TestDocsCandidateRoundtrips(t *testing.T) {
	for _, v := range [][]int64{nil, {0}, {-1, 0, 1}, {math.MinInt64, math.MaxInt64, 0}, {1, 1, 1, math.MaxInt64}} {
		raw := docsRaw(v)
		got, e := docsUnpatched(docsPatched(raw))
		if e != nil || !bytes.Equal(got, raw) {
			t.Fatal(v, e)
		}
		for _, delta := range []bool{false, true} {
			got, e = docsUnmini(docsMini(raw, delta), delta)
			if e != nil || !bytes.Equal(got, raw) {
				t.Fatal(v, delta, e)
			}
		}
		if !bytes.Equal(docsShuffle(docsShuffle(raw, false), true), raw) {
			t.Fatal("shuffle")
		}
	}
	for width := 0; width <= 64; width++ {
		v := footprintValues("random", 257)
		for i := range v {
			if width < 64 {
				v[i] = int64(uint64(v[i]) & ((uint64(1) << width) - 1))
			}
		}
		raw := docsRaw(v)
		got, e := docsUnpatched(docsPatched(raw))
		if e != nil || !bytes.Equal(got, raw) {
			t.Fatal(width, e)
		}
	}
	for _, b := range [][]byte{nil, {65, 0, 0, 0, 0}, {0, 255, 255, 255, 255}, {64, 1, 0, 0, 0}} {
		if _, e := docsUnpatched(b); e == nil {
			t.Fatal("accepted malformed")
		}
	}
}
