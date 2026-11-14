// Copyright 2026 NornicDB contributors. Licensed under the Apache License, Version 2.0.
package nornic

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/rand"
	"testing"
)

func assertColumn(t *testing.T, values []int64) {
	t.Helper()
	encoded, err := EncodeInt64(values)
	if err != nil {
		t.Fatal(err)
	}
	col, err := OpenInt64(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if col.Len() != len(values) {
		t.Fatal("row count")
	}
	for i, want := range values {
		got, err := col.At(i)
		if err != nil || got != want {
			t.Fatalf("row %d got %d want %d: %v", i, got, want, err)
		}
	}
	if _, err := col.At(-1); err == nil {
		t.Fatal("negative row accepted")
	}
	if _, err := col.At(len(values)); err == nil {
		t.Fatal("past-end row accepted")
	}
	if len(encoded) > columnHeader+8*len(values) {
		t.Fatal("expanded beyond raw")
	}
}

func TestInt64ColumnExactAcrossAllBitWidths(t *testing.T) {
	for _, values := range [][]int64{nil, {0}, {-7, -7, -7}, {math.MinInt64, math.MaxInt64, 0, -1, 1}, {-3, -2, -1, 0, 1, 2, 3}} {
		assertColumn(t, values)
	}
	rng := rand.New(rand.NewSource(42))
	for width := 1; width <= 63; width++ {
		mask := (uint64(1) << width) - 1
		values := make([]int64, 257)
		values[1] = int64(mask)
		for i := 2; i < len(values); i++ {
			values[i] = int64(rng.Uint64() & mask)
		}
		assertColumn(t, values)
		for i := range values {
			values[i] += math.MinInt64
		}
		assertColumn(t, values)
	}
}

func TestInt64ColumnEncodingChoiceAndValidation(t *testing.T) {
	constant, _ := EncodeInt64([]int64{9, 9, 9})
	if len(constant) != columnHeader || constant[4] != constantInt64 {
		t.Fatal("constant choice")
	}
	packed, _ := EncodeInt64([]int64{100, 101, 102, 103})
	if len(packed) != columnHeader+1 || packed[4] != frameInt64 {
		t.Fatal("frame choice")
	}
	raw, _ := EncodeInt64([]int64{math.MinInt64, math.MaxInt64})
	if raw[4] != rawInt64 {
		t.Fatal("raw choice")
	}
	if _, err := EncodeInt64(make([]int64, MaxColumnValues+1)); err == nil {
		t.Fatal("unbounded block")
	}
	for _, mutate := range []func([]byte){func(b []byte) { b[0] = 0 }, func(b []byte) { b[4] = 9 }, func(b []byte) { b[5] = 64 }, func(b []byte) { b[6] = 1 }, func(b []byte) { binary.LittleEndian.PutUint32(b[8:], math.MaxUint32) }} {
		bad := bytes.Clone(packed)
		mutate(bad)
		if _, err := OpenInt64(bad); err == nil {
			t.Fatal("malformed block accepted")
		}
	}
	if _, err := OpenInt64(packed[:len(packed)-1]); err == nil {
		t.Fatal("truncated block accepted")
	}
}

func FuzzInt64Column(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 255, 128})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxColumnValues*8 {
			t.Skip()
		}
		values := make([]int64, len(data)/8)
		for i := range values {
			values[i] = int64(binary.LittleEndian.Uint64(data[i*8:]))
		}
		assertColumn(t, values)
	})
}

var benchmarkValue int64

func BenchmarkInt64Column(b *testing.B) {
	for _, test := range []struct {
		name string
		make func(int) int64
	}{{"constant", func(int) int64 { return 900 }}, {"small-range", func(i int) int64 { return int64(1000 + i%64) }}, {"monotone", func(i int) int64 { return int64(i) }}, {"high-entropy", func(i int) int64 { return int64(uint64(i) * 0x9e3779b97f4a7c15) }}} {
		values := make([]int64, 4096)
		for i := range values {
			values[i] = test.make(i)
		}
		encoded, _ := EncodeInt64(values)
		col, _ := OpenInt64(encoded)
		b.Run(test.name+"/encode", func(b *testing.B) {
			b.ReportAllocs()
			b.ReportMetric(float64(len(encoded)), "encoded-bytes/block")
			for i := 0; i < b.N; i++ {
				if _, err := EncodeInt64(values); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(test.name+"/decode", func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(values) * 8))
			for n := 0; n < b.N; n++ {
				var v int64
				for i := range values {
					x, err := col.At(i)
					if err != nil {
						b.Fatal(err)
					}
					v ^= x
				}
				benchmarkValue = v
			}
		})
		b.Run(test.name+"/raw-reference", func(b *testing.B) {
			raw := make([]byte, len(values)*8)
			for i, v := range values {
				binary.LittleEndian.PutUint64(raw[i*8:], uint64(v))
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(raw)))
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				var v int64
				for i := range values {
					v ^= int64(binary.LittleEndian.Uint64(raw[i*8:]))
				}
				benchmarkValue = v
			}
		})
	}
}
