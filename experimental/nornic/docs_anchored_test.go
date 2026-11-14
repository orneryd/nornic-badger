// Copyright 2026 NornicDB contributors. Apache-2.0.
package nornic

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// Keep the first delta anchor outside the packed residuals. Otherwise the one
// absolute timestamp can poison the range of the entire first mini-block.
func docsAnchored(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	v := docsInts(b)
	first := v[0]
	for i := len(v) - 1; i > 0; i-- {
		v[i] = int64(uint64(v[i]) - uint64(v[i-1]))
	}
	out := binary.LittleEndian.AppendUint64(nil, uint64(first))
	return append(out, docsMini(docsRaw(v[1:]), false)...)
}
func docsUnanchored(b []byte) ([]byte, error) {
	if len(b) == 0 {
		return nil, nil
	}
	if len(b) < 8 {
		return nil, ErrFormat
	}
	tail, e := docsUnmini(b[8:], false)
	if e != nil {
		return nil, e
	}
	v := append([]int64{int64(binary.LittleEndian.Uint64(b))}, docsInts(tail)...)
	for i := 1; i < len(v); i++ {
		v[i] = int64(uint64(v[i]) + uint64(v[i-1]))
	}
	return docsRaw(v), nil
}
func TestDocsAnchoredRoundtrip(t *testing.T) {
	for _, v := range [][]int64{nil, {1}, {-1, 1, -1}, {-9223372036854775808, 9223372036854775807, 0}, footprintValues("random", 4096)} {
		raw := docsRaw(v)
		got, e := docsUnanchored(docsAnchored(raw))
		if e != nil || !bytes.Equal(got, raw) {
			t.Fatal(e)
		}
	}
}
