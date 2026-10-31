package near

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"io"
	"math/big"
	"strings"
	"testing"

	"github.com/decred/base58"
)

// Decode the complete Borsh wire format independently of transaction(), rather
// than treating display text or a payload field as evidence of what is signed.
func TestStakingSignedWireContainsOnlyContractAction(t *testing.T) {
	for _, method := range []string{"deposit_and_stake", "unstake", "unstake_all", "withdraw_all"} {
		t.Run(method, func(t *testing.T) {
			p := stakingFixture(t, method)
			if p.Memo != "" {
				t.Fatal("staking plan has a memo")
			}
			a := New()
			signed, err := a.Sign(seed(), p)
			if err != nil {
				t.Fatal(err)
			}
			r := bytes.NewReader(signed.Bytes)
			read := func(n int) []byte {
				t.Helper()
				b := make([]byte, n)
				if _, err := io.ReadFull(r, b); err != nil {
					t.Fatalf("truncated Borsh field: %v", err)
				}
				return b
			}
			readString := func() string {
				t.Helper()
				n := binary.LittleEndian.Uint32(read(4))
				if n > 2048 {
					t.Fatal("unexpected unbounded Borsh string")
				}
				return string(read(int(n)))
			}
			if got := readString(); got != p.Account.Address {
				t.Fatalf("signer account %q", got)
			}
			if read(1)[0] != 0 {
				t.Fatal("non-Ed25519 public key")
			}
			publicKey := read(ed25519.PublicKeySize)
			if !bytes.Equal(publicKey, base58.Decode(strings.TrimPrefix(p.Account.PublicKey, "ed25519:"))) {
				t.Fatal("wrong signer public key")
			}
			if got := binary.LittleEndian.Uint64(read(8)); got != 42 {
				t.Fatalf("nonce %d", got)
			}
			if got := readString(); got != "oracle.pool.f863973.m0" {
				t.Fatalf("contract receiver %q", got)
			}
			if !bytes.Equal(read(32), make([]byte, 32)) {
				t.Fatal("wrong recent block hash")
			}
			if got := binary.LittleEndian.Uint32(read(4)); got != 1 {
				t.Fatalf("staking contains %d actions instead of one", got)
			}
			if read(1)[0] != 2 {
				t.Fatal("staking action is not FunctionCall")
			}
			if got := readString(); got != method {
				t.Fatalf("contract method %q", got)
			}
			wantArgs := "{}"
			if method == "unstake" {
				wantArgs = `{"amount":"123456789012345678901234"}`
			}
			if got := readString(); got != wantArgs {
				t.Fatalf("on-chain arguments %q; want exactly %q", got, wantArgs)
			}
			if got := binary.LittleEndian.Uint64(read(8)); got != 100_000_000_000_000 {
				t.Fatalf("prepaid gas %d", got)
			}
			deposit := read(16)
			for i, j := 0, len(deposit)-1; i < j; i, j = i+1, j-1 {
				deposit[i], deposit[j] = deposit[j], deposit[i]
			}
			wantDeposit := "0"
			if method == "deposit_and_stake" {
				wantDeposit = "123456789012345678901234"
			}
			if got := new(big.Int).SetBytes(deposit).String(); got != wantDeposit {
				t.Fatalf("attached yoctoNEAR %s; want %s", got, wantDeposit)
			}
			unsignedSize := len(signed.Bytes) - r.Len()
			if read(1)[0] != 0 {
				t.Fatal("non-Ed25519 signature")
			}
			signature := read(ed25519.SignatureSize)
			if r.Len() != 0 {
				t.Fatal("unexpected data after the sole action and signature")
			}
			raw, err := transaction(p)
			if err != nil || !bytes.Equal(raw, signed.Bytes[:unsignedSize]) {
				t.Fatal("signed transaction differs from unsigned Borsh action")
			}
			hash := sha256.Sum256(raw)
			if signed.Identity != base58.Encode(hash[:]) || !ed25519.Verify(publicKey, hash[:], signature) {
				t.Fatal("independent transaction hash/signature verification failed")
			}
			summary, ok := StakingEffects(p)
			if !ok || !strings.Contains(summary, "\nExpected action:") {
				t.Fatal("missing separate multiline approval summary")
			}
			for _, text := range []string{summary, "NEAR staking pool contract:", "Expected action:", "Unstaking normally requires four epochs;"} {
				if bytes.Contains(raw, []byte(text)) || bytes.Contains(signed.Bytes, []byte(text)) {
					t.Fatal("approval description was serialized into the transaction")
				}
			}
		})
	}
}

func TestStakingRejectsApprovalSummaryAsMemoOrArguments(t *testing.T) {
	for _, method := range []string{"deposit_and_stake", "unstake", "unstake_all", "withdraw_all"} {
		p := stakingFixture(t, method)
		summary, ok := StakingEffects(p)
		if !ok {
			t.Fatal("missing approval summary")
		}
		summary += "\nUI_ONLY_SUMMARY_MUST_NOT_REACH_CHAIN"
		for _, field := range []string{"memo", "arguments"} {
			t.Run(method+"/"+field, func(t *testing.T) {
				q := p
				if field == "memo" {
					q.Memo = summary
				} else {
					var d Payload
					if err := json.Unmarshal(p.Payload, &d); err != nil {
						t.Fatal(err)
					}
					// Even syntactically valid JSON cannot add a UI comment to
					// the allowlisted staking contract's exact arguments.
					args := map[string]string{"comment": summary}
					if method == "unstake" {
						args["amount"] = p.Amount
					}
					b, err := json.Marshal(args)
					if err != nil {
						t.Fatal(err)
					}
					d.Staking.Args = string(b)
					q.Payload, err = json.Marshal(d)
					if err != nil {
						t.Fatal(err)
					}
				}
				if New().ValidatePlan(q) == nil {
					t.Fatal("approval summary accepted as on-chain intent")
				}
				if signed, err := New().Sign(seed(), q); err == nil || len(signed.Bytes) != 0 {
					t.Fatal("approval summary produced signed transaction bytes")
				}
			})
		}
	}
}
