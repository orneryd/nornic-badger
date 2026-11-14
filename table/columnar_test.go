// Copyright 2026 NornicDB contributors. Apache-2.0.
package table

import (
	"bytes"
	"fmt"
	"github.com/dgraph-io/badger/v4/experimental/columnar"
	"github.com/dgraph-io/badger/v4/options"
	"github.com/dgraph-io/badger/v4/y"
	"github.com/dgraph-io/ristretto/v2/z"
	"github.com/stretchr/testify/require"
	"testing"
)

// BenchmarkColumnarPhysicalCodec uses real SST entry/footer bytes and includes
// the allocation/copy made by the table reader, rather than only the transform.
func BenchmarkColumnarPhysicalCodec(b *testing.B) {
	opts := getTestTableOptions()
	opts.Compression = options.None
	opts.BlockSize = 64 << 10
	builder := NewTableBuilder(opts)
	defer builder.Close()
	for i := 0; i < 128; i++ {
		builder.Add(y.KeyWithTs([]byte(fmt.Sprintf("key/%04d", i)), uint64(i)), y.ValueStruct{Value: bytes.Repeat([]byte{byte(i % 8)}, 200)}, 0)
	}
	builder.finishBlock()
	raw := builder.blockList[0].data[:builder.blockList[0].end]
	encoded, err := compressColumnarBlock(raw, 1)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("encode", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, e := compressColumnarBlock(raw, 1); e != nil {
				b.Fatal(e)
			}
		}
	})
	b.Run("decode", func(b *testing.B) {
		b.ReportAllocs()
		table := &Table{opt: &Options{Compression: options.NornicColumnar, BlockSize: opts.BlockSize}}
		for i := 0; i < b.N; i++ {
			block := &Block{data: encoded}
			if e := table.decompress(block); e != nil {
				b.Fatal(e)
			}
			if !bytes.Equal(block.data, raw) {
				b.Fatal("bytes changed")
			}
			if block.freeMe {
				z.Free(block.data)
			}
		}
	})
}

func TestColumnarTableRoundtrip(t *testing.T) {
	for _, blockSize := range []int{4096, 32768} {
		t.Run(fmt.Sprint(blockSize), func(t *testing.T) {
			opts := getTestTableOptions()
			opts.Compression = options.NornicColumnar
			opts.ZSTDCompressionLevel = 1
			opts.BlockSize = blockSize
			opts.ChkMode = options.OnTableAndBlockRead
			var rows [][]string
			for i := 0; i < 1500; i++ {
				rows = append(rows, []string{fmt.Sprintf("node/%06d", i), fmt.Sprintf("node=%06d; property=status; value=%d; %s", i, i%16, bytes.Repeat([]byte("documentation "), i%8))})
			}
			table := buildTable(t, rows, opts)
			defer table.DecrRef()
			iterator := table.NewIterator(0)
			defer iterator.Close()
			i := 0
			for iterator.Rewind(); iterator.Valid(); iterator.Next() {
				require.Equal(t, y.KeyWithTs([]byte(rows[i][0]), 0), iterator.Key())
				require.Equal(t, []byte(rows[i][1]), iterator.Value().Value)
				i++
			}
			require.Equal(t, len(rows), i)
			iterator.Seek(y.KeyWithTs([]byte(rows[900][0]), 0))
			require.True(t, iterator.Valid())
			require.Equal(t, []byte(rows[900][1]), iterator.Value().Value)
		})
	}
}
func TestColumnarPhysicalBlockBytes(t *testing.T) {
	opts := getTestTableOptions()
	opts.Compression = options.None
	builder := NewTableBuilder(opts)
	defer builder.Close()
	for i := 0; i < 64; i++ {
		builder.Add(y.KeyWithTs([]byte(fmt.Sprintf("key/%04d", i)), uint64(i)), y.ValueStruct{Value: bytes.Repeat([]byte{byte(i)}, 100), Meta: byte(i), UserMeta: byte(i + 1), ExpiresAt: uint64(i + 20)}, 0)
	}
	builder.finishBlock()
	for _, block := range builder.blockList {
		raw := block.data[:block.end]
		encoded, e := compressColumnarBlock(raw, 1)
		require.NoError(t, e)
		restored, e := decompressColumnarBlock(encoded)
		require.NoError(t, e)
		require.Equal(t, raw, restored)
		require.LessOrEqual(t, len(encoded), maxEncodedLen(options.NornicColumnar, len(raw)))
		// A new write policy must still read the earlier raw/Zstd/column-Zstd
		// block modes, including checksum and original entry offsets.
		zstd, e := y.ZSTDCompress(nil, raw, 1)
		require.NoError(t, e)
		spans, e := physicalValueSpans(raw)
		require.NoError(t, e)
		frame, e := columnar.Transpose(raw, spans)
		require.NoError(t, e)
		if frame != nil {
			zframe, e := y.ZSTDCompress(nil, frame, 1)
			require.NoError(t, e)
			for _, historical := range [][]byte{append([]byte{0}, raw...), append([]byte{1}, zstd...), append([]byte{2}, zframe...)} {
				got, e := decompressColumnarBlock(historical)
				require.NoError(t, e)
				require.Equal(t, raw, got)
			}
		}
	}
}
