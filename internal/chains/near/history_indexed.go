package near

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"localwallet/internal/chains/chainutil"
	"localwallet/internal/model"
)

// NearBlocks v3 exposes independent, keyset-paginated transaction, receipt and
// FT indexes. See its accounts/{txns,receipts,fts}/{request,response}.ts and
// common.ts schemas. Provider cursors are only query positions, never URLs or
// authority for an amount, asset, execution status or wallet transaction.
const historicalCandidateLimit = 25

var errHistoricalUnavailable = errors.New("NEAR historical RPC evidence unavailable")

func historicalRPCError(err error) error {
	if errors.Is(err, chainutil.ErrInvalid) || errors.Is(err, errInvalid) {
		return err
	}
	return fmt.Errorf("%w: %v", errHistoricalUnavailable, err)
}

type historicalPosition struct {
	Version int    `json:"version"`
	Binding string `json:"binding"`
	Head    string `json:"head"`
	Height  string `json:"height"`
	Before  string `json:"before"`
	Phase   string `json:"phase"`
	Next    string `json:"next,omitempty"`
	Tokens  uint32 `json:"tokens"`
	Pinned  bool   `json:"pinned"`
	Partial bool   `json:"partial,omitempty"`
}

type historicalCandidate struct {
	Hash        string `json:"transaction_hash"`
	Signer      string `json:"signer_account_id"`
	Receiver    string `json:"receiver_account_id"`
	Predecessor string `json:"predecessor_account_id"`
	Receipt     string `json:"receipt_id"`
	Contract    string `json:"contract_account_id"`
	Affected    string `json:"affected_account_id"`
}

type historicalCandidates struct {
	Data   *[]historicalCandidate `json:"data"`
	Errors []json.RawMessage      `json:"errors"`
	Meta   struct {
		Next json.RawMessage `json:"next_page"`
	} `json:"meta"`
}

func historyProviderCursor(s string) bool {
	if len(s) > 1024 || strings.ContainsAny(s, "\x00\r\n") {
		return false
	}
	if s == "" {
		return true
	}
	b, err := base64.StdEncoding.DecodeString(s)
	return err == nil && len(b) > 0 && base64.StdEncoding.EncodeToString(b) == s
}

func (a *Adapter) historyBinding(ac model.Account) string {
	value, _ := json.Marshal(struct {
		Account model.Account
		RPC     string
		Indexer string
		Archive string
		Tokens  []model.NEP141Token
	}{ac, a.endpoints[ac.Network], a.historyEndpoints[ac.Network], historyArchiveEndpoint(ac.Network, a.endpoints[ac.Network]), a.networkTokens(ac.Network)})
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

// Official FastNear archives expose older transaction/receipt/state records;
// regular public RPCs prune them. Only exact built-in public configurations use
// this reviewed read-only provider. A user-selected/local RPC is never bypassed.
// https://docs.fastnear.com/rpc (archival base URLs, reviewed 2026-10-30).
func historyArchiveEndpoint(network, endpoint string) string {
	endpoint = strings.TrimSuffix(endpoint, "/")
	switch network {
	case "near-mainnet":
		if endpoint == "https://rpc.mainnet.near.org" || endpoint == "https://rpc.mainnet.fastnear.com" {
			return "https://archival-rpc.mainnet.fastnear.com"
		}
	case "near-testnet":
		if endpoint == "https://rpc.testnet.near.org" || endpoint == "https://rpc.testnet.fastnear.com" {
			return "https://archival-rpc.testnet.fastnear.com"
		}
	}
	return ""
}

func encodeHistoricalPosition(p historicalPosition) (string, error) {
	raw, err := json.Marshal(p)
	if err != nil || len(raw) > 3000 {
		return "", errInvalid
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (a *Adapter) historyPosition(ctx context.Context, ac model.Account, cursor string) (historicalPosition, error) {
	p := historicalPosition{Version: 1, Binding: a.historyBinding(ac), Phase: "txns"}
	if err := a.account(ac); err != nil {
		return p, err
	}
	if cursor == "" {
		state, _, err := a.state(ctx, ac)
		if err != nil {
			return p, err
		}
		p.Head, p.Height = state.Hash, state.Height.String()
	} else {
		if len(cursor) > 4096 {
			return p, errInvalid
		}
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || base64.RawURLEncoding.EncodeToString(raw) != cursor || chainutil.DecodeStrict(raw, &p) != nil || p.Version != 1 || !p.Pinned || p.Binding != a.historyBinding(ac) || !historyProviderCursor(p.Next) || p.Phase != "txns" && p.Phase != "receipts" && p.Phase != "ft-txns" {
			return p, errors.New("NEAR history continuation changed or is invalid; refresh history")
		}
		if err := a.identity(ctx, ac.Network); err != nil {
			return p, err
		}
	}
	if !blockHash(p.Head) {
		return p, errInvalid
	}
	height, err := chainutil.Uint(p.Height, 64)
	if err != nil {
		return p, err
	}
	var block historyBlock
	if err := a.rpc(ctx, ac.Network, "block", map[string]string{"block_id": p.Head}, &block); err != nil {
		return p, err
	}
	actualHeight, err := chainutil.Uint(block.Header.Height.String(), 64)
	if err != nil || block.Header.Hash != p.Head || actualHeight != height {
		return p, errInvalid
	}
	nanos, err := chainutil.Uint(block.Header.Timestamp.String(), 64)
	if err != nil || nanos == 0 || nanos == ^uint64(0) || nanos/1_000_000_000 > uint64(time.Now().Unix()+300) {
		return p, errInvalid
	}
	before := strconv.FormatUint(nanos+1, 10)
	if len(before) != 19 || p.Before != "" && p.Before != before {
		return p, errInvalid
	}
	p.Before = before
	return p, nil
}

func historicalNextPhase(p *historicalPosition, tokens bool) {
	p.Next = ""
	switch p.Phase {
	case "txns":
		p.Phase = "receipts"
	case "receipts":
		if tokens {
			p.Phase = "ft-txns"
		} else {
			p.Phase = ""
		}
	default:
		p.Phase = ""
	}
}

// HistoryPage is address-indexed rather than tied to the latest block window.
// Every indexed transaction yields a source-independent complete observation so
// overlapping native/receipt/token candidates can be compared exactly by core.
func (a *Adapter) HistoryPage(ctx context.Context, ac model.Account, cursor string) (model.HistoryPage, error) {
	page := model.HistoryPage{Entries: []model.Receipt{}, Scope: a.HistoryScope(), Complete: false}
	if a.historyEndpoints[ac.Network] == "" {
		if cursor != "" {
			return page, errors.New("NEAR history continuation unavailable without an address indexer")
		}
		rows, err := a.recentHistory(ctx, ac, false)
		page.Entries = rows
		page.Scope += " Address indexing is not configured; only the last 12 final blocks were scanned."
		return page, err
	}
	if chainutil.Endpoint(a.historyEndpoints[ac.Network]) != nil {
		return page, errInvalid
	}
	p, err := a.historyPosition(ctx, ac, cursor)
	if err != nil {
		return page, err
	}
	policies := a.networkTokens(ac.Network)
	reader, err := a.WithTokens(policies)
	if err != nil {
		return page, err
	}
	if archive := historyArchiveEndpoint(ac.Network, a.endpoints[ac.Network]); archive != "" {
		reader.endpoints[ac.Network] = archive
		if err := reader.identity(ctx, ac.Network); err != nil {
			return page, err
		}
		if _, err := reader.historicalBlock(ctx, ac.Network, p.Head, p); err != nil {
			return page, err
		}
		page.Scope += " Historical RPC evidence uses the chain-checked FastNear archive at " + archive + "; current sending/signing settings are unchanged."
	}
	checked := make([]model.NEP141Token, 0, len(policies))
	mask := uint32(1<<uint(len(policies))) - 1
	if len(policies) == 32 {
		mask = ^uint32(0)
	}
	if p.Tokens & ^mask != 0 {
		return page, errInvalid
	}
	for i, policy := range policies {
		bit := uint32(1) << uint(i)
		if p.Pinned && p.Tokens&bit == 0 {
			continue
		}
		if err := reader.verifyTokenAt(ctx, policy, tokenBlock{Hash: p.Head, Height: p.Height}); err != nil {
			if p.Pinned {
				return page, errors.New("NEAR pinned history token evidence changed or is unavailable; refresh history")
			}
			p.Partial = true
			continue
		}
		p.Tokens |= bit
		checked = append(checked, policy)
	}
	p.Pinned = true
	if len(checked) != len(policies) {
		page.Scope += " A tracked token's runtime or metadata was unavailable at the checked head; that token's history remains unknown for this scan."
	}
	if p.Partial {
		page.Scope += " This scan has unavailable address-index, transaction, receipt or token evidence; missing records remain unknown. Refresh to retry unavailable records."
	}
	reader, err = reader.WithTokens(checked)
	if err != nil {
		return page, err
	}
	query := url.Values{"limit": {strconv.Itoa(historicalCandidateLimit)}, "before_ts": {p.Before}}
	if p.Next != "" {
		query.Set("next", p.Next)
	}
	// Native receipts exclude protocol refunds after independent RPC decoding.
	// No indexer-supplied action/amount/status is accepted as transfer evidence.
	var candidates historicalCandidates
	err = a.client.Do(ctx, "GET", a.historyEndpoints[ac.Network]+"/v3/accounts/"+url.PathEscape(ac.Address)+"/"+p.Phase+"?"+query.Encode(), "", nil, &candidates)
	if err != nil {
		if errors.Is(err, chainutil.ErrInvalid) {
			return page, err
		}
		page.Scope += " Address-indexed " + p.Phase + " history is unavailable/incomplete; missing records remain unknown. Refresh to retry this index."
		p.Partial = true
		if cursor == "" {
			page.Entries, err = reader.recentHistory(ctx, ac, false, p)
			if err != nil {
				return page, err
			}
		}
		historicalNextPhase(&p, len(policies) > 0)
		if p.Phase != "" {
			page.NextCursor, err = encodeHistoricalPosition(p)
		}
		return page, err
	}
	providerNext := ""
	if len(candidates.Meta.Next) > 0 && (string(candidates.Meta.Next) == "null" || json.Unmarshal(candidates.Meta.Next, &providerNext) != nil) {
		return page, errInvalid
	}
	if candidates.Data == nil || len(*candidates.Data) > historicalCandidateLimit || len(candidates.Errors) != 0 || !historyProviderCursor(providerNext) || providerNext != "" && providerNext == p.Next || len(*candidates.Data) == 0 && providerNext != "" {
		return page, errInvalid
	}
	cache := map[string]historicalObservation{}
	unavailable := false
	for _, candidate := range *candidates.Data {
		if err := reader.validateHistoricalCandidate(ac, p.Phase, candidate); err != nil {
			if errors.Is(err, errHistoricalUnavailable) {
				unavailable = true
				continue
			}
			return page, err
		}
		if p.Phase == "ft-txns" {
			if _, ok := reader.token(ac.Network, ac.Network+":nep141:"+candidate.Contract); !ok {
				continue
			}
		}
		signer := candidate.Signer
		if signer == "" {
			var detail struct {
				Data *struct {
					Hash   string `json:"transaction_hash"`
					Signer string `json:"signer_account_id"`
				} `json:"data"`
			}
			if err := a.client.Do(ctx, "GET", a.historyEndpoints[ac.Network]+"/v3/txns/"+url.PathEscape(candidate.Hash), "", nil, &detail); err != nil {
				if errors.Is(err, chainutil.ErrInvalid) || ctx.Err() != nil {
					return page, err
				}
				unavailable = true
				continue
			}
			if detail.Data == nil || detail.Data.Hash != candidate.Hash || a.ValidateAddress(ac.Network, detail.Data.Signer) != nil {
				return page, errInvalid
			}
			signer = detail.Data.Signer
		}
		row, ok := cache[candidate.Hash]
		if !ok {
			row, err = reader.historicalTransaction(ctx, ac, candidate.Hash, signer, candidate, p)
			if err != nil {
				if errors.Is(err, errHistoricalUnavailable) && ctx.Err() == nil {
					unavailable = true
					continue
				}
				return page, err
			}
			cache[candidate.Hash] = row
		} else {
			if row.Signer != signer {
				return page, errInvalid
			}
			if err := validateHistoricalObservation(candidate, p.Phase, row); err != nil {
				return page, err
			}
		}
	}
	if unavailable {
		p.Partial = true
		page.Scope += " Some indexed transaction bindings or transaction/receipt payloads from the RPC/archive provider are unavailable; those records remain unknown. Checked rows are retained."
	}
	for _, row := range cache {
		if len(row.Row.Transfers) != 0 {
			page.Entries = append(page.Entries, row.Row)
		}
	}
	sort.Slice(page.Entries, func(i, j int) bool {
		if page.Entries[i].Timestamp == page.Entries[j].Timestamp {
			return page.Entries[i].Identity < page.Entries[j].Identity
		}
		return page.Entries[i].Timestamp > page.Entries[j].Timestamp
	})
	p.Next = providerNext
	if p.Next == "" {
		historicalNextPhase(&p, len(policies) > 0)
	}
	if p.Phase != "" {
		page.NextCursor, err = encodeHistoricalPosition(p)
	} else {
		page.Scope += " All supplied address-index positions are exhausted; omissions and unsupported effects remain unknown."
	}
	return page, err
}

func (a *Adapter) validateHistoricalCandidate(ac model.Account, phase string, c historicalCandidate) error {
	switch phase {
	case "txns":
		if a.ValidateAddress(ac.Network, c.Signer) != nil || a.ValidateAddress(ac.Network, c.Receiver) != nil || c.Signer != ac.Address && c.Receiver != ac.Address {
			return errInvalid
		}
	case "receipts":
		if !blockHash(c.Receipt) || a.ValidateAddress(ac.Network, c.Predecessor) != nil || a.ValidateAddress(ac.Network, c.Receiver) != nil || c.Predecessor != ac.Address && c.Receiver != ac.Address {
			return errInvalid
		}
	case "ft-txns":
		if !blockHash(c.Receipt) || c.Affected != ac.Address || a.ValidateAddress(ac.Network, c.Contract) != nil {
			return errInvalid
		}
		// The documented FT response permits an omitted transaction_hash while
		// receipt indexing catches up. Such a candidate supplies no transfer
		// authority. Preserve checked tracked rows and keep this gap explicit;
		// untracked events cannot block the wallet's requested token history.
		if c.Hash == "" {
			if _, tracked := a.token(ac.Network, ac.Network+":nep141:"+c.Contract); !tracked {
				return nil
			}
			return fmt.Errorf("%w: indexed FT transaction binding is pending", errHistoricalUnavailable)
		}
	default:
		return errInvalid
	}
	// Transaction and native-receipt response schemas require this identity.
	// A supplied malformed identity is never treated as an availability gap.
	if !blockHash(c.Hash) {
		return errInvalid
	}
	return nil
}

func checkHistoricalCandidateEffect(candidate historicalCandidate, phase string, row model.Receipt) error {
	if phase != "ft-txns" {
		return nil
	}
	for _, effect := range row.Transfers {
		if effect.Asset.Contract == candidate.Contract && strings.HasPrefix(effect.ID, "nep141:"+candidate.Receipt+":") {
			return nil
		}
	}
	return fmt.Errorf("NEAR indexed token event has no exact successful owned RPC effect")
}
