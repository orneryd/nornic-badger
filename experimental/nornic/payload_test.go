// Copyright 2026 NornicDB contributors. Apache-2.0.
package nornic

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestPayloadRoundtripAndRawFallback(t *testing.T) {
	for _, src := range [][]byte{nil, []byte("small"), bytes.Repeat([]byte("documentation/property/path/"), 4096)} {
		e, err := EncodePayload(src, 6)
		if err != nil {
			t.Fatal(err)
		}
		if len(src) < 512 {
			if e != nil {
				t.Fatal("small property framed")
			}
			continue
		}
		if e == nil {
			t.Fatal("expected saving")
		}
		got, err := DecodePayload(e)
		if err != nil || !bytes.Equal(got, src) {
			t.Fatal(err)
		}
	}
	random := docsRaw(footprintValues("random", 4096))
	if e, err := EncodePayload(random, 6); err != nil || e != nil {
		t.Fatal("random expanded", err)
	}
}

func TestPayloadFastCodecAndLegacyZstd(t *testing.T) {
	source := bytes.Repeat([]byte("documentation/property/path/"), 4096)
	fast, err := EncodePayload(source, 6)
	if err != nil || len(fast) < 9 || fast[0] != 3 {
		t.Fatalf("expected fast checked frame: %v", err)
	}
	bad := bytes.Clone(fast)
	bad[5] ^= 1 // checksum corruption must be reported even if the stream decodes
	if _, err := DecodePayload(bad); err == nil {
		t.Fatal("accepted checksum corruption")
	}
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	legacy := make([]byte, 5)
	legacy[0] = 1
	binary.LittleEndian.PutUint32(legacy[1:5], uint32(len(source)))
	legacy = encoder.EncodeAll(source, legacy)
	for _, encoded := range [][]byte{legacy, fast} {
		got, err := DecodePayload(encoded)
		if err != nil || !bytes.Equal(got, source) {
			t.Fatal("compatible decode", err)
		}
	}
}
func TestPayloadRejectsCorruptionAndDeclaredSize(t *testing.T) {
	e, _ := EncodePayload(bytes.Repeat([]byte("abc"), 4096), 6)
	for _, bad := range [][]byte{nil, {2, 0, 0, 0, 0}, e[:len(e)-1]} {
		if _, err := DecodePayload(bad); err == nil {
			t.Fatal("accepted invalid payload")
		}
	}
	bad := bytes.Clone(e)
	binary.LittleEndian.PutUint32(bad[1:5], MaxPayloadBytes+1)
	if _, err := DecodePayload(bad); err == nil {
		t.Fatal("oversized frame")
	}
	bad = bytes.Clone(e)
	binary.LittleEndian.PutUint32(bad[1:5], 1)
	if _, err := DecodePayload(bad); err == nil {
		t.Fatal("length mismatch")
	}
}

func TestPayloadScratchOwnershipAndBound(t *testing.T) {
	source := bytes.Repeat([]byte("documentation/property/"), 4096)
	first, err := EncodePayload(source, 6)
	if err != nil || first == nil {
		t.Fatal("expected compressed payload", err)
	}
	saved := bytes.Clone(first)
	for i := 0; i < 32; i++ {
		if _, err = EncodePayload(bytes.Repeat([]byte{byte(i)}, 2048+i), 6); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = EncodePayload(docsRaw(footprintValues("random", 65536)), 6); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, saved) {
		t.Fatal("pooled scratch escaped into caller-owned payload")
	}
	decoded, err := DecodePayload(first)
	if err != nil || !bytes.Equal(source, decoded) {
		t.Fatal("payload changed after scratch reuse", err)
	}
	state := payloadEncoders.Get().(*payloadEncoder)
	capacity := cap(state.scratch)
	payloadEncoders.Put(state)
	if capacity > maxPayloadScratchBytes {
		t.Fatalf("retained scratch capacity %d exceeds bound", capacity)
	}
}

func TestDecodePayloadIntoReusesDestination(t *testing.T) {
	source := bytes.Repeat([]byte("documentation/property/"), 4096)
	encoded, err := EncodePayload(source, 6)
	if err != nil {
		t.Fatal(err)
	}
	dst := make([]byte, 17, len(source))
	got, err := DecodePayloadInto(dst, encoded)
	if err != nil || !bytes.Equal(got, source) {
		t.Fatal("decode", err)
	}
	if &got[0] != &dst[:cap(dst)][0] {
		t.Fatal("destination was not reused")
	}
	owned, err := DecodePayload(encoded)
	if err != nil {
		t.Fatal(err)
	}
	clear(got)
	if !bytes.Equal(owned, source) {
		t.Fatal("owned decode aliases caller scratch")
	}
	for _, size := range []int{0, 1, len(source) - 1, len(source) + 1} {
		bad := bytes.Clone(encoded)
		binary.LittleEndian.PutUint32(bad[1:5], uint32(size))
		if _, err := DecodePayloadInto(dst, bad); err == nil {
			t.Fatal("accepted wrong output size", size)
		}
	}
	if _, err := DecodePayloadInto(dst, encoded[:len(encoded)-1]); err == nil {
		t.Fatal("accepted truncated frame")
	}
	got, err = DecodePayloadInto(nil, encoded)
	if err != nil || !bytes.Equal(got, source) {
		t.Fatal("grow destination", err)
	}
}
