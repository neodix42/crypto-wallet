package model

import (
	"strings"
	"testing"
)

func TestBalanceBatchPublicSchemaRejectsAmbiguousOrUnboundedObservations(t *testing.T) {
	valid := BalanceBatchResult{AccountID: "owned", Network: "ethereum-mainnet", Values: []Balance{{Asset: "ethereum-mainnet:native", Atomic: "1000000000000000001", State: "provider-observed"}, {Asset: "ethereum-mainnet:USDC", State: "unavailable"}}}
	if !ValidBalanceBatchResult(valid) {
		t.Fatal("exact native and independently unavailable token rejected")
	}
	for _, change := range []func(*BalanceBatchResult){
		func(r *BalanceBatchResult) { r.AccountID = "" },
		func(r *BalanceBatchResult) { r.AccountID = strings.Repeat("a", 257) },
		func(r *BalanceBatchResult) { r.Network = "\x00" },
		func(r *BalanceBatchResult) { r.Values = nil },
		func(r *BalanceBatchResult) { r.Error = "raw provider secret" },
		func(r *BalanceBatchResult) { r.Error = "BALANCE_UNAVAILABLE" },
		func(r *BalanceBatchResult) { r.Values[0].Asset = "base-mainnet:native" },
		func(r *BalanceBatchResult) { r.Values[1].Asset = r.Values[0].Asset },
		func(r *BalanceBatchResult) { r.Values[0].Atomic = "01" },
		func(r *BalanceBatchResult) { r.Values[0].Atomic = "1e3" },
		func(r *BalanceBatchResult) { r.Values[0].Atomic = "-1" },
		func(r *BalanceBatchResult) { r.Values[0].Atomic = strings.Repeat("1", 161) },
		func(r *BalanceBatchResult) { r.Values[0].State = strings.Repeat("x", 1025) },
		func(r *BalanceBatchResult) { r.Values[0].At = "a\nb" },
		func(r *BalanceBatchResult) { r.Values = make([]Balance, 65) },
	} {
		copy := valid
		copy.Values = append([]Balance(nil), valid.Values...)
		change(&copy)
		if ValidBalanceBatchResult(copy) {
			t.Fatal("hostile balance DTO accepted", copy)
		}
	}
	for _, code := range []string{"BALANCE_UNAVAILABLE", "TON_RPC_RATE_LIMITED"} {
		if !ValidBalanceBatchResult(BalanceBatchResult{AccountID: "owned", Network: "ton-mainnet", Error: code}) {
			t.Fatal("bounded public error rejected", code)
		}
	}
}
