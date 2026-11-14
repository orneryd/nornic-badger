// Copyright 2026 NornicDB contributors. Apache-2.0.
package nornic

import "testing"

func FuzzDecodeFormats(f *testing.F) {
	column, _ := EncodeInt64([]int64{-2, 4, 9})
	f.Add(column)
	f.Add([]byte("bad format"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		if column, err := OpenInt64(data); err == nil {
			for i := 0; i < column.Len(); i++ {
				if _, err := column.At(i); err != nil {
					t.Fatal(err)
				}
			}
		}
	})
}
