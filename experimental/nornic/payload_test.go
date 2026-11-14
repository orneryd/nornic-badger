// Copyright 2026 NornicDB contributors. Apache-2.0.
package nornic

import (
	"bytes"
	"encoding/binary"
	"testing"
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
