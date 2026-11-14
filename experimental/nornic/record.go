// Copyright 2026 NornicDB contributors. Licensed under the Apache License, Version 2.0.

// Package nornic contains experimental projected-record and column codecs.
// Its formats are provisional and are not a supported Badger storage format.
package nornic

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
)

const recordHeader = 12
const directoryEntry = 24 // uint64 token, absolute offset, encoded length

var ErrFormat = errors.New("invalid experimental storage format")

// Field carries an opaque, already typed property encoding. Token is resolved
// by Nornic, never by Badger. Zero-length values differ from absent tokens.
type Field struct {
	Token   uint64
	Encoded []byte
}

// EncodeRecord produces a canonical sorted directory without mutating fields.
func EncodeRecord(fields []Field) ([]byte, error) {
	if uint64(len(fields)) > math.MaxUint32 || len(fields) > (math.MaxInt-recordHeader)/directoryEntry {
		return nil, ErrFormat
	}
	fields = append([]Field(nil), fields...)
	sort.Slice(fields, func(i, j int) bool { return fields[i].Token < fields[j].Token })
	total := recordHeader + len(fields)*directoryEntry
	for i, f := range fields {
		if i > 0 && f.Token == fields[i-1].Token {
			return nil, fmt.Errorf("%w: duplicate token %d", ErrFormat, f.Token)
		}
		if len(f.Encoded) > math.MaxInt-total {
			return nil, ErrFormat
		}
		total += len(f.Encoded)
	}
	out := make([]byte, total)
	copy(out, "NPR1")
	binary.LittleEndian.PutUint32(out[8:12], uint32(len(fields)))
	offset := recordHeader + len(fields)*directoryEntry
	for i, f := range fields {
		entry := out[recordHeader+i*directoryEntry:]
		binary.LittleEndian.PutUint64(entry, f.Token)
		binary.LittleEndian.PutUint64(entry[8:], uint64(offset))
		binary.LittleEndian.PutUint64(entry[16:], uint64(len(f.Encoded)))
		copy(out[offset:], f.Encoded)
		offset += len(f.Encoded)
	}
	return out, nil
}

type propertySpan struct {
	token  uint64
	offset int64
	length int
}

// Record holds a reader and its validated directory, not the property payloads.
// The caller must keep the reader and the selected record version alive and
// immutable until Project returns. ReaderAt byte counts are logical, not disk I/O.
type Record struct {
	reader io.ReaderAt
	spans  []propertySpan
}

func OpenRecord(reader io.ReaderAt, size int64) (*Record, error) {
	if size < recordHeader {
		return nil, ErrFormat
	}
	var header [recordHeader]byte
	if _, err := reader.ReadAt(header[:], 0); err != nil {
		return nil, err
	}
	if string(header[:4]) != "NPR1" || binary.LittleEndian.Uint32(header[4:8]) != 0 {
		return nil, ErrFormat
	}
	count := uint64(binary.LittleEndian.Uint32(header[8:]))
	if count > uint64((size-recordHeader)/directoryEntry) || count > uint64((math.MaxInt-recordHeader)/directoryEntry) {
		return nil, ErrFormat
	}
	raw := make([]byte, int(count)*directoryEntry)
	if len(raw) > 0 {
		if _, err := reader.ReadAt(raw, recordHeader); err != nil {
			return nil, err
		}
	}
	spans := make([]propertySpan, int(count))
	next := uint64(recordHeader) + count*directoryEntry
	for i := range spans {
		entry := raw[i*directoryEntry:]
		token := binary.LittleEndian.Uint64(entry)
		off := binary.LittleEndian.Uint64(entry[8:])
		length := binary.LittleEndian.Uint64(entry[16:])
		if (i > 0 && token <= spans[i-1].token) || off != next || off > uint64(size) || length > uint64(size)-off || length > math.MaxInt {
			return nil, ErrFormat
		}
		spans[i] = propertySpan{token: token, offset: int64(off), length: int(length)}
		next = off + length
	}
	if next != uint64(size) {
		return nil, ErrFormat
	}
	return &Record{reader: reader, spans: spans}, nil
}

// Project visits present fields in requested order; repeated tokens repeat.
// Encoded is borrowed until visit returns. Only selected payload spans are read;
// directory validation is paid once by OpenRecord. No per-property KV lookup.
func (r *Record) Project(ctx context.Context, tokens []uint64, visit func(Field) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var scratch []byte
	for _, token := range tokens {
		if err := ctx.Err(); err != nil {
			return err
		}
		at := sort.Search(len(r.spans), func(i int) bool { return r.spans[i].token >= token })
		if at == len(r.spans) || r.spans[at].token != token {
			continue
		}
		span := r.spans[at]
		if cap(scratch) < span.length {
			scratch = make([]byte, span.length)
		} else {
			scratch = scratch[:span.length]
		}
		if len(scratch) > 0 {
			if _, err := r.reader.ReadAt(scratch, span.offset); err != nil {
				return err
			}
		}
		if err := visit(Field{Token: token, Encoded: scratch}); err != nil {
			return err
		}
	}
	return nil
}
