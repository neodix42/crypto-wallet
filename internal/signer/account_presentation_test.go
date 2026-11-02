package signer

import (
	"bytes"
	"encoding/json"
	"errors"
	"localwallet/internal/chains/evm"
	"localwallet/internal/derivation"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/uiprompt"
	"reflect"
	"strings"
	"testing"
)

func TestAccountPresentationPreservesActualSignedUnknownAcrossEncryptedRestart(t *testing.T) {
	e, u, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, u, "testnet")
	var account model.Account
	for _, a := range e.Snapshot().Accounts {
		if a.Network == "ethereum-sepolia" {
			account = a
		}
	}
	p := evmPlan(account)
	if err := e.Prepare(p); err != nil {
		t.Fatal(err)
	}
	u.inputs = append(u.inputs, input(syntheticPassword))
	signed, err := e.Sign(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Update(p.ID, "SUBMITTING", nil); err != nil {
		t.Fatal(err)
	}
	if err = e.Update(p.ID, "UNKNOWN", &model.Receipt{Identity: signed.Identity, State: "UNKNOWN"}); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(e.data.Operations)
	root := bytes.Clone(e.data.Entropy)
	defer clear(root)
	if e.RenameWallet(account.ID, "Savings ETH") != nil || e.RenameGroup(0, "Long term") != nil || e.DeleteWallet(account.ID) != nil || e.DeleteGroup(0) != nil {
		t.Fatal("presentation mutation failed")
	}
	after, _ := json.Marshal(e.data.Operations)
	if !bytes.Equal(before, after) || !bytes.Equal(root, e.data.Entropy) {
		t.Fatal("local metadata altered signed operations or recovery root")
	}
	if !journal.Conflicts(e.data.Operations, p) || !e.data.AccountPresentation.DeletedGroups["0"] {
		t.Fatal("archiving released reservation or group marker")
	}
	e.Lock()
	u.inputs = append(u.inputs, input(syntheticPassword))
	if err = e.Unlock(); err != nil {
		t.Fatal(err)
	}
	after, _ = json.Marshal(e.data.Operations)
	if !bytes.Equal(before, after) || e.data.AccountPresentation.WalletLabels[account.ID] != "Savings ETH" || e.data.AccountPresentation.GroupLabels["0"] != "Long term" || !e.data.AccountPresentation.DeletedGroups["0"] {
		t.Fatal("encrypted restart lost label/archive/original transaction")
	}
	op := e.data.Operations[0]
	if op.State != "UNKNOWN" || op.Digest != journal.Digest(p) || !bytes.Equal(op.Signed.Bytes, signed.Bytes) || op.Signed.Identity != signed.Identity {
		t.Fatal("original signed identity replaced")
	}
	if e.HideAccount(account.ID, false) != nil || e.data.AccountPresentation.DeletedGroups["0"] {
		t.Fatal("unhide did not restore group marker")
	}
	if !bytes.Equal(root, e.data.Entropy) || !journal.Conflicts(e.data.Operations, p) {
		t.Fatal("restoring visibility changed authority")
	}
	public, _ := json.Marshal(e.Snapshot())
	if bytes.Contains(public, []byte(`"bytes"`)) {
		t.Fatal("presentation export leaked signed transaction bytes")
	}
}

func TestAddNextAccountUsesLatestStoredIndexWithoutReusingArchivedHoles(t *testing.T) {
	e, u, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, u, "testnet")
	if e.AddAccount(5) != nil || e.DeleteGroup(5) != nil {
		t.Fatal("seed noncontiguous hidden group")
	}
	if e.AddNextAccount() != nil {
		t.Fatal("automatic next account")
	}
	indices := map[uint32]bool{}
	for _, a := range e.data.Accounts {
		indices[a.Index] = true
	}
	if !reflect.DeepEqual(indices, map[uint32]bool{0: true, 5: true, 6: true}) {
		t.Fatal("next reused a hole", indices)
	}
	if !strings.Contains(u.summaries[len(u.summaries)-1], "index 6") {
		t.Fatal("next index absent from confirmation")
	}

	// Retired-only groups are not exported as active wallets, but remain reserved
	// derivation indices when selecting the next account.
	legacyDecredAccount(t, e, "decred-testnet3", 12)
	page, err := e.SnapshotPage("")
	if err != nil || page.NextAccountIndex == nil || *page.NextAccountIndex != 13 {
		t.Fatal("retired-only index omitted from authoritative next index", err)
	}
	for _, a := range page.Accounts {
		if a.Index == 12 {
			t.Fatal("retired wallet reintroduced into active snapshot")
		}
	}
	if e.AddNextAccount() != nil {
		t.Fatal("next account after retired index")
	}
	if e.data.Accounts[len(e.data.Accounts)-1].Index != 13 {
		t.Fatal("retired index was reused")
	}
	// A hidden last valid index still prevents wrapping or index reuse.
	e.data.Accounts[len(e.data.Accounts)-1].Index = derivation.MaxIndex - 1
	if e.AddNextAccount() == nil {
		t.Fatal("automatic index wrapped/exceeded derivation bound")
	}
	bounded, err := e.SnapshotPage("")
	if err != nil || bounded.NextAccountIndex != nil {
		t.Fatal("exhausted index incorrectly advertised", err)
	}
}

type presentationHookUI struct {
	*testUI
	hook func()
}

func (u *presentationHookUI) Confirm(title, summary string) bool {
	if u.hook != nil {
		u.hook()
	}
	return u.testUI.Confirm(title, summary)
}
func TestPresentationCancellationForeignNamesAndChangedGenerationCannotMutate(t *testing.T) {
	e, u, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, u, "testnet")
	id := e.data.Accounts[0].ID
	calls := []func() error{func() error { return e.RenameWallet(id, "new") }, func() error { return e.RenameGroup(0, "new") }, func() error { return e.DeleteWallet(id) }, func() error { return e.DeleteGroup(0) }, e.AddNextAccount}
	u.approve = false
	before, _ := json.Marshal(e.data)
	for _, call := range calls {
		if err := call(); !errors.Is(err, uiprompt.ErrCancelled) {
			t.Fatal("declined presentation confirmation lost cancellation category", err)
		}
	}
	after, _ := json.Marshal(e.data)
	if !bytes.Equal(before, after) {
		t.Fatal("cancel changed encrypted state")
	}
	u.approve = true
	n := len(u.summaries)
	for _, bad := range []string{"", " name", "a\nNetwork: wrong", "a\u202eb", strings.Repeat("x", 81)} {
		if e.RenameWallet(id, bad) == nil || e.RenameGroup(0, bad) == nil {
			t.Fatal("unsafe name accepted")
		}
	}
	if e.RenameWallet("foreign", "name") == nil || e.RenameGroup(999, "name") == nil || e.DeleteWallet("foreign") == nil || e.DeleteGroup(999) == nil || len(u.summaries) != n {
		t.Fatal("foreign/invalid target reached confirmation")
	}
	accountsBefore, _ := json.Marshal(e.data.Accounts)
	for _, call := range calls {
		e.UI = &presentationHookUI{testUI: u, hook: func() { e.data.Generation++ }}
		if err := call(); !errors.Is(err, ErrDenied) || errors.Is(err, uiprompt.ErrCancelled) {
			t.Fatal("stale confirmation lost denial category", err)
		}
		if !e.data.AccountPresentation.IsZero() {
			t.Fatal("generation change accepted metadata")
		}
		accountsAfter, _ := json.Marshal(e.data.Accounts)
		if !bytes.Equal(accountsBefore, accountsAfter) {
			t.Fatal("stale confirmation changed visibility or derived accounts")
		}
	}
	// A prompt returning false because its session changed must remain a
	// failure; only a decline in the original live session is cancellation.
	u.approve = false
	for _, call := range calls {
		e.UI = &presentationHookUI{testUI: u, hook: func() { e.data.Generation++ }}
		if err := call(); !errors.Is(err, ErrDenied) || errors.Is(err, uiprompt.ErrCancelled) {
			t.Fatal("stale cancelled prompt was reported as an ordinary decline", err)
		}
	}
	e.UI = &presentationHookUI{testUI: u, hook: e.Lock}
	if err := e.RenameWallet(id, "new"); !errors.Is(err, ErrDenied) || errors.Is(err, uiprompt.ErrCancelled) || e.data != nil {
		t.Fatal("lock during prompt was accepted or misreported as cancellation", err)
	}
	for _, call := range calls {
		if err := call(); !errors.Is(err, ErrDenied) || errors.Is(err, uiprompt.ErrCancelled) {
			t.Fatal("locked metadata request was reported as an ordinary decline", err)
		}
	}
}

func TestPresentationPagesStayBoundedScopedAndInvalidateOldCursors(t *testing.T) {
	e := publicPagingFixture(t, 260, 1)
	e.data.AccountPresentation = model.AccountPresentation{WalletLabels: map[string]string{}, GroupLabels: map[string]string{}, DeletedGroups: map[string]bool{}}
	for _, a := range e.data.Accounts {
		e.data.AccountPresentation.WalletLabels[a.ID] = strings.Repeat("w", 80)
		e.data.AccountPresentation.GroupLabels[model.AccountGroupKey(a.Index)] = strings.Repeat("g", 80)
		if a.Index%2 == 0 {
			e.data.AccountPresentation.DeletedGroups[model.AccountGroupKey(a.Index)] = true
		}
	}
	cursor := ""
	seen := map[string]bool{}
	firstCursor := ""
	for {
		page, err := e.SnapshotPage(cursor)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(page)
		if len(raw) > PublicPageBytes || len(page.Accounts) > PublicPageRecords || !page.Presentation.ValidFor(page.Accounts) {
			t.Fatal("unbounded/foreign presentation page")
		}
		for _, a := range page.Accounts {
			if seen[a.ID] || page.Presentation.WalletLabels[a.ID] != strings.Repeat("w", 80) {
				t.Fatal("metadata page lost/doubled wallet", a.ID)
			}
			seen[a.ID] = true
		}
		if firstCursor == "" {
			firstCursor = page.AccountsCursor
		}
		if page.AccountsCursor == "" {
			break
		}
		cursor = page.AccountsCursor
	}
	if len(seen) != 260 {
		t.Fatal("presentation lost accounts", len(seen))
	}
	e.data.Generation++
	if _, err := e.SnapshotPage(firstCursor); err == nil {
		t.Fatal("stale metadata cursor accepted")
	}
	e.Lock()
	page, err := e.SnapshotPage("")
	if err != nil || !page.Presentation.IsZero() {
		t.Fatal("locked page retained names")
	}
}
