package walletcore

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"localwallet/internal/chains/chainutil"
	"localwallet/internal/model"
)

// Validate interpreted fields before any adapter observation reaches the public
// page. Provider text never supplies browser authority or an asset policy.
func validateHistoryReceipt(adapter model.Adapter, account model.Account, row model.Receipt) error {
	text := func(value string, limit int) bool {
		return len(value) <= limit && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
	}
	if row.Identity == "" || !text(row.Identity, 512) || !text(row.State, 64) || !text(row.Block, 512) || !text(row.Detail, historyPageBytes) || row.Timestamp < 0 || row.Timestamp > time.Now().Unix()+300 || len(row.Transfers) > 1000 {
		return errors.New("invalid structured history transaction")
	}
	if row.Explorer != "" && row.Explorer != chainutil.TransactionExplorer(account.Network, row.Identity) {
		return errors.New("history explorer does not match the selected network and transaction")
	}
	assets := map[string]model.Asset{}
	for _, network := range adapter.Networks() {
		if network.ID == account.Network {
			for _, asset := range network.Assets {
				assets[asset.ID] = asset
			}
		}
	}
	seen := map[string]bool{}
	for _, effect := range row.Transfers {
		asset, registered := assets[effect.Asset.ID]
		if effect.ID == "" || !text(effect.ID, 256) || seen[effect.ID] || !text(effect.Source, 4096) || !text(effect.Destination, 4096) || !text(effect.Comment, 4096) || !registered || asset != effect.Asset || effect.Asset.Decimals > 77 {
			return errors.New("invalid or untracked structured history effect")
		}
		if _, err := chainutil.Big(effect.Atomic, 256); err != nil {
			return errors.New("invalid structured history quantity")
		}
		seen[effect.ID] = true
	}
	return nil
}
