package walletcore

import "testing"

func TestExchangeDepositUsesPrivateBybitSessionApprovalAndOriginalPaymentPipeline(t *testing.T) {
	c, _, adapter, path := fixtureExchangeCoordinator(t)
	result, err := c.handle(exchangeMessage("exchangeDeposit", map[string]any{"amount": "0.1"}))
	if err != nil || result.(exchangeResult).State != "CONFIRMED" || adapter.submits != 1 {
		t.Fatal("approved deposit did not retain its ordinary single-submission pipeline", result, err, adapter.submits)
	}
	state := readPublicState(t, path)
	counts := map[string]int{}
	for _, value := range state["calls"].([]any) {
		counts[value.(string)]++
	}
	if counts["prepareBybitDeposit"] != 1 || counts["signBybitDeposit"] != 1 || counts["prepare"] != 0 || counts["sign"] != 0 {
		t.Fatal("deposit did not use its privately bound Bybit approval path", counts)
	}
	if len(state["ops"].([]any)) != 1 {
		t.Fatal("approval created a duplicate payment")
	}
}

func TestPublicRouterCannotSelectBybitDepositSessionSigning(t *testing.T) {
	for _, method := range []string{"prepareBybitDeposit", "signBybitDeposit", "sendBybitDeposit"} {
		t.Run(method, func(t *testing.T) {
			c, _, adapter, path := fixtureExchangeCoordinator(t)
			if _, err := c.handle(message(method, map[string]string{"id": "untrusted-public-id"})); err == nil {
				t.Fatal("private deposit authority reached the public router")
			}
			if adapter.prepares != 0 || adapter.submits != 0 || len(readPublicState(t, path)["ops"].([]any)) != 0 {
				t.Fatal("private method rejection prepared or sent a payment")
			}
		})
	}
	for _, flag := range []string{"skipPassword", "sessionAuthorized", "bybitDeposit"} {
		t.Run(flag, func(t *testing.T) {
			c, _, adapter, _ := fixtureExchangeCoordinator(t)
			if _, err := c.handle(exchangeMessage("exchangeDeposit", map[string]any{"amount": "0.1", flag: true})); err == nil || adapter.prepares != 0 || adapter.submits != 0 {
				t.Fatal("caller-selected authentication flag was accepted", flag, err)
			}
		})
	}
}
