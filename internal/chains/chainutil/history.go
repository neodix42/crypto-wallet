package chainutil

import (
	"errors"
	"strings"
	"unicode/utf8"

	"localwallet/internal/model"
)

// MergeHistory combines separately verified effects of the same transaction.
// Conflicting inclusion/status/time or duplicate effect identities fail closed.
func MergeHistory(rows []model.Receipt) ([]model.Receipt, error) {
	out := []model.Receipt{}
	positions := map[string]int{}
	effects := map[string]bool{}
	for _, row := range rows {
		if row.Identity == "" || len(row.Identity) > 512 || !utf8.ValidString(row.Identity) || row.Timestamp < 0 || len(row.Transfers) > 1000 {
			return nil, errors.New("invalid structured history")
		}
		for _, effect := range row.Transfers {
			key := row.Identity + "\x00" + effect.ID
			if effect.ID == "" || len(effect.ID) > 256 || effects[key] || len(effect.Comment) > 4096 || !utf8.ValidString(effect.Comment) || strings.ContainsRune(effect.Comment, 0) || effect.Asset.ID == "" || effect.Asset.Decimals > 77 {
				return nil, errors.New("invalid or duplicate history effect")
			}
			if _, err := Big(effect.Atomic, 256); err != nil {
				return nil, err
			}
			effects[key] = true
		}
		if index, ok := positions[row.Identity]; ok {
			old := &out[index]
			if old.State != row.State || old.Block != row.Block || old.Timestamp != row.Timestamp || old.Explorer != row.Explorer {
				return nil, errors.New("conflicting history transaction observations")
			}
			if len(row.Transfers) > 1000-len(old.Transfers) {
				return nil, errors.New("history transaction effect count exceeds bound")
			}
			old.Transfers = append(old.Transfers, row.Transfers...)
		} else {
			positions[row.Identity] = len(out)
			out = append(out, row)
		}
	}
	return out, nil
}
