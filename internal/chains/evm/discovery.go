package evm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"localwallet/internal/model"
)

// Discovery is read-only and never installs a token policy or enables signing.
// The indexer supplies candidate addresses only. All displayed metadata and
// atomic balances come from the configured, chain-checked RPC at one block.
type DiscoveredToken struct {
	Token   model.ERC20Token `json:"token"`
	Balance model.Balance    `json:"balance"`
}
type TokenDiscovery struct {
	Items       []DiscoveredToken `json:"items"`
	Unavailable int               `json:"unavailable"`
	Limited     bool              `json:"limited"`
	Provider    string            `json:"provider"`
}

func discoveryEndpoint(network string) string {
	switch network {
	case "base-mainnet":
		return "https://base.blockscout.com"
	case "base-sepolia":
		return "https://base-sepolia.blockscout.com"
	}
	return ""
}

func (a *Adapter) DiscoverTokens(ctx context.Context, account model.Account) (TokenDiscovery, error) {
	result := TokenDiscovery{Items: []DiscoveredToken{}, Provider: "Blockscout candidate index; balances checked through Base RPC. Inventory may be incomplete."}
	n, err := a.validateAccount(account)
	endpoint := discoveryEndpoint(account.Network)
	if err != nil || endpoint == "" || a.HTTPClient == nil {
		return result, errors.New("token discovery requires an owned Base wallet")
	}
	// No caller-supplied URL, credentials, remote artwork, redirects or response
	// error text can cross this boundary. The service learns this public address.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/api/v2/addresses/"+account.Address+"/token-balances", nil)
	if err != nil {
		return result, errors.New("token discovery request unavailable")
	}
	req.Header.Set("Accept", "application/json")
	client := *a.HTTPClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(req)
	if err != nil {
		return result, errors.New("token discovery indexer unavailable; use an exact contract address")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return result, errors.New("token discovery indexer denied or unavailable; use an exact contract address")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (256<<10)+1))
	if err != nil || len(raw) > 256<<10 {
		return result, errors.New("token discovery response exceeded its bound")
	}
	var checked any
	if StrictJSON(raw, &checked) != nil {
		return result, errors.New("invalid token discovery response")
	}
	var entries []struct {
		Token struct {
			Address string `json:"address_hash"`
			Type    string `json:"type"`
		} `json:"token"`
	}
	if json.Unmarshal(raw, &entries) != nil || entries == nil || len(entries) > 256 {
		return result, errors.New("invalid or excessive token discovery inventory")
	}
	b, err := a.current(ctx, n)
	if err != nil {
		return result, err
	}
	seen := map[string]bool{}
	for _, asset := range n.Assets {
		if asset.Contract != "" {
			seen[strings.ToLower(asset.Contract)] = true
		}
	}
	inspected := 0
	for _, entry := range entries {
		if entry.Token.Type != "ERC-20" {
			continue
		}
		contract := entry.Token.Address
		if a.ValidateAddress(n.ID, contract) != nil {
			result.Unavailable++
			continue
		}
		contract = common.HexToAddress(contract).Hex()
		key := strings.ToLower(contract)
		if seen[key] {
			continue
		}
		seen[key] = true
		if inspected >= MaxImportedTokens {
			result.Limited = true
			break
		}
		inspected++
		if ctx.Err() != nil {
			result.Limited = true
			break
		}
		token, err := a.inspectTokenAt(ctx, n, contract, b.Number)
		if err != nil {
			result.Unavailable++
			continue
		}
		// This temporary adapter exists only for observed balances. The original
		// adapter and signer policy remain unchanged even on successful discovery.
		observed, err := a.WithTokens([]model.ERC20Token{token})
		if err != nil {
			result.Unavailable++
			continue
		}
		value, err := observed.balance(ctx, n, account, token.Asset, b.Number)
		if err != nil {
			result.Unavailable++
			continue
		}
		if value.Sign() == 0 {
			continue
		}
		result.Items = append(result.Items, DiscoveredToken{Token: token, Balance: model.Balance{Asset: token.Asset.ID, Atomic: value.String(), State: "provider-observed", At: b.Hash}})
	}
	return result, nil
}
