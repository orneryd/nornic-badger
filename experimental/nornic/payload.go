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

const maxPayloadScratchBytes = 128 << 10

type payloadEncoder struct {
	encoder *zstd.Encoder
	scratch []byte
}

var payloadEncoders = sync.Pool{New: func() any {
	// Each property is independent. A bounded window avoids retaining an 8 MiB
	// history per pooled encoder for predominantly small documentation values.
	e, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1), zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithWindowSize(128<<10))
	if err != nil {
		panic(err)
	}
	return &payloadEncoder{encoder: e}
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
	e := payloadEncoders.Get().(*payloadEncoder)
	if cap(e.scratch) < 5 {
		e.scratch = make([]byte, 5, 4096)
	}
	out := e.encoder.EncodeAll(src, e.scratch[:5])
	if cap(out) <= maxPayloadScratchBytes {
		e.scratch = out[:0]
	}
	if len(out)+framingBytes >= len(src)-len(src)/8 {
		payloadEncoders.Put(e)
		return nil, nil
	}
	out[0] = 1
	binary.LittleEndian.PutUint32(out[1:5], uint32(len(src)))
	// The caller owns accepted bytes; no pooled buffer may escape or alias it.
	owned := append([]byte(nil), out...)
	payloadEncoders.Put(e)
	return owned, nil
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
