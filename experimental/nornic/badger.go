// Copyright 2026 NornicDB contributors. Licensed under the Apache License, Version 2.0.
package nornic

import (
	"bytes"
	"context"

	badger "github.com/dgraph-io/badger/v4"
)

// ProjectItem exercises projection through Badger's existing versioned Item.
// It does NOT bypass Item.Value or claim partial physical reads, decompression,
// or checksumming. Field bytes are valid only during the visit callback.
func ProjectItem(ctx context.Context, item *badger.Item, tokens []uint64, visit func(Field) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return item.Value(func(encoded []byte) error {
		record, err := OpenRecord(bytes.NewReader(encoded), int64(len(encoded)))
		if err != nil {
			return err
		}
		return record.Project(ctx, tokens, visit)
	})
}
