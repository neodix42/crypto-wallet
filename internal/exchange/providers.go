package exchange

import "localwallet/internal/exchange/bybit"

// Provider contains only public, reviewed service configuration. Credentials
// are supplied separately through the wallet's masked dialog and encrypted vault.
type Provider struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Endpoints          []string `json:"endpoints"`
	RequiresPassphrase bool     `json:"requiresPassphrase"`
}

func Providers() []Provider {
	return []Provider{
		{ID: "bybit", Name: "Bybit", Endpoints: bybit.Endpoints()},
		{ID: "binance", Name: "Binance", Endpoints: []string{"https://api.binance.com"}},
		{ID: "kucoin", Name: "KuCoin", Endpoints: []string{"https://api.kucoin.com"}, RequiresPassphrase: true},
	}
}

func ProviderByID(id string) (Provider, bool) {
	if id == "" {
		id = "bybit"
	}
	for _, provider := range Providers() {
		if provider.ID == id {
			return provider, true
		}
	}
	return Provider{}, false
}

func ProviderForEndpoint(endpoint string) (Provider, bool) {
	for _, provider := range Providers() {
		for _, allowed := range provider.Endpoints {
			if endpoint == allowed {
				return provider, true
			}
		}
	}
	return Provider{}, false
}

func ValidateProviderEndpoint(id, endpoint string) bool {
	provider, ok := ProviderByID(id)
	actual, exact := ProviderForEndpoint(endpoint)
	return ok && exact && actual.ID == provider.ID
}
