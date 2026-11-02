package nativewallet

import (
	"context"
	"errors"
	"strings"
	"testing"

	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/uiprompt"
)

func TestPendingInboxFindsLateOperationsOnceAndLaterLeavesEveryOriginalUnchanged(t *testing.T) {
	a, ui, core := fixture(t)
	original := pendingFixture(t, a, "UNKNOWN")
	a.active = "group/0"
	terminal := original
	terminal.Plan.ID = "historical-terminal"
	terminal.State = "CONFIRMED"
	before := viewText(original)
	reads, prompts := 0, 0
	core.handler = func(_ uint64, method string, body, out any) error {
		if method == "exchangeWizardLoad" {
			return answer(out, map[string]any{"wizard": nil})
		}
		if method != "history" {
			t.Error("inbox requested payment authority", method)
			return errors.New("unexpected mutation")
		}
		reads++
		q := body.(map[string]string)
		if q["accountId"] != "" && q["accountId"] != original.Plan.Account.ID {
			t.Error("foreign query")
		}
		if q["accountId"] == "" && q["cursor"] == "" {
			return answer(out, journalPage{Operations: []journal.Operation{terminal}, Total: 2, Revision: "journal-1", NextCursor: strings.Repeat("b", 32)})
		}
		return answer(out, journalPage{Operations: []journal.Operation{original}, Total: map[bool]int{true: 2, false: 1}[q["accountId"] == ""], Revision: "journal-1", Complete: true})
	}
	ui.prompt = func(p uiprompt.Prompt) (uiprompt.Reply, error) {
		prompts++
		if p.Kind != "pending" || len(p.Choices) != 1 || p.Choices[0].ID == original.Plan.ID || !strings.Contains(p.Choices[0].Summary, original.Plan.ID) || !strings.Contains(p.Choices[0].Summary, "1 ETH") {
			t.Error("pending dialog omitted original or leaked operation authority")
		}
		return uiprompt.Reply{PromptID: p.ID, Accepted: false}, nil
	}
	a.queuePendingPayments("")
	if !a.nextPendingPayments() {
		t.Fatal("unlock inbox not queued")
	}
	ui.apply(t)
	if prompts != 1 || reads != 2 || a.busy || a.background || viewText(original) != before {
		t.Fatal("Later changed original or hid late-page operation", reads, prompts)
	}
	a.queuePendingPayments(original.Plan.Account.ID)
	if !a.nextPendingPayments() {
		t.Fatal("wallet-open check missing")
	}
	ui.apply(t)
	if prompts != 1 {
		t.Fatal("unchanged dismissed operation nagged on wallet open")
	}
	a.state.Revision = "unrelated-metadata-revision"
	a.queuePendingPayments("")
	a.nextPendingPayments()
	ui.apply(t)
	if prompts != 1 {
		t.Fatal("metadata-only mutation reopened unchanged pending operations")
	}
	original.State = "SUBMITTED"
	a.queuePendingPayments(original.Plan.Account.ID)
	a.nextPendingPayments()
	ui.apply(t)
	if prompts != 2 {
		t.Fatal("changed pending state stayed hidden")
	}
}

func TestPendingInboxSelectionCannotSubstituteAnotherOriginal(t *testing.T) {
	a, ui, core := fixture(t)
	original := pendingFixture(t, a, "UNKNOWN")
	calls := []string{}
	core.handler = func(_ uint64, method string, body, out any) error {
		calls = append(calls, method)
		switch method {
		case "exchangeWizardLoad":
			return answer(out, map[string]any{"wizard": nil})
		case "history":
			return answer(out, journalPage{Operations: []journal.Operation{original}, Total: 1, Revision: "one", Complete: true})
		case "pendingPayment":
			replacement := original
			replacement.Plan.ID = "another-original"
			return answer(out, pendingPaymentResponse{Operation: &replacement})
		default:
			return errors.New("unexpected payment authority")
		}
	}
	ui.prompt = func(p uiprompt.Prompt) (uiprompt.Reply, error) {
		if p.Kind != "pending" {
			t.Fatal("selected original opened another approval")
		}
		return uiprompt.Reply{PromptID: p.ID, Accepted: true, Values: []string{p.Choices[0].ID}}, nil
	}
	a.queuePendingPayments("")
	a.nextPendingPayments()
	ui.apply(t)
	ui.apply(t)
	if strings.Join(calls, ",") != "history,exchangeWizardLoad,pendingPayment" || !strings.Contains(a.status, "could not be verified") {
		t.Fatal("stale selection substituted original", calls, a.status)
	}
}

func TestPendingInboxPagingShowsAllOriginalsWithoutActionsUntilSelection(t *testing.T) {
	a, ui, _ := fixture(t)
	base := pendingFixture(t, a, "UNKNOWN")
	ops := make([]journal.Operation, 17)
	for i := range ops {
		ops[i] = base
		ops[i].Plan.ID = string(rune('a'+i)) + "-original"
	}
	pages := 0
	ui.prompt = func(p uiprompt.Prompt) (uiprompt.Reply, error) {
		pages++
		if pages == 1 {
			if len(p.Choices) != 17 || !strings.Contains(p.Summary, "1–16") {
				t.Fatal("unbounded/missing first page")
			}
			return uiprompt.Reply{PromptID: p.ID, Accepted: true, Values: []string{p.Choices[16].ID}}, nil
		}
		if len(p.Choices) != 2 || !strings.Contains(p.Choices[0].Summary, ops[16].Plan.ID) {
			t.Fatal("late pending original missing")
		}
		return uiprompt.Reply{PromptID: p.ID, Accepted: false}, nil
	}
	a.showPendingOperations(ops)
	if pages != 2 || len(a.pendingInbox.Seen) != 17 || a.busy {
		t.Fatal("pending pages lost/did not leave originals unchanged")
	}
}

func TestPendingInboxRejectsMalformedIncompleteSignedAndForeignPages(t *testing.T) {
	for _, mode := range []string{"signed", "duplicate", "missing-account", "revision", "count", "cursor-cycle", "state", "payload", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			a, _, core := fixture(t)
			op := pendingFixture(t, a, "UNKNOWN")
			calls := 0
			core.handler = func(_ uint64, method string, body, out any) error {
				calls++
				p := journalPage{Operations: []journal.Operation{op}, Total: 1, Revision: "one", Complete: true}
				switch mode {
				case "signed":
					p.Operations[0].Signed = &model.Signed{Bytes: []byte("never-public")}
				case "duplicate":
					p.Operations = append(p.Operations, op)
					p.Total = 2
				case "missing-account":
					p.Operations[0].Plan.Account.ID = ""
				case "revision":
					p.Total = 2
					p.Complete = false
					p.NextCursor = strings.Repeat("b", 32)
					if calls > 1 {
						p.Revision = "changed"
					}
				case "count":
					p.Total = 2
				case "cursor-cycle":
					p.Total = 10
					p.Complete = false
					p.NextCursor = strings.Repeat("b", 32)
					p.Operations[0].Plan.ID += strings.Repeat("x", calls)
				case "state":
					p.Operations[0].State = "private-provider-canary"
				case "payload":
					p.Operations[0].Plan.Payload = []byte(`"null"`)
				case "oversized":
					p.Operations[0].Plan.Memo = strings.Repeat("x", 4097)
				}
				return answer(out, p)
			}
			if _, err := readPendingOperations(context.Background(), core, core.Generation(), op.Plan.Account.ID); err == nil {
				t.Fatal("invalid inbox accepted")
			}
		})
	}
}

func TestPendingChoiceRejectsStaleForgedAndCancelledReplies(t *testing.T) {
	for _, mode := range []string{"valid", "later", "forged", "lock", "generation", "revision", "prompt-error"} {
		t.Run(mode, func(t *testing.T) {
			a, ui, core := fixture(t)
			choice, _ := newPendingChoice("Original UNKNOWN", "Public original amount/address")
			ui.prompt = func(p uiprompt.Prompt) (uiprompt.Reply, error) {
				r := uiprompt.Reply{PromptID: p.ID, Accepted: true, Values: []string{choice.ID}}
				switch mode {
				case "later":
					r.Accepted = false
					r.Values = nil
				case "forged":
					r.Values = []string{strings.Repeat("f", 32)}
				case "lock":
					a.lock()
				case "generation":
					core.generation.Add(1)
				case "revision":
					a.privateRevision.Add(1)
				case "prompt-error":
					return r, errors.New("private-canary")
				}
				return r, nil
			}
			_, accepted, displayed := a.pendingChoiceDialog("Pending operations", "Originals", []uiprompt.PendingChoice{choice})
			if accepted != (mode == "valid") || displayed != (mode == "valid" || mode == "later") {
				t.Fatal("stale/foreign selection acquired authority", accepted, displayed)
			}
		})
	}
}

func TestPendingInboxGlobalQueryDoesNotRevokeWalletLocalContinuation(t *testing.T) {
	a, _, core := fixture(t)
	op := pendingFixture(t, a, "UNKNOWN")
	other := op
	other.Plan.ID = "other-owned-original"
	other.Plan.Account = a.state.Accounts[0]
	if other.Plan.Account.ID == op.Plan.Account.ID {
		other.Plan.Account.ID = "other-public-owner"
	}
	localCursor := strings.Repeat("d", 32)
	a.walletHistory = map[string]walletHistoryState{op.Plan.Account.ID: {Loaded: true, Local: journalPage{NextCursor: localCursor}}}
	core.handler = func(_ uint64, method string, body, out any) error {
		q := body.(map[string]string)
		if method != "history" || q["accountId"] != "" {
			t.Fatal("pending scan entered wallet-table cursor namespace")
		}
		return answer(out, journalPage{Operations: []journal.Operation{op, other}, Total: 2, Revision: "one", Complete: true})
	}
	operations, err := readPendingOperations(context.Background(), core, core.Generation(), op.Plan.Account.ID)
	if err != nil || len(operations) != 1 || operations[0].Plan.ID != op.Plan.ID || a.walletHistory[op.Plan.Account.ID].Local.NextCursor != localCursor {
		t.Fatal("pending query mixed account effects or revoked local continuation", err)
	}
	a.pendingInbox.Seen = map[string][32]byte{op.Plan.ID: pendingOperationFingerprint(op), other.Plan.ID: pendingOperationFingerprint(other)}
	a.pendingInbox.SeenOwners = map[string]string{op.Plan.ID: op.Plan.Account.ID, other.Plan.ID: other.Plan.Account.ID}
	a.forcePendingPayments(op.Plan.Account.ID)
	if _, ok := a.pendingInbox.Seen[op.Plan.ID]; ok {
		t.Fatal("explicit check remained hidden after Later")
	}
	if _, ok := a.pendingInbox.Seen[other.Plan.ID]; !ok {
		t.Fatal("explicit wallet check revoked another wallet's Later acknowledgement")
	}
}

func TestPendingInboxStaleWorkerNeverOpensDialogOrStealsForeground(t *testing.T) {
	for _, mode := range []string{"locked", "closed", "generation", "foreground"} {
		t.Run(mode, func(t *testing.T) {
			a, ui, core := fixture(t)
			op := pendingFixture(t, a, "UNKNOWN")
			started, release := make(chan struct{}), make(chan struct{})
			core.handler = func(_ uint64, method string, body, out any) error {
				if method == "exchangeWizardLoad" {
					return answer(out, map[string]any{"wizard": nil})
				}
				if method != "history" {
					return errors.New("unexpected")
				}
				close(started)
				<-release
				return answer(out, journalPage{Operations: []journal.Operation{op}, Total: 1, Revision: "one", Complete: true})
			}
			prompts := 0
			ui.prompt = func(p uiprompt.Prompt) (uiprompt.Reply, error) {
				prompts++
				return uiprompt.Reply{PromptID: p.ID, Accepted: false}, nil
			}
			a.queuePendingPayments("")
			a.nextPendingPayments()
			<-started
			switch mode {
			case "locked":
				a.lock()
			case "closed":
				a.close()
			case "generation":
				core.generation.Add(1)
			case "foreground":
				a.busy = true
			}
			close(release)
			ui.apply(t)
			if prompts != 0 {
				t.Fatal("stale/background worker opened a dialog")
			}
			if mode == "foreground" && (!a.busy || a.pendingInbox.Request != "*" || len(a.pendingInbox.Seen) != 0) {
				t.Fatal("pending callback stole foreground or marked an unseen original as shown")
			}
		})
	}
}

func TestPausedPendingInboxCannotClearNewBackgroundOwnerOrDismissalState(t *testing.T) {
	a, ui, core := fixture(t)
	op := pendingFixture(t, a, "UNKNOWN")
	a.pendingInbox.Seen = map[string][32]byte{op.Plan.ID: pendingOperationFingerprint(op)}
	a.pendingInbox.SeenOwners = map[string]string{op.Plan.ID: op.Plan.Account.ID}
	started, release := make(chan struct{}), make(chan struct{})
	core.handler = func(_ uint64, method string, body, out any) error {
		if method == "exchangeWizardLoad" {
			return answer(out, map[string]any{"wizard": nil})
		}
		close(started)
		<-release
		return answer(out, journalPage{Operations: []journal.Operation{op}, Total: 1, Revision: "one", Complete: true})
	}
	ui.prompt = func(uiprompt.Prompt) (uiprompt.Reply, error) {
		t.Fatal("paused scan reopened pending dialog")
		return uiprompt.Reply{}, nil
	}
	a.queuePendingPayments("")
	a.nextPendingPayments()
	<-started
	a.pausePendingPayments()
	if a.pendingInbox.Loading || a.background || a.pendingInbox.Request != "*" || len(a.pendingInbox.Seen) != 1 {
		t.Fatal("pause lost ownership, requeue or Later acknowledgement")
	}
	a.background = true
	close(release)
	ui.apply(t)
	if !a.background || a.pendingInbox.Loading || a.pendingInbox.Request != "*" || len(a.pendingInbox.Seen) != 1 {
		t.Fatal("old pending callback cleared the new worker or dedupe state")
	}
}

func TestSubmittedSendConflictShowsOriginalDialogBeforeStatusRead(t *testing.T) {
	for _, accept := range []bool{false, true} {
		t.Run(map[bool]string{false: "Later", true: "Check"}[accept], func(t *testing.T) {
			a, ui, core := fixture(t)
			op := pendingFixture(t, a, "SUBMITTED")
			calls := []string{}
			core.handler = func(_ uint64, method string, body, out any) error {
				calls = append(calls, method)
				switch method {
				case "pendingPayment":
					return answer(out, pendingPaymentResponse{Operation: &op})
				case "reconcile":
					return answer(out, model.Receipt{Identity: op.Receipt.Identity, State: "SUBMITTED"})
				default:
					return errors.New("unexpected payment authority")
				}
			}
			ui.prompt = func(p uiprompt.Prompt) (uiprompt.Reply, error) {
				if p.Kind != "pending" || !strings.Contains(p.Summary, op.Plan.ID) || !strings.Contains(p.Summary, op.Receipt.Identity) {
					t.Fatal("pending conflict lacked an actionable original dialog")
				}
				r := uiprompt.Reply{PromptID: p.ID, Accepted: accept}
				if accept {
					r.Values = []string{p.Choices[0].ID}
				}
				return r, nil
			}
			a.resolvePendingPayment(op.Plan.Account, nil, a.planRevision)
			ui.apply(t)
			if accept {
				ui.apply(t)
			}
			want := "pendingPayment"
			if accept {
				want += ",reconcile"
			}
			if strings.Join(calls, ",") != want || a.plan != nil || op.State != "SUBMITTED" {
				t.Fatal("pending dialog mutated original or queried before consent", calls)
			}
		})
	}
}

func TestSelectedUnsignedOriginalCanBeCancelledWithoutOpeningApprovalOrPreparingReplacement(t *testing.T) {
	for _, accept := range []bool{false, true} {
		t.Run(map[bool]string{false: "Later", true: "Cancel unsigned"}[accept], func(t *testing.T) {
			a, ui, core := fixture(t)
			op := pendingFixture(t, a, "PREPARED")
			before := viewText(op)
			calls := []string{}
			core.handler = func(_ uint64, method string, body, out any) error {
				calls = append(calls, method)
				switch method {
				case "pendingPayment":
					return answer(out, pendingPaymentResponse{Operation: &op})
				case "cancelPrepared":
					if viewText(body) != `{"id":"original-operation"}` {
						t.Fatal("cancel changed original identity")
					}
					return nil
				default:
					t.Error("unsigned cancellation requested signing or replacement", method)
					return errors.New("unexpected authority")
				}
			}
			ui.prompt = func(p uiprompt.Prompt) (uiprompt.Reply, error) {
				if p.Kind != "pending" || p.Title != "Original unsigned payment" || !strings.Contains(p.Summary, op.Plan.ID) {
					t.Fatal("unsigned original missing from action dialog")
				}
				r := uiprompt.Reply{PromptID: p.ID, Accepted: accept}
				if accept {
					for _, choice := range p.Choices {
						if choice.Label == "Cancel unsigned preview" {
							r.Values = []string{choice.ID}
						}
					}
					if len(r.Values) != 1 {
						t.Fatal("unexpired preview has no explicit cancellation")
					}
				}
				return r, nil
			}
			a.resolvePendingPaymentExpected(op.Plan.Account, nil, a.planRevision, op.Plan.ID)
			ui.apply(t)
			if accept {
				ui.apply(t)
			}
			want := "pendingPayment"
			if accept {
				want += ",cancelPrepared"
			}
			if strings.Join(calls, ",") != want || viewText(op) != before {
				t.Fatal("unsigned action altered a signed state or replaced original", calls)
			}
		})
	}
}
