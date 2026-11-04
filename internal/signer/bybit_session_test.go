package signer

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"localwallet/internal/chains/near"
	"localwallet/internal/chains/ton"
	"localwallet/internal/derivation"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/vault"
)

type bybitSessionUI struct {
	*testUI
	inputCalls   int
	confirmCalls int
	titles       []string
	confirmHook  func()
}

func TestLockedSigningRejectsBeforeJournalOrPrompt(t *testing.T) {
	e, original, _ := newEngine(t, nil)
	u := &bybitSessionUI{testUI: original}
	e.UI = u
	defer func() {
		if recover() != nil {
			t.Fatal("locked signing crossed the session boundary")
		}
	}()
	signed, err := e.Sign("public-unowned-operation")
	if err == nil || len(signed.Bytes) != 0 || signed.Identity != "" || u.confirmCalls != 0 || u.inputCalls != 0 {
		t.Fatal("locked signing crossed the session boundary")
	}
}

func (u *bybitSessionUI) Input(title string, labels ...string) ([][]byte, error) {
	u.inputCalls++
	return u.testUI.Input(title, labels...)
}
func (u *bybitSessionUI) Confirm(title, summary string) bool {
	u.confirmCalls++
	u.titles = append(u.titles, title)
	accepted := u.testUI.Confirm(title, summary)
	if u.confirmHook != nil {
		u.confirmHook()
	}
	return accepted
}

func TestBybitSessionRemoteActionsKeepExactConfirmationAndOneUseWithoutPassword(t *testing.T) {
	for _, kind := range []string{"transfer", "swap", "return", "withdraw"} {
		t.Run(kind, func(t *testing.T) {
			e, original, account := currentFundsGRAMEngine(t)
			u := &bybitSessionUI{testUI: original}
			e.UI = u
			op, err := e.PrepareExchangeCurrentFunds(currentFundsGRAMIntent(account, kind))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = e.ExchangeAuth(exchangeRequest(op)); err == nil {
				t.Fatal("preparation authorized a debit without confirmation")
			}
			op, err = e.AuthorizeExchange(op.ID)
			if err != nil || op.State != "AUTHORIZED" || u.inputCalls != 0 || u.confirmCalls != 1 || len(original.inputs) != 0 {
				t.Fatal("Bybit action requested its password or lost exact confirmation", err)
			}
			summary := original.summaries[len(original.summaries)-1]
			for _, exact := range []string{op.ID, op.Endpoint, account.Address, "GRAM", "Only this new operation is authorized."} {
				if !strings.Contains(summary, exact) {
					t.Fatal("current-session approval omitted exact authority", exact)
				}
			}
			request := exchangeRequest(op)
			if auth, err := e.ExchangeAuth(request); err != nil || auth.Signature == "" {
				t.Fatal("confirmed current-session request not signed", err)
			}
			if _, err = e.ExchangeAuth(request); err == nil {
				t.Fatal("current-session authorization replayed")
			}
			stored, err := e.ExchangeOperation(op.ID)
			if err != nil || stored.State != "SUBMITTING" {
				t.Fatal("signature escaped before durable original submission")
			}
		})
	}
}

func TestBybitSessionRemoteConsentRejectsClosedChangedAndCancelledAuthority(t *testing.T) {
	for _, scenario := range []string{"cancel", "lock", "recovery", "owner", "generation", "credential", "endpoint", "provider", "exchange", "expires", "state", "payload", "persist"} {
		t.Run(scenario, func(t *testing.T) {
			e, original, account := currentFundsGRAMEngine(t)
			op, err := e.PrepareExchangeCurrentFunds(currentFundsGRAMIntent(account, "transfer"))
			if err != nil {
				t.Fatal(err)
			}
			u := &bybitSessionUI{testUI: original}
			e.UI = u
			u.confirmHook = func() {
				switch scenario {
				case "cancel":
				case "lock":
					e.Lock()
				case "recovery":
					e.data.RecoveryConfirmed = false
				case "owner":
					copy := *e.data
					e.data = &copy
				case "generation":
					e.data.Generation++
				case "credential":
					e.data.Exchange.Credentials.APIKey = []byte("CHANGED_PUBLIC_FIXTURE_KEY")
				case "endpoint":
					e.data.Exchange.Credentials.Endpoint = "https://api.bybit.ae"
				case "provider":
					e.data.Exchange.SelectedProvider = "binance"
				case "exchange":
					copy := *e.data.Exchange
					e.data.Exchange = &copy
				case "expires":
					e.data.Exchange.Operations[0].ExpiresAt = time.Now().Unix() - 1
				case "state":
					e.data.Exchange.Operations[0].State = "UNKNOWN"
				case "payload":
					e.data.Exchange.Operations[0].Payload = strings.Replace(op.Payload, "0.5", "0.6", 1)
					e.data.Exchange.Operations[0].Digest = vault.ExchangeDigest(e.data.Exchange.Operations[0])
				case "persist":
					e.parameters = vault.Parameters{}
				}
			}
			if scenario == "cancel" {
				original.approve = false
			}
			if authorized, err := e.AuthorizeExchange(op.ID); err == nil || authorized.ID != "" {
				t.Fatal("changed or declined session released exchange authority", scenario)
			}
			if auth, err := e.ExchangeAuth(exchangeRequest(op)); err == nil || auth.Signature != "" || u.inputCalls != 0 {
				t.Fatal("denied session signed or requested a password", scenario)
			}
			if scenario == "cancel" {
				stored, err := e.ExchangeOperation(op.ID)
				if err != nil || stored.State != "CANCELLED" {
					t.Fatal("declined exact confirmation did not cancel its own unsigned operation")
				}
			}
		})
	}
}

func bybitDepositMainnetPlan(account model.Account) model.Plan {
	p := evmPlan(account)
	p.Payload = []byte(strings.Replace(string(p.Payload), `"chainId":"11155111"`, `"chainId":"1"`, 1))
	p.ID = ""
	p.ID = "evm:" + journal.Digest(p)
	return p
}

func TestBybitSessionDepositSignsExactPlanOnceWithoutPassword(t *testing.T) {
	e, original, account := exchangeEngine(t)
	u := &bybitSessionUI{testUI: original}
	e.UI = u
	plan := bybitDepositMainnetPlan(account)
	if err := e.PrepareBybitDeposit(plan); err != nil || u.confirmCalls != 0 || u.inputCalls != 0 {
		t.Fatal("private preparation failed or authorized the payment", err)
	}
	if e.bybitDepositPreparations[plan.ID] == nil {
		t.Fatal("exact private preparation lost its current-session capability")
	}
	signed, err := e.SignBybitDeposit(plan.ID)
	if err != nil || len(signed.Bytes) == 0 || signed.Identity == "" || u.inputCalls != 0 || u.confirmCalls != 1 {
		t.Fatal("Bybit deposit repeated the password or missed its exact confirmation", err)
	}
	if u.titles[0] != "Authorize exact Bybit deposit — Go signer" {
		t.Fatal("deposit confirmation was not identified as Bybit")
	}
	summary := original.summaries[len(original.summaries)-1]
	for _, exact := range []string{"Deposit to Bybit Funding", "https://api.bybit.com", plan.Account.Address, plan.Recipient, plan.Asset, plan.Amount, plan.MaxFee, plan.Nonce} {
		if !strings.Contains(summary, exact) {
			t.Fatal("Bybit approval omitted exact payment details", exact)
		}
	}
	if e.bybitDepositPreparations[plan.ID] != nil || e.bybitDepositSigning != nil || e.data.Operations[0].State != "SIGNED_NOT_SUBMITTED" {
		t.Fatal("private capability survived signing or signed state was not durable")
	}
	if replay, err := e.SignBybitDeposit(plan.ID); err == nil || len(replay.Bytes) != 0 || u.confirmCalls != 1 {
		t.Fatal("private deposit was signed twice")
	}
}

func TestBybitSessionDepositCannotConvertOrdinaryWalletSendToSessionApproval(t *testing.T) {
	for _, scenario := range []string{"ordinary-no-password", "ordinary-password", "private-prepared-normal-sign"} {
		t.Run(scenario, func(t *testing.T) {
			e, original, account := exchangeEngine(t)
			u := &bybitSessionUI{testUI: original}
			e.UI = u
			p := bybitDepositMainnetPlan(account)
			var err error
			if scenario == "private-prepared-normal-sign" {
				err = e.PrepareBybitDeposit(p)
			} else {
				err = e.Prepare(p)
			}
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "private-prepared-normal-sign" {
				if signed, err := e.SignBybitDeposit(p.ID); err == nil || len(signed.Bytes) != 0 || u.confirmCalls != 0 || u.inputCalls != 0 {
					t.Fatal("normal prepared payment gained private Bybit authority")
				}
			}
			if scenario == "ordinary-password" {
				original.inputs = append(original.inputs, input(syntheticPassword))
			}
			signed, err := e.Sign(p.ID)
			if u.inputCalls != 1 || u.confirmCalls != 1 {
				t.Fatal("ordinary wallet Send lost its exact confirmation or password")
			}
			if scenario == "ordinary-password" {
				if err != nil || len(signed.Bytes) == 0 {
					t.Fatal("normal authenticated Send stopped working", err)
				}
			} else if err == nil || len(signed.Bytes) != 0 {
				t.Fatal("ordinary wallet Send signed without its password")
			}
		})
	}
}

func TestBybitSessionDepositRejectsChangedAuthorityBeforeAndAfterConfirmation(t *testing.T) {
	for _, phase := range []string{"before", "confirm"} {
		for _, scenario := range []string{"lock", "recovery", "owner", "generation", "credential", "endpoint", "provider", "exchange", "environment", "owned-account", "expires", "state", "plan", "persist"} {
			t.Run(phase+"/"+scenario, func(t *testing.T) {
				e, original, account := exchangeEngine(t)
				p := bybitDepositMainnetPlan(account)
				if err := e.PrepareBybitDeposit(p); err != nil {
					t.Fatal(err)
				}
				u := &bybitSessionUI{testUI: original}
				e.UI = u
				change := func() {
					switch scenario {
					case "lock":
						e.Lock()
					case "recovery":
						e.data.RecoveryConfirmed = false
					case "owner":
						copy := *e.data
						e.data = &copy
					case "generation":
						e.data.Generation++
					case "credential":
						e.data.Exchange.Credentials.APIKey = []byte("CHANGED_PUBLIC_FIXTURE_KEY")
					case "endpoint":
						e.data.Exchange.Credentials.Endpoint = "https://api.bybit.ae"
					case "provider":
						e.data.Exchange.SelectedProvider = "binance"
					case "exchange":
						copy := *e.data.Exchange
						e.data.Exchange = &copy
					case "environment":
						e.data.Environment = "testnet"
					case "owned-account":
						e.data.Accounts[0].Address = "0x2222222222222222222222222222222222222222"
					case "expires":
						e.data.Operations[0].Plan.Expires = time.Now().Unix() - 1
						e.data.Operations[0].Digest = journal.Digest(e.data.Operations[0].Plan)
					case "state":
						e.data.Operations[0].State = "UNKNOWN"
					case "plan":
						e.data.Operations[0].Plan.Memo = "unapproved changed memo"
						e.data.Operations[0].Digest = journal.Digest(e.data.Operations[0].Plan)
					case "persist":
						e.parameters = vault.Parameters{}
					}
				}
				if phase == "before" {
					change()
				} else {
					u.confirmHook = change
				}
				if signed, err := e.SignBybitDeposit(p.ID); err == nil || len(signed.Bytes) != 0 || signed.Identity != "" || u.inputCalls != 0 {
					t.Fatal("stale Bybit deposit authority signed or requested a password", phase, scenario)
				}
				if e.bybitDepositPreparations[p.ID] != nil || e.bybitDepositSigning != nil {
					t.Fatal("failed private capability survived its one attempt")
				}
			})
		}
	}
}

func TestBybitSessionDepositCancellationAndRestartDoNotRestoreAuthority(t *testing.T) {
	for _, scenario := range []string{"cancel", "restart"} {
		t.Run(scenario, func(t *testing.T) {
			e, original, account := exchangeEngine(t)
			p := bybitDepositMainnetPlan(account)
			if err := e.PrepareBybitDeposit(p); err != nil {
				t.Fatal(err)
			}
			u := &bybitSessionUI{testUI: original}
			e.UI = u
			if scenario == "cancel" {
				original.approve = false
			} else {
				e.Lock()
				original.inputs = append(original.inputs, input(syntheticPassword))
				if err := e.Unlock(); err != nil {
					t.Fatal(err)
				}
				u.inputCalls = 0 // Unlock itself still authenticates the storage password.
			}
			if signed, err := e.SignBybitDeposit(p.ID); err == nil || len(signed.Bytes) != 0 || u.inputCalls != 0 {
				t.Fatal("cancelled/restarted private deposit retained authority")
			}
			if e.data.Operations[0].State != "CANCELLED" {
				t.Fatal("unsigned denied deposit was not durably cancelled")
			}
		})
	}
}

func TestBybitSessionDepositPreparationIsNarrowAndCannotBeSerializedAsAuthority(t *testing.T) {
	for _, scenario := range []string{"locked", "recovery", "testnet", "disconnected", "binance", "foreign-account", "foreign-asset", "zero", "expired", "mutated-bytes"} {
		t.Run(scenario, func(t *testing.T) {
			e, original, account := exchangeEngine(t)
			p := bybitDepositMainnetPlan(account)
			u := &bybitSessionUI{testUI: original}
			e.UI = u
			switch scenario {
			case "locked":
				e.Lock()
			case "recovery":
				e.data.RecoveryConfirmed = false
			case "testnet":
				e.data.Environment = "testnet"
			case "disconnected":
				e.data.Exchange.Credentials = nil
			case "binance":
				e.data.Exchange.SelectedProvider = "binance"
			case "foreign-account":
				p.Account.ID = "foreign-owned-descriptor"
			case "foreign-asset":
				p.Asset = "ethereum-mainnet:unapproved"
			case "zero":
				p.Amount = "0"
			case "expired":
				p.Expires = time.Now().Unix() - 1
			case "mutated-bytes":
				p.Recipient = "0x2222222222222222222222222222222222222222"
			}
			if err := e.PrepareBybitDeposit(p); err == nil || len(e.bybitDepositPreparations) != 0 || u.inputCalls != 0 || u.confirmCalls != 0 {
				t.Fatal("unreviewed private preparation acquired authority", scenario)
			}
		})
	}
	e, _, account := exchangeEngine(t)
	p := bybitDepositMainnetPlan(account)
	if err := e.PrepareBybitDeposit(p); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(e.Snapshot())
	for _, forbidden := range []string{"bybitDepositPreparations", "bybitDepositSigning", "currentSession", "skipPassword"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("private session capability entered public/persisted authority", forbidden)
		}
	}
}

func TestBybitSessionGRAMDepositPreservesRealSignedMemoWithoutAnotherPassword(t *testing.T) {
	e, original, account := currentFundsGRAMEngine(t)
	seed, err := derivation.Seed(e.data.Entropy, e.data.Passphrase)
	if err != nil {
		t.Fatal(err)
	}
	recipient, err := ton.New().Derive(seed, 1, "ton-mainnet")
	clear(seed)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(ton.Payload{Deploy: true, SourceBalance: "10000000000", EstimatedFee: "1000000", StorageReserve: "10000000"})
	p := model.Plan{Account: account, Asset: "ton-mainnet:TON", Recipient: recipient.Address, Amount: "123456789", FeeAsset: "ton-mainnet:TON", MaxFee: "3000000", Nonce: "0", Expires: time.Now().Add(time.Minute).Unix(), Memo: "PUBLIC-BYBIT-DEPOSIT-MEMO-73916", Payload: payload}
	p = tonDeploymentPlanID(p)
	u := &bybitSessionUI{testUI: original}
	e.UI = u
	if err := e.PrepareBybitDeposit(p); err != nil {
		t.Fatal(err)
	}
	signed, err := e.SignBybitDeposit(p.ID)
	if err != nil || len(signed.Bytes) == 0 || signed.Identity == "" || !bytes.Contains(signed.Bytes, []byte(p.Memo)) || u.inputCalls != 0 || u.confirmCalls != 1 {
		t.Fatal("GRAM deposit lost its real signed memo, exact confirmation or session policy", err)
	}
	if !strings.Contains(original.summaries[len(original.summaries)-1], p.Memo) || !strings.Contains(original.summaries[len(original.summaries)-1], p.Recipient) {
		t.Fatal("GRAM exact deposit approval omitted the required memo/address")
	}
}

func TestBybitSessionDepositCannotAuthorizeAnOtherwiseValidNEARStakingCall(t *testing.T) {
	e, ui, _ := newEngine(t, []model.Adapter{near.New()})
	createImported(t, e, ui, "mainnet")
	ui.inputs = append(ui.inputs, input(exchangeTestKey, exchangeTestSecret))
	if err := e.ConnectExchange("https://api.bybit.com"); err != nil {
		t.Fatal(err)
	}
	p := stakingSignerPlan(findAccount(t, e, "near-mainnet"), "deposit_and_stake")
	p.Recipient = "astro-stakers.poolv1.near"
	if err := near.New().ValidatePlan(p); err != nil {
		t.Fatal("negative fixture must be an otherwise valid staking plan", err)
	}
	if err := e.PrepareBybitDeposit(p); err == nil || len(e.data.Operations) != 0 || len(e.bybitDepositPreparations) != 0 {
		t.Fatal("private Bybit transfer policy acquired staking-call authority")
	}
}
