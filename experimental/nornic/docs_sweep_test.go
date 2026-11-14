// Copyright 2026 NornicDB contributors. Apache-2.0.
package nornic

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDocsOutlierSweep(t *testing.T) {
	root := os.Getenv("NORNIC_DOCS_CORPUS")
	if root == "" {
		t.Skip("opt-in documentation corpus")
	}
	out := os.Getenv("NORNIC_DOCS_SWEEP_RESULTS")
	if out == "" {
		t.Skip("set NORNIC_DOCS_SWEEP_RESULTS")
	}
	seed, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	seedHash := sha256.Sum256(seed)
	codecs := []docsCodec{{"for", func(b []byte) []byte { v, _ := EncodeInt64(docsInts(b)); return v }, func(b []byte) ([]byte, error) {
		c, e := OpenInt64(b)
		if e != nil {
			return nil, e
		}
		v := make([]int64, c.Len())
		e = c.DecodeInto(v)
		return docsRaw(v), e
	}}, {"mini-for", func(b []byte) []byte { return docsMini(b, false) }, func(b []byte) ([]byte, error) { return docsUnmini(b, false) }}, {"delta-mini-for", func(b []byte) []byte { return docsMini(b, true) }, func(b []byte) ([]byte, error) { return docsUnmini(b, true) }}, {"patched-for", docsPatched, docsUnpatched}}
	var rows []docsResult
	for _, count := range []int{0, 4, 41, 205, 819, 4096} {
		v := make([]int64, 4096)
		for i := range v {
			h := sha256.Sum256([]byte(fmt.Sprintf("%x/row/%d", seedHash, i)))
			v[i] = int64(h[0] % 16)
			if i < count {
				v[i] = int64(binary.LittleEndian.Uint64(h[8:]))
			}
		}
		// Spread exceptions across the block without introducing non-deterministic RNG.
		shuffled := make([]int64, len(v))
		for i, x := range v {
			shuffled[(i*2053)%len(v)] = x
		}
		raw := docsRaw(shuffled)
		for _, c := range codecs {
			r := docsResult{Shape: fmt.Sprintf("outliers-%d-of-4096", count), Codec: c.name, InputBytes: len(raw)}
			var encoded []byte
			begin := time.Now()
			for i := 0; i < 100; i++ {
				encoded = c.encode(raw)
			}
			r.EncodeNS = time.Since(begin).Nanoseconds() / 100
			r.EncodedBytes = len(encoded)
			var got []byte
			begin = time.Now()
			for i := 0; i < 100; i++ {
				got, err = c.decode(encoded)
				if err != nil {
					t.Fatal(err)
				}
			}
			r.DecodeNS = time.Since(begin).Nanoseconds() / 100
			if !bytes.Equal(got, raw) {
				t.Fatal(c.name, count)
			}
			rows = append(rows, r)
		}
	}
	data, _ := json.MarshalIndent(struct {
		SeedSHA256 string
		Rows       []docsResult
	}{fmt.Sprintf("%x", seedHash), rows}, "", "  ")
	if err = os.WriteFile(out, data, 0644); err != nil {
		t.Fatal(err)
	}
}
