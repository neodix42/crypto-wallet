package nativewallet

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"localwallet/internal/ipc"
)

func TestFreeExchangeAcceptedResultSurvivesFailedFollowupLoad(t *testing.T) {
	for _, state := range []string{"PENDING", "CONFIRMED", "EXCHANGE_COMPLETED", "UNKNOWN"} {
		t.Run(state, func(t *testing.T) {
			a, ui, backend, _, _ := freeNativeFixture(t)
			calls, loads := 0, 0
			backend.handler = func(_ uint64, method string, _, _ any) error {
				loads++
				if method != "exchangeWizardLoad" {
					t.Error("follow-up gained unrelated authority", method)
				}
				return ipc.NewRequestError("PROVIDER_UNAVAILABLE")
			}
			result := wizardResult{ID: strings.Repeat("e", 32), Kind: "transfer", State: state, RemoteID: "public-exact-ack"}
			if state == "EXCHANGE_COMPLETED" {
				result.Kind = "withdraw"
			}
			a.freeWork("Submit original action", true, func(uint64) (freeExchangeUpdate, error) {
				calls++
				return freeExchangeUpdate{result: &freeExchangeOperation{Stage: 2, wizardResult: result}}, nil
			})
			ui.apply(t)
			operations := a.freeData().operations
			if calls != 1 || loads != 1 || len(operations) != 1 || !reflect.DeepEqual(operations[0].wizardResult, result) {
				t.Fatal("follow-up lost, replaced or replayed the returned operation", calls, loads, operations)
			}
			if len(a.errorQueue) != 0 || a.statusIsError || !strings.Contains(a.status, "Saved exchange details could not be refreshed (PROVIDER_UNAVAILABLE)") || !strings.Contains(a.status, result.RemoteID) || strings.Contains(a.status, "Request completed") {
				t.Fatal("read failure became an action failure or lost its explicit limitation", a.errorQueue, a.status)
			}
			if (state == "PENDING" || state == "UNKNOWN") && (strings.Contains(a.status, "CONFIRMED") || strings.Contains(a.status, "EXCHANGE_COMPLETED")) {
				t.Fatal("unverified acknowledgement became completion", a.status)
			}
		})
	}
}

func TestFreeExchangeFollowupReadDoesNotHideOperationErrorsOrLostAcknowledgements(t *testing.T) {
	for _, scenario := range []string{"operationError", "operationErrorWithResult", "lostAcknowledgement", "lockedRead", "noResult"} {
		t.Run(scenario, func(t *testing.T) {
			a, ui, backend, _, saved := freeNativeFixture(t)
			loadErr := ipc.NewRequestError("PROVIDER_UNAVAILABLE")
			if scenario == "lockedRead" {
				loadErr = ipc.NewRequestError("WALLET_LOCKED")
			}
			backend.handler = func(_ uint64, method string, _, out any) error {
				if method != "exchangeWizardLoad" {
					t.Error("unexpected follow-up", method)
				}
				if scenario == "operationError" {
					return freeSavedAnswer(out, saved, []any{})
				}
				return loadErr
			}
			result := wizardResult{ID: strings.Repeat("d", 32), Kind: "swap", State: "PENDING", RemoteID: "public-exact-ack"}
			a.freeWork("Submit original action", true, func(uint64) (freeExchangeUpdate, error) {
				update := freeExchangeUpdate{result: &freeExchangeOperation{Stage: 3, wizardResult: result}}
				switch scenario {
				case "operationError":
					return freeExchangeUpdate{}, ipc.NewRequestError("PROVIDER_REQUEST_REJECTED")
				case "operationErrorWithResult":
					return update, ipc.NewRequestError("PROVIDER_REQUEST_REJECTED")
				case "lostAcknowledgement":
					update.result.State, update.result.RemoteID = "UNKNOWN", ""
				case "noResult":
					return freeExchangeUpdate{}, nil
				}
				return update, nil
			})
			ui.apply(t)
			if !a.statusIsError || len(a.errorQueue) != 1 || strings.Contains(a.status, "Saved exchange details could not be refreshed") {
				t.Fatal("actual failure was hidden by read-warning feedback", a.errorQueue, a.status)
			}
			if scenario == "operationError" || scenario == "operationErrorWithResult" {
				if a.errorQueue[0].Code != "PROVIDER_REQUEST_REJECTED" {
					t.Fatal("follow-up replaced the actual operation error", a.errorQueue)
				}
			} else if scenario == "lostAcknowledgement" {
				if a.errorQueue[0].Title != "Submission status unknown" || !strings.Contains(a.errorQueue[0].Message, result.ID) || len(a.freeData().operations) != 1 || a.freeData().operations[0].State != "UNKNOWN" || a.freeData().operations[0].RemoteID != "" {
					t.Fatal("lost acknowledgement invented acceptance or lost its original identity", a.errorQueue)
				}
			} else if a.errorQueue[0].Code != ipc.ClassifyErrorCode(loadErr) {
				t.Fatal("unaccepted or locked follow-up did not retain its failure", a.errorQueue)
			}
		})
	}
}

func TestFreeExchangeFollowupWarningCannotCrossSessionGuards(t *testing.T) {
	for _, stale := range []string{"epoch", "generation", "locked"} {
		t.Run(stale, func(t *testing.T) {
			a, ui, backend, _, _ := freeNativeFixture(t)
			u := a.freeData()
			backend.handler = func(_ uint64, method string, _, _ any) error {
				if method != "exchangeWizardLoad" {
					t.Error("unexpected follow-up", method)
				}
				return ipc.NewRequestError("PROVIDER_UNAVAILABLE")
			}
			a.freeWork("Submit original action", true, func(uint64) (freeExchangeUpdate, error) {
				return freeExchangeUpdate{result: &freeExchangeOperation{Stage: 2, wizardResult: wizardResult{ID: strings.Repeat("c", 32), Kind: "transfer", State: "PENDING", RemoteID: "public-exact-ack"}}}, nil
			})
			var apply func()
			select {
			case apply = <-ui.queue:
			case <-time.After(3 * time.Second):
				t.Fatal("follow-up callback did not become ready")
			}
			switch stale {
			case "epoch":
				a.epoch++
			case "generation":
				backend.generation.Add(1)
			case "locked":
				a.lock()
			}
			apply()
			if len(u.operations) != 0 || strings.Contains(a.status, "Saved exchange details could not be refreshed") || strings.Contains(a.status, "acknowledged") {
				t.Fatal("a stale result or warning reached another session", a.status)
			}
			if stale == "generation" && (!a.state.Locked || len(a.errorQueue) != 1 || a.errorQueue[0].Code != "WALLET_LOCKED") {
				t.Fatal("core replacement bypassed the original lock/failure guard", a.state.Locked, a.errorQueue)
			}
		})
	}
}
