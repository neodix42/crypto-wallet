package aptos

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strconv"

	"localwallet/internal/chains/chainutil"
	"localwallet/internal/model"
)

const indexedHistoryLimit = 32

// The official account index includes incoming activity, unlike the fullnode's
// sequence-number-based /accounts/{address}/transactions endpoint. It supplies
// positions only: amounts, effects and status always come from checked REST.
const indexedHistoryQuery = `query WalletAccountHistory($address: String!, $head: bigint!, $before: bigint!, $limit: Int!) {
 ledger_infos { chain_id }
 account_transactions(where: {account_address: {_eq: $address}, transaction_version: {_lte: $head, _lt: $before}}, order_by: {transaction_version: desc}, limit: $limit) { account_address transaction_version }
}`

type indexedHistoryPosition struct {
	Version int    `json:"version"`
	Account string `json:"account"`
	RPC     string `json:"rpc"`
	Head    string `json:"head"`
	Before  string `json:"before"`
}

func historyDigest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (a *Adapter) historyIndexer(ac model.Account) string {
	// Explicit local nodes must not leak fixture addresses or require public
	// services. The same rule preserves existing loopback integration fixtures.
	u, err := url.Parse(a.endpoints[ac.Network])
	if err != nil {
		return ""
	}
	ip := net.ParseIP(u.Hostname())
	if u.Hostname() == "localhost" || ip != nil && ip.IsLoopback() {
		return ""
	}
	switch ac.Network {
	case "aptos-mainnet":
		return "https://api.mainnet.aptoslabs.com/v1/graphql"
	case "aptos-testnet":
		return "https://api.testnet.aptoslabs.com/v1/graphql"
	}
	return ""
}

func (a *Adapter) History(ctx context.Context, ac model.Account) ([]model.Receipt, error) {
	page, err := a.HistoryPage(ctx, ac, "")
	return page.Entries, err
}

func (a *Adapter) HistoryPage(ctx context.Context, ac model.Account, cursor string) (model.HistoryPage, error) {
	if err := a.account(ac); err != nil {
		return model.HistoryPage{}, err
	}
	if a.historyIndexer(ac) == "" {
		if cursor != "" {
			return model.HistoryPage{}, errors.New("local Aptos bounded snapshot has no continuation")
		}
		rows, err := a.localHistory(ctx, ac)
		return model.HistoryPage{Entries: rows, Scope: "Local/loopback Aptos node: last 256 committed ledger versions; recognized direct native APT transfers and checked owned APT events only. Older versions remain unresolved; no public indexer was contacted.", Complete: false}, err
	}
	return a.indexedHistoryPage(ctx, ac, cursor)
}

func (a *Adapter) indexedHistoryPage(ctx context.Context, ac model.Account, cursor string) (model.HistoryPage, error) {
	page := model.HistoryPage{Entries: []model.Receipt{}, Scope: a.HistoryScope()}
	position := indexedHistoryPosition{Version: 1, Account: historyDigest(ac), RPC: historyDigest(a.endpoints[ac.Network])}
	if cursor != "" {
		if len(cursor) > 4096 {
			return model.HistoryPage{}, errInvalid
		}
		b, err := base64.RawURLEncoding.DecodeString(cursor)
		var p indexedHistoryPosition
		if err != nil || chainutil.DecodeStrict(b, &p) != nil || p.Version != position.Version || p.Account != position.Account || p.RPC != position.RPC {
			return model.HistoryPage{}, errInvalid
		}
		canonical, _ := json.Marshal(p)
		if base64.RawURLEncoding.EncodeToString(canonical) != cursor {
			return model.HistoryPage{}, errInvalid
		}
		head, headErr := chainutil.Uint(p.Head, 64)
		before, beforeErr := chainutil.Uint(p.Before, 64)
		if headErr != nil || beforeErr != nil || before == 0 || before > head {
			return model.HistoryPage{}, errInvalid
		}
		position = p
	}
	ledger, err := a.identity(ctx, ac.Network)
	if err != nil {
		return model.HistoryPage{}, err
	}
	if _, err = a.authority(ctx, ac, ledger.Version); err != nil {
		return model.HistoryPage{}, err
	}
	current, _ := chainutil.Uint(ledger.Version, 64)
	if cursor == "" {
		position.Head = ledger.Version
		// GraphQL bigint is signed, and a committed Aptos ledger version must
		// fit it. This also permits the exclusive initial upper bound head+1.
		if current >= 1<<63-1 {
			return model.HistoryPage{}, errInvalid
		}
		position.Before = strconv.FormatUint(current+1, 10)
	} else if head, _ := chainutil.Uint(position.Head, 64); head > current {
		return model.HistoryPage{}, errors.New("Aptos history ledger moved behind the selected snapshot")
	}
	request, _ := json.Marshal(struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}{indexedHistoryQuery, map[string]any{"address": ac.Address, "head": position.Head, "before": position.Before, "limit": indexedHistoryLimit}})
	var response struct {
		Errors json.RawMessage `json:"errors"`
		Data   *struct {
			Ledgers []struct {
				ChainID json.Number `json:"chain_id"`
			} `json:"ledger_infos"`
			Rows *[]struct {
				Account string      `json:"account_address"`
				Version json.Number `json:"transaction_version"`
			} `json:"account_transactions"`
		} `json:"data"`
	}
	if err := a.client.Do(ctx, "POST", a.historyIndexer(ac), "application/json", request, &response); err != nil {
		return model.HistoryPage{}, errors.New("Aptos account history indexer is unavailable; indexed activity has not been verified")
	}
	if len(response.Errors) > 0 && string(response.Errors) != "[]" || response.Data == nil || response.Data.Rows == nil || len(response.Data.Ledgers) != 1 || len(*response.Data.Rows) > indexedHistoryLimit {
		return model.HistoryPage{}, errInvalid
	}
	chain, _ := chainID(ac.Network)
	if id, err := chainutil.Uint(response.Data.Ledgers[0].ChainID.String(), 8); err != nil || id != uint64(chain) {
		return model.HistoryPage{}, errors.New("Aptos history indexer returned another chain")
	}
	head, _ := chainutil.Uint(position.Head, 64)
	previous, _ := chainutil.Uint(position.Before, 64)
	seenHashes := map[string]bool{}
	retained := 0
	var archive *Adapter
	var archiveVersion uint64
	for _, candidate := range *response.Data.Rows {
		owner, ownerErr := address(candidate.Account)
		version, versionErr := chainutil.Uint(candidate.Version.String(), 64)
		if ownerErr != nil || owner.StringLong() != ac.Address || versionErr != nil || version > head || version >= previous {
			return model.HistoryPage{}, errInvalid
		}
		previous = version
		var tx historyTransaction
		path := "/transactions/by_version/" + candidate.Version.String()
		err := a.request(ctx, ac.Network, "GET", path, nil, &tx)
		if err != nil {
			var rejected *chainutil.HTTPError
			if !errors.As(err, &rejected) || rejected.Status != 410 {
				return model.HistoryPage{}, err
			}
			// The ordinary official fullnode prunes historical versions. Its
			// HTTP 410 can trigger only these fixed official read-only archives;
			// never follow an archive URL or redirect supplied by a provider.
			if archive == nil {
				endpoint := "https://archive.mainnet.aptoslabs.com/v1"
				if ac.Network == "aptos-testnet" {
					endpoint = "https://archive.testnet.aptoslabs.com/v1"
				}
				archive = &Adapter{endpoints: map[string]string{ac.Network: endpoint}, client: a.client}
				archivedLedger, archiveErr := archive.identity(ctx, ac.Network)
				if archiveErr != nil {
					return model.HistoryPage{}, archiveErr
				}
				archiveVersion, _ = chainutil.Uint(archivedLedger.Version, 64)
			}
			if version > archiveVersion {
				return model.HistoryPage{}, errors.New("Aptos archival history has not reached the requested version")
			}
			if err := archive.request(ctx, ac.Network, "GET", path, nil, &tx); err != nil {
				return model.HistoryPage{}, err
			}
		}
		// A version is a globally unique committed transaction position. The
		// candidate cannot choose a different response/version or pending row.
		if tx.Version != candidate.Version.String() || tx.Type != "user_transaction" || tx.Success == nil || tx.Timestamp == "" {
			return model.HistoryPage{}, errInvalid
		}
		row, relevant, err := nativeHistoryTransaction(ac, tx)
		if err != nil {
			return model.HistoryPage{}, err
		}
		if !relevant {
			// This account-wide index also contains non-APT activity. Absence
			// of an interpreted effect is not a zero balance or known transfer.
			continue
		}
		if seenHashes[row.Identity] {
			return model.HistoryPage{}, errInvalid
		}
		encoded, err := json.Marshal(row)
		if err != nil || len(encoded) > (4<<20)-retained {
			return model.HistoryPage{}, errors.New("Aptos indexed history page reached its memory bound")
		}
		retained += len(encoded)
		seenHashes[row.Identity] = true
		page.Entries = append(page.Entries, row)
	}
	if len(*response.Data.Rows) == indexedHistoryLimit && previous > 0 {
		position.Before = strconv.FormatUint(previous, 10)
		b, _ := json.Marshal(position)
		page.NextCursor = base64.RawURLEncoding.EncodeToString(b)
	} else {
		page.Scope += " The address index returned no further candidates in this snapshot; indexer completeness and unsupported effects remain unverified."
	}
	// Exhausting an untrusted, potentially lagging index never proves a full
	// on-chain account history, so Complete deliberately remains false.
	return page, nil
}
