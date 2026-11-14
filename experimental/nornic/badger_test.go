// Copyright 2026 NornicDB contributors. Licensed under the Apache License, Version 2.0.
package nornic

import (
	"bytes"
	"context"
	"testing"

	badger "github.com/dgraph-io/badger/v4"
)

func TestBadgerProjectionSnapshotAndReopen(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		name := "plaintext"
		if encrypted {
			name = "encrypted"
		}
		t.Run(name, func(t *testing.T) {
			for _, threshold := range []int64{1, 1 << 20} {
				t.Run(func() string {
					if threshold == 1 {
						return "value-log"
					}
					return "inline"
				}(), func(t *testing.T) {
					dir := t.TempDir()
					options := badger.DefaultOptions(dir).WithLogger(nil).WithValueThreshold(threshold)
					if encrypted {
						options = options.WithEncryptionKey(bytes.Repeat([]byte{0x53}, 32)).WithIndexCacheSize(8 << 20)
					}
					db, err := badger.Open(options)
					if err != nil {
						t.Fatal(err)
					}
					defer func() {
						if db != nil {
							_ = db.Close()
						}
					}()
					key := []byte("experimental-record")
					put := func(title string) {
						t.Helper()
						encoded, e := EncodeRecord([]Field{{1, []byte(title)}, {2, bytes.Repeat([]byte("body"), 4096)}})
						if e != nil {
							t.Fatal(e)
						}
						if e = db.Update(func(tx *badger.Txn) error { return tx.Set(key, encoded) }); e != nil {
							t.Fatal(e)
						}
					}
					read := func(tx *badger.Txn, want string) {
						t.Helper()
						item, e := tx.Get(key)
						if e != nil {
							t.Fatal(e)
						}
						calls := 0
						e = ProjectItem(context.Background(), item, []uint64{1}, func(f Field) error {
							calls++
							if string(f.Encoded) != want {
								t.Fatalf("got %q want %q", f.Encoded, want)
							}
							return nil
						})
						if e != nil || calls != 1 {
							t.Fatal(e, calls)
						}
					}
					pending, err := EncodeRecord([]Field{{1, []byte("pending")}})
					if err != nil {
						t.Fatal(err)
					}
					if err = db.Update(func(tx *badger.Txn) error {
						if e := tx.Set(key, pending); e != nil {
							return e
						}
						read(tx, "pending")
						return nil
					}); err != nil {
						t.Fatal(err)
					}
					put("before")
					snapshot := db.NewTransaction(false)
					put("after")
					read(snapshot, "before")
					snapshot.Discard()
					if err = db.View(func(tx *badger.Txn) error { read(tx, "after"); return nil }); err != nil {
						t.Fatal(err)
					}
					column, _ := EncodeInt64([]int64{-20, -19, -18})
					if err = db.Update(func(tx *badger.Txn) error { return tx.Set([]byte("column"), column) }); err != nil {
						t.Fatal(err)
					}
					if err = db.Close(); err != nil {
						t.Fatal(err)
					}
					db = nil
					db, err = badger.Open(options)
					if err != nil {
						t.Fatal(err)
					}
					if err = db.View(func(tx *badger.Txn) error {
						read(tx, "after")
						item, e := tx.Get([]byte("column"))
						if e != nil {
							return e
						}
						return item.Value(func(data []byte) error {
							c, e := OpenInt64(data)
							if e != nil {
								return e
							}
							v, e := c.At(2)
							if e != nil {
								return e
							}
							if v != -18 {
								t.Fatal(v)
							}
							return nil
						})
					}); err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}
