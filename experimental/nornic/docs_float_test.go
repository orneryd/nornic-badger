// Copyright 2026 NornicDB contributors. Apache-2.0.
package nornic

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func docsXOR(b []byte) []byte {
	out := make([]byte, len(b))
	var previous uint64
	for i := 0; i < len(b); i += 8 {
		x := binary.LittleEndian.Uint64(b[i:])
		binary.LittleEndian.PutUint64(out[i:], x^previous)
		previous = x
	}
	return out
}
func docsUnXOR(b []byte) []byte {
	out := make([]byte, len(b))
	var previous uint64
	for i := 0; i < len(b); i += 8 {
		previous ^= binary.LittleEndian.Uint64(b[i:])
		binary.LittleEndian.PutUint64(out[i:], previous)
	}
	return out
}
func TestDocsFloatBitPreservation(t *testing.T) {
	patterns := []uint64{0, 1 << 63, 0x7ff0000000000000, 0xfff0000000000000, 0x7ff8000000000001, 0x7ff8000000000002, 1, 0x000fffffffffffff, math.Float64bits(1.25)}
	var raw []byte
	for _, p := range patterns {
		if math.Float64bits(math.Float64frombits(p)) != p {
			t.Fatal("float roundtrip")
		}
		raw = binary.LittleEndian.AppendUint64(raw, p)
	}
	if !bytes.Equal(docsUnXOR(docsXOR(raw)), raw) {
		t.Fatal("xor")
	}
	for _, codec := range []docsCodec{{"patched", docsPatched, docsUnpatchedDirect}, {"anchor", docsAnchored, docsUnanchored}} {
		got, e := codec.decode(codec.encode(raw))
		if e != nil || !bytes.Equal(got, raw) {
			t.Fatal(codec.name, e)
		}
	}
}

// No floating-point arithmetic occurs in these storage loops.
func BenchmarkDocsFloatStorage(b *testing.B) {
	const n = 4096
	floats := make([]float64, n)
	integers := make([]uint64, n)
	out := make([]byte, n*8)
	for i := range floats {
		floats[i] = float64(i) / 100
		integers[i] = math.Float64bits(floats[i])
	}
	for _, mode := range []string{"float-bits", "uint64-bits"} {
		b.Run(mode, func(b *testing.B) {
			b.SetBytes(n * 8)
			b.ReportAllocs()
			for k := 0; k < b.N; k++ {
				if mode == "float-bits" {
					for i, x := range floats {
						binary.LittleEndian.PutUint64(out[i*8:], math.Float64bits(x))
					}
				} else {
					for i, x := range integers {
						binary.LittleEndian.PutUint64(out[i*8:], x)
					}
				}
			}
			docsFloatSink = out
		})
	}
}

var docsFloatSink []byte
