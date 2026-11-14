// Copyright 2026 NornicDB contributors. Apache-2.0.
package badger

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/dgraph-io/badger/v4/experimental/columnar"
	"github.com/dgraph-io/badger/v4/options"
	"github.com/stretchr/testify/require"
)

func TestColumnarDatabaseCompatibility(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		t.Run(fmt.Sprint(encrypted), func(t *testing.T) {
			var selected atomic.Bool
			selected.Store(true)
			dir := t.TempDir()
			opts := DefaultOptions(dir).WithLogger(nil).WithCompression(options.NornicColumnar).WithMemTableSize(1 << 20).WithBaseTableSize(1 << 20).WithBaseLevelSize(4 << 20).WithValueThreshold(32 << 10).WithBlockCacheSize(8 << 20).WithIndexCacheSize(4 << 20).WithNumMemtables(2).WithNumCompactors(2).WithNumVersionsToKeep(3).WithChecksumVerificationMode(options.OnTableAndBlockRead)
			opts = opts.WithColumnarValueLayout(func(key, value []byte) []columnar.Span {
				if len(value) < 24 {
					return nil
				}
				return []columnar.Span{{Start: 0, Length: 8, Kind: 1, Column: 1}, {Start: 8, Length: 8, Kind: 1, Column: 2}, {Start: 16, Length: len(value) - 16, Kind: 1, Column: 3}}
			})
			opts = opts.WithCompressionSelector(func() options.CompressionType {
				if selected.Load() {
					return options.NornicColumnar
				}
				return options.Snappy
			})
			if encrypted {
				opts = opts.WithEncryptionKey(bytes.Repeat([]byte{7}, 32))
			}
			db, e := Open(opts)
			require.NoError(t, e)
			makeValue := func(i int) []byte {
				v := make([]byte, 24)
				binary.BigEndian.PutUint64(v, uint64(i))
				binary.BigEndian.PutUint64(v[8:], uint64(i%64))
				copy(v[16:], "adjacent")
				return v
			}
			batch := db.NewWriteBatch()
			for i := 0; i < 12000; i++ {
				entry := NewEntry([]byte(fmt.Sprintf("node/%06d", i)), makeValue(i)).WithMeta(77)
				entry.ExpiresAt = 4102444800
				require.NoError(t, batch.SetEntry(entry))
			}
			require.NoError(t, batch.Flush())
			old := db.NewTransaction(false)
			require.NoError(t, db.Update(func(tx *Txn) error { return tx.Set([]byte("node/000017"), []byte("updated")) }))
			require.NoError(t, db.Flatten(1))
			item, e := old.Get([]byte("node/000017"))
			require.NoError(t, e)
			value, e := item.ValueCopy(nil)
			require.NoError(t, e)
			require.Equal(t, makeValue(17), value)
			old.Discard()
			require.NoError(t, db.Close())
			foundColumnar := false
			for _, entry := range db.manifest.manifest.Tables {
				foundColumnar = foundColumnar || entry.Compression == options.NornicColumnar
			}
			require.True(t, foundColumnar, "the dynamic selector writes columnar tables")
			selected.Store(false)
			// Opening with ordinary compression still reads persisted experimental tables.
			db, e = Open(opts.WithCompression(options.Snappy).WithColumnarValueLayout(nil))
			require.NoError(t, e)
			require.NoError(t, db.View(func(tx *Txn) error {
				for i := 0; i < 12000; i++ {
					item, e := tx.Get([]byte(fmt.Sprintf("node/%06d", i)))
					if e != nil {
						return e
					}
					v, e := item.ValueCopy(nil)
					if e != nil {
						return e
					}
					expected := makeValue(i)
					if i == 17 {
						expected = []byte("updated")
					} else {
						require.Equal(t, byte(77), item.UserMeta())
						require.Equal(t, uint64(4102444800), item.ExpiresAt())
					}
					require.Equal(t, expected, v)
				}
				return nil
			}))
			var backup bytes.Buffer
			_, e = db.Backup(&backup, 0)
			require.NoError(t, e)
			require.NoError(t, db.Close())
			target, e := Open(DefaultOptions(t.TempDir()).WithLogger(nil))
			require.NoError(t, e)
			defer target.Close()
			require.NoError(t, target.Load(&backup, 16))
			require.NoError(t, target.View(func(tx *Txn) error {
				iterator := tx.NewIterator(DefaultIteratorOptions)
				defer iterator.Close()
				count := 0
				for iterator.Rewind(); iterator.Valid(); iterator.Next() {
					count++
				}
				require.Equal(t, 12000, count)
				return nil
			}))
		})
	}
}
