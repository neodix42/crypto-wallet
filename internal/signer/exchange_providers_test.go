package signer

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"localwallet/internal/chains/evm"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/model"
	"localwallet/internal/vault"
)

func TestExchangeProvidersKeepIndependentEncryptedCredentialsAndRestoreSelection(t *testing.T) {
	e, u, _ := exchangeEngine(t)
	ui := &exchangeConnectUI{testUI: u}
	e.UI = ui
	for _, provider := range []struct {
		id, endpoint string
		values       []string
	}{
		{"binance", "https://api.binance.com", []string{"PUBLIC_BINANCE_API_KEY_18474", "PUBLIC_BINANCE_API_SECRET_82474757"}},
		{"kucoin", "https://api.kucoin.com", []string{"PUBLIC_KUCOIN_API_KEY_725487", "PUBLIC_KUCOIN_API_SECRET_82474757", "PUBLIC_KUCOIN_PASSPHRASE_82474", "3"}},
	} {
		u.inputs = append(u.inputs, input(provider.values...))
		before := len(ui.titles)
		if err := e.ConnectExchangeProvider(provider.id, provider.endpoint); err != nil {
			t.Fatal("provider connection", err)
		}
		if len(ui.titles) != before+1 || len(ui.labels[len(ui.labels)-1]) != len(provider.values) {
			t.Fatal("connection requested another app password or lost required private fields")
		}
		status, err := e.ExchangeStatus()
		if err != nil || status.Provider != provider.id || status.Endpoint != provider.endpoint || len(status.Providers) != 3 {
			t.Fatal("selected connection lost")
		}
		public, _ := json.Marshal(status)
		privateValues := provider.values
		if provider.id == "kucoin" {
			privateValues = privateValues[:len(privateValues)-1]
		}
		for _, value := range privateValues {
			if bytes.Contains(public, []byte(value)) {
				t.Fatal("credential escaped status")
			}
			for _, file := range []string{"vault.lwv", "vault.previous.lwv"} {
				encrypted, err := e.Files.Read(file)
				if err != nil || bytes.Contains(encrypted, []byte(value)) {
					t.Fatal("credential persisted outside encryption")
				}
			}
		}
	}
	if e.data.Exchange.Credentials == nil || len(e.data.Exchange.ProviderCredentials) != 2 {
		t.Fatal("adding service destroyed Bybit credential")
	}
	key := e.data.Exchange.Credential("kucoin").APIKey
	secret := e.data.Exchange.Credential("kucoin").Secret
	phrase := e.data.Exchange.Credential("kucoin").Passphrase
	e.Lock()
	for _, value := range [][]byte{key, secret, phrase} {
		if !bytes.Equal(value, make([]byte, len(value))) {
			t.Fatal("provider secret survived lock")
		}
	}
	u.inputs = append(u.inputs, input(syntheticPassword))
	if err := e.Unlock(); err != nil {
		t.Fatal(err)
	}
	status, err := e.ExchangeStatus()
	if err != nil || status.Provider != "kucoin" || !status.Connected || e.data.Exchange.Credential("kucoin").KeyVersion != 3 || len(e.data.Exchange.ProviderCredentials) != 2 {
		t.Fatal("encrypted restart lost service")
	}
	if err = e.SelectExchange("bybit"); err != nil {
		t.Fatal(err)
	}
	status, _ = e.ExchangeStatus()
	if status.Provider != "bybit" || status.Endpoint != "https://api.bybit.com" {
		t.Fatal("legacy service not restored")
	}
	for _, result := range ui.returned {
		for _, value := range result {
			if !bytes.Equal(value, make([]byte, len(value))) {
				t.Fatal("private input survived prompt")
			}
		}
	}
}

func TestExchangeProviderSelectionCannotBypassUnresolvedOriginalOperation(t *testing.T) {
	e, u, account := exchangeEngine(t)
	operation, err := e.PrepareExchange(exchangeSwap(account))
	if err != nil {
		t.Fatal(err)
	}
	operation = authorizeExchange(t, e, u, operation)
	if e.SelectExchange("binance") == nil {
		t.Fatal("authorized operation allowed provider switch")
	}
	if e.ConnectExchangeProvider("bybit", "https://api.bybit.com") == nil {
		t.Fatal("credential reconnect bypassed outstanding authorization")
	}
	if _, err = e.ExchangeAuth(exchangeRequest(operation)); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"SUBMITTING", "UNKNOWN", "PENDING"} {
		if state != "SUBMITTING" {
			if err = e.UpdateExchange(operation.ID, state, ""); err != nil {
				t.Fatal(err)
			}
		}
		if e.SelectExchange("kucoin") == nil {
			t.Fatal("unresolved original identity bypassed by changing service")
		}
	}
	if err = e.UpdateExchange(operation.ID, "CONFIRMED", "original_remote_id"); err != nil {
		t.Fatal(err)
	}
	if err = e.SelectExchange("binance"); err != nil {
		t.Fatal("terminal operation prevented explicit service selection", err)
	}
	status, _ := e.ExchangeStatus()
	if status.Provider != "binance" || status.Connected || len(status.Operations) != 1 || status.Operations[0].ExternalID != "original_remote_id" {
		t.Fatal("selection lost historical identity or claimed a connection")
	}
}

func TestExchangeProviderReadAuthorizationBindsCurrentSelectedCredentialAndFreshness(t *testing.T) {
	e, u, _ := exchangeEngine(t)
	u.inputs = append(u.inputs, input("PUBLIC_KUCOIN_API_KEY_28757", "PUBLIC_KUCOIN_API_SECRET_8924724", "PUBLIC_KUCOIN_API_PASSPHRASE_47128", "2"))
	if err := e.ConnectExchangeProvider("kucoin", "https://api.kucoin.com"); err != nil {
		t.Fatal(err)
	}
	request := bybit.AuthRequest{Endpoint: "https://api.kucoin.com", Method: "GET", Path: "/api/v1/user/api-key", TimestampMS: time.Now().UnixMilli(), ReceiveWindowMS: 5000}
	auth, err := e.ExchangeAuth(request)
	if err != nil || auth.KeyVersion != 2 || auth.Passphrase == "" || auth.Passphrase == string(e.data.Exchange.Credential("kucoin").Passphrase) {
		t.Fatal("KuCoin passphrase header not transformed")
	}
	for _, mutate := range []func(*bybit.AuthRequest){func(r *bybit.AuthRequest) { r.Endpoint = "https://api.binance.com" }, func(r *bybit.AuthRequest) { r.Method = "POST" }, func(r *bybit.AuthRequest) { r.Path = "/api/v3/withdrawals" }, func(r *bybit.AuthRequest) { r.TimestampMS -= 5001 }, func(r *bybit.AuthRequest) { r.TimestampMS += 60000 }, func(r *bybit.AuthRequest) { r.Payload = "amount=1" }} {
		wrong := request
		mutate(&wrong)
		if _, err = e.ExchangeAuth(wrong); err == nil {
			t.Fatal("changed or stale request authenticated")
		}
	}
	if err = e.SelectExchange("bybit"); err != nil {
		t.Fatal(err)
	}
	if _, err = e.ExchangeAuth(request); err == nil {
		t.Fatal("old selected provider retained access")
	}
	e.Lock()
	if _, err = e.ExchangeAuth(request); err == nil {
		t.Fatal("locked service authorization")
	}
}

func TestExchangeNewProviderCredentialsRejectWrongServicePassphraseAndCancelledState(t *testing.T) {
	for _, mode := range []string{"wrong-endpoint", "missing-passphrase", "wrong-version", "control-passphrase", "cancel", "lock-confirm", "missing-provider"} {
		t.Run(mode, func(t *testing.T) {
			e, u, _ := exchangeEngine(t)
			ui := &exchangeConnectUI{testUI: u}
			e.UI = ui
			before, _ := e.Files.Read("vault.lwv")
			provider, endpoint := "kucoin", "https://api.kucoin.com"
			values := []string{"PUBLIC_KUCOIN_API_KEY_72457", "PUBLIC_KUCOIN_API_SECRET_724577", "PUBLIC_KUCOIN_API_PASSPHRASE_2747", "3"}
			switch mode {
			case "wrong-endpoint":
				endpoint = "https://api.binance.com"
			case "missing-passphrase":
				values[2] = ""
			case "wrong-version":
				values[3] = "1"
			case "control-passphrase":
				values[2] = "bad\npassphrase"
			case "cancel":
				u.approve = false
			case "lock-confirm":
				ui.confirmHook = e.Lock
			case "missing-provider":
				provider = ""
			}
			u.inputs = append(u.inputs, input(values...))
			if e.ConnectExchangeProvider(provider, endpoint) == nil {
				t.Fatal("invalid service credential accepted")
			}
			after, _ := e.Files.Read("vault.lwv")
			if !bytes.Equal(before, after) {
				t.Fatal("rejected connection changed encrypted state")
			}
		})
	}
}

func TestExchangeDisconnectClearsOnlySelectedServiceAndRequiresItsPassword(t *testing.T) {
	e, u, account := exchangeEngine(t)
	otherPrepared, err := e.PrepareExchange(exchangeSwap(account))
	if err != nil {
		t.Fatal(err)
	}
	u.inputs = append(u.inputs, input("PUBLIC_BINANCE_API_KEY_28475", "PUBLIC_BINANCE_API_SECRET_827457"))
	if err := e.ConnectExchangeProvider("binance", "https://api.binance.com"); err != nil {
		t.Fatal(err)
	}
	selectedPrepared, err := e.PrepareExchange(nativeExchangeSwap(account.ID))
	if err != nil {
		t.Fatal(err)
	}
	key := e.data.Exchange.Credential("binance").APIKey
	u.inputs = append(u.inputs, input("wrong-public-password"))
	if e.DisconnectExchange() == nil || e.data.Exchange.Credential("binance") == nil {
		t.Fatal("disconnect bypassed current password")
	}
	u.inputs = append(u.inputs, input(syntheticPassword))
	if err := e.DisconnectExchange(); err != nil {
		t.Fatal(err)
	}
	if e.data.Exchange.Credential("binance") != nil || e.data.Exchange.Credentials == nil || !bytes.Equal(key, make([]byte, len(key))) {
		t.Fatal("disconnect cleared wrong service or retained selected key")
	}
	oldOperation, oldErr := e.ExchangeOperation(otherPrepared.ID)
	selectedOperation, selectedErr := e.ExchangeOperation(selectedPrepared.ID)
	if oldErr != nil || selectedErr != nil || oldOperation.State != "PREPARED" || selectedOperation.State != "CANCELLED" {
		t.Fatal("disconnect cancelled another service draft or retained selected draft")
	}
	if err := e.SelectExchange("bybit"); err != nil {
		t.Fatal(err)
	}
	status, _ := e.ExchangeStatus()
	if !status.Connected {
		t.Fatal("another provider disconnect removed Bybit")
	}
}

func TestExchangeLegacyApprovalDigestIsUnchangedByOtherProviderCredentials(t *testing.T) {
	e, u, account := exchangeEngine(t)
	operation, err := e.PrepareExchange(exchangeSwap(account))
	if err != nil {
		t.Fatal(err)
	}
	before := vault.ExchangeDigest(operation)
	u.inputs = append(u.inputs, input("PUBLIC_BINANCE_API_KEY_85724", "PUBLIC_BINANCE_API_SECRET_827457"))
	if err = e.ConnectExchangeProvider("binance", "https://api.binance.com"); err != nil {
		t.Fatal(err)
	}
	if _, err = e.AuthorizeExchange(operation.ID); err == nil {
		t.Fatal("other service approved legacy prepared operation")
	}
	if err = e.SelectExchange("bybit"); err != nil {
		t.Fatal(err)
	}
	stored, err := e.ExchangeOperation(operation.ID)
	if err != nil || stored.Digest != before || vault.ExchangeDigest(stored) != before || !strings.Contains(stored.Endpoint, "bybit") {
		t.Fatal("service credentials rewrote original operation")
	}
}

func TestExchangeProviderCredentialsAreNotReconstructedFromRecoveryPhrase(t *testing.T) {
	original, ui, _ := exchangeEngine(t)
	ui.inputs = append(ui.inputs, input("PUBLIC_KUCOIN_API_KEY_53824", "PUBLIC_KUCOIN_API_SECRET_528472", "PUBLIC_KUCOIN_API_PASSPHRASE_27345", "3"))
	if err := original.ConnectExchangeProvider("kucoin", "https://api.kucoin.com"); err != nil {
		t.Fatal(err)
	}
	restored, otherUI, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, restored, otherUI, "mainnet")
	want := map[string]string{}
	for _, account := range original.Snapshot().Accounts {
		want[account.Network] = account.Address
	}
	for _, account := range restored.Snapshot().Accounts {
		if want[account.Network] != account.Address {
			t.Fatal("phrase restore changed canonical wallet")
		}
		delete(want, account.Network)
	}
	if len(want) != 0 {
		t.Fatal("phrase restore lost wallet networks")
	}
	status, err := restored.ExchangeStatus()
	if err != nil || status.Provider != "bybit" || status.Connected || len(status.Operations) != 0 {
		t.Fatal("phrase restored custodial credentials/history or inherited another profile selection")
	}
	for _, provider := range status.Providers {
		if provider.Connected || provider.Endpoint != "" || provider.KeySuffix != "" {
			t.Fatal("phrase restored provider secret metadata")
		}
	}
}
