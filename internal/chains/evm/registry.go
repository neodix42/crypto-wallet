// Package evm implements native and exact-policy ERC-20 transfers on active EVM networks.
package evm

import (
	"fmt"
	"localwallet/internal/model"
	"net/http"
	"time"
)

const Profile = "evm-bip39-bip32-v1"
const USDTContract = "0xdAC17F958D2ee523a2206206994597C13D831ec7"
const USDTAsset = "ethereum-mainnet:erc20:" + USDTContract

// Frozen against official deployments on 2026-10-23. An unexpected upgrade fails
// closed and requires a reviewed registry update. These are runtime code hashes.
var tokenCodeHashes = map[string]string{
	"ethereum-mainnet": "0xb44fb4e949d0f78f87f79ee46428f23a2a5713ce6fc6e0beb3dda78c2ac1ea55",
	"base-mainnet":     "0x92a6294a008ae778c19a6f894f1c0f66818ce2a00c32d25eb6fa6a7923b23c3c",
	"base-sepolia":     "0xd0ed2947f1aaa2a3eecc272403016fe1a8e49e21beadf7d189946f679d1a4a39",
}

// Adapter has no secret state. HTTPClient is public for bounded custom transports.
// Its transport must not log request bodies. Endpoint changes still require chain-ID checks.
type Adapter struct {
	endpoints  map[string]string
	tokens     []model.ERC20Token
	HTTPClient *http.Client
}

func registry() []model.Network {
	n := func(id, name, chain, rpc, symbol, contract string, test bool) model.Network {
		assets := []model.Asset{{ID: id + ":native", Symbol: symbol, Decimals: 18}}
		if id == "ethereum-mainnet" {
			assets = append(assets, model.Asset{ID: USDTAsset, Symbol: "USDT", Decimals: 6, Contract: USDTContract})
		}
		if contract != "" {
			assets = append(assets, model.Asset{ID: id + ":erc20:" + contract, Symbol: "ZEN", Decimals: 18, Contract: contract})
		}
		return model.Network{ID: id, Family: "evm", Name: name, ChainID: chain, RPC: rpc, Testnet: test, Assets: assets, Trust: "TLS RPC provider; chain ID checked, not a consensus proof"}
	}
	return []model.Network{
		n("ethereum-mainnet", "Ethereum", "1", "https://ethereum-rpc.publicnode.com", "ETH", "", false),
		n("ethereum-sepolia", "Ethereum Sepolia", "11155111", "https://ethereum-sepolia-rpc.publicnode.com", "ETH", "", true),
		n("hyperevm-mainnet", "HyperEVM", "999", "https://rpc.hyperliquid.xyz/evm", "HYPE", "", false),
		n("hyperevm-testnet", "HyperEVM Testnet", "998", "https://rpc.hyperliquid-testnet.xyz/evm", "HYPE", "", true),
		n("base-mainnet", "Base", "8453", "https://mainnet.base.org", "ETH", "0xf43eB8De897Fbc7F2502483B2Bef7Bb9EA179229", false),
		n("base-sepolia", "Base Sepolia", "84532", "https://sepolia.base.org", "ETH", "0x107fdE93838e3404934877935993782F977324BB", true),
	}
}

func New() *Adapter { a, _ := NewWithEndpoints(nil); return a }
func NewWithEndpoints(endpoints map[string]string) (*Adapter, error) {
	a := &Adapter{endpoints: map[string]string{}, HTTPClient: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	for id, endpoint := range endpoints {
		if _, err := network(id); err != nil {
			return nil, err
		}
		if err := ValidateEndpoint(endpoint); err != nil {
			return nil, err
		}
		a.endpoints[id] = endpoint
	}
	return a, nil
}
func (a *Adapter) Networks() []model.Network {
	ns := registry()
	for i := range ns {
		if ep, ok := a.endpoints[ns[i].ID]; ok {
			ns[i].RPC = ep
		}
		for _, token := range a.tokens {
			if token.Network == ns[i].ID {
				ns[i].Assets = append(ns[i].Assets, token.Asset)
			}
		}
	}
	return ns
}

func (a *Adapter) network(id string) (model.Network, error) {
	for _, n := range a.Networks() {
		if n.ID == id {
			return n, nil
		}
	}
	return model.Network{}, fmt.Errorf("unsupported EVM network")
}
func network(id string) (model.Network, error) {
	for _, n := range registry() {
		if n.ID == id {
			return n, nil
		}
	}
	return model.Network{}, fmt.Errorf("unsupported EVM network")
}
func asset(n model.Network, id string) (model.Asset, error) {
	for _, s := range n.Assets {
		if s.ID == id {
			return s, nil
		}
	}
	return model.Asset{}, fmt.Errorf("unsupported asset identity")
}
func rollup(n model.Network) bool {
	return n.ChainID == "8453" || n.ChainID == "84532"
}
