package badger

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOrderedTransactionWritesReopen(t *testing.T) {
	dir := t.TempDir()
	opts := DefaultOptions(dir).WithLogger(nil).WithSyncWrites(true)
	db, err := OpenManaged(opts)
	require.NoError(t, err)

	keys := []string{"prefix", "prefix-key", "prefix-key-a"}
	for i := 0; i < 1024; i++ {
		keys = append(keys, fmt.Sprintf("key-%04d", (i*571)%1024))
	}
	for version := uint64(1); version <= 3; version++ {
		txn := db.NewTransactionAt(version-1, true)
		if version == 2 {
			txn.EnableOrderedWrites()
		}
		for index, key := range keys {
			if version == 3 && index%2 == 0 {
				continue
			}
			value := fmt.Sprintf("%s@%d", key, version)
			require.NoError(t, txn.Set([]byte(key), []byte(value)))
		}
		require.NoError(t, txn.CommitAt(version, nil))
	}
	require.NoError(t, db.Close())

	db, err = OpenManaged(opts)
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()
	read := db.NewTransactionAt(3, false)
	defer read.Discard()
	for index, key := range keys {
		item, err := read.Get([]byte(key))
		require.NoError(t, err)
		value, err := item.ValueCopy(nil)
		require.NoError(t, err)
		version := 3
		if index%2 == 0 {
			version = 2
		}
		require.Equal(t, fmt.Sprintf("%s@%d", key, version), string(value))
	}
}
