package decred

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/decred/dcrd/wire"
)

func TestWireAllocationPreflightPreservesOfficialEncodings(t *testing.T) {
	tx := wire.NewMsgTx()
	for i := 0; i < 2; i++ {
		in := wire.NewTxIn(&wire.OutPoint{Index: uint32(i), Tree: int8(i)}, int64(1000+i), bytes.Repeat([]byte{byte(i)}, 253+i*256))
		tx.AddTxIn(in)
	}
	for _, size := range []int{0, 1, 252, 253, 4096} {
		tx.AddTxOut(wire.NewTxOut(100, bytes.Repeat([]byte{0x51}, size)))
	}
	for _, kind := range []wire.TxSerializeType{wire.TxSerializeFull, wire.TxSerializeNoWitness, wire.TxSerializeOnlyWitness} {
		tx.SerType = kind
		data, e := tx.Bytes()
		if e != nil || !boundedWireTransaction(data) {
			t.Fatal("official encoding rejected", kind, e)
		}
		var decoded wire.MsgTx
		if e = decoded.Deserialize(bytes.NewReader(data)); e != nil {
			t.Fatal(e)
		}
		for cut := 0; cut < len(data); cut++ {
			if boundedWireTransaction(data[:cut]) {
				t.Fatalf("truncation accepted type=%d offset=%d", kind, cut)
			}
		}
		if boundedWireTransaction(append(data, 0)) {
			t.Fatal("trailing bytes accepted", kind)
		}
	}
	tx.SerType = wire.TxSerializeFull
	block := &wire.MsgBlock{Transactions: []*wire.MsgTx{tx}, STransactions: []*wire.MsgTx{tx.Copy()}}
	var data bytes.Buffer
	if e := block.Serialize(&data); e != nil || !boundedWireBlock(data.Bytes()) {
		t.Fatal("official two-tree block rejected", e)
	}
	for cut := 0; cut < data.Len(); cut++ {
		if boundedWireBlock(data.Bytes()[:cut]) {
			t.Fatal("truncated block accepted", cut)
		}
	}
	if boundedWireBlock(append(data.Bytes(), 0)) {
		t.Fatal("block trailing bytes accepted")
	}
}

func TestWireAllocationPreflightRejectsDeclaredCountAmplification(t *testing.T) {
	header := func(kind wire.TxSerializeType) []byte {
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, uint32(kind)<<16|1)
		return b
	}
	var vectors [][]byte
	// These counts fit the upstream global32MiB maximum but cannot fit the
	// actual bytes. The preflight must reject before the upstream allocations.
	vectors = append(vectors, append(header(wire.TxSerializeFull), 0xfd, 0xff, 0xff))
	vectors = append(vectors, append(header(wire.TxSerializeFull), 0, 0xfd, 0xff, 0xff))
	vectors = append(vectors, append(header(wire.TxSerializeOnlyWitness), 0xfd, 0xff, 0xff))
	vectors = append(vectors, append(header(wire.TxSerializeOnlyWitness), 1, 0))
	vectors = append(vectors, append(header(99), 0, 0, 0))
	vectors = append(vectors, append(header(wire.TxSerializeFull), 0xfd, 0, 0))
	// A script's declared length is checked against the remaining input too.
	largeScript := append(header(wire.TxSerializeFull), 0, 1)
	largeScript = append(largeScript, make([]byte, 10)...)
	largeScript = append(largeScript, 0xfe, 0, 0, 16, 0)
	largeScript = append(largeScript, make([]byte, 9)...)
	vectors = append(vectors, largeScript)
	noncanonicalScript := append(header(wire.TxSerializeOnlyWitness), 1)
	noncanonicalScript = append(noncanonicalScript, make([]byte, 16)...)
	noncanonicalScript = append(noncanonicalScript, 0xfd, 0, 0)
	vectors = append(vectors, noncanonicalScript)
	consumingScript := append(header(wire.TxSerializeFull), 0, 2)
	consumingScript = append(consumingScript, make([]byte, 10)...)
	consumingScript = append(consumingScript, 20)
	consumingScript = append(consumingScript, make([]byte, 20)...)
	vectors = append(vectors, consumingScript)
	missingExpiry := append(header(wire.TxSerializeFull), 0, 0)
	missingExpiry = append(missingExpiry, make([]byte, 7)...)
	vectors = append(vectors, missingExpiry)
	for i, data := range vectors {
		if boundedWireTransaction(data) {
			t.Fatal("malformed allocation vector accepted", i)
		}
		if allocations := testing.AllocsPerRun(50, func() { _ = boundedWireTransaction(data) }); allocations > 16 {
			t.Fatal("count-driven allocation amplification", i, allocations)
		}
	}
	for _, suffix := range [][]byte{{0xfd, 0xff, 0xff}, {1, 1, 0, 99, 0, 0}, {0, 0xfd, 0xff, 0xff}} {
		data := append(make([]byte, wire.MaxBlockHeaderPayload), suffix...)
		if boundedWireBlock(data) {
			t.Fatal("malformed block allocation vector accepted")
		}
	}
}

func BenchmarkDeclaredInputCountUpstream(b *testing.B) {
	data := []byte{1, 0, 0, 0, 0xfd, 0xff, 0xff}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var tx wire.MsgTx
		_ = tx.Deserialize(bytes.NewReader(data))
	}
}

func BenchmarkDeclaredInputCountBounded(b *testing.B) {
	data := []byte{1, 0, 0, 0, 0xfd, 0xff, 0xff}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = boundedWireTransaction(data)
	}
}
