package decred

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/decred/dcrd/wire"
)

// The official decoder limits declared vector lengths against a global 32 MiB
// message bound. Preflight additionally ties each allocation to the actual
// bounded input bytes. It does no consensus or signature validation; Deserialize
// remains authoritative after this allocation-free structural walk.
func skipWire(r *bytes.Reader, n uint64) bool {
	if n > uint64(r.Len()) {
		return false
	}
	_, _ = r.Seek(int64(n), io.SeekCurrent)
	return true
}

// Each output/witness has fixed bytes followed by a canonical varint script.
func wireScriptVector(r *bytes.Reader, fixed uint64) bool {
	count, e := wire.ReadVarInt(r, 0)
	if e != nil || count > uint64(r.Len())/(fixed+1) {
		return false
	}
	for i := uint64(0); i < count; i++ {
		if !skipWire(r, fixed) {
			return false
		}
		n, e := wire.ReadVarInt(r, 0)
		if e != nil || !skipWire(r, n) {
			return false
		}
	}
	return true
}

func wireTransactionShape(r *bytes.Reader) bool {
	var header [4]byte
	if _, e := io.ReadFull(r, header[:]); e != nil {
		return false
	}
	typeID := wire.TxSerializeType(binary.LittleEndian.Uint32(header[:]) >> 16)
	switch typeID {
	case wire.TxSerializeFull, wire.TxSerializeNoWitness:
		count, e := wire.ReadVarInt(r, 0)
		// Prefix input = hash(32), index(4), tree(1), sequence(4).
		if e != nil || count > uint64(r.Len())/41 {
			return false
		}
		_ = skipWire(r, count*41)
		if !wireScriptVector(r, 10) || !skipWire(r, 8) {
			return false
		}
		if typeID == wire.TxSerializeNoWitness {
			return true
		}
	case wire.TxSerializeOnlyWitness:
	default:
		return false
	}
	return wireScriptVector(r, 16)
}

func boundedWireTransaction(data []byte) bool {
	r := bytes.NewReader(data)
	return wireTransactionShape(r) && r.Len() == 0
}

func boundedWireBlock(data []byte) bool {
	r := bytes.NewReader(data)
	if !skipWire(r, wire.MaxBlockHeaderPayload) {
		return false
	}
	for tree := 0; tree < 2; tree++ {
		count, e := wire.ReadVarInt(r, 0)
		// Even the smallest witness-only encoding needs version + count.
		if e != nil || count > uint64(r.Len())/5 {
			return false
		}
		for i := uint64(0); i < count; i++ {
			if !wireTransactionShape(r) {
				return false
			}
		}
	}
	return r.Len() == 0
}
