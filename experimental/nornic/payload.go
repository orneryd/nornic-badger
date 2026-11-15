// Copyright 2026 NornicDB contributors. Apache-2.0.
package nornic

import (
	"encoding/binary"
	"hash/crc32"
	"sync"

	"github.com/klauspost/compress/s2"
	"github.com/klauspost/compress/zstd"
)

// MaxPayloadBytes bounds one independently decoded experimental property.
// Larger properties remain in the caller's original encoding.
const MaxPayloadBytes = 64 << 20

const maxPayloadScratchBytes = 128 << 10

type payloadEncoder struct {
	scratch []byte
}

var payloadEncoders = sync.Pool{New: func() any { return new(payloadEncoder) }}
var payloadChecksumTable = crc32.MakeTable(crc32.Castagnoli)
var payloadDecoders = sync.Pool{New: func() any {
	d, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(MaxPayloadBytes), zstd.WithDecodeAllCapLimit(true))
	if err != nil {
		panic(err)
	}
	return d
}}

// EncodePayload returns a versioned compressed property only if its full frame
// saves at least 12.5% plus the caller's extension framing. A nil result means
// keep the exact original bytes. Payloads remain independently addressable.
func EncodePayload(src []byte, framingBytes int) ([]byte, error) {
	if len(src) < 512 || len(src) > MaxPayloadBytes {
		return nil, nil
	}
	e := payloadEncoders.Get().(*payloadEncoder)
	// Property reads are latency-sensitive: use the fast block codec here.
	// Table compression remains independent, and legacy Zstd frames still read.
	out := s2.Encode(e.scratch[:0], src)
	if cap(out) <= maxPayloadScratchBytes {
		e.scratch = out[:0]
	}
	if len(out)+9+framingBytes >= len(src)-len(src)/8 {
		payloadEncoders.Put(e)
		return nil, nil
	}
	// The caller owns accepted bytes; no pooled buffer may escape or alias it.
	owned := make([]byte, 9+len(out))
	owned[0] = 3
	binary.LittleEndian.PutUint32(owned[1:5], uint32(len(src)))
	binary.LittleEndian.PutUint32(owned[5:9], crc32.Checksum(src, payloadChecksumTable))
	copy(owned[9:], out)
	payloadEncoders.Put(e)
	return owned, nil
}

// DecodePayload returns the exact typed bytes supplied to EncodePayload.
// Framing and output-length checks bound allocation before decompression.
func DecodePayload(src []byte) ([]byte, error) {
	return DecodePayloadInto(nil, src)
}

// DecodePayloadInto decodes into dst's capacity, replacing its contents. If
// capacity is insufficient it allocates the declared, bounded output size.
// The returned bytes belong to the caller and may alias dst, never src. dst
// must not overlap src. This lets a typed consumer reuse scratch until it has
// materialized its independently owned result. Errors may modify dst.
func DecodePayloadInto(dst, src []byte) ([]byte, error) {
	if len(src) < 5 || (src[0] != 1 && src[0] != 3) {
		return nil, ErrFormat
	}
	n := uint64(binary.LittleEndian.Uint32(src[1:5]))
	if n > MaxPayloadBytes {
		return nil, ErrFormat
	}
	if src[0] == 3 {
		if len(src) < 9 {
			return nil, ErrFormat
		}
		decoded, err := s2.DecodedLen(src[9:])
		if err != nil || uint64(decoded) != n {
			return nil, ErrFormat
		}
		if cap(dst) < decoded {
			dst = make([]byte, 0, decoded)
		}
		out, err := s2.Decode(dst[:0:decoded], src[9:])
		if err != nil {
			return nil, err
		}
		if crc32.Checksum(out, payloadChecksumTable) != binary.LittleEndian.Uint32(src[5:9]) {
			return nil, ErrFormat
		}
		return out, nil
	}
	if cap(dst) < int(n) {
		dst = make([]byte, 0, int(n))
	}
	d := payloadDecoders.Get().(*zstd.Decoder)
	// Bound the decoder to the declared length even when scratch has spare
	// capacity; malformed frames must retain the same allocation bound.
	out, err := d.DecodeAll(src[5:], dst[:0:int(n)])
	payloadDecoders.Put(d)
	if err != nil {
		return nil, err
	}
	if uint64(len(out)) != n {
		return nil, ErrFormat
	}
	return out, nil
}
