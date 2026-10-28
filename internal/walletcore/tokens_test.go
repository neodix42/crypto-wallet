package walletcore

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"localwallet/internal/chains/evm"
	"localwallet/internal/model"
)

func tokenMetadataServer(t *testing.T, fail *bool, chain ...string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Error("bounded fixture received invalid RPC")
			return
		}
		var result any
		switch request.Method {
		case "eth_chainId":
			result = "0xaa36a7"
			if len(chain) > 0 {
				result = chain[0]
			}
		case "eth_getBlockByNumber":
			result = map[string]string{"number": "0x10", "hash": "0x" + strings.Repeat("a", 64), "timestamp": fmt.Sprintf("0x%x", time.Now().Unix()), "baseFeePerGas": "0x1"}
		case "eth_getCode":
			result = "0x6001"
		case "eth_call":
			var call map[string]string
			_ = json.Unmarshal(request.Params[0], &call)
			if call["data"] == "0x313ce567" {
				result = fmt.Sprintf("0x%064x", 6)
			} else {
				result = fmt.Sprintf("0x%064x%064x54455354%s", 32, 4, strings.Repeat("0", 56))
			}
		default:
			t.Error("token import attempted an unapproved RPC method", request.Method)
		}
		if *fail {
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "error": map[string]any{"code": -1, "message": "synthetic unavailable"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
	}))
	t.Cleanup(server.Close)
	return server
}

func TestTokenImportUsesOnlyCoreObservedMetadataAndRefreshesApprovedRegistry(t *testing.T) {
	fail := false
	server := tokenMetadataServer(t, &fail)
	adapter, err := evm.NewWithEndpoints(map[string]string{"ethereum-sepolia": server.URL})
	if err != nil {
		t.Fatal(err)
	}
	c, statePath := fakeCoordinator(t, &publicAdapter{}, nil)
	c.adapters = []model.Adapter{adapter}
	seed := make([]byte, 64)
	seed[0] = 1 // disposable public fixture, never a user profile
	account, err := adapter.Derive(seed, 0, "ethereum-sepolia")
	if err != nil {
		t.Fatal(err)
	}
	state := readPublicState(t, statePath)
	state["accounts"] = []model.Account{account}
	state["settings"] = map[string]any{account.Network: map[string]any{"url": server.URL}}
	writePublicState(t, statePath, state)
	contract := "0x1234567890123456789012345678901234567890"
	request := map[string]any{"accountId": account.ID, "contract": contract}
	state["reject"] = []string{"importToken"}
	writePublicState(t, statePath, state)
	if _, err := c.handle(message("importToken", request)); err == nil {
		t.Fatal("signer rejection was ignored during token import")
	}
	state = readPublicState(t, statePath)
	delete(state, "reject")
	writePublicState(t, statePath, state)
	if _, err := c.handle(message("importToken", request)); err != nil {
		t.Fatal("core rejected its inspected policy", err)
	}
	updated, _ := c.adapter(account.Network)
	found := false
	for _, network := range updated.Networks() {
		for _, asset := range network.Assets {
			if network.ID == account.Network && asset.Contract == contract {
				found = asset.Symbol == "TEST" && asset.Decimals == 6 && asset.ID == account.Network+":erc20:"+contract
			}
		}
	}
	if !found {
		t.Fatal("successful import did not refresh the approved network assets")
	}
	if _, err := c.handle(message("importToken", request)); err == nil {
		t.Fatal("duplicate imported policy was silently re-approved")
	}
	for _, body := range []string{`null`, `{}`, `{"accountId":null,"contract":"x"}`, `{"accountId":"x","contract":"x","decimals":18}`} {
		m := message("importToken", nil)
		m.Body = []byte(body)
		if _, err := c.handle(m); err == nil {
			t.Fatal("public caller supplied private token policy metadata or malformed input")
		}
	}
	fail = true
	if _, err := c.handle(message("importToken", map[string]string{"accountId": account.ID, "contract": "0x3333333333333333333333333333333333333333"})); err == nil {
		t.Fatal("unverified failed RPC metadata reached signer authorization")
	}
	if _, _, _, err := c.exchangeAsset(account.ID, account.Network+":erc20:"+contract); err == nil {
		t.Fatal("custom token became an unreviewed Bybit route")
	}
	fail = false
	state = readPublicState(t, statePath)
	state["reject"] = []string{"settings"}
	writePublicState(t, statePath, state)
	if _, err := c.handle(message("importToken", map[string]string{"accountId": account.ID, "contract": "0x3333333333333333333333333333333333333333"})); err == nil {
		t.Fatal("failed policy refresh was presented as complete")
	}
}

func TestTokenImportRejectsForeignAccountAndNonEthereumAdapter(t *testing.T) {
	c, statePath := fakeCoordinator(t, &publicAdapter{}, nil)
	for _, accountID := range []string{"missing", publicAccount().ID} {
		if _, err := c.handle(message("importToken", map[string]string{"accountId": accountID, "contract": "0x1234567890123456789012345678901234567890"})); err == nil {
			t.Fatal("foreign/non-Ethereum account authorized ERC-20 metadata import")
		}
	}
	state := readPublicState(t, statePath)
	account := publicAccount()
	account.Network = "unsupported"
	state["accounts"] = []model.Account{account}
	writePublicState(t, statePath, state)
	if _, err := c.handle(message("importToken", map[string]string{"accountId": account.ID, "contract": "0x1234567890123456789012345678901234567890"})); err == nil {
		t.Fatal("unavailable account adapter bypassed import network checks")
	}
}
