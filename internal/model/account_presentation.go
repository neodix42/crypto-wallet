package model

import "strconv"

// AccountPresentation is local encrypted display metadata. It is never part of
// an Account descriptor, transaction plan, signing digest or key derivation.
type AccountPresentation struct {
	WalletLabels  map[string]string `json:"walletLabels,omitempty"`
	GroupLabels   map[string]string `json:"groupLabels,omitempty"`
	DeletedGroups map[string]bool   `json:"deletedGroups,omitempty"`
}

func (p AccountPresentation) IsZero() bool {
	return len(p.WalletLabels) == 0 && len(p.GroupLabels) == 0 && len(p.DeletedGroups) == 0
}

func AccountGroupKey(index uint32) string { return strconv.FormatUint(uint64(index), 10) }
func PresentationGroupIndex(key string) (uint32, bool) {
	if len(key) == 0 || len(key) > 10 {
		return 0, false
	}
	n, err := strconv.ParseUint(key, 10, 32)
	return uint32(n), err == nil && strconv.FormatUint(n, 10) == key
}
func (p AccountPresentation) ValidFor(accounts []Account) bool {
	if len(p.WalletLabels) > 10000 || len(p.GroupLabels) > 10000 || len(p.DeletedGroups) > 10000 {
		return false
	}
	ids := map[string]bool{}
	groups := map[string]bool{}
	for _, a := range accounts {
		ids[a.ID] = true
		groups[AccountGroupKey(a.Index)] = true
	}
	for id, label := range p.WalletLabels {
		if !ids[id] || !AddressBookText(label, 80, false) {
			return false
		}
	}
	for key, label := range p.GroupLabels {
		if _, ok := PresentationGroupIndex(key); !ok || !groups[key] || !AddressBookText(label, 80, false) {
			return false
		}
	}
	for key, deleted := range p.DeletedGroups {
		if _, ok := PresentationGroupIndex(key); !ok || !groups[key] || !deleted {
			return false
		}
	}
	return true
}
func (p AccountPresentation) ForAccounts(accounts []Account) AccountPresentation {
	out := AccountPresentation{}
	for _, a := range accounts {
		if label, ok := p.WalletLabels[a.ID]; ok {
			if out.WalletLabels == nil {
				out.WalletLabels = map[string]string{}
			}
			out.WalletLabels[a.ID] = label
		}
		key := AccountGroupKey(a.Index)
		if label, ok := p.GroupLabels[key]; ok {
			if out.GroupLabels == nil {
				out.GroupLabels = map[string]string{}
			}
			out.GroupLabels[key] = label
		}
		if p.DeletedGroups[key] {
			if out.DeletedGroups == nil {
				out.DeletedGroups = map[string]bool{}
			}
			out.DeletedGroups[key] = true
		}
	}
	return out
}
