// Copyright 2026 NornicDB contributors. Apache-2.0.
package table

import (
	"bytes"
	"fmt"
	"math"
	"sync"
	"testing"

	"github.com/dgraph-io/badger/v4/experimental/columnar"
	"github.com/dgraph-io/badger/v4/options"
	"github.com/dgraph-io/badger/v4/y"
	"github.com/dgraph-io/ristretto/v2/z"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
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

// columnarTestBlock builds one real SST block through the builder so tests
// decode bytes the writer actually produces.
func columnarTestBlock(t *testing.T) []byte {
	t.Helper()
	opts := getTestTableOptions()
	opts.Compression = options.None
	builder := NewTableBuilder(opts)
	defer builder.Close()
	for i := 0; i < 64; i++ {
		builder.Add(y.KeyWithTs([]byte(fmt.Sprintf("key/%04d", i)), uint64(i)), y.ValueStruct{Value: bytes.Repeat([]byte{byte(i)}, 100)}, 0)
	}
	builder.finishBlock()
	return builder.blockList[0].data[:builder.blockList[0].end]
}

// columnarFrameHeader returns a Zstd frame header without block data. When size
// is nil the frame leaves its content size unknown, which is how a hostile
// block avoids declaring how much it expands to. Sizes below 256 are not
// encodable here because Zstd omits them unless the segment is single-sized.
func columnarFrameHeader(t *testing.T, size *uint64) []byte {
	t.Helper()
	h := zstd.Header{WindowSize: 1 << 20}
	if size != nil {
		require.GreaterOrEqual(t, *size, uint64(256))
		h.FrameContentSize = *size
	}
	out, err := h.AppendTo(nil)
	require.NoError(t, err)
	var decoded zstd.Header
	require.NoError(t, decoded.Decode(out))
	require.Equal(t, size != nil, decoded.HasFCS)
	return out
}

// columnarRawBlockFrame returns a Zstd frame of unknown content size holding one
// last raw block with the given payload, so the decoded size is exactly
// len(payload) without declaring it in the frame header.
func columnarRawBlockFrame(t *testing.T, payload []byte) []byte {
	t.Helper()
	require.NotZero(t, payload)
	require.LessOrEqual(t, len(payload), 128<<10)
	header := columnarFrameHeader(t, nil)
	bh := uint32(len(payload))<<3 | 1 // raw block, last block
	frame := append(header, byte(bh), byte(bh>>8), byte(bh>>16))
	return append(frame, payload...)
}

// columnarRawBlockChainFrame returns a frame of unknown content size that
// expands to exactly total bytes through a chain of maximal raw blocks.
func columnarRawBlockChainFrame(t *testing.T, total int) []byte {
	t.Helper()
	require.Greater(t, total, 0)
	frame := columnarFrameHeader(t, nil)
	for remaining := total; remaining > 0; {
		n := min(remaining, 128<<10)
		bh := uint32(n) << 3 // raw block
		if n == remaining {
			bh |= 1 // last block
		}
		frame = append(frame, byte(bh), byte(bh>>8), byte(bh>>16))
		frame = append(frame, bytes.Repeat([]byte{0x11}, n)...)
		remaining -= n
	}
	return frame
}

// boundedAllocator records the sizes of decoder output buffers and fails if the
// decoder ever asks for more than the pinned limit.
func boundedAllocator(t *testing.T, requests *[]int, limit int) func(int) []byte {
	t.Helper()
	return func(n int) []byte {
		*requests = append(*requests, n)
		require.LessOrEqual(t, n, limit, "decoder reserved beyond the bound")
		return make([]byte, n)
	}
}

// TestColumnarDecodeBoundRejectsOversizedFrames pins that every physical mode
// rejects a frame larger than the bound before reserving that size, including
// frames that declare nothing and frames that declare more than MaxInt bytes.
func TestColumnarDecodeBoundRejectsOversizedFrames(t *testing.T) {
	const limit = 4096
	maxInt := uint64(math.MaxInt64)
	limitPlusOne := uint64(limit + 1)
	framePlusOne := uint64(limit + maxColumnarFrameOverhead + 1)
	for _, tc := range []struct {
		name  string
		block func(t *testing.T) []byte
	}{
		{
			name: "raw mode",
			block: func(t *testing.T) []byte {
				return append([]byte{0}, make([]byte, limit+1)...)
			},
		},
		{
			name: "zstd declared maxint",
			block: func(t *testing.T) []byte {
				return append([]byte{1}, columnarFrameHeader(t, &maxInt)...)
			},
		},
		{
			name: "zstd declared limit+1",
			block: func(t *testing.T) []byte {
				return append([]byte{1}, columnarFrameHeader(t, &limitPlusOne)...)
			},
		},
		{
			name: "zstd unknown size",
			block: func(t *testing.T) []byte {
				return append([]byte{1}, columnarRawBlockFrame(t, make([]byte, limit+1))...)
			},
		},
		{
			name: "transposed declared maxint",
			block: func(t *testing.T) []byte {
				return append([]byte{2}, columnarFrameHeader(t, &maxInt)...)
			},
		},
		{
			name: "transposed declared above frame bound",
			block: func(t *testing.T) []byte {
				return append([]byte{2}, columnarFrameHeader(t, &framePlusOne)...)
			},
		},
		{
			name: "transposed unknown size",
			block: func(t *testing.T) []byte {
				return append([]byte{2}, columnarRawBlockChainFrame(t, limit+maxColumnarFrameOverhead+1)...)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests []int
			decoded, err := decompressColumnarBlockLimited(tc.block(t), boundedAllocator(t, &requests, limit), limit)
			require.ErrorIs(t, err, columnar.ErrFormat)
			require.Nil(t, decoded)
			require.Empty(t, requests, "rejected blocks must not reserve decoded storage")
		})
	}
}

// TestColumnarDecodeBoundAcceptsValidBlocks keeps valid encoded blocks readable
// at the same bound, including frames of exactly the limit and frames without a
// declared content size.
func TestColumnarDecodeBoundAcceptsValidBlocks(t *testing.T) {
	const limit = 4096
	t.Run("raw mode", func(t *testing.T) {
		raw := bytes.Repeat([]byte{0xa5}, limit)
		var requests []int
		got, err := decompressColumnarBlockLimited(append([]byte{0}, raw...), boundedAllocator(t, &requests, limit), limit)
		require.NoError(t, err)
		require.Equal(t, raw, got)
		require.Equal(t, []int{limit}, requests)
	})
	t.Run("zstd declared size", func(t *testing.T) {
		raw := make([]byte, limit)
		for i := range raw {
			raw[i] = byte(i)
		}
		encoded, err := y.ZSTDCompress(nil, raw, 1)
		require.NoError(t, err)
		var header zstd.Header
		require.NoError(t, header.Decode(encoded))
		require.Equal(t, uint64(limit), header.FrameContentSize, "the encoder records the exact size")
		var requests []int
		got, err := decompressColumnarBlockLimited(append([]byte{1}, encoded...), boundedAllocator(t, &requests, limit), limit)
		require.NoError(t, err)
		require.Equal(t, raw, got)
		// The hot path keeps reserving the declared size exactly once.
		require.Equal(t, []int{limit}, requests)
	})
	t.Run("zstd unknown size", func(t *testing.T) {
		raw := bytes.Repeat([]byte("unknown size "), 200)
		var requests []int
		got, err := decompressColumnarBlockLimited(append([]byte{1}, columnarRawBlockFrame(t, raw)...),
			boundedAllocator(t, &requests, limit), limit)
		require.NoError(t, err)
		require.Equal(t, raw, got)
		require.Equal(t, []int{len(raw)}, requests)
	})
	t.Run("transposed frame", func(t *testing.T) {
		raw := columnarTestBlock(t)
		encoded, err := compressColumnarBlock(raw, 1)
		require.NoError(t, err)
		require.Equal(t, byte(2), encoded[0], "the fixture exercises the transposed mode")
		var requests []int
		got, err := decompressColumnarBlockLimited(encoded, boundedAllocator(t, &requests, len(raw)), len(raw))
		require.NoError(t, err)
		require.Equal(t, raw, got)
	})
}

// TestColumnarDecodeBoundLyingFrame pins that a frame declaring a small content
// size is decoded only into that declared size.
func TestColumnarDecodeBoundLyingFrame(t *testing.T) {
	const limit = 4096
	declared := uint64(512)
	payload := bytes.Repeat([]byte{0x5a}, limit)
	header := columnarFrameHeader(t, &declared)
	bh := uint32(len(payload))<<3 | 1
	frame := append(header, byte(bh), byte(bh>>8), byte(bh>>16))
	frame = append(frame, payload...)
	var requests []int
	decoded, err := decompressColumnarBlockLimited(append([]byte{1}, frame...), boundedAllocator(t, &requests, limit), limit)
	require.Error(t, err)
	require.Nil(t, decoded)
	require.Equal(t, []int{int(declared)}, requests, "only the declared size may be reserved")
}

// TestColumnarDecodeBoundRejectsCorruptHeader pins that an unparsable frame is
// a format error instead of an unbounded decode attempt.
func TestColumnarDecodeBoundRejectsCorruptHeader(t *testing.T) {
	var requests []int
	decoded, err := decompressColumnarBlockLimited([]byte{1, 0xff, 0xff, 0xff, 0xff},
		boundedAllocator(t, &requests, maxColumnarDecodedBlock), maxColumnarDecodedBlock)
	require.ErrorIs(t, err, columnar.ErrFormat)
	require.Nil(t, decoded)
	require.Empty(t, requests)
}

// TestColumnarDecodeBoundSharedDecoders exercises the pooled decoders from
// several goroutines so -race covers concurrent block decodes.
func TestColumnarDecodeBoundSharedDecoders(t *testing.T) {
	const limit = 4096
	raw := bytes.Repeat([]byte("concurrent columnar decode "), 100)
	encoded, err := y.ZSTDCompress(nil, raw, 1)
	require.NoError(t, err)
	block := append([]byte{1}, encoded...)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 32; j++ {
				got, err := decompressColumnarBlockLimited(block, func(n int) []byte { return make([]byte, n) }, limit)
				if err != nil {
					errs <- err
					return
				}
				if !bytes.Equal(raw, got) {
					errs <- fmt.Errorf("decoded %d bytes, want %d", len(got), len(raw))
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}

// TestColumnarTableReaderRejectsOversizedFrame checks the real reader path with
// its pooled allocator: a block declaring a huge content size fails before any
// buffer is reserved, and a valid block still round trips through the reader.
func TestColumnarTableReaderRejectsOversizedFrame(t *testing.T) {
	table := &Table{opt: &Options{Compression: options.NornicColumnar, BlockSize: 64 << 10}}
	maxInt := uint64(math.MaxInt64)
	before := z.NumAllocBytes()
	block := &Block{data: append([]byte{1}, columnarFrameHeader(t, &maxInt)...)}
	require.ErrorIs(t, table.decompress(block), columnar.ErrFormat)
	require.False(t, block.freeMe)
	require.Equal(t, before, z.NumAllocBytes(), "a rejected frame must not reserve a buffer")

	raw := columnarTestBlock(t)
	encoded, err := compressColumnarBlock(raw, 1)
	require.NoError(t, err)
	block = &Block{data: encoded}
	require.NoError(t, table.decompress(block))
	require.Equal(t, raw, block.data)
	if block.freeMe {
		z.Free(block.data)
	}
}
