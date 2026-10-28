package walletcore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"localwallet/internal/ipc"
)

func TestAddressBookCoordinatorRejectsInvalidSchemasWithoutStartingSigner(t *testing.T) {
	c, _ := fakeCoordinator(t, &publicAdapter{}, nil)
	c.child.Path = filepath.Join(t.TempDir(), "must-not-run")
	for method, bodies := range map[string][]string{
		"addressBook":   {`{}`, `{"accountId":null}`, `{"accountId":"owned","network":"near-mainnet"}`, `{"accountId":"owned","accountId":"foreign"}`},
		"saveAddress":   {`{}`, `{"accountId":"owned","name":"Contact","address":null}`, `{"accountId":"owned","name":null,"address":"receiver.near"}`, `{"accountId":"owned","name":"Contact","address":"receiver.near","memo":null}`, `{"accountId":"owned","name":"Contact","address":"receiver.near","network":"near-mainnet"}`},
		"removeAddress": {`{}`, `{"accountId":"owned","id":null}`, `{"accountId":"owned","id":"short"}`, `{"accountId":"owned","id":"` + strings.Repeat("a", 32) + `","unexpected":true}`},
	} {
		bodies = append(bodies, "null", "[]", "", `false`)
		for _, body := range bodies {
			if out, err := c.handle(ipc.Message{Method: method, Body: []byte(body)}); err == nil || out != nil {
				t.Fatal("invalid contact schema accepted", method, body)
			}
		}
	}
	if c.child.Generation() != 0 {
		t.Fatal("malformed contact request started signer")
	}
}

func TestAddressBookCoordinatorForwardsOnlyTypedPublicContactData(t *testing.T) {
	c, path := fakeCoordinator(t, &publicAdapter{}, nil)
	fixture := strings.Replace(publicChild, "elif method=='pending':", `elif method in ['addressBook','saveAddress','removeAddress']:
  state['addressBookRequest']=body
  entry={'id':'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','accountId':body['accountId'],'network':'unit-testnet','name':body.get('name','Public recipient'),'address':body.get('address','public-recipient')}
  output=[entry] if method=='addressBook' else entry if method=='saveAddress' else None
 elif method=='pending':`, 1)
	if fixture == publicChild {
		t.Fatal("fixture insertion failed")
	}
	if err := os.WriteFile(c.child.Path, []byte(fixture), 0700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(fixture))
	manifest, _ := json.Marshal(map[string]string{"wallet-signer": hex.EncodeToString(digest[:])})
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "manifest.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	for method, body := range map[string]map[string]string{
		"addressBook":   {"accountId": "owned"},
		"saveAddress":   {"accountId": "owned", "name": "Public recipient", "address": "public-recipient"},
		"removeAddress": {"accountId": "owned", "id": strings.Repeat("a", 32)},
	} {
		out, err := c.handle(message(method, body))
		if err != nil {
			t.Fatal(err)
		}
		if out == nil {
			t.Fatal("public contact response absent")
		}
		state := readPublicState(t, path)
		raw, _ := json.Marshal(state["addressBookRequest"])
		expected, _ := json.Marshal(body)
		if string(raw) != string(expected) {
			t.Fatal("coordinator changed typed account or destination")
		}
	}
}

func TestAddressBookCoordinatorDiscardsCompletionAfterSessionChange(t *testing.T) {
	for _, method := range []string{"addressBook", "saveAddress", "removeAddress"} {
		t.Run(method, func(t *testing.T) {
			c, path := fakeCoordinator(t, &publicAdapter{}, nil)
			state := readPublicState(t, path)
			state["delayMethod"] = method
			writePublicState(t, path, state)
			body := map[string]string{"accountId": "owned"}
			if method == "saveAddress" {
				body["name"] = "Recipient"
				body["address"] = "public-recipient"
			}
			if method == "removeAddress" {
				body["id"] = strings.Repeat("a", 32)
			}
			done := make(chan error, 1)
			go func() {
				out, err := c.handle(message(method, body))
				if err == nil || out != nil {
					done <- errors.New("stale contact completion escaped")
					return
				}
				done <- nil
			}()
			dir := filepath.Dir(path)
			waitPublicFixture(t, filepath.Join(dir, "native-ready"))
			c.mu.Lock()
			c.epoch++
			c.active = false
			c.mu.Unlock()
			if err := os.WriteFile(filepath.Join(dir, "native-release"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAddressBookCoordinatorPreservesOnlyAllowlistedFailureCategories(t *testing.T) {
	c, path := fakeCoordinator(t, &publicAdapter{}, nil)
	fixture := strings.Replace(publicChild, "if error:response['error']=error", "if error:response['error']=state.get('errorOverride',error)", 1)
	if fixture == publicChild {
		t.Fatal("failure fixture insertion failed")
	}
	if err := os.WriteFile(c.child.Path, []byte(fixture), 0700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(fixture))
	manifest, _ := json.Marshal(map[string]string{"wallet-signer": hex.EncodeToString(digest[:])})
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "manifest.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	for _, category := range []string{"ADDRESS_BOOK_INVALID_INPUT", "ADDRESS_BOOK_INVALID_ADDRESS", "ADDRESS_BOOK_DUPLICATE_NAME", "ADDRESS_BOOK_DUPLICATE_RECIPIENT", "ADDRESS_BOOK_LIMIT", "ADDRESS_BOOK_STALE", "ADDRESS_BOOK_UNAVAILABLE", "ADDRESS_BOOK_WRITE_FAILED", "CANARY-private-error-path", "ADDRESS_BOOK_INVALID_INPUT\nCANARY-private-context"} {
		state := readPublicState(t, path)
		state["reject"] = []string{"saveAddress"}
		state["errorOverride"] = category
		writePublicState(t, path, state)
		out, err := c.handle(message("saveAddress", map[string]string{"accountId": "owned", "name": "Public recipient", "address": "public-recipient"}))
		want := category
		if strings.Contains(category, "CANARY") {
			want = "REQUEST_FAILED_STATE_UNKNOWN"
		}
		if err == nil || out != nil || ipc.PublicErrorCode(err) != want || strings.Contains(err.Error(), "CANARY") {
			t.Fatal("coordinator lost safe failure category or relayed private context", err)
		}
	}
	// Invalid public fields fail before child transport with the same fixed
	// input category, rather than pretending the encrypted book is unavailable.
	out, err := c.handle(ipc.Message{Method: "saveAddress", Body: []byte(`{"accountId":"owned","name":null,"address":"CANARY-private-address"}`)})
	if out != nil || ipc.PublicErrorCode(err) != "ADDRESS_BOOK_INVALID_INPUT" || strings.Contains(err.Error(), "CANARY") {
		t.Fatal("public schema failure was not categorized/redacted", err)
	}
}
