// Copyright 2026 NornicDB contributors. Apache-2.0.
package table

import (
	"bytes"
	"fmt"
	"github.com/dgraph-io/badger/v4/options"
	"github.com/dgraph-io/badger/v4/y"
	"github.com/stretchr/testify/require"
	"testing"
)

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
	}
}
