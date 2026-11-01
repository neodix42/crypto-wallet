package evm

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/net/html"
	"localwallet/internal/model"
)

const publicHistoryPageSize = 25
const publicHistoryMaxPages = 400
const publicHistorySource = "arbiscan-public-v1"

// The public explorer is a last-resort candidate index, not a source of amounts,
// execution, ownership, token metadata or finality. No HTML is rendered and no
// provider links, scripts, cookies or advertised API keys are followed.
func (a *Adapter) historyPublicCandidates(ctx context.Context, account model.Account, page int, tokens bool, previous string) ([]string, bool, string, error) {
	if account.Network != "arbitrum-mainnet" || page < 1 || page > publicHistoryMaxPages || previous != "" && !hashValid(previous) {
		return nil, false, "", errors.New("invalid public history position")
	}
	path := "/txs"
	if tokens {
		path = "/tokentxns"
	}
	query := url.Values{"a": {strings.ToLower(account.Address)}, "p": {strconv.Itoa(page)}, "ps": {strconv.Itoa(publicHistoryPageSize)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://arbiscan.io"+path+"?"+query.Encode(), nil)
	if err != nil || a.HTTPClient == nil {
		return nil, false, "", errHistoryIndexUnavailable
	}
	req.Header.Set("Accept", "text/html")
	req.Header.Set("User-Agent", "CryptoWallet")
	client := *a.HTTPClient
	// Index reads carry only the public address, including when a caller uses a
	// cookie jar for its separately configured RPC. No HTTP session is needed.
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return nil, false, "", errHistoryIndexUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, false, "", errHistoryIndexUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 {
		return nil, false, "", errors.New("public history response exceeded bound")
	}
	ids, err := publicHistoryIdentities(raw, account.Address)
	if err != nil {
		return nil, false, "", err
	}
	// A repeated page must not manufacture an advancing continuation. Keep only
	// a commitment, never HTML or displayed metadata, inside the private cursor.
	commitment := crypto.Keccak256Hash([]byte(strings.Join(ids, ","))).Hex()
	if len(ids) != 0 && commitment == previous {
		return nil, false, "", errors.New("public history page did not advance; refresh required")
	}
	return ids, len(ids) < publicHistoryPageSize, commitment, nil
}

// Parse a single bounded explorer transaction table. Only exact relative tx
// paths inside its body become candidates. Challenge/login/error pages, changed
// table schemas and malformed rows fail rather than looking like empty history.
func publicHistoryIdentities(raw []byte, owner string) ([]string, error) {
	if len(raw) > 2<<20 || !strings.HasPrefix(strings.ToLower(owner), "0x") || len(owner) != 42 {
		return nil, errors.New("invalid public history input")
	}
	z := html.NewTokenizer(bytes.NewReader(raw))
	z.SetMaxBuf(2 << 20)
	var ids []string
	var inTable, inHead, inBody, inRow bool
	var validHeader, headerHash, headerFrom, headerTo, foundTable bool
	var rowID string
	rowOwned, emptyMarker := false, false
	rowText, rowEmpty, rowEmptyHint, rowLayout, rowActivity := false, false, false, false, false
	finishRow := func(implicit bool) error {
		if rowID != "" {
			if implicit || !rowOwned || emptyMarker || rowEmpty || len(ids) >= publicHistoryPageSize {
				return errors.New("unbound or excessive public history row")
			}
			ids = append(ids, rowID)
			return nil
		}
		if rowOwned || rowText || rowActivity || rowEmptyHint && !rowEmpty || rowEmpty && len(ids) != 0 {
			return errors.New("public history row is missing its transaction identity")
		}
		if rowEmpty {
			emptyMarker = true
			return nil
		}
		if rowLayout || implicit && emptyMarker && len(ids) == 0 {
			return nil
		}
		return errors.New("unrecognized public history row")
	}
	depth, tableDepth := 0, 0
	rows := 0
	for count := 0; ; count++ {
		if count > 131072 {
			return nil, errors.New("public history token count exceeded bound")
		}
		typ := z.Next()
		if typ == html.ErrorToken {
			if z.Err() != io.EOF || !foundTable || inTable || inRow {
				return nil, errors.New("invalid public history table")
			}
			if len(ids) == 0 && !emptyMarker {
				return nil, errors.New("public history missing explicit empty result")
			}
			return ids, nil
		}
		t := z.Token()
		switch typ {
		case html.StartTagToken, html.SelfClosingTagToken:
			// Count nested table elements explicitly; general HTML has void tags
			// and optional end tags, so ordinary tag depth is not an authority.
			if t.Data == "table" {
				depth++
				if depth > 8 || inTable || foundTable {
					return nil, errors.New("ambiguous public history table")
				}
				inTable, tableDepth = true, depth
			}
			if !inTable {
				continue
			}
			switch t.Data {
			case "thead":
				inHead = true
			case "tbody":
				validHeader = headerHash && headerFrom && headerTo
				if !validHeader {
					return nil, errors.New("public history table schema changed")
				}
				inBody = true
			case "tr":
				if inBody {
					if inRow {
						if err := finishRow(true); err != nil {
							return nil, err
						}
					}
					inRow, rowOwned, rowID = true, false, ""
					rowText, rowEmpty, rowEmptyHint, rowLayout, rowActivity = false, false, false, false, false
					for _, attr := range t.Attr {
						if attr.Key == "style" && strings.TrimSpace(attr.Val) == "visibility:collapse" {
							rowLayout = true
						}
					}
					rows++
					if rows > publicHistoryPageSize+2 { // empty pages may include a layout row
						return nil, errors.New("public history row count exceeded bound")
					}
				}
			}
			if inBody && inRow {
				switch t.Data {
				case "a", "button", "script", "img", "input", "svg", "iframe":
					rowActivity = true
				}
				for _, attr := range t.Attr {
					if (attr.Key == "data-full-address" || attr.Key == "data-highlight-target") && strings.EqualFold(attr.Val, owner) {
						rowOwned = true
					}
					if t.Data != "a" || attr.Key != "href" || !strings.HasPrefix(attr.Val, "/tx/") {
						continue
					}
					id := strings.TrimPrefix(attr.Val, "/tx/")
					if !hashValid(id) || id != strings.ToLower(id) || rowID != "" && rowID != id {
						return nil, errors.New("invalid public history candidate identity")
					}
					rowID = id
				}
			}
		case html.TextToken:
			text := strings.TrimSpace(t.Data)
			if inHead {
				headerHash = headerHash || text == "Transaction Hash"
				headerFrom = headerFrom || text == "From"
				headerTo = headerTo || text == "To"
			}
			if inBody && !inRow && text != "" {
				return nil, errors.New("public history text is outside its row")
			}
			if inBody && inRow && text != "" {
				if text == "There are no matching entries" {
					rowEmpty = true
				} else if text == "Please try again later" {
					rowEmptyHint = true
				} else {
					rowText = true
				}
			}
		case html.EndTagToken:
			if t.Data == "thead" {
				inHead = false
			}
			if t.Data == "tr" && inBody && inRow {
				if err := finishRow(false); err != nil {
					return nil, err
				}
				inRow = false
			}
			if t.Data == "tbody" {
				// Public explorer empty placeholders sometimes omit </tr>.
				if inRow {
					if err := finishRow(true); err != nil {
						return nil, err
					}
				}
				inBody, inRow = false, false
			}
			if t.Data == "table" {
				if inTable && depth == tableDepth {
					inTable, foundTable = false, validHeader
				}
				depth--
			}
		}
	}
}
