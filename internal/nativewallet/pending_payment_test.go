package nativewallet

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/uiprompt"
)

func pendingFixture(t *testing.T, a *App, state string) journal.Operation {
	t.Helper()
	account := ethAccount(t, a)
	request := paymentQuoteRequest{AccountID: account.ID, Asset: a.asset(account).ID, Recipient: quoteRecipient(account), Amount: "1"}
	plan := quotePlan(a, account, request)
	plan.ID = "original-operation"
	op := journal.Operation{Plan: plan, State: state, Digest: journal.Digest(plan)}
	if state != "PREPARED" {
		op.Receipt = &model.Receipt{Identity: "original-transaction", State: state}
	}
	a.active, a.sendForms[account.ID] = "wallet/"+account.ID, true
	return op
}

func TestPendingPaymentConfirmationRequiresCurrentBoundExplicitReply(t *testing.T) {
	for _, mode := range []string{"accepted", "cancelled", "error", "foreign-id", "extra-values", "locked", "generation", "unconfirmed", "closed"} {
		t.Run(mode, func(t *testing.T) {
			a, ui, core := fixture(t)
			ui.prompt = func(p uiprompt.Prompt) (uiprompt.Reply, error) {
				if p.Kind != "confirm" || !uiprompt.ValidID(p.ID) {
					t.Fatal("not an ordinary main-window confirmation")
				}
				r := uiprompt.Reply{PromptID: p.ID, Accepted: true}
				switch mode {
				case "cancelled":
					r.Accepted = false
				case "error":
					return r, errors.New("private-input-canary")
				case "foreign-id":
					r.PromptID = strings.Repeat("f", 32)
				case "extra-values":
					r.Values = []string{"unexpected"}
				case "locked":
					a.lock()
				case "generation":
					core.generation.Add(1)
				case "unconfirmed":
					a.state.RecoveryConfirmed = false
				case "closed":
					a.close()
				}
				return r, nil
			}
			if got := a.confirmPaymentDialog("Original payment", "Exact original effect"); got != (mode == "accepted") {
				t.Fatal("stale/cancelled reply authorized an operation", mode)
			}
		})
	}
}

func TestExpiredUnsignedPaymentCancellationUsesPopupAndOriginalID(t *testing.T) {
	for _, accept := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "confirm"}[accept], func(t *testing.T) {
			a, ui, core := fixture(t)
			op := pendingFixture(t, a, "PREPARED")
			op.Plan.Expires = 1
			account := op.Plan.Account
			calls := []string{}
			core.handler = func(_ uint64, method string, body, out any) error {
				calls = append(calls, method)
				switch method {
				case "pendingPayment":
					return answer(out, struct {
						Operation *journal.Operation `json:"operation"`
					}{&op})
				case "cancelPrepared":
					if viewText(body) != `{"id":"original-operation"}` {
						t.Fatal("cancel replaced original identity")
					}
					return nil
				default:
					t.Fatal("unsigned cancellation prepared, signed or broadcast", method)
					return nil
				}
			}
			ui.prompt = func(p uiprompt.Prompt) (uiprompt.Reply, error) {
				for _, want := range []string{op.Plan.ID, account.Address, op.Plan.Recipient, "1 ETH", "unsigned"} {
					if !strings.Contains(p.Summary, want) {
						t.Fatal("confirmation omitted original effect", want)
					}
				}
				return uiprompt.Reply{PromptID: p.ID, Accepted: accept}, nil
			}
			a.resolvePendingPayment(account, nil, a.planRevision)
			ui.apply(t)
			if accept {
				ui.apply(t)
			}
			want := 1
			if accept {
				want = 2
			}
			if len(calls) != want {
				t.Fatal("cancelled popup mutated original", calls)
			}
		})
	}
}

func TestUnresolvedSignedPaymentBroadcastRequiresPopupAndNeverPreparesReplacement(t *testing.T) {
	for _, mode := range []string{"accept", "cancel", "wrong-identity", "bad-state", "provider-error"} {
		t.Run(mode, func(t *testing.T) {
			a, ui, core := fixture(t)
			op := pendingFixture(t, a, "UNKNOWN")
			account := op.Plan.Account
			calls := []string{}
			prompts := 0
			core.handler = func(_ uint64, method string, body, out any) error {
				calls = append(calls, method)
				if method == "pendingPayment" {
					return answer(out, struct {
						Operation *journal.Operation `json:"operation"`
					}{&op})
				}
				if viewText(body) != `{"id":"original-operation"}` {
					t.Fatal("lost original identity")
				}
				if method == "reconcile" {
					r := model.Receipt{Identity: "original-transaction", State: "UNKNOWN"}
					if mode == "wrong-identity" {
						r.Identity = "another-transaction"
					}
					if mode == "bad-state" {
						r.State = "private-provider-canary"
					}
					if mode == "provider-error" {
						return errors.New("private-provider-canary")
					}
					return answer(out, r)
				}
				if method == "retry" {
					return answer(out, json.RawMessage(`{"identity":"original-transaction","state":"UNKNOWN"}`))
				}
				t.Fatal("rebroadcast attempted new preparation/signature", method)
				return nil
			}
			ui.prompt = func(p uiprompt.Prompt) (uiprompt.Reply, error) {
				prompts++
				for _, want := range []string{"original-operation", "original-transaction", account.Address, op.Plan.Recipient, "1 ETH", "already-approved"} {
					if !strings.Contains(p.Summary, want) {
						t.Fatal("broadcast dialog omitted effect", want)
					}
				}
				return uiprompt.Reply{PromptID: p.ID, Accepted: mode == "accept"}, nil
			}
			a.resolvePendingPayment(account, nil, a.planRevision)
			ui.apply(t)
			ui.apply(t)
			if mode == "accept" {
				ui.apply(t)
				if len(calls) != 3 || calls[2] != "retry" {
					t.Fatal("approved popup did not reuse exact signed original", calls)
				}
			} else if len(calls) != 2 {
				t.Fatal("unconfirmed popup broadcast", calls)
			}
			if mode != "accept" && mode != "cancel" && prompts != 0 {
				t.Fatal("invalid reconciliation offered authority")
			}
			if strings.Contains(viewText(a.view()), "private-provider-canary") {
				t.Fatal("raw provider detail leaked")
			}
		})
	}
}

func TestSignedUnsubmittedPaymentUsesConsentThenOriginalRetryWithoutStandaloneReconcile(t *testing.T) {
	for _, mode := range []string{"accept", "cancel", "locked", "generation", "closed", "foreign-id", "prompt-error"} {
		t.Run(mode, func(t *testing.T) {
			a, ui, core := fixture(t)
			original := pendingFixture(t, a, "SIGNED_NOT_SUBMITTED")
			original.Receipt = nil // A real signature may exist without any receipt.
			account := original.Plan.Account
			before := viewText(original)
			calls := []string{}
			core.handler = func(_ uint64, method string, body, out any) error {
				calls = append(calls, method)
				switch method {
				case "pendingPayment":
					return answer(out, pendingPaymentResponse{Operation: &original})
				case "retry":
					if viewText(body) != `{"id":"original-operation"}` {
						t.Error("signed operation identity was replaced")
					}
					return answer(out, model.Receipt{Identity: "original-transaction", State: "UNKNOWN"})
				default:
					t.Error("signed-unsubmitted dialog reconciled, prepared or signed separately", method)
					return errors.New("unexpected signed-operation mutation")
				}
			}
			ui.prompt = func(p uiprompt.Prompt) (uiprompt.Reply, error) {
				for _, want := range []string{original.Plan.ID, account.Address, original.Plan.Recipient, "1 ETH", "already-approved", "original identity is checked"} {
					if !strings.Contains(strings.ToLower(p.Summary), strings.ToLower(want)) {
						t.Error("signed-operation consent omitted original effect", want)
					}
				}
				reply := uiprompt.Reply{PromptID: p.ID, Accepted: mode != "cancel"}
				switch mode {
				case "locked":
					a.lock()
				case "generation":
					core.generation.Add(1)
				case "closed":
					a.close()
				case "foreign-id":
					reply.PromptID = strings.Repeat("f", 32)
				case "prompt-error":
					return reply, errors.New("private-prompt-canary")
				}
				return reply, nil
			}
			a.resolvePendingPayment(account, nil, a.planRevision)
			ui.apply(t)
			if mode == "accept" {
				ui.apply(t)
				if strings.Join(calls, ",") != "pendingPayment,retry" {
					t.Fatal("confirmed signed original was not sent through checked retry", calls)
				}
			} else if strings.Join(calls, ",") != "pendingPayment" {
				t.Fatal("cancelled or stale consent granted broadcast authority", calls)
			}
			if viewText(original) != before || strings.Contains(viewText(a.view()), "private-prompt-canary") {
				t.Fatal("dialog rewrote the original operation or exposed prompt errors")
			}
		})
	}
}

func TestPendingPaymentRejectsForeignSignedAndTerminalPublicRecords(t *testing.T) {
	for _, mode := range []string{"foreign", "signed-bytes", "terminal"} {
		t.Run(mode, func(t *testing.T) {
			a, ui, core := fixture(t)
			op := pendingFixture(t, a, "PREPARED")
			account := op.Plan.Account
			switch mode {
			case "foreign":
				op.Plan.Account.ID = "foreign"
			case "signed-bytes":
				op.Signed = &model.Signed{Bytes: []byte("must-not-enter-UI")}
			case "terminal":
				op.State = "CONFIRMED"
			}
			core.handler = func(_ uint64, method string, body, out any) error {
				if method != "pendingPayment" {
					t.Fatal("invalid public record requested authority", method)
				}
				return answer(out, struct {
					Operation *journal.Operation `json:"operation"`
				}{&op})
			}
			ui.prompt = func(uiprompt.Prompt) (uiprompt.Reply, error) {
				t.Fatal("invalid original opened popup")
				return uiprompt.Reply{}, nil
			}
			a.resolvePendingPayment(account, nil, a.planRevision)
			ui.apply(t)
			if a.busy || !strings.Contains(a.status, "could not be verified") {
				t.Fatal("invalid record not rejected safely", a.status)
			}
		})
	}
}

func TestExistingValidUnsignedPaymentGoesStraightToSignerConfirmation(t *testing.T) {
	a, ui, core := fixture(t)
	op := pendingFixture(t, a, "PREPARED")
	account := op.Plan.Account
	a.fields["recipient/"+account.ID] = op.Plan.Recipient
	a.fields["amount/"+account.ID] = "1"
	draft := a.paymentDraft(account)
	calls := []string{}
	core.handler = func(_ uint64, method string, body, out any) error {
		calls = append(calls, method)
		switch method {
		case "pendingPayment":
			return answer(out, struct {
				Operation *journal.Operation `json:"operation"`
			}{&op})
		case "send":
			if viewText(body) != `{"id":"original-operation"}` {
				t.Fatal("resumed another operation")
			}
			return answer(out, json.RawMessage(`{"state":"SUBMITTED"}`))
		default:
			t.Fatal("existing preview was replaced", method)
			return nil
		}
	}
	a.resolvePendingPayment(account, &draft, a.planRevision)
	ui.apply(t)
	ui.apply(t)
	if strings.Join(calls, ",") != "pendingPayment,send" {
		t.Fatal("original was not passed directly to exact signer approval", calls)
	}
}

func TestResolvedOriginalStopsWithoutPreparingOrPromptingAnotherPayment(t *testing.T) {
	for _, terminal := range []string{"CONFIRMED", "REPLACED", "FAILED"} {
		t.Run(terminal, func(t *testing.T) {
			a, ui, core := fixture(t)
			original := pendingFixture(t, a, "UNKNOWN")
			account := original.Plan.Account
			a.fields["recipient/"+account.ID], a.fields["amount/"+account.ID] = original.Plan.Recipient, "1"
			draft := a.paymentDraft(account)
			var calls []string
			core.handler = func(_ uint64, method string, body, out any) error {
				calls = append(calls, method)
				switch method {
				case "pendingPayment":
					return answer(out, struct {
						Operation *journal.Operation `json:"operation"`
					}{&original})
				case "reconcile":
					if viewText(body) != `{"id":"original-operation"}` {
						t.Error("terminal status replaced original identity", body)
					}
					return answer(out, model.Receipt{Identity: "original-transaction", State: terminal})
				default:
					t.Error("terminal reconciliation automatically created another payment", method)
					return errors.New("unexpected payment mutation")
				}
			}
			ui.prompt = func(uiprompt.Prompt) (uiprompt.Reply, error) {
				t.Error("resolved original opened an unsolicited replacement approval")
				return uiprompt.Reply{}, errors.New("no replacement consent")
			}
			a.resolvePendingPayment(account, &draft, a.planRevision)
			ui.apply(t)
			ui.apply(t)
			if strings.Join(calls, ",") != "pendingPayment,reconcile" || a.busy || a.plan != nil || a.paymentDraft(account) != draft || !strings.Contains(a.status, terminal) {
				t.Fatal("terminal resolution did not stop for a new explicit Send intent", calls, a.status)
			}
		})
	}
}
