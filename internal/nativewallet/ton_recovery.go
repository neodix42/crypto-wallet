package nativewallet

import (
	"encoding/json"
	"errors"
	"strconv"

	"localwallet/internal/chains/ton"
	"localwallet/internal/derivation"
	"localwallet/internal/ipc"
	"localwallet/internal/model"
)

// Recovery selects a separately authenticated identity. It never edits the
// contract parameters of the wallet currently displayed in another tab.
func (a *App) recoverTONProfile(index uint32, profile string) {
	if a.closed || a.state.Locked || !a.state.RecoveryConfirmed || a.busy {
		return
	}
	if index >= derivation.MaxIndex || profile != ton.Profile && profile != ton.ProfileSubwallet42 {
		a.notice("Choose a supported TON profile and a valid account index.")
		a.render()
		return
	}
	network := "ton-mainnet"
	if a.environment == "testnet" {
		network = "ton-testnet"
	}
	if len(a.tabs) >= 64 {
		alreadyOpen := false
		for _, account := range a.state.Accounts {
			if account.Network == network && account.Index == index && account.Profile == profile {
				for _, tab := range a.tabs {
					alreadyOpen = alreadyOpen || tab == account.ID
				}
			}
		}
		if !alreadyOpen {
			a.notice("Close a wallet tab before opening another TON profile.")
			a.render()
			return
		}
	}
	opened, recovered := false, false
	a.workWithFeedback("Recovering the selected TON profile…", func(g uint64) (func(), error) {
		var raw json.RawMessage
		err := a.callAt(g, "addTonAccount", map[string]any{"index": index, "profile": profile}, &raw)
		clear(raw)
		if err != nil {
			return nil, err
		}
		state, err := readAccounts(a.core, g)
		if err != nil || state.Locked || !state.RecoveryConfirmed || state.Environment != a.environment {
			return nil, errors.New("recovered TON account snapshot is unavailable")
		}
		var selected model.Account
		matches := 0
		for _, account := range state.Accounts {
			if account.Family == "ton" && account.Network == network && account.Index == index && account.Profile == profile && !account.Hidden {
				selected = account
				matches++
			}
		}
		if matches != 1 || ton.ValidateAccount(selected) != nil {
			return nil, errors.New("recovered TON profile identity is unavailable")
		}
		var networks []model.Network
		if err := a.callAt(g, "networks", struct{}{}, &networks); err != nil {
			return nil, err
		}
		return func() {
			found := false
			for _, id := range a.tabs {
				found = found || id == selected.ID
			}
			if !found {
				if len(a.tabs) >= 64 {
					a.applySnapshot(state, networks)
					recovered = true
					return
				}
				a.tabs = append(a.tabs, selected.ID)
			}
			a.active = "wallet/" + selected.ID
			a.applySnapshot(state, networks)
			opened, recovered = true, true
		}, nil
	}, func(err error) {
		if ipc.PublicErrorCode(err) == "REQUEST_CANCELLED" {
			a.notice("TON profile recovery cancelled. Existing wallets are unchanged.")
			return
		}
		if err != nil || !recovered {
			a.notice("The selected TON profile could not be opened. Recovery does not deploy a contract or send funds. Existing wallet addresses are unchanged.")
			return
		}
		if !opened {
			a.notice("TON profile recovered. Close a wallet tab, then open the recovered profile from the main account.")
			return
		}
		id := 0
		if profile == ton.ProfileSubwallet42 {
			id = 42
		}
		a.notice("Opened TON wallet ID " + strconv.Itoa(id) + ", account index " + strconv.FormatUint(uint64(index), 10) + ".")
	})
}
