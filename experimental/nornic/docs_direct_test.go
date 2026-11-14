// Copyright 2026 NornicDB contributors. Apache-2.0.
package nornic

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// Same experimental bytes as docsUnpatched, without an intermediate int64 array.
func docsUnpatchedDirect(b []byte) ([]byte, error) {
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
	out := make([]byte, n*8)
	for i := 0; i < n; i++ {
		x := readPacked(b[5:end], i*w, w)
		binary.LittleEndian.PutUint64(out[i*8:], uint64(int64(x>>1)^-int64(x&1)))
	}
	for p := end; p < len(b); p += 10 {
		i := int(binary.LittleEndian.Uint16(b[p:]))
		if i >= n {
			return nil, ErrFormat
		}
		x := binary.LittleEndian.Uint64(b[p+2:])
		binary.LittleEndian.PutUint64(out[i*8:], uint64(int64(x>>1)^-int64(x&1)))
	}
	return out, nil
}
func TestDocsDirectRoundtrip(t *testing.T) {
	for _, v := range [][]int64{nil, {0}, {-1, 0, 1}, {-9223372036854775808, 9223372036854775807, 0}, footprintValues("random", 4096)} {
		raw := docsRaw(v)
		got, e := docsUnpatchedDirect(docsPatched(raw))
		if e != nil || !bytes.Equal(got, raw) {
			t.Fatal(e)
		}
	}
}
