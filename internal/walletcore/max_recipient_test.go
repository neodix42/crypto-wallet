package walletcore

import (
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"localwallet/internal/chains/aptos"
	"localwallet/internal/chains/evm"
	"localwallet/internal/chains/hypercore"
	"localwallet/internal/chains/near"
	"localwallet/internal/chains/ton"
	"localwallet/internal/model"
)

func TestMaximumRecipientCanonicalizationKeepsExactChainDestination(t *testing.T) {
	const eth = "0x73410EA1FcbE1b8bCA883762dC56beF46403A469"
	cases := []struct {
		adapter                          model.Adapter
		family, network, input, prepared string
		want                             bool
	}{
		{evm.New(), "evm", "ethereum-mainnet", eth, strings.ToLower(eth), true},
		{evm.New(), "evm", "ethereum-mainnet", eth, "0x1111111111111111111111111111111111111111", false},
		{evm.New(), "evm", "ethereum-mainnet", " " + eth, eth, false},
		{evm.New(), "evm", "ethereum-mainnet", eth, eth + "\n", false},
		{hypercore.New(), "hypercore", "hypercore-mainnet", eth, strings.ToLower(eth), true},
		{hypercore.New(), "hypercore", "hypercore-mainnet", eth, "0x2222222222222222222222222222222222222222", false},
		{aptos.New(), "aptos", "aptos-mainnet", "0x1", "0x" + strings.Repeat("0", 63) + "1", true},
		{aptos.New(), "aptos", "aptos-mainnet", "0xAbC", "0x" + strings.Repeat("0", 61) + "abc", true},
		{aptos.New(), "aptos", "aptos-mainnet", "0x1", "0x2", false},
		{aptos.New(), "aptos", "aptos-mainnet", "0x+1", "0x1", false},
		{aptos.New(), "aptos", "aptos-mainnet", " 0x1", "0x1", false},
		{near.New(), "near", "near-mainnet", "alice.near", "alice.near", true},
		{near.New(), "near", "near-mainnet", "alice.near", "bob.near", false},
		{near.New(), "near", "near-mainnet", "alice.near", "ALICE.NEAR", false},
	}
	tonAdapter := ton.New()
	account, err := tonAdapter.Derive(make([]byte, 64), 0, "ton-mainnet")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := address.ParseAddr(account.Address)
	if err != nil {
		t.Fatal(err)
	}
	cases = append(cases, struct {
		adapter                          model.Adapter
		family, network, input, prepared string
		want                             bool
	}{tonAdapter, "ton", "ton-mainnet", account.Address, parsed.StringRaw(), true},
		struct {
			adapter                          model.Adapter
			family, network, input, prepared string
			want                             bool
		}{tonAdapter, "ton", "ton-mainnet", account.Address, parsed.Bounce(false).String(), true},
		struct {
			adapter                          model.Adapter
			family, network, input, prepared string
			want                             bool
		}{tonAdapter, "ton", "ton-mainnet", account.Address, parsed.Testnet(true).String(), false},
		struct {
			adapter                          model.Adapter
			family, network, input, prepared string
			want                             bool
		}{tonAdapter, "ton", "ton-mainnet", account.Address, "0:" + strings.Repeat("1", 64), false})
	for _, row := range cases {
		ac := model.Account{Family: row.family, Network: row.network}
		if got := maximumRecipientEqual(row.adapter, ac, row.input, row.prepared); got != row.want {
			t.Fatal("recipient identity changed or legitimate canonicalization rejected", row.family, row.input, row.prepared, got)
		}
	}
}
