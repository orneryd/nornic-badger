package badger

import (
	"bytes"
	"expvar"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValueRangeLargeVlogPlaintextAndEncrypted(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		for _, verifyChecksum := range []bool{false, true} {
			name := "plaintext"
			if encrypted {
				name = "encrypted"
			}
			if verifyChecksum {
				name += "_whole_checksum"
			}
			t.Run(name, func(t *testing.T) {
				dir := t.TempDir()
				options := DefaultOptions(dir).WithLogger(nil).WithValueThreshold(1 << 10).WithIndexCacheSize(8 << 20)
				options.VerifyValueChecksum = verifyChecksum
				if encrypted {
					options = options.WithEncryptionKey(bytes.Repeat([]byte{7}, 32))
				}
				open := func() *DB {
					db, err := Open(options)
					require.NoError(t, err)
					return db
				}
				value := make([]byte, 2<<20)
				for i := range value {
					value[i] = byte(i*131 + i/257)
				}
				db := open()
				require.NoError(t, db.Update(func(txn *Txn) error { return txn.Set([]byte("node"), value) }))
				require.NoError(t, db.Close())
				db = open()
				defer db.Close()
				metric, ok := expvar.Get("badger_read_bytes_vlog").(*expvar.Int)
				require.True(t, ok)
				before := metric.Value()
				require.NoError(t, db.View(func(txn *Txn) error {
					item, err := txn.Get([]byte("node"))
					if err != nil {
						return err
					}
					return item.ValueRange(12345, 31, func(got []byte) error {
						require.Equal(t, value[12345:12376], got)
						return nil
					})
				}))
				if verifyChecksum {
					require.Greater(t, metric.Value()-before, int64(2<<20))
				} else {
					require.Less(t, metric.Value()-before, int64(1<<10))
				}
				require.NoError(t, db.View(func(txn *Txn) error {
					item, err := txn.Get([]byte("node"))
					if err != nil {
						return err
					}
					return item.ValueRange(uint32(len(value)-7), 7, func(got []byte) error {
						require.Equal(t, value[len(value)-7:], got)
						return nil
					})
				}))
				require.NoError(t, db.View(func(txn *Txn) error {
					item, err := txn.Get([]byte("node"))
					if err != nil {
						return err
					}
					require.Error(t, item.ValueRange(uint32(len(value)-1), 2, nil))
					return nil
				}))
			})
		}
	}
}

func TestValueRangeFollowsRelocatedValueLogRecord(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		name := "plaintext"
		if encrypted {
			name = "encrypted"
		}
		t.Run(name, func(t *testing.T) {
			options := DefaultOptions(t.TempDir()).WithLogger(nil).WithValueThreshold(1 << 10).
				WithValueLogFileSize(1 << 20).WithIndexCacheSize(8 << 20)
			if encrypted {
				options = options.WithEncryptionKey(bytes.Repeat([]byte{7}, 32))
			}
			db, err := Open(options)
			require.NoError(t, err)
			defer db.Close()
			value := bytes.Repeat([]byte("range-record"), 24<<10)
			for i := 0; i < 12; i++ {
				key := []byte(fmt.Sprintf("node-%02d", i))
				require.NoError(t, db.Update(func(txn *Txn) error { return txn.Set(key, value) }))
			}
			for i := 1; i < 12; i++ {
				key := []byte(fmt.Sprintf("node-%02d", i))
				require.NoError(t, db.Update(func(txn *Txn) error { return txn.Delete(key) }))
			}
			require.NoError(t, db.Flatten(1))
			pointer := func() valuePointer {
				var vp valuePointer
				require.NoError(t, db.View(func(txn *Txn) error {
					item, err := txn.Get([]byte("node-00"))
					if err != nil {
						return err
					}
					require.NotZero(t, item.meta&bitValuePointer)
					vp.Decode(item.vptr)
					return nil
				}))
				return vp
			}
			before := pointer()
			db.vlog.filesLock.RLock()
			file := db.vlog.filesMap[before.Fid]
			db.vlog.filesLock.RUnlock()
			require.NotNil(t, file)
			require.NoError(t, db.vlog.rewrite(file))
			require.NotEqual(t, before, pointer(), "GC must relocate the live record")
			require.NoError(t, db.View(func(txn *Txn) error {
				item, err := txn.Get([]byte("node-00"))
				if err != nil {
					return err
				}
				return item.ValueRange(1000, 37, func(got []byte) error {
					require.Equal(t, value[1000:1037], got)
					return nil
				})
			}))
		})
	}
}
