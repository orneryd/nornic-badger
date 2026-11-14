// Copyright 2026 NornicDB contributors. Apache-2.0.
package table

import (
	"fmt"

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
func decompressColumnarBlock(data []byte) ([]byte, error) {
	return decompressColumnarBlockWithAllocator(data, func(n int) []byte { return make([]byte, n) })
}

func decompressColumnarBlockWithAllocator(data []byte, allocate func(int) []byte) ([]byte, error) {
	if len(data) < 1 {
		return nil, columnar.ErrFormat
	}
	switch data[0] {
	case 0:
		out := allocate(len(data) - 1)
		copy(out, data[1:])
		return out, nil
	case 1:
		// The normal encoder records content size. Unknown-size frames retain
		// Zstd's normal growth behavior rather than changing accepted frames.
		var header zstd.Header
		size := len(data) * 4
		if err := header.Decode(data[1:]); err == nil && header.HasFCS && header.FrameContentSize <= uint64(^uint(0)>>1) {
			size = int(header.FrameContentSize)
		}
		return y.ZSTDDecompress(allocate(size), data[1:])
	case 2:
		frame, err := y.ZSTDDecompress(nil, data[1:])
		if err != nil {
			return nil, err
		}
		return columnar.RestoreWithAllocator(frame, allocate)
	default:
		return nil, fmt.Errorf("%w: unknown physical codec %d", columnar.ErrFormat, data[0])
	}
}
