// Copyright 2026 NornicDB contributors. Apache-2.0.
package table

import (
	"errors"
	"fmt"
	"sync"

	"github.com/dgraph-io/badger/v4/experimental/columnar"
	"github.com/dgraph-io/badger/v4/y"
	"github.com/klauspost/compress/zstd"
)

// physicalValueSpans uses the builder's existing entry and footer formats.
// Values include their original meta/TTL bytes; keys and offsets stay opaque.
func physicalValueSpans(data []byte) ([]columnar.Span, error) {
	if len(data) < 12 {
		return nil, columnar.ErrFormat
	}
	checksum := int(y.BytesToU32(data[len(data)-4:]))
	end := len(data) - 4 - checksum - 4
	if end < 0 {
		return nil, columnar.ErrFormat
	}
	count := uint64(y.BytesToU32(data[end : end+4]))
	if count > uint64(end/4) {
		return nil, columnar.ErrFormat
	}
	index := end - int(count)*4
	if count < 4 || count > 65536 {
		return nil, nil
	}
	offsets := y.BytesToU32Slice(data[index:end])
	spans := make([]columnar.Span, len(offsets))
	previous := 0
	for i, offset := range offsets {
		start := int(offset)
		stop := index
		if i+1 < len(offsets) {
			stop = int(offsets[i+1])
		}
		if start < previous || start > stop || stop > index || stop-start < int(headerSize) {
			return nil, columnar.ErrFormat
		}
		var h header
		h.Decode(data[start:])
		value := start + int(headerSize) + int(h.diff)
		if value > stop {
			return nil, columnar.ErrFormat
		}
		spans[i] = columnar.Span{Start: value, Length: stop - value}
		previous = stop
	}
	return spans, nil
}

// compressColumnarBlock selects raw, Zstd, or byte-column+Zstd using complete
// encoded sizes. This is a physical multi-record layout, not property schemas.
func compressColumnarBlock(data []byte, level int, annotated ...[]columnar.Span) ([]byte, error) {
	mode := byte(0)
	chosen := data
	zstd, err := y.ZSTDCompress(nil, data, level)
	if err != nil {
		return nil, err
	}
	if len(zstd) < len(chosen) {
		mode, chosen = 1, zstd
	}
	spans, err := physicalValueSpans(data)
	if err != nil {
		return nil, err
	}
	candidates := [][]columnar.Span{spans}
	if len(annotated) > 0 && len(annotated[0]) >= 4 {
		candidates = append(candidates, annotated[0])
	}
	for _, candidate := range candidates {
		frame, err := columnar.Transpose(data, candidate)
		if err != nil {
			return nil, err
		}
		if frame == nil {
			continue
		}
		encoded, err := y.ZSTDCompress(nil, frame, level)
		if err != nil {
			return nil, err
		}
		if len(encoded) < len(chosen) {
			mode, chosen = 2, encoded
		}
	}
	return append([]byte{mode}, chosen...), nil
}

// Experimental columnar blocks are decoded from SST bytes, so every mode
// decodes inside an explicit ceiling instead of following a size taken from the
// block itself. Mode 1 previously reserved whatever a Zstd frame declared, and
// mode 2 decompressed without any bound at all before restoring a frame.
const (
	// maxColumnarDecodedBlock bounds one decoded block. The builder never writes
	// a larger block and experimental/columnar refuses to restore a larger
	// frame, so the ceiling only rejects corrupt or hostile input.
	maxColumnarDecodedBlock = columnar.MaxBlockBytes
	// maxColumnarFrameOverhead is the largest span index a transposed frame can
	// add to its source: 12 header bytes plus at most 21 bytes per span.
	maxColumnarFrameOverhead = 12 + 65536*21
	// maxColumnarDecodedFrame bounds the transposed frame of a mode 2 block.
	maxColumnarDecodedFrame = columnar.MaxBlockBytes + maxColumnarFrameOverhead
)

// columnarDecoders keeps bounded Zstd decoders off the per-block path.
// WithDecodeAllCapLimit binds each decode to the capacity of the buffer we
// reserve and WithDecoderMaxMemory rejects larger frames before expanding them.
var columnarDecoders = sync.Pool{New: func() any {
	d, err := zstd.NewReader(nil,
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderMaxMemory(maxColumnarDecodedFrame),
		zstd.WithDecodeAllCapLimit(true),
	)
	if err != nil {
		panic(err)
	}
	return d
}}

func decompressColumnarBlock(data []byte) ([]byte, error) {
	return decompressColumnarBlockWithAllocator(data, func(n int) []byte { return make([]byte, n) })
}

func decompressColumnarBlockWithAllocator(data []byte, allocate func(int) []byte) ([]byte, error) {
	return decompressColumnarBlockLimited(data, allocate, maxColumnarDecodedBlock)
}

// decompressColumnarBlockLimited decodes one block without ever reserving more
// than limit decoded bytes. Callers own the result and may pass a pooled
// allocator that therefore never sees an oversized request.
func decompressColumnarBlockLimited(data []byte, allocate func(int) []byte, limit int) ([]byte, error) {
	if len(data) < 1 {
		return nil, columnar.ErrFormat
	}
	switch data[0] {
	case 0:
		if len(data)-1 > limit {
			return nil, fmt.Errorf("%w: raw block of %d bytes exceeds limit %d",
				columnar.ErrFormat, len(data)-1, limit)
		}
		out := allocate(len(data) - 1)
		copy(out, data[1:])
		return out, nil
	case 1:
		return decodeColumnarZstd(data[1:], limit, allocate)
	case 2:
		frame, err := decodeColumnarZstd(data[1:], limit+maxColumnarFrameOverhead,
			func(n int) []byte { return make([]byte, n) })
		if err != nil {
			return nil, err
		}
		return columnar.RestoreWithAllocator(frame, allocate)
	default:
		return nil, fmt.Errorf("%w: unknown physical codec %d", columnar.ErrFormat, data[0])
	}
}

// decodeColumnarZstd decodes one complete Zstd frame into at most limit bytes,
// returning storage from reserve. reserve returns a zeroed buffer of length
// size and is only called once a size within the limit is known or proven.
func decodeColumnarZstd(src []byte, limit int, reserve func(int) []byte) ([]byte, error) {
	var header zstd.Header
	if err := header.Decode(src); err != nil {
		return nil, fmt.Errorf("%w: invalid Zstd frame: %v", columnar.ErrFormat, err)
	}
	if !header.HasFCS {
		// Unknown content size: grow bounded probes instead of letting the
		// decoder allocate as much as the frame asks for. Valid frames still
		// decode, and oversized ones are rejected at the limit.
		decoded, err := decodeColumnarZstdUnknown(src, limit)
		if err != nil {
			return nil, err
		}
		out := reserve(len(decoded))
		copy(out, decoded)
		return out[:len(decoded)], nil
	}
	if header.FrameContentSize > uint64(limit) {
		return nil, fmt.Errorf("%w: Zstd frame declares %d bytes, limit is %d",
			columnar.ErrFormat, header.FrameContentSize, limit)
	}
	// The normal encoder records content size, so valid blocks reserve exactly
	// once and the decoder cannot expand past that buffer.
	return decodeColumnarZstdInto(src, reserve, int(header.FrameContentSize))
}

// decodeColumnarZstdUnknown expands a frame of unknown size in bounded probes
// and rejects it as soon as a probe proves it exceeds limit.
func decodeColumnarZstdUnknown(src []byte, limit int) ([]byte, error) {
	size := min(limit, max(4*len(src), 64<<10))
	for {
		out, err := decodeColumnarZstdInto(src, func(n int) []byte { return make([]byte, n) }, size)
		if err == nil {
			return out, nil
		}
		if !errors.Is(err, zstd.ErrDecoderSizeExceeded) {
			return nil, err
		}
		if size >= limit {
			return nil, fmt.Errorf("%w: Zstd frame exceeds limit %d", columnar.ErrFormat, limit)
		}
		size = min(4*size, limit)
	}
}

// decodeColumnarZstdInto decodes src into a buffer of exactly size bytes. The
// buffer capacity bounds the decoder output, so a frame cannot grow past it.
func decodeColumnarZstdInto(src []byte, reserve func(int) []byte, size int) ([]byte, error) {
	dst := reserve(size)
	if cap(dst) < size || len(dst) < size {
		return nil, fmt.Errorf("%w: decoder buffer of %d bytes, need %d",
			columnar.ErrFormat, cap(dst), size)
	}
	d := columnarDecoders.Get().(*zstd.Decoder)
	out, err := d.DecodeAll(src, dst[:0:size])
	columnarDecoders.Put(d)
	if err != nil {
		// A decoder error may return partially decoded bytes; callers must not
		// mistake them for a block.
		return nil, err
	}
	return out, nil
}
