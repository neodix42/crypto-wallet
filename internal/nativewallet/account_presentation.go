package nativewallet

import (
	"errors"
	"localwallet/internal/derivation"
	"localwallet/internal/model"
	"localwallet/internal/uiprompt"
	"localwallet/internal/webui"
	"reflect"
	"strconv"
	"strings"
)

func (a *App) groupTitle(index uint32) string {
	if name := a.state.Presentation.GroupLabels[strconv.FormatUint(uint64(index), 10)]; name != "" {
		return name
	}
	return groupName(index)
}
func (a *App) walletTitle(account model.Account) string {
	if name := a.state.Presentation.WalletLabels[account.ID]; name != "" {
		return name
	}
	asset := a.asset(account)
	network := a.network(account.Network)
	if account.Family == "near" && a.fields["walletSection/"+account.ID] == "Staking" && len(network.Assets) > 0 {
		asset = network.Assets[0]
	}
	return asset.Symbol + " · " + network.Name
}
func (a *App) groupDeleted(index uint32) bool {
	return a.state.Presentation.DeletedGroups[strconv.FormatUint(uint64(index), 10)]
}
func (a *App) nextAccountIndex() (uint32, bool) {
	if a.state.NextAccountIndex != nil {
		return *a.state.NextAccountIndex, *a.state.NextAccountIndex < derivation.MaxIndex
	}
	if a.state.Revision != "" {
		return 0, false
	}
	var latest uint32
	found := false
	if a.latestAccountIndex != nil {
		latest = *a.latestAccountIndex
		found = true
	}
	for _, account := range a.state.Accounts {
		if !found || account.Index > latest {
			latest = account.Index
			found = true
		}
	}
	if !found {
		return 0, true
	}
	if latest >= derivation.MaxIndex-1 {
		return 0, false
	}
	return latest + 1, true
}
func (a *App) accountMenuNodes(index uint32) []webui.Node {
	_, available := a.nextAccountIndex()
	add := a.button("addNextAccount", "Add deterministic account", !available || !a.state.RecoveryConfirmed)
	if next, ok := a.nextAccountIndex(); ok {
		add.Tooltip = "Create the next deterministic account (index " + strconv.FormatUint(uint64(next), 10) + "). Existing and archived indexes are never reused."
	}
	key := strconv.FormatUint(uint64(index), 10)
	return []webui.Node{add, a.button("renameGroup/"+key, "Rename", !a.state.RecoveryConfirmed), a.button("deleteGroup/"+key, "Delete", a.groupDeleted(index) || !a.state.RecoveryConfirmed)}
}
func (a *App) accountPresentationEvent(kind, id string) bool {
	if kind != "renameWallet" && kind != "deleteWallet" && kind != "renameGroup" && kind != "deleteGroup" {
		return false
	}
	if a.closed || a.state.Locked || !a.state.RecoveryConfirmed || a.busy {
		return true
	}
	body := map[string]any{}
	var before model.Account
	var index uint32
	wallet := strings.HasSuffix(kind, "Wallet")
	title := ""
	if wallet {
		var ok bool
		before, ok = a.account(id)
		if !ok {
			return true
		}
		body["accountId"] = id
		title = a.walletTitle(before)
	} else {
		n, err := strconv.ParseUint(id, 10, 32)
		if err != nil || strconv.FormatUint(n, 10) != id {
			return true
		}
		index = uint32(n)
		owned := false
		for _, account := range a.state.Accounts {
			owned = owned || account.Index == index
		}
		if !owned {
			return true
		}
		body["index"] = index
		title = a.groupTitle(index)
	}
	if strings.HasPrefix(kind, "delete") {
		a.mutation(kind, body)
		return true
	}
	a.pausePendingPayments()
	a.stopBalanceRefresh(true)
	a.stopPaymentQuote()
	if a.historyCancel != nil {
		a.historyCancel()
	}
	epoch, generation, revision := a.epoch, a.core.Generation(), a.privateRevision.Load()
	a.busy = true
	a.render()
	ui := uiprompt.UI{Exchange: a.ui.Prompt}
	values, err := ui.PublicInput("Rename "+title, "New name")
	if a.closed || a.epoch != epoch {
		return true
	}
	a.busy = false
	if a.core.Generation() != generation {
		a.lock()
		return true
	}
	valid := !a.state.Locked && a.privateRevision.Load() == revision
	if wallet {
		current, ok := a.account(id)
		valid = valid && ok && reflect.DeepEqual(current, before) && a.walletTitle(current) == title
	} else {
		valid = valid && a.groupTitle(index) == title
	}
	if !valid {
		a.render()
		return true
	}
	if err != nil || len(values) != 1 {
		a.render()
		return true
	}
	name := strings.TrimSpace(values[0])
	if !model.AddressBookText(name, 80, false) {
		a.errorText("Rename account or wallet", "Enter a name of 1–80 bytes without control or invisible characters.")
		a.render()
		return true
	}
	body["name"] = name
	a.mutation(kind, body)
	return true
}

// Presentation values are page-scoped and must agree for every repeated group.
func mergePresentation(dst *model.AccountPresentation, src model.AccountPresentation, accounts []model.Account, seen map[string]bool) error {
	if !src.ValidFor(accounts) {
		return errors.New("invalid account presentation")
	}
	if dst.WalletLabels == nil {
		dst.WalletLabels = map[string]string{}
		dst.GroupLabels = map[string]string{}
		dst.DeletedGroups = map[string]bool{}
	}
	for id, name := range src.WalletLabels {
		if _, ok := dst.WalletLabels[id]; ok {
			return errors.New("duplicate wallet label")
		}
		dst.WalletLabels[id] = name
	}
	groups := map[string]bool{}
	for _, account := range accounts {
		groups[strconv.FormatUint(uint64(account.Index), 10)] = true
	}
	for key := range groups {
		if seen[key] && (dst.GroupLabels[key] != src.GroupLabels[key] || dst.DeletedGroups[key] != src.DeletedGroups[key]) {
			return errors.New("account group presentation changed")
		}
		seen[key] = true
		if name := src.GroupLabels[key]; name != "" {
			dst.GroupLabels[key] = name
		}
		if src.DeletedGroups[key] {
			dst.DeletedGroups[key] = true
		}
	}
	return nil
}
