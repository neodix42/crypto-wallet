package near

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/decred/base58"
	"localwallet/internal/chains/chainutil"
	"localwallet/internal/model"
)

type historicalReceipt struct {
	ID          string `json:"receipt_id"`
	Predecessor string `json:"predecessor_id"`
	Receiver    string `json:"receiver_id"`
	Receipt     struct {
		Action *struct {
			Signer  string            `json:"signer_id"`
			Actions []json.RawMessage `json:"actions"`
		} `json:"Action"`
		Data json.RawMessage `json:"Data"`
	} `json:"receipt"`
}

type historicalObservation struct {
	Row      model.Receipt
	Signer   string
	Receiver string
	Receipts map[string]historicalReceipt
}

// HistoryPagesOverlap advertises source-independent whole-transaction rows.
// Multiple account indexes legitimately return the same original transaction.
func (*Adapter) HistoryPagesOverlap() bool { return true }

func (a *Adapter) historicalTransaction(ctx context.Context, ac model.Account, hash, signer string, candidate historicalCandidate, position historicalPosition) (historicalObservation, error) {
	observed := historicalObservation{Receipts: map[string]historicalReceipt{}}
	var raw json.RawMessage
	if err := a.rpc(ctx, ac.Network, "tx", map[string]string{"tx_hash": hash, "sender_account_id": signer, "wait_until": "FINAL"}, &raw); err != nil {
		return observed, historicalRPCError(err)
	}
	var result execution
	var transaction struct {
		Transaction struct {
			Hash      string            `json:"hash"`
			Signer    string            `json:"signer_id"`
			Receiver  string            `json:"receiver_id"`
			PublicKey string            `json:"public_key"`
			Nonce     json.Number       `json:"nonce"`
			Actions   []json.RawMessage `json:"actions"`
		} `json:"transaction"`
	}
	if json.Unmarshal(raw, &result) != nil || json.Unmarshal(raw, &transaction) != nil || result.Transaction.Hash != hash || result.Transaction.Signer != signer || a.ValidateAddress(ac.Network, signer) != nil || a.ValidateAddress(ac.Network, result.Transaction.Receiver) != nil {
		return observed, errInvalid
	}
	tx := transaction.Transaction
	if nonce, err := chainutil.Uint(tx.Nonce.String(), 64); err != nil || nonce == 0 || len(tx.Actions) == 0 || len(tx.Actions) > 100 {
		return observed, errInvalid
	}
	// Public key/nonce are public historical transaction metadata. They are not
	// matched against the account's *current* access key, which may have rotated.
	if !historicalPublicKey(tx.PublicKey) {
		return observed, errInvalid
	}
	base, err := receipt(hash, result)
	if err != nil || base.State != "CONFIRMED" && base.State != "FAILED" {
		return observed, errors.New("NEAR historical execution is not final")
	}
	observed.Signer, observed.Receiver = signer, tx.Receiver
	stamp, err := a.historicalBlock(ctx, ac.Network, base.Block, position)
	if err != nil {
		return observed, historicalRPCError(err)
	}
	base.Timestamp, base.Explorer = stamp, chainutil.TransactionExplorer(ac.Network, hash)
	base.Detail = "Provider-observed native NEAR actions and tracked NEP-141 events; FINAL RPC transaction and complete receipt graph checked. This read-only observation does not change the local payment journal or establish indexer completeness."
	if base.State == "CONFIRMED" {
		checked, err := stakingReceipt(model.Plan{Account: ac, Recipient: tx.Receiver}, hash, result)
		if err != nil || checked.State != "CONFIRMED" {
			return observed, errors.New("NEAR historical receipt graph is incomplete or failed")
		}
		base.State = "OBSERVED"
	} else if !a.historicalFailedGraph(ac, result) {
		return observed, errors.New("NEAR failed historical execution has an incomplete receipt graph")
	}
	transfers, err := a.historicalNativeActions(ac, signer, tx.Receiver, tx.Actions, "native:transaction", true)
	if err != nil {
		return observed, err
	}
	base.Transfers = transfers
	{
		blocks := map[string]bool{base.Block: true}
		for _, outcome := range result.Receipts {
			if !blocks[outcome.Block] {
				if _, err := a.historicalBlock(ctx, ac.Network, outcome.Block, position); err != nil {
					return observed, historicalRPCError(err)
				}
				blocks[outcome.Block] = true
			}
			var receipt historicalReceipt
			if err := a.rpc(ctx, ac.Network, "EXPERIMENTAL_receipt", map[string]string{"receipt_id": outcome.ID}, &receipt); err != nil {
				return observed, historicalRPCError(err)
			}
			if receipt.ID != outcome.ID || receipt.Receiver != outcome.Outcome.Executor || a.ValidateAddress(ac.Network, receipt.Predecessor) != nil || a.ValidateAddress(ac.Network, receipt.Receiver) != nil || receipt.Receipt.Action == nil && len(receipt.Receipt.Data) == 0 || receipt.Receipt.Action != nil && len(receipt.Receipt.Data) != 0 {
				return observed, errInvalid
			}
			observed.Receipts[receipt.ID] = receipt
			if receipt.Receipt.Action == nil || receipt.Predecessor == "system" {
				continue
			}
			if a.ValidateAddress(ac.Network, receipt.Receipt.Action.Signer) != nil || len(receipt.Receipt.Action.Actions) == 0 || len(receipt.Receipt.Action.Actions) > 100 {
				return observed, errInvalid
			}
			// The root receipt executes the transaction's own actions. Report them
			// exactly once, under the original transaction's stable effect IDs.
			root := false
			for _, id := range result.Outcome.Outcome.ReceiptIDs {
				if receipt.ID == id {
					root = true
					break
				}
			}
			if root {
				if receipt.Predecessor != signer || receipt.Receiver != tx.Receiver || !sameHistoricalActions(receipt.Receipt.Action.Actions, tx.Actions) {
					return observed, errInvalid
				}
				continue
			}
			if base.State == "FAILED" {
				continue
			}
			effects, err := a.historicalNativeActions(ac, receipt.Predecessor, receipt.Receiver, receipt.Receipt.Action.Actions, "native:"+receipt.ID, true)
			if err != nil || len(effects) > 1000-len(base.Transfers) {
				return observed, errInvalid
			}
			base.Transfers = append(base.Transfers, effects...)
		}
	}
	if base.State != "FAILED" {
		tokenRows, err := a.tokenHistoryExecution(ctx, ac, hash, signer, "", "", result)
		if err != nil {
			return observed, err
		}
		for _, row := range tokenRows {
			if len(row.Transfers) > 1000-len(base.Transfers) {
				return observed, errInvalid
			}
			base.Transfers = append(base.Transfers, row.Transfers...)
		}
	}
	sort.Slice(base.Transfers, func(i, j int) bool { return base.Transfers[i].ID < base.Transfers[j].ID })
	observed.Row = base
	if err := validateHistoricalObservation(candidate, position.Phase, observed); err != nil {
		return observed, err
	}
	return observed, nil
}

// Incoming history can originate from another NEAR key scheme. This validates
// bounded canonical public metadata, not a current full-access authority or a
// signature. The wallet's Ed25519 derivation/signing policy remains unchanged.
// KeyType/PublicKey sizes: nearcore core/crypto/src/signature.rs.
func historicalPublicKey(s string) bool {
	if len(s) > 2700 {
		return false
	}
	kind, encoded, ok := strings.Cut(s, ":")
	if !ok {
		return false
	}
	size := 0
	switch kind {
	case "ed25519":
		size = 32
	case "secp256k1":
		size = 64
	case "ml-dsa-65":
		size = 1952
	default:
		return false
	}
	decoded := base58.Decode(encoded)
	return len(decoded) == size && base58.Encode(decoded) == encoded
}

func (a *Adapter) historicalBlock(ctx context.Context, network, hash string, position historicalPosition) (int64, error) {
	if !blockHash(hash) {
		return 0, errInvalid
	}
	var block historyBlock
	if err := a.rpc(ctx, network, "block", map[string]string{"block_id": hash}, &block); err != nil {
		return 0, err
	}
	height, err := chainutil.Uint(block.Header.Height.String(), 64)
	head, headErr := chainutil.Uint(position.Height, 64)
	nanos, timeErr := chainutil.Uint(block.Header.Timestamp.String(), 64)
	before, beforeErr := chainutil.Uint(position.Before, 64)
	if err != nil || headErr != nil || timeErr != nil || beforeErr != nil || block.Header.Hash != hash || height > head || hash == position.Head && height != head || nanos == 0 || nanos >= before {
		return 0, errInvalid
	}
	return int64(nanos / 1_000_000_000), nil
}

// A failed root is a failed *attempt*, not evidence of native/token settlement.
// Reuse the existing strict graph checker after replacing only syntactically
// valid Failure statuses in an ephemeral copy; it still rejects missing,
// duplicate, cyclic and disconnected receipts. No signing or journal path uses
// this read-only metadata normalization, and no failed child effect is credited.
func (a *Adapter) historicalFailedGraph(ac model.Account, result execution) bool {
	var root map[string]json.RawMessage
	var failure map[string]json.RawMessage
	if chainutil.DecodeStrict(result.Status, &root) != nil || len(root) != 1 || chainutil.DecodeStrict(root["Failure"], &failure) != nil || len(failure) == 0 {
		return false
	}
	copy := result
	copy.Status = json.RawMessage(`{"SuccessValue":""}`)
	copy.Receipts = append([]receiptOutcome(nil), result.Receipts...)
	for index, receipt := range copy.Receipts {
		var status map[string]json.RawMessage
		if chainutil.DecodeStrict(receipt.Outcome.Status, &status) != nil || len(status) != 1 {
			return false
		}
		if raw, ok := status["Failure"]; ok {
			var detail map[string]json.RawMessage
			if chainutil.DecodeStrict(raw, &detail) != nil || len(detail) == 0 {
				return false
			}
			copy.Receipts[index].Outcome.Status = json.RawMessage(`{"SuccessValue":""}`)
		}
	}
	checked, err := stakingReceipt(model.Plan{Account: ac, Recipient: result.Transaction.Receiver}, result.Transaction.Hash, copy)
	return err == nil && checked.State == "CONFIRMED"
}

func sameHistoricalActions(left, right []json.RawMessage) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		var l, r any
		ld, rd := json.NewDecoder(bytes.NewReader(left[i])), json.NewDecoder(bytes.NewReader(right[i]))
		ld.UseNumber()
		rd.UseNumber()
		if chainutil.CheckJSON(left[i]) != nil || chainutil.CheckJSON(right[i]) != nil || ld.Decode(&l) != nil || rd.Decode(&r) != nil {
			return false
		}
		lb, _ := json.Marshal(l)
		rb, _ := json.Marshal(r)
		if string(lb) != string(rb) {
			return false
		}
	}
	return true
}

func validateHistoricalObservation(c historicalCandidate, phase string, observation historicalObservation) error {
	switch phase {
	case "txns":
		if c.Signer != observation.Signer || c.Receiver != observation.Receiver {
			return errInvalid
		}
	case "receipts":
		r, ok := observation.Receipts[c.Receipt]
		if !ok || r.Predecessor != c.Predecessor || r.Receiver != c.Receiver {
			return errors.New("NEAR indexed receipt does not bind to the checked transaction graph")
		}
	case "ft-txns":
		return checkHistoricalCandidateEffect(c, phase, observation.Row)
	default:
		return errInvalid
	}
	return nil
}

func (a *Adapter) historicalNativeActions(ac model.Account, source, destination string, actions []json.RawMessage, identity string, includeDeposit bool) ([]model.HistoryTransfer, error) {
	if len(actions) == 0 || len(actions) > 100 {
		return nil, errInvalid
	}
	effects := []model.HistoryTransfer{}
	for index, raw := range actions {
		// ActionView::CreateAccount is a serde unit variant: the actual RPC
		// representation is the string "CreateAccount", including inside the
		// root action receipt. It carries no native amount itself; retain any
		// separately encoded Transfer in the same account-funding transaction.
		var unit string
		if chainutil.DecodeStrict(raw, &unit) == nil {
			if unit != "CreateAccount" {
				return nil, errInvalid
			}
			continue
		}
		var tags map[string]json.RawMessage
		if chainutil.DecodeStrict(raw, &tags) != nil || len(tags) != 1 {
			return nil, errInvalid
		}
		deposit, comment := "", ""
		if value, ok := tags["Transfer"]; ok {
			var transfer struct {
				Deposit string `json:"deposit"`
			}
			if chainutil.DecodeStrict(value, &transfer) != nil {
				return nil, errInvalid
			}
			deposit = transfer.Deposit
		} else if value, ok := tags["FunctionCall"]; ok {
			var call struct {
				Method  string      `json:"method_name"`
				Args    string      `json:"args"`
				Gas     json.Number `json:"gas"`
				Deposit string      `json:"deposit"`
			}
			if chainutil.DecodeStrict(value, &call) != nil || len(call.Method) == 0 || len(call.Method) > 256 || !utf8.ValidString(call.Method) || strings.ContainsRune(call.Method, 0) || len(call.Args) > 24576 {
				return nil, errInvalid
			}
			gas, err := chainutil.Uint(call.Gas.String(), 64)
			args, argsErr := base64.StdEncoding.DecodeString(call.Args)
			if err != nil || gas == 0 || argsErr != nil || base64.StdEncoding.EncodeToString(args) != call.Args {
				return nil, errInvalid
			}
			if includeDeposit {
				deposit, comment = call.Deposit, "Attached deposit: "+call.Method
			} else {
				if _, err := chainutil.Big(call.Deposit, 128); err != nil {
					return nil, err
				}
			}
		} else {
			// Other actions have no inferred native amount. They remain visible
			// through an exact interpreted effect in the same transaction, if any.
			continue
		}
		if deposit == "" {
			continue
		}
		value, err := chainutil.Big(deposit, 128)
		if err != nil {
			return nil, err
		}
		if value.Sign() == 0 || source != ac.Address && destination != ac.Address {
			continue
		}
		effects = append(effects, model.HistoryTransfer{ID: identity + ":" + strconv.Itoa(index), Source: source, Destination: destination, Asset: model.Asset{ID: asset(ac.Network), Symbol: "NEAR", Decimals: 24}, Atomic: deposit, Comment: comment})
	}
	if len(effects) > 1000 {
		return nil, fmt.Errorf("NEAR native history effect count exceeds bound")
	}
	return effects, nil
}
