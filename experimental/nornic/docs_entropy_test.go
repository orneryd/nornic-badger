// Copyright 2026 NornicDB contributors. Apache-2.0.
package nornic

// This opt-in experiment deliberately keeps candidate codecs out of production paths.
import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	badger "github.com/dgraph-io/badger/v4"
	"github.com/klauspost/compress/s2"
	"github.com/klauspost/compress/zstd"
)

type docsBlock struct {
	key     string
	data    []byte
	numeric bool
}
type docsResult struct {
	Shape          string
	Codec          string
	InputBytes     int
	EncodedBytes   int
	EncodeNS       int64
	DecodeNS       int64
	AllocBytes     int64
	DirectoryBytes int64
}

func docsRaw(v []int64) []byte {
	b := make([]byte, len(v)*8)
	for i, x := range v {
		binary.LittleEndian.PutUint64(b[i*8:], uint64(x))
	}
	return b
}
func docsInts(b []byte) []int64 {
	v := make([]int64, len(b)/8)
	for i := range v {
		v[i] = int64(binary.LittleEndian.Uint64(b[i*8:]))
	}
	return v
}
func docsMini(b []byte, delta bool) []byte {
	v := docsInts(b)
	if delta {
		for i := len(v) - 1; i > 0; i-- {
			v[i] = int64(uint64(v[i]) - uint64(v[i-1]))
		}
	}
	var out []byte
	for start := 0; start < len(v); start += 128 {
		end := min(start+128, len(v))
		e, _ := EncodeInt64(v[start:end])
		out = binary.LittleEndian.AppendUint32(out, uint32(len(e)))
		out = append(out, e...)
	}
	return out
}
func docsUnmini(b []byte, delta bool) ([]byte, error) {
	var v []int64
	for len(b) > 0 {
		if len(b) < 4 {
			return nil, ErrFormat
		}
		n := int(binary.LittleEndian.Uint32(b))
		b = b[4:]
		if n > len(b) {
			return nil, ErrFormat
		}
		c, err := OpenInt64(b[:n])
		if err != nil {
			return nil, err
		}
		x := make([]int64, c.Len())
		if err = c.DecodeInto(x); err != nil {
			return nil, err
		}
		v = append(v, x...)
		b = b[n:]
	}
	if delta {
		for i := 1; i < len(v); i++ {
			v[i] = int64(uint64(v[i]) + uint64(v[i-1]))
		}
	}
	return docsRaw(v), nil
}
func docsShuffle(b []byte, inverse bool) []byte {
	out := make([]byte, len(b))
	n := len(b) / 8
	for i := 0; i < n; i++ {
		for j := 0; j < 8; j++ {
			if inverse {
				out[i*8+j] = b[j*n+i]
			} else {
				out[j*n+i] = b[i*8+j]
			}
		}
	}
	return out
}

type docsCodec struct {
	name   string
	encode func([]byte) []byte
	decode func([]byte) ([]byte, error)
}

func TestDocsEntropyExperiment(t *testing.T) {
	root := os.Getenv("NORNIC_DOCS_CORPUS")
	if root == "" {
		t.Skip("set NORNIC_DOCS_CORPUS to a Nornic docs directory")
	}
	outdir := os.Getenv("NORNIC_DOCS_RESULTS")
	if outdir == "" {
		t.Fatal("NORNIC_DOCS_RESULTS required")
	}
	if err := os.MkdirAll(outdir, 0755); err != nil {
		t.Fatal(err)
	}
	var paths []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".md") {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		t.Fatal("no documents")
	}
	var dictionary []byte
	manifest := map[string]string{}
	groups := map[string][]docsBlock{}
	digest := sha256.New()
	// Stable per-document/per-row hashes make entropy independent between blocks and reproducible.
	for docIndex, p := range paths {
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(b)
		manifest[rel] = hex.EncodeToString(h[:])
		fmt.Fprintf(digest, "%s\x00%x\n", rel, h)
		if len(paths) > 32 && docIndex < 16 {
			dictionary = append(dictionary, b[:min(4096, len(b))]...)
			continue
		}
		id := sha256.Sum256([]byte(rel))
		key := fmt.Sprintf("docs/%x", id[:16])
		for off := 0; off < len(b); off += 32768 {
			groups["documentation"] = append(groups["documentation"], docsBlock{fmt.Sprintf("%s/body/%08d", key, off), b[off:min(off+32768, len(b))], false})
		}
		// Each document contributes 128 generated records. Payloads are projections of
		// stable properties: path, body excerpt, status, sequence, and independent hash.
		var mixed, urls, random []byte
		low := make([]int64, 128)
		sparse := make([]int64, 128)
		ordered := make([]int64, 128)
		noise := make([]int64, 128)
		floats := make([]int64, 128)
		for i := 0; i < 128; i++ {
			r := sha256.Sum256([]byte(fmt.Sprintf("%s/%x/row/%d", rel, h, i)))
			low[i] = int64(r[0] % 16)
			sparse[i] = low[i]
			if i%100 == 0 {
				sparse[i] = int64(binary.LittleEndian.Uint64(r[:8]))
			}
			ordered[i] = 1700000000000000000 + int64(i)*1000000
			noise[i] = int64(binary.LittleEndian.Uint64(r[8:16]))
			floats[i] = int64(math.Float64bits(float64(i)/100 + float64(h[0])))
			random = append(random, r[:]...)
			urls = append(urls, fmt.Sprintf("https://nornic.example/docs/%s#row-%d\n", rel, i)...)
			row := struct {
				ID, Path, Status, Excerpt, Nonce string
				Sequence                         int64
			}{fmt.Sprintf("%s/%d", key, i), rel, []string{"active", "archived", "draft"}[r[0]%3], string(b[:min(160, len(b))]), hex.EncodeToString(r[:]), ordered[i]}
			j, e := json.Marshal(row)
			if e != nil {
				t.Fatal(e)
			}
			mixed = append(mixed, j...)
			mixed = append(mixed, '\n')
		}
		for shape, data := range map[string][]byte{"mixed-json": mixed, "unique-paths": urls, "independent-hashes": random, "low-range": docsRaw(low), "outliers": docsRaw(sparse), "timestamps": docsRaw(ordered), "random-int64": docsRaw(noise), "decimal-float-bits": docsRaw(floats)} {
			numeric := shape == "low-range" || shape == "outliers" || shape == "timestamps" || shape == "random-int64" || shape == "decimal-float-bits"
			groups[shape] = append(groups[shape], docsBlock{key + "/" + shape, data, numeric})
		}
	}
	metadata := struct {
		CorpusSHA256 string
		Documents    map[string]string
	}{hex.EncodeToString(digest.Sum(nil)), manifest}
	m, _ := json.MarshalIndent(metadata, "", "  ")
	if err = os.WriteFile(filepath.Join(outdir, "corpus.json"), m, 0644); err != nil {
		t.Fatal(err)
	}
	ze, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1), zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		t.Fatal(err)
	}
	de, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	defer ze.Close()
	defer de.Close()
	codecs := []docsCodec{
		{"raw", func(b []byte) []byte { return bytes.Clone(b) }, func(b []byte) ([]byte, error) { return bytes.Clone(b), nil }},
		{"snappy", func(b []byte) []byte { return s2.EncodeSnappy(nil, b) }, func(b []byte) ([]byte, error) { return s2.Decode(nil, b) }},
		{"zstd", func(b []byte) []byte { return ze.EncodeAll(b, nil) }, func(b []byte) ([]byte, error) { return de.DecodeAll(b, nil) }},
		{"adaptive-zstd", func(b []byte) []byte {
			e := ze.EncodeAll(b, nil)
			if len(e)+1 >= len(b)-len(b)/100 {
				return append([]byte{0}, b...)
			}
			return append([]byte{1}, e...)
		}, func(b []byte) ([]byte, error) {
			if b[0] == 0 {
				return bytes.Clone(b[1:]), nil
			}
			return de.DecodeAll(b[1:], nil)
		}},
		{"for", func(b []byte) []byte { e, _ := EncodeInt64(docsInts(b)); return e }, func(b []byte) ([]byte, error) {
			c, e := OpenInt64(b)
			if e != nil {
				return nil, e
			}
			v := make([]int64, c.Len())
			e = c.DecodeInto(v)
			return docsRaw(v), e
		}},
		{"mini-for", func(b []byte) []byte { return docsMini(b, false) }, func(b []byte) ([]byte, error) { return docsUnmini(b, false) }},
		{"delta-mini-for", func(b []byte) []byte { return docsMini(b, true) }, func(b []byte) ([]byte, error) { return docsUnmini(b, true) }},
		{"shuffle-zstd", func(b []byte) []byte { return ze.EncodeAll(docsShuffle(b, false), nil) }, func(b []byte) ([]byte, error) { x, e := de.DecodeAll(b, nil); return docsShuffle(x, true), e }},
	}
	codecs = append(codecs, docsCodec{"patched-for", docsPatched, docsUnpatched}, docsCodec{"anchored-delta", docsAnchored, docsUnanchored}, docsCodec{"patched-direct", docsPatched, docsUnpatchedDirect})
	if len(dictionary) > 0 {
		dw, e := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1), zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithEncoderDictRaw(42, dictionary))
		if e != nil {
			t.Fatal(e)
		}
		defer dw.Close()
		dr, e := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1), zstd.WithDecoderDictRaw(42, dictionary))
		if e != nil {
			t.Fatal(e)
		}
		defer dr.Close()
		codecs = append(codecs, docsCodec{"dictionary-zstd", func(b []byte) []byte { return dw.EncodeAll(b, nil) }, func(b []byte) ([]byte, error) { return dr.DecodeAll(b, nil) }})
	}
	codecs = append(codecs, docsCodec{"sampled-zstd", func(b []byte) []byte {
		sample := b[:min(512, len(b))]
		trial := ze.EncodeAll(sample, nil)
		if len(trial)+1 >= len(sample)-len(sample)/100 {
			return append([]byte{0}, b...)
		}
		e := ze.EncodeAll(b, nil)
		if len(e)+1 >= len(b)-len(b)/100 {
			return append([]byte{0}, b...)
		}
		return append([]byte{1}, e...)
	}, func(b []byte) ([]byte, error) {
		if b[0] == 0 {
			return bytes.Clone(b[1:]), nil
		}
		return de.DecodeAll(b[1:], nil)
	}})
	codecs = append(codecs, docsCodec{"xor-zstd", func(b []byte) []byte { return ze.EncodeAll(docsXOR(b), nil) }, func(b []byte) ([]byte, error) {
		x, e := de.DecodeAll(b, nil)
		if e != nil {
			return nil, e
		}
		return docsUnXOR(x), nil
	}})
	var results []docsResult
	var shapes []string
	for shape := range groups {
		shapes = append(shapes, shape)
	}
	sort.Strings(shapes)
	for _, shape := range shapes {
		if selection := os.Getenv("NORNIC_DOCS_SHAPES"); selection != "" && !strings.Contains(","+selection+",", ","+shape+",") {
			continue
		}
		blocks := groups[shape]
		for ci, c := range codecs {
			if selection := os.Getenv("NORNIC_DOCS_CODECS"); selection != "" && !strings.Contains(","+selection+",", ","+c.name+",") {
				continue
			}
			if ci >= 4 && c.name != "dictionary-zstd" && c.name != "sampled-zstd" && !blocks[0].numeric {
				continue
			}
			r := docsResult{Shape: shape, Codec: c.name}
			if c.name == "dictionary-zstd" {
				r.EncodedBytes = len(dictionary)
			}
			encoded := make([][]byte, len(blocks))
			begin := time.Now()
			for i, b := range blocks {
				encoded[i] = c.encode(b.data)
				r.InputBytes += len(b.data)
				r.EncodedBytes += len(encoded[i])
			}
			r.EncodeNS = time.Since(begin).Nanoseconds()
			begin = time.Now()
			for i, b := range blocks {
				got, e := c.decode(encoded[i])
				if e != nil || !bytes.Equal(got, b.data) {
					t.Fatalf("%s/%s block %d: %v", shape, c.name, i, e)
				}
			}
			r.DecodeNS = time.Since(begin).Nanoseconds()
			// Allocation measurement covers full per-block decode, separate from timings.
			bench := testing.Benchmark(func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					_, e := c.decode(encoded[i%len(encoded)])
					if e != nil {
						b.Fatal(e)
					}
				}
			})
			r.AllocBytes = bench.AllocedBytesPerOp()
			dir := t.TempDir()
			db, e := badger.Open(badger.DefaultOptions(dir).WithLogger(nil).WithSyncWrites(true))
			if e != nil {
				t.Fatal(e)
			}
			for i, b := range blocks {
				e = db.Update(func(tx *badger.Txn) error { return tx.Set([]byte(b.key), encoded[i]) })
				if e != nil {
					t.Fatal(e)
				}
			}
			if e = db.Close(); e != nil {
				t.Fatal(e)
			}
			e = filepath.WalkDir(dir, func(p string, d os.DirEntry, e error) error {
				if e != nil {
					return e
				}
				if !d.IsDir() {
					info, e := d.Info()
					if e != nil {
						return e
					}
					r.DirectoryBytes += info.Size()
				}
				return nil
			})
			if e != nil {
				t.Fatal(e)
			}
			db, e = badger.Open(badger.DefaultOptions(dir).WithLogger(nil))
			if e != nil {
				t.Fatal(e)
			}
			e = db.View(func(tx *badger.Txn) error {
				for i, b := range blocks {
					item, e := tx.Get([]byte(b.key))
					if e != nil {
						return e
					}
					e = item.Value(func(v []byte) error {
						if !bytes.Equal(v, encoded[i]) {
							return ErrFormat
						}
						got, e := c.decode(v)
						if e != nil {
							return e
						}
						if !bytes.Equal(got, b.data) {
							return ErrFormat
						}
						return nil
					})
					if e != nil {
						return e
					}
				}
				return nil
			})
			ce := db.Close()
			if e != nil || ce != nil {
				t.Fatalf("reopen: %v %v", e, ce)
			}
			results = append(results, r)
			t.Logf("%s/%s input=%d encoded=%d disk=%d", shape, c.name, r.InputBytes, r.EncodedBytes, r.DirectoryBytes)
		}
	}
	data, _ := json.MarshalIndent(results, "", "  ")
	if err = os.WriteFile(filepath.Join(outdir, "results.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}
