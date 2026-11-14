// Copyright 2026 NornicDB contributors. Licensed under the Apache License, Version 2.0.
package nornic

import (
	"bytes"
	"context"
	"testing"
)

// A read-only segment composes the same directory with column payloads. Row
// position is local to this sample generation, not a public graph identity.
// Nulls, identity maps and live segment publication are later plan milestones.
func TestProjectedColumnSegment(t *testing.T) {
	first, _ := EncodeInt64([]int64{10, 11, 12, 13})
	second, _ := EncodeInt64([]int64{-100, -100, -100, -100})
	ignored := bytes.Repeat([]byte("unselected text"), 4096)
	segment, err := EncodeRecord([]Field{{1, first}, {2, second}, {3, ignored}})
	if err != nil {
		t.Fatal(err)
	}
	reader := &trackedReader{Reader: bytes.NewReader(segment)}
	record, err := OpenRecord(reader, int64(len(segment)))
	if err != nil {
		t.Fatal(err)
	}
	reader.requests = nil
	err = record.Project(context.Background(), []uint64{2}, func(field Field) error {
		col, err := OpenInt64(field.Encoded)
		if err != nil {
			return err
		}
		for row := 0; row < col.Len(); row++ {
			value, e := col.At(row)
			if e != nil {
				return e
			}
			if value != -100 {
				t.Fatal(value)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.requests) != 1 || reader.requests[0].size != len(second) {
		t.Fatalf("read extra columns: %+v", reader.requests)
	}
}

func FuzzDecodeFormats(f *testing.F) {
	record, _ := EncodeRecord([]Field{{1, []byte("value")}})
	column, _ := EncodeInt64([]int64{-2, 4, 9})
	f.Add(record)
	f.Add(column)
	f.Add([]byte("bad format"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		if r, e := OpenRecord(bytes.NewReader(data), int64(len(data))); e == nil {
			if e = r.Project(context.Background(), []uint64{0, 1, 2, 255}, func(Field) error { return nil }); e != nil {
				t.Fatal(e)
			}
		}
		if c, e := OpenInt64(data); e == nil {
			for i := 0; i < c.Len(); i++ {
				if _, e = c.At(i); e != nil {
					t.Fatal(e)
				}
			}
		}
	})
}
