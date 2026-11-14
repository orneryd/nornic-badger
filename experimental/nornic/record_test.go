// Copyright 2026 NornicDB contributors. Licensed under the Apache License, Version 2.0.
package nornic

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

type readRequest struct {
	offset int64
	size   int
}
type trackedReader struct {
	*bytes.Reader
	requests []readRequest
}

func (r *trackedReader) ReadAt(p []byte, off int64) (int, error) {
	r.requests = append(r.requests, readRequest{off, len(p)})
	return r.Reader.ReadAt(p, off)
}

func TestRecordProjectsOnlyRequestedPayloads(t *testing.T) {
	large := bytes.Repeat([]byte("x"), 1<<20)
	fields := []Field{{9, []byte("title")}, {2, large}, {7, []byte{}}}
	encoded, err := EncodeRecord(fields)
	if err != nil {
		t.Fatal(err)
	}
	if fields[0].Token != 9 {
		t.Fatal("input order mutated")
	}
	r := &trackedReader{Reader: bytes.NewReader(encoded)}
	record, err := OpenRecord(r, int64(len(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.requests) != 2 || r.requests[0].size != recordHeader || r.requests[1].size != 3*directoryEntry {
		t.Fatalf("directory reads: %+v", r.requests)
	}
	r.requests = nil
	var tokens []uint64
	var payloads [][]byte
	err = record.Project(context.Background(), []uint64{9, 42, 7, 9}, func(f Field) error {
		tokens = append(tokens, f.Token)
		payloads = append(payloads, bytes.Clone(f.Encoded))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 3 || tokens[0] != 9 || tokens[1] != 7 || tokens[2] != 9 || string(payloads[0]) != "title" || len(payloads[1]) != 0 {
		t.Fatalf("projection: %v %q", tokens, payloads)
	}
	if len(r.requests) != 2 {
		t.Fatalf("payload requests: %+v", r.requests)
	}
	for _, read := range r.requests {
		if read.size != 5 || read.offset < int64(recordHeader+3*directoryEntry+len(large)) {
			t.Fatalf("unrequested payload read: %+v", read)
		}
	}
}

func TestRecordRejectsMalformedDirectoryAndDuplicateTokens(t *testing.T) {
	if _, err := EncodeRecord([]Field{{1, nil}, {1, nil}}); !errors.Is(err, ErrFormat) {
		t.Fatal(err)
	}
	original, _ := EncodeRecord([]Field{{1, []byte("a")}, {2, []byte("b")}})
	for _, mutate := range []func([]byte){
		func(b []byte) { b[0] = 'X' }, func(b []byte) { b[4] = 1 }, func(b []byte) { binary.LittleEndian.PutUint32(b[8:], ^uint32(0)) },
		func(b []byte) { binary.LittleEndian.PutUint64(b[recordHeader+directoryEntry:], 1) },
		func(b []byte) { binary.LittleEndian.PutUint64(b[recordHeader+8:], 0) },
		func(b []byte) { binary.LittleEndian.PutUint64(b[recordHeader+16:], ^uint64(0)) },
	} {
		b := bytes.Clone(original)
		mutate(b)
		if _, err := OpenRecord(bytes.NewReader(b), int64(len(b))); !errors.Is(err, ErrFormat) {
			t.Fatalf("accepted malformed directory: %v", err)
		}
	}
	if _, err := OpenRecord(bytes.NewReader(original), int64(len(original)+1)); !errors.Is(err, ErrFormat) {
		t.Fatal(err)
	}
	if _, err := OpenRecord(bytes.NewReader(original[:10]), int64(len(original))); err == nil {
		t.Fatal("accepted truncated header")
	}
}

func TestRecordCancellationAndConsumerErrors(t *testing.T) {
	encoded, _ := EncodeRecord([]Field{{1, []byte("a")}, {2, []byte("b")}})
	record, _ := OpenRecord(bytes.NewReader(encoded), int64(len(encoded)))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := record.Project(ctx, []uint64{1}, func(Field) error { t.Fatal("callback after cancellation"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	sentinel := errors.New("consumer stopped")
	calls := 0
	if err := record.Project(context.Background(), []uint64{1, 2}, func(Field) error { calls++; return sentinel }); !errors.Is(err, sentinel) || calls != 1 {
		t.Fatal(err, calls)
	}
	// The directory was readable, but the later selected payload is truncated.
	short, _ := OpenRecord(bytes.NewReader(encoded), int64(len(encoded)))
	short.reader = bytes.NewReader(encoded[:len(encoded)-1])
	if err := short.Project(context.Background(), []uint64{2}, func(Field) error { return nil }); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	empty, _ := EncodeRecord(nil)
	if _, err := OpenRecord(bytes.NewReader(empty), int64(len(empty))); err != nil {
		t.Fatal(err)
	}
}

func FuzzRecordProjection(f *testing.F) {
	f.Add([]byte("title"), []byte("body"))
	f.Fuzz(func(t *testing.T, a, b []byte) {
		if len(a)+len(b) > 1<<20 {
			t.Skip()
		}
		encoded, err := EncodeRecord([]Field{{2, a}, {8, b}})
		if err != nil {
			t.Fatal(err)
		}
		record, err := OpenRecord(bytes.NewReader(encoded), int64(len(encoded)))
		if err != nil {
			t.Fatal(err)
		}
		err = record.Project(context.Background(), []uint64{8, 2}, func(v Field) error {
			want := a
			if v.Token == 8 {
				want = b
			}
			if !bytes.Equal(v.Encoded, want) {
				t.Fatal("payload mismatch")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func BenchmarkRecordProjection(b *testing.B) {
	fields := make([]Field, 100)
	for i := range fields {
		fields[i] = Field{uint64(i), bytes.Repeat([]byte("x"), 1024)}
	}
	encoded, _ := EncodeRecord(fields)
	b.Run("open-and-project-two-of-100", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			r, err := OpenRecord(bytes.NewReader(encoded), int64(len(encoded)))
			if err != nil {
				b.Fatal(err)
			}
			if err = r.Project(context.Background(), []uint64{1, 99}, func(Field) error { return nil }); err != nil {
				b.Fatal(err)
			}
		}
	})
	record, _ := OpenRecord(bytes.NewReader(encoded), int64(len(encoded)))
	for _, test := range []struct {
		name   string
		tokens []uint64
	}{{"project-two-of-100", []uint64{1, 99}}, {"project-all-100", func() []uint64 {
		v := make([]uint64, 100)
		for i := range v {
			v[i] = uint64(i)
		}
		return v
	}()}} {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := record.Project(context.Background(), test.tokens, func(Field) error { return nil }); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
