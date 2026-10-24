// Package evm implements only allowlisted native, canonical ZEN and Ethereum USDT transfers.
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
	"horizen-mainnet":  "0x244bb43a2f687bfcd9cbe2f0a64b2e54436a597367ab7052fd1ab925c4cabe14",
	"horizen-testnet":  "0x676dedf00282df1f07ddbff58e1c7bf7817fefcf52fa2506f1e7ee3f3723fc01",
}

// Adapter has no secret state. HTTPClient is public for bounded custom transports.
// Its transport must not log request bodies. Endpoint changes still require chain-ID checks.
type Adapter struct {
	endpoints  map[string]string
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
		n("horizen-mainnet", "Horizen", "26514", "https://horizen.calderachain.xyz/http", "ETH", "0x57da2D504bf8b83Ef304759d9f2648522D7a9280", false),
		n("horizen-testnet", "Horizen Testnet", "2651420", "https://horizen-testnet.rpc.caldera.xyz/http", "ETH", "0xb06EC4ce262D8dbDc24Fac87479A49A7DC4cFb87", true),
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
	}
	return ns
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
	return n.ChainID == "8453" || n.ChainID == "84532" || n.ChainID == "26514" || n.ChainID == "2651420"
}
