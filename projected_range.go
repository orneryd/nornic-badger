package badger

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"fmt"

	"github.com/dgraph-io/badger/v4/y"
)

// ValueRange calls fn with a bounded region of the logical value. A value-log
// resident value is read without decrypting unrelated value bytes. An inline
// or prefetched value has already been loaded by Badger and is sliced in memory.
// The callback must not retain the borrowed bytes after it returns.
//
// A database configured to verify whole value-log checksums uses the ordinary
// full-value read, preserving that integrity contract. Applications using
// selective reads must independently authenticate or checksum their regions.
func (item *Item) ValueRange(start, length uint32, fn func([]byte) error) error {
	item.wg.Wait()
	if item.status == prefetched || item.meta&bitValuePointer == 0 {
		return item.Value(func(value []byte) error {
			if uint64(start)+uint64(length) > uint64(len(value)) {
				return fmt.Errorf("value range [%d:%d] exceeds value length %d", start, uint64(start)+uint64(length), len(value))
			}
			if fn != nil {
				return fn(value[int(start) : int(start)+int(length)])
			}
			return nil
		})
	}
	if !item.hasValue() || item.txn == nil {
		return fmt.Errorf("value range requires a live value-log item")
	}
	if len(item.vptr) < int(vptrSize) {
		return fmt.Errorf("value range has a truncated value pointer")
	}
	var vp valuePointer
	vp.Decode(item.vptr)
	if item.txn.db.opt.VerifyValueChecksum {
		value, unlock, err := item.txn.db.vlog.Read(vp, item.slice)
		defer runCallback(unlock)
		if err != nil {
			return err
		}
		if uint64(start)+uint64(length) > uint64(len(value)) {
			return fmt.Errorf("value range [%d:%d] exceeds value length %d", start, uint64(start)+uint64(length), len(value))
		}
		if fn != nil {
			return fn(value[int(start) : int(start)+int(length)])
		}
		return nil
	}
	value, unlock, err := item.txn.db.vlog.readRange(vp, start, length)
	defer runCallback(unlock)
	if err != nil {
		return err
	}
	if fn != nil {
		return fn(value)
	}
	return nil
}

func (vlog *valueLog) readRange(vp valuePointer, start, length uint32) ([]byte, func(), error) {
	lf, err := vlog.getFileRLocked(vp)
	if err != nil {
		return nil, nil, err
	}
	unlock := lf.lock.RUnlock
	entry, err := lf.read(vp)
	if err != nil {
		return nil, unlock, err
	}
	// Decode only the bounded header. A corrupt length must not reach a slice or
	// the encryption stream as an unchecked offset.
	if len(entry) < 2+3+4 {
		return nil, unlock, fmt.Errorf("value-log entry has a truncated header")
	}
	position := 2
	var fields [3]uint64
	for i := range fields {
		if position >= len(entry)-4 {
			return nil, unlock, fmt.Errorf("value-log entry has a truncated header")
		}
		value, n := binary.Uvarint(entry[position:min(position+binary.MaxVarintLen64, len(entry))])
		if n <= 0 {
			return nil, unlock, fmt.Errorf("value-log entry has an invalid header field")
		}
		fields[i] = value
		position += n
	}
	if fields[0] > uint64(len(entry)) || fields[1] > uint64(len(entry)) ||
		uint64(position)+fields[0]+fields[1]+4 > uint64(len(entry)) ||
		uint64(start)+uint64(length) > fields[1] {
		return nil, unlock, fmt.Errorf("value range [%d:%d] exceeds value-log bounds", start, uint64(start)+uint64(length))
	}
	valueOffset := position + int(fields[0]) + int(start)
	selected := entry[valueOffset : valueOffset+int(length)]
	y.NumReadsVlogAdd(vlog.db.opt.MetricsEnabled, 1)
	y.NumBytesReadsVlogAdd(vlog.db.opt.MetricsEnabled, int64(position+int(fields[0])+int(length)))
	if !lf.encryptionEnabled() {
		return selected, unlock, nil
	}
	// Badger encrypts key and value as one AES-CTR stream. Advance the counter
	// by complete blocks, then consume the leading bytes of the selected block.
	streamOffset := fields[0] + uint64(start)
	iv := lf.generateIV(vp.Offset)
	blocks := streamOffset / aes.BlockSize
	for i := len(iv) - 1; i >= 0 && blocks != 0; i-- {
		sum := uint64(iv[i]) + blocks&0xff
		iv[i] = byte(sum)
		blocks = (blocks >> 8) + (sum >> 8)
	}
	if blocks != 0 {
		return nil, unlock, fmt.Errorf("value-log encryption counter overflow")
	}
	block, err := aes.NewCipher(lf.dataKey.Data)
	if err != nil {
		return nil, unlock, err
	}
	stream := cipher.NewCTR(block, iv)
	var skipped [aes.BlockSize]byte
	stream.XORKeyStream(skipped[:streamOffset%aes.BlockSize], skipped[:streamOffset%aes.BlockSize])
	plain := make([]byte, len(selected))
	stream.XORKeyStream(plain, selected)
	return plain, unlock, nil
}
