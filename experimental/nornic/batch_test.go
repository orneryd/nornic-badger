package nornic

import (
	"encoding/binary"
	"math"
	"math/rand"
	"testing"
)

func TestBatchAllWidths(t *testing.T) {
	r := rand.New(rand.NewSource(81))
	for w := 0; w <= 64; w++ {
		for _, n := range []int{0, 1, 7, 257, 4096} {
			v := make([]int64, n)
			for i := range v {
				if w == 64 {
					v[i] = int64(r.Uint64())
				} else if w > 0 {
					v[i] = int64(r.Uint64()&((uint64(1)<<w)-1)) + math.MinInt64
				}
			}
			e, _ := EncodeInt64(v)
			c, err := OpenInt64(e)
			if err != nil {
				t.Fatal(err)
			}
			dst := make([]int64, n)
			if err = c.DecodeInto(dst); err != nil {
				t.Fatal(err)
			}
			for i := range v {
				if dst[i] != v[i] {
					t.Fatalf("width %d row %d", w, i)
				}
			}
			if err = c.DecodeInto(make([]int64, n+1)); err == nil {
				t.Fatal("length accepted")
			}
		}
	}
}
func BenchmarkBatchComparison(b *testing.B) {
	for _, shape := range []string{"constant", "small-range", "monotone", "high-entropy"} {
		v := make([]int64, 4096)
		r := rand.New(rand.NewSource(9))
		for i := range v {
			switch shape {
			case "constant":
				v[i] = 7
			case "small-range":
				v[i] = int64(i % 64)
			case "monotone":
				v[i] = int64(i)
			default:
				v[i] = int64(r.Uint64())
			}
		}
		e, _ := EncodeInt64(v)
		c, _ := OpenInt64(e)
		dst := make([]int64, len(v))
		raw := make([]byte, len(v)*8)
		for i, x := range v {
			binary.LittleEndian.PutUint64(raw[i*8:], uint64(x))
		}
		for _, mode := range []string{"scalar", "batch", "raw"} {
			b.Run(shape+"/"+mode, func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(raw)))
				for n := 0; n < b.N; n++ {
					switch mode {
					case "scalar":
						for i := range dst {
							dst[i], _ = c.At(i)
						}
					case "batch":
						if err := c.DecodeInto(dst); err != nil {
							b.Fatal(err)
						}
					default:
						for i := range dst {
							dst[i] = int64(binary.LittleEndian.Uint64(raw[i*8:]))
						}
					}
					benchmarkValue = dst[n%len(dst)]
				}
			})
		}
	}
}
