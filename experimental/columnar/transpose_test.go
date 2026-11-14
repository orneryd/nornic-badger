// Copyright 2026 NornicDB contributors. Apache-2.0.
package columnar

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestTransposeRoundtrip(t *testing.T) {
	for _, ragged := range []bool{false, true} {
		var src []byte
		var spans []Span
		for i := 0; i < 64; i++ {
			src = append(src, byte(i), byte(i+1))
			start := len(src)
			length := 80
			if ragged {
				length += i % 5
			}
			for j := 0; j < length; j++ {
				src = append(src, byte(i+j))
			}
			spans = append(spans, Span{Start: start, Length: length})
		}
		src = append(src, []byte("offsets/checksum/footer")...)
		encoded, e := Transpose(src, spans)
		if e != nil || encoded == nil {
			t.Fatal(e)
		}
		got, e := Restore(encoded)
		if e != nil || !bytes.Equal(got, src) {
			t.Fatal("roundtrip", e)
		}
		var destination []byte
		got, e = RestoreWithAllocator(encoded, func(n int) []byte {
			destination = bytes.Repeat([]byte{0xff}, n)
			return destination
		})
		if e != nil || !bytes.Equal(got, src) || &got[0] != &destination[0] {
			t.Fatal("restore did not reconstruct directly into caller storage", e)
		}
		for _, bad := range [][]byte{nil, encoded[:len(encoded)-1], append(bytes.Clone(encoded), 0)} {
			if _, e := Restore(bad); e == nil {
				t.Fatal("malformed accepted")
			}
			if _, e := RestoreWithAllocator(bad, func(int) []byte { t.Fatal("allocated before frame validation"); return nil }); e == nil {
				t.Fatal("malformed accepted with allocator")
			}
		}
	}
}
func TestTransposeDeclinesSkewAndRejectsBounds(t *testing.T) {
	src := make([]byte, 1000)
	if got, e := Transpose(src, []Span{{Start: 0, Length: 1}, {Start: 1, Length: 1}, {Start: 2, Length: 1}, {Start: 3, Length: 997}, {Start: 1000, Length: 0}}); e != nil || got == nil {
		t.Fatal("skew not declined", e)
	}
	if _, e := Transpose(src, []Span{{Start: 0, Length: 5}, {Start: 4, Length: 5}, {Start: 9, Length: 5}, {Start: 14, Length: 5}}); e == nil {
		t.Fatal("overlap accepted")
	}
	bad := make([]byte, 12)
	copy(bad, "BCT2")
	binary.LittleEndian.PutUint32(bad[4:], ^uint32(0))
	binary.LittleEndian.PutUint32(bad[8:], 4)
	if _, e := Restore(bad); e == nil {
		t.Fatal("oversize accepted")
	}
}
func FuzzRestore(f *testing.F) {
	f.Add([]byte("BCT2"))
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = Restore(data) })
}
