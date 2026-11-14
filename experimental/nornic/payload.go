// Copyright 2026 NornicDB contributors. Apache-2.0.
package nornic

import (
	"encoding/binary"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// MaxPayloadBytes bounds one independently decoded experimental property.
// Larger properties remain in the caller's original encoding.
const MaxPayloadBytes = 64 << 20

var payloadEncoders = sync.Pool{New: func() any {
	e, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1), zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		panic(err)
	}
	return e
}}
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
	e := payloadEncoders.Get().(*zstd.Encoder)
	out := e.EncodeAll(src, make([]byte, 5, 5+len(src)))
	payloadEncoders.Put(e)
	if len(out)+framingBytes >= len(src)-len(src)/8 {
		return nil, nil
	}
	out[0] = 1
	binary.LittleEndian.PutUint32(out[1:5], uint32(len(src)))
	return out, nil
}

// DecodePayload returns the exact typed bytes supplied to EncodePayload.
// Framing and output-length checks bound allocation before decompression.
func DecodePayload(src []byte) ([]byte, error) {
	if len(src) < 5 || src[0] != 1 {
		return nil, ErrFormat
	}
	n := uint64(binary.LittleEndian.Uint32(src[1:5]))
	if n > MaxPayloadBytes {
		return nil, ErrFormat
	}
	d := payloadDecoders.Get().(*zstd.Decoder)
	out, err := d.DecodeAll(src[5:], make([]byte, 0, int(n)))
	payloadDecoders.Put(d)
	if err != nil {
		return nil, err
	}
	if uint64(len(out)) != n {
		return nil, ErrFormat
	}
	return out, nil
}
