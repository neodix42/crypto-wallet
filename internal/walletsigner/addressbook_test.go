package walletsigner

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"localwallet/internal/model"
)

func TestAddressBookIPCUsesOwnedNetworkAndPersistsNamedDestinationWithoutPrivatePrompt(t *testing.T) {
	engine, ui := dispatchEngine(t, "testnet", true)
	unlockFixture(t, engine, ui)
	account := accountFor(t, engine, "evm")
	inputs, confirmations := ui.inputCalls, ui.confirmCalls
	raw := dispatch(t, engine, "saveAddress", map[string]string{"accountId": account.ID, "name": "IPC public recipient", "address": account.Address})
	var entry model.AddressBookEntry
	if json.Unmarshal(raw, &entry) != nil || entry.ID == "" || entry.AccountID != account.ID || entry.Network != account.Network {
		t.Fatal("signer did not assign exact owned network")
	}
	var entries []model.AddressBookEntry
	if json.Unmarshal(dispatch(t, engine, "addressBook", map[string]string{"accountId": account.ID}), &entries) != nil || len(entries) != 1 || entries[0] != entry {
		t.Fatal("scoped saved recipient not available")
	}
	if ui.inputCalls != inputs || ui.confirmCalls != confirmations {
		t.Fatal("public contact mutation asked for unnecessary private authority")
	}
	dispatch(t, engine, "lock", struct{}{})
	if out, err := handle(engine, request("addressBook", map[string]string{"accountId": account.ID}), "testnet"); err == nil || out != nil {
		t.Fatal("locked contact metadata escaped")
	}
	unlockFixture(t, engine, ui)
	if json.Unmarshal(dispatch(t, engine, "addressBook", map[string]string{"accountId": account.ID}), &entries) != nil || len(entries) != 1 || entries[0] != entry {
		t.Fatal("IPC reload lost recipient")
	}
	dispatch(t, engine, "removeAddress", map[string]string{"accountId": account.ID, "id": entry.ID})
	if string(dispatch(t, engine, "addressBook", map[string]string{"accountId": account.ID})) != "[]" {
		t.Fatal("deleted recipient still returned")
	}
}

func TestAddressBookIPCRejectsAmbiguousFieldsBeforePromptOrPersistence(t *testing.T) {
	engine, ui := dispatchEngine(t, "testnet", true)
	unlockFixture(t, engine, ui)
	account := accountFor(t, engine, "evm")
	base, _ := json.Marshal(map[string]string{"accountId": account.ID, "name": "Recipient", "address": account.Address})
	before, err := engine.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	inputs, confirmations := ui.inputCalls, ui.confirmCalls
	for method, bodies := range map[string][]string{
		"addressBook": {`{}`, `{"accountId":null}`, `{"accountId":"foreign"}`, `{"accountId":"` + account.ID + `","network":"ethereum-mainnet"}`},
		"saveAddress": {`{}`, `{"accountId":null}`, `{"accountId":"foreign","name":"Name","address":"` + account.Address + `"}`,
			strings.Replace(string(base), `"name":"Recipient"`, `"name":null`, 1),
			strings.Replace(string(base), `"name":"Recipient"`, `"name":"Recipient","name":"Other"`, 1),
			strings.Replace(string(base), `"name":"Recipient"`, `"name":"Recipient","network":"ethereum-mainnet"`, 1),
			strings.Replace(string(base), `"name":"Recipient"`, `"name":"Recipient","memo":null`, 1),
			strings.Replace(string(base), `"name":"Recipient"`, `"name":"Recipient","id":"`+strings.Repeat("a", 32)+`"`, 1)},
		"removeAddress": {`{}`, `{"accountId":"` + account.ID + `","id":null}`, `{"accountId":"` + account.ID + `","id":"` + strings.Repeat("a", 32) + `"}`, `{"accountId":"` + account.ID + `","id":"a","network":"ethereum-sepolia"}`},
	} {
		bodies = append(bodies, "null", "[]", "", string(base)+"{}")
		for _, body := range bodies {
			m := request(method, struct{}{})
			m.Body = []byte(body)
			if out, err := handle(engine, m, "testnet"); err == nil || out != nil {
				t.Fatal("ambiguous contact request accepted", method, body)
			}
		}
	}
	after, err := engine.Files.Read("vault.lwv")
	if err != nil || !bytes.Equal(before, after) || ui.inputCalls != inputs || ui.confirmCalls != confirmations {
		t.Fatal("invalid contact request modified storage or prompted")
	}
}
