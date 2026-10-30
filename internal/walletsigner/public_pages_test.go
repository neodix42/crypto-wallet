package walletsigner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"localwallet/internal/chains/evm"
	"localwallet/internal/derivation"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/signer"
	"localwallet/internal/vault"
)

func TestPrivatePagedDispatchUnlocksLargeJournalAndRetainsExactArtifacts(t *testing.T) {
	files, err := vault.NewFiles(filepath.Join(t.TempDir(), "profile"))
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	entropy, err := derivation.Import([]byte(publicPhrase))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(entropy)
	seed, err := derivation.Seed(entropy, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(seed)
	adapter := evm.New()
	data := vault.Data{Version: 1, WalletID: strings.Repeat("a", 32), Generation: 1, Scheme: "bip39-english-v1", Entropy: entropy, Environment: "testnet", RecoveryConfirmed: true, Endpoints: map[string]vault.Endpoint{}}
	for i := uint32(0); i < 300; i++ {
		account, err := adapter.Derive(seed, i, "ethereum-sepolia")
		if err != nil {
			t.Fatal(err)
		}
		data.Accounts = append(data.Accounts, account)
	}
	for i := 0; i < 300; i++ {
		plan := model.Plan{ID: fmt.Sprintf("PUBLIC-SYNTHETIC-ARCHIVE-%04d", i), Account: data.Accounts[i], Asset: "ethereum-sepolia:native", Recipient: data.Accounts[0].Address, Amount: "1", Memo: strings.Repeat("p", 4096)}
		data.Operations = append(data.Operations, journal.Operation{Plan: plan, Digest: journal.Digest(plan), State: "UNKNOWN", Signed: &model.Signed{Identity: plan.ID, Bytes: []byte{1, 2, 3}}})
	}
	plain, err := data.Encode()
	if err != nil {
		t.Fatal(err)
	}
	defer clear(plain)
	if len(plain) <= 1<<20 {
		t.Fatal("fixture did not exceed old IPC frame")
	}
	ciphertext, err := vault.Seal(plain, []byte(publicPassword), vault.Parameters{Memory: 64 * 1024, Passes: 3, Lanes: 4})
	if err != nil {
		t.Fatal(err)
	}
	if err = files.Write("vault.lwv", ciphertext, false); err != nil {
		t.Fatal(err)
	}
	ui := &dispatchUI{approve: true, inputs: [][][]byte{{[]byte(publicPassword)}}}
	engine := signer.New(files, ui, []model.Adapter{adapter})
	engine.Environment = "testnet"
	defer engine.Lock()
	raw := dispatch(t, engine, "unlock", struct{}{})
	if len(raw) > signer.PublicPageBytes || bytes.Contains(raw, []byte(`"signed"`)) {
		t.Fatal("unlock still returns whole archive")
	}
	var first signer.SnapshotPage
	if json.Unmarshal(raw, &first) != nil || len(first.Accounts) != 128 || first.TotalAccounts != 600 || first.TotalOperations != 300 || first.AccountsCursor == "" {
		t.Fatal("bounded unlock omitted metadata including 300 migrated Arbitrum accounts")
	}
	count := len(first.Accounts)
	cursor := first.AccountsCursor
	for cursor != "" {
		var page signer.SnapshotPage
		raw = dispatch(t, engine, "accountPage", map[string]string{"cursor": cursor})
		if json.Unmarshal(raw, &page) != nil {
			t.Fatal("account page codec")
		}
		count += len(page.Accounts)
		cursor = page.AccountsCursor
	}
	if count != 600 {
		t.Fatal("account pages truncated", count)
	}
	var account model.Account
	if json.Unmarshal(dispatch(t, engine, "account", map[string]string{"accountId": data.Accounts[299].ID}), &account) != nil || account.ID != data.Accounts[299].ID {
		t.Fatal("late typed account unavailable")
	}
	seen := map[string]bool{}
	cursor = ""
	for {
		var page signer.JournalPage
		raw = dispatch(t, engine, "journalPage", map[string]string{"accountId": "", "asset": "", "cursor": cursor})
		if json.Unmarshal(raw, &page) != nil || len(raw) > signer.PublicPageBytes || bytes.Contains(raw, []byte(`"signed"`)) {
			t.Fatal("journal page exposed artifact or exceeded bound")
		}
		for _, op := range page.Operations {
			if seen[op.Plan.ID] {
				t.Fatal("duplicate operation")
			}
			seen[op.Plan.ID] = true
		}
		cursor = page.NextCursor
		if cursor == "" {
			if !page.Complete {
				t.Fatal("partial completion")
			}
			break
		}
	}
	if len(seen) != 300 {
		t.Fatal("large journal truncated", len(seen))
	}
	var exact journal.Operation
	if json.Unmarshal(dispatch(t, engine, "operation", map[string]string{"id": data.Operations[299].Plan.ID}), &exact) != nil || exact.Signed == nil || !bytes.Equal(exact.Signed.Bytes, []byte{1, 2, 3}) {
		t.Fatal("exact original signed artifact lost")
	}
	// Retained legacy private pending enumeration fails explicitly on oversize;
	// the live wallet retrieves one exact operation instead of losing the pipe.
	denied(t, engine, "pending", struct{}{})
	for method, body := range map[string]any{"accountPage": map[string]any{"cursor": "bad", "extra": true}, "account": map[string]any{"accountId": "x", "seed": "forbidden"}, "journalPage": map[string]any{"accountId": "", "asset": "", "cursor": "", "extra": true}, "operation": map[string]any{"id": "x", "bytes": "forbidden"}} {
		denied(t, engine, method, body)
	}
	for method, body := range map[string]any{"accountPage": map[string]string{"cursor": "bad"}, "account": map[string]string{"accountId": "missing"}, "operation": map[string]string{"id": "missing"}} {
		denied(t, engine, method, body)
	}
}

func TestPrivatePageDispatchRejectsNullIdentifierFields(t *testing.T) {
	engine, _ := dispatchEngine(t, "testnet", false)
	for method, bodies := range map[string][]string{
		"accountPage": {`{"cursor":null}`},
		"account":     {`{"accountId":null}`},
		"operation":   {`{"id":null}`},
		"journalPage": {`{"accountId":null,"asset":"","cursor":""}`, `{"accountId":"","asset":null,"cursor":""}`, `{"accountId":"","asset":"","cursor":null}`},
	} {
		for _, body := range bodies {
			request := request(method, struct{}{})
			request.Body = []byte(body)
			if _, err := handle(engine, request, "testnet"); err == nil {
				t.Fatal("null private page field accepted", method)
			}
		}
	}
}
