// Copyright 2026 NornicDB contributors. Apache-2.0.
package nornic

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	badger "github.com/dgraph-io/badger/v4"
)

func footprintValues(shape string, n int) []int64 {
	values := make([]int64, n)
	r := rand.New(rand.NewSource(123))
	for i := range values {
		switch shape {
		case "constant":
			values[i] = 7
		case "small-range":
			values[i] = int64(i % 64)
		case "monotone":
			values[i] = int64(i)
		default:
			values[i] = int64(r.Uint64())
		}
	}
	return values
}

func TestCountEqualMatchesMaterialized(t *testing.T) {
	for _, v := range [][]int64{nil, {7, 7, 7}, {math.MinInt64, math.MaxInt64, 0, -1, 0}, {-30, -29, -28, -29}} {
		e, _ := EncodeInt64(v)
		c, err := OpenInt64(e)
		if err != nil {
			t.Fatal(err)
		}
		for _, target := range append(append([]int64{}, v...), math.MinInt64, math.MaxInt64, -31, 99) {
			want := 0
			for _, x := range v {
				if x == target {
					want++
				}
			}
			if got := c.CountEqual(target); got != want {
				t.Fatalf("%v target %d: %d != %d", v, target, got, want)
			}
		}
	}
	r := rand.New(rand.NewSource(47))
	for width := 1; width < 64; width++ {
		v := make([]int64, 257)
		mask := (uint64(1) << width) - 1
		for i := range v {
			v[i] = int64(r.Uint64()&mask) + math.MinInt64
		}
		e, _ := EncodeInt64(v)
		c, _ := OpenInt64(e)
		target := v[100]
		want := 0
		for _, x := range v {
			if x == target {
				want++
			}
		}
		if c.CountEqual(target) != want {
			t.Fatal(width)
		}
	}
}

var benchmarkCount int

func BenchmarkPredicateMemory(b *testing.B) {
	for _, shape := range []string{"constant", "small-range", "monotone", "high-entropy"} {
		values := footprintValues(shape, 4096)
		encoded, _ := EncodeInt64(values)
		c, _ := OpenInt64(encoded)
		target := values[17]
		for _, mode := range []string{"encoded-count", "allocate-decode-count", "reuse-decode-count"} {
			b.Run(shape+"/"+mode, func(b *testing.B) {
				reusable := make([]int64, c.Len())
				b.ReportAllocs()
				b.ResetTimer()
				for n := 0; n < b.N; n++ {
					if mode == "encoded-count" {
						benchmarkCount = c.CountEqual(target)
						continue
					}
					dst := reusable
					if mode == "allocate-decode-count" {
						dst = make([]int64, c.Len())
					}
					if err := c.DecodeInto(dst); err != nil {
						b.Fatal(err)
					}
					count := 0
					for _, v := range dst {
						if v == target {
							count++
						}
					}
					benchmarkCount = count
				}
			})
		}
	}
}

// Opt-in measurement: fixed-seed 1,048,576-value fixtures, no production data.
// Sizes include every regular file after clean close, not allocated disk sectors.
// Both encodings use identical Badger options, keys, sync policy and record count.
func TestBadgerStorageFootprint(t *testing.T) {
	if os.Getenv("NORNIC_FOOTPRINT_EXPERIMENT") != "1" {
		t.Skip("opt-in storage measurement")
	}
	const blocks = 256
	const rows = 4096
	for _, shape := range []string{"constant", "small-range", "monotone", "high-entropy"} {
		for _, threshold := range []int64{1, 1 << 20} {
			for _, encoding := range []string{"raw", "column"} {
				t.Run(fmt.Sprintf("%s/threshold-%d/%s", shape, threshold, encoding), func(t *testing.T) {
					dir := t.TempDir()
					opts := badger.DefaultOptions(dir).WithLogger(nil).WithValueThreshold(threshold).WithSyncWrites(true)
					db, err := badger.Open(opts)
					if err != nil {
						t.Fatal(err)
					}
					closed := false
					defer func() {
						if !closed {
							_ = db.Close()
						}
					}()
					payloadBytes := 0
					for block := 0; block < blocks; block++ {
						values := footprintValues(shape, rows)
						// Avoid identical blocks being mistaken for an engine-wide dedup benefit.
						for i := range values {
							values[i] = int64(uint64(values[i]) + uint64(block)*0x100000000)
						}
						var encoded []byte
						if encoding == "raw" {
							encoded = make([]byte, rows*8)
							for i, v := range values {
								binary.LittleEndian.PutUint64(encoded[i*8:], uint64(v))
							}
						} else {
							encoded, err = EncodeInt64(values)
							if err != nil {
								t.Fatal(err)
							}
						}
						payloadBytes += len(encoded)
						key := []byte(fmt.Sprintf("column/%04d", block))
						if err = db.Update(func(tx *badger.Txn) error { return tx.Set(key, encoded) }); err != nil {
							t.Fatal(err)
						}
					}
					if err = db.Close(); err != nil {
						t.Fatal(err)
					}
					closed = true
					var total int64
					err = filepath.WalkDir(dir, func(path string, d os.DirEntry, e error) error {
						if e != nil {
							return e
						}
						if d.IsDir() {
							return nil
						}
						info, e := d.Info()
						if e != nil {
							return e
						}
						if info.Mode().IsRegular() {
							total += info.Size()
						}
						return nil
					})
					if err != nil {
						t.Fatal(err)
					}
					// Reopen all stored blocks and compare every value, not only file sizes.
					db, err = badger.Open(opts)
					if err != nil {
						t.Fatal(err)
					}
					closed = false
					err = db.View(func(tx *badger.Txn) error {
						for block := 0; block < blocks; block++ {
							item, e := tx.Get([]byte(fmt.Sprintf("column/%04d", block)))
							if e != nil {
								return e
							}
							e = item.Value(func(data []byte) error {
								values := footprintValues(shape, rows)
								var c Int64Column
								if encoding == "column" {
									var e error
									c, e = OpenInt64(data)
									if e != nil {
										return e
									}
								}
								for i, v := range values {
									want := int64(uint64(v) + uint64(block)*0x100000000)
									var got int64
									if encoding == "raw" {
										got = int64(binary.LittleEndian.Uint64(data[i*8:]))
									} else {
										var e error
										got, e = c.At(i)
										if e != nil {
											return e
										}
									}
									if got != want {
										return fmt.Errorf("row mismatch block=%d row=%d", block, i)
									}
								}
								return nil
							})
							if e != nil {
								return e
							}
						}
						return nil
					})
					if err != nil {
						t.Fatal(err)
					}
					if err = db.Close(); err != nil {
						t.Fatal(err)
					}
					closed = true
					t.Logf("FOOTPRINT shape=%s threshold=%d encoding=%s values=%d payload_bytes=%d closed_directory_bytes=%d", shape, threshold, encoding, blocks*rows, payloadBytes, total)
				})
			}
		}
	}
}
