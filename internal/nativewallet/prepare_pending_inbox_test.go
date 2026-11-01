package nativewallet

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"localwallet/internal/ipc"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/uiprompt"
)

// A durable prepare can outlive its acknowledgement or its current form. Every
// family must expose that original through the read-only local inbox, including
// signed/ambiguous records; discovering one must never mutate or retry it.
func TestEveryPrepareCompletionQueuesOriginalPendingDialog(t *testing.T) {
	for _, family := range []string{"payment", "ton-deployment", "near-staking", "near-token-registration"} {
		for _, outcome := range []string{"success", "lost-ack-prepared", "lost-ack-signed", "lost-ack-unknown", "invalid-preview", "stale-preview"} {
			t.Run(family+"/"+outcome, func(t *testing.T) {
				a, ui, core := fixture(t)
				var account model.Account
				var plan model.Plan
				var prepareMethod string
				var start, stale func()
				switch family {
				case "payment":
					account = ethAccount(t, a)
					a.active, a.sendForms[account.ID] = "wallet/"+account.ID, true
					a.fields["recipient/"+account.ID], a.fields["amount/"+account.ID] = quoteRecipient(account), "1"
					plan = quotePlan(a, account, paymentQuoteRequest{AccountID: account.ID, Asset: a.asset(account).ID, Recipient: quoteRecipient(account), Amount: "1"})
					prepareMethod = "prepare"
					start = func() { a.preparePayment(account) }
					stale = func() { a.planRevision++; a.fields["amount/"+account.ID] = "2" }
				case "ton-deployment":
					a, ui, core, account = tonFixture(t)
					a.active = "wallet/" + account.ID
					a.tonStates = map[string]tonObservation{account.ID: {State: tonStateFixture(t, account, "uninit", "200000000", true)}}
					plan = nativeTONDeploymentPlan(account)
					prepareMethod = "prepareTonDeployment"
					start = func() { a.tonEvent("tonDeploy", account.ID) }
					stale = func() {
						for i := range a.state.Accounts {
							if a.state.Accounts[i].ID == account.ID {
								a.state.Accounts[i].Address = "changed-contract-while-preparing"
							}
						}
					}
				case "near-staking":
					account = nearAccount(t, a)
					a.active = "wallet/" + account.ID
					a.fields["stakingAmount/"+account.ID] = "1.25"
					plan = stakingPlan(account, a.stakingPool(account), "deposit_and_stake")
					prepareMethod = "prepareNearStaking"
					start = func() { a.stakingEvent("stakingPrepare", account.ID+"/stake") }
					stale = func() { a.stakingRevision++; a.fields["stakingPool/"+account.ID] = "other.poolv1.near" }
				case "near-token-registration":
					var token, other model.Asset
					account, token, other = nearTokenFixture(t, a)
					plan = nearRegistrationFixture(account, token)
					prepareMethod = "prepareNearTokenRegistration"
					start = func() { a.nearTokenEvent("nearTokenRegister", account.ID) }
					stale = func() { a.fields["asset/"+account.ID] = other.ID; a.nearTokenRevision++ }
				}
				original := journal.Operation{Plan: plan, State: "PREPARED", Digest: journal.Digest(plan)}
				switch outcome {
				case "lost-ack-signed":
					original.State = "SIGNED_NOT_SUBMITTED"
				case "lost-ack-unknown":
					original.State = "UNKNOWN"
					original.Receipt = &model.Receipt{Identity: "original-public-transaction", State: "UNKNOWN"}
				}
				before := viewText(original)
				response := plan
				if outcome == "invalid-preview" {
					response.Recipient = "foreign-recipient"
				}
				prepared := make(chan struct{})
				var prepares, sends, reads atomic.Int32
				core.handler = func(_ uint64, method string, body, out any) error {
					switch method {
					case prepareMethod:
						prepares.Add(1)
						defer close(prepared)
						if strings.HasPrefix(outcome, "lost-ack-") {
							return errors.New("synthetic acknowledgement lost after durable record")
						}
						return answer(out, response)
					case "send":
						sends.Add(1)
						if outcome != "success" || family != "payment" && family != "ton-deployment" || viewText(body) != viewText(map[string]string{"id": plan.ID}) {
							t.Error("preparation changed existing approval routing", method, body)
						}
						return ipc.NewRequestError("REQUEST_CANCELLED")
					case "history":
						reads.Add(1)
						if viewText(body) != `{"accountId":"","cursor":""}` {
							t.Error("inbox acquired wallet-table cursor or provider authority", body)
						}
						return answer(out, journalPage{Operations: []journal.Operation{original}, Total: 1, Revision: "durable-original", Complete: true})
					default:
						t.Error("pending discovery attempted a ledger mutation or provider request", method)
						return errors.New("unexpected operation")
					}
				}
				prompts := 0
				ui.prompt = func(p uiprompt.Prompt) (uiprompt.Reply, error) {
					prompts++
					if p.Kind != "pending" || len(p.Choices) != 1 || !strings.Contains(p.Choices[0].Summary, original.Plan.ID) || !strings.Contains(p.Choices[0].Summary, original.State) {
						t.Error("original pending operation stayed hidden", p.Kind)
					}
					return uiprompt.Reply{PromptID: p.ID}, nil // Later, never consent.
				}
				start()
				<-prepared
				if outcome == "stale-preview" {
					stale()
				}
				ui.apply(t)
				if a.busy {
					ui.apply(t) // Existing immediate approval was explicitly cancelled.
				}
				if prepares.Load() != 1 || a.pendingInbox.Request != account.ID || !a.nextPendingPayments() {
					t.Fatal("prepare completion failed to queue its saved original", prepares.Load(), a.pendingInbox.Request, a.busy)
				}
				ui.apply(t)
				wantSends := int32(0)
				if outcome == "success" && (family == "payment" || family == "ton-deployment") {
					wantSends = 1
				}
				if reads.Load() != 1 || prompts != 1 || sends.Load() != wantSends || viewText(original) != before || a.busy || a.background {
					t.Fatal("local pending dialog changed, retried or hid the durable operation", reads.Load(), prompts, sends.Load(), a.busy, a.background)
				}
			})
		}
	}
}
