// Package bybit implements the bounded Bybit V5 spot/funding API. It never
// receives an API secret: a caller-owned authorizer signs each exact request.
package bybit

import (
	"context"
	"encoding/json"
)

// AuthRequest binds the exact endpoint, method and wire payload. Payload is the
// URL-encoded query for GET and the exact JSON body for POST.
type AuthRequest struct {
	Endpoint        string `json:"endpoint"`
	Method          string `json:"method"`
	Path            string `json:"path"`
	Payload         string `json:"payload"`
	TimestampMS     int64  `json:"timestampMs"`
	ReceiveWindowMS int64  `json:"receiveWindowMs"`
}
type Auth struct {
	APIKey     string `json:"apiKey"`
	Signature  string `json:"signature"`
	Passphrase string `json:"passphrase,omitempty"`
	KeyVersion int    `json:"keyVersion,omitempty"`
}
type Authorizer func(context.Context, AuthRequest) (Auth, error)

type APIKeyInformation struct {
	UserID      int64 `json:"userID"`
	ReadOnly    int   `json:"readOnly"`
	IsMaster    bool  `json:"isMaster"`
	UTA         int   `json:"uta"`
	Permissions struct {
		Spot   []string `json:"Spot"`
		Wallet []string `json:"Wallet"`
	} `json:"permissions"`
}
type Coin struct {
	Name   string  `json:"name"`
	Coin   string  `json:"coin"`
	Chains []Chain `json:"chains"`
}
type Chain struct {
	Chain                 string `json:"chain"`
	ChainType             string `json:"chainType"`
	Confirmation          string `json:"confirmation"`
	WithdrawFee           string `json:"withdrawFee"`
	DepositMin            string `json:"depositMin"`
	WithdrawMin           string `json:"withdrawMin"`
	MinAccuracy           string `json:"minAccuracy"`
	ChainDeposit          string `json:"chainDeposit"`
	ChainWithdraw         string `json:"chainWithdraw"`
	WithdrawPercentageFee string `json:"withdrawPercentageFee"`
	ContractAddress       string `json:"contractAddress"`
	SafeConfirmNumber     string `json:"safeConfirmNumber"`
	WithdrawMax           string `json:"withdrawMax"`
}
type Deposit struct {
	ChainType         string `json:"chainType"`
	Address           string `json:"addressDeposit"`
	Tag               string `json:"tagDeposit"`
	Chain             string `json:"chain"`
	ContractAddress   string `json:"contractAddress"`
	BatchReleaseLimit string `json:"batchReleaseLimit"`
}
type Balance struct {
	AccountType string `json:"accountType"`
	AccountID   string `json:"accountId"`
	MemberID    string `json:"memberId"`
	Balance     struct {
		Coin               string `json:"coin"`
		WalletBalance      string `json:"walletBalance"`
		TransferBalance    string `json:"transferBalance"`
		TransferSafeAmount string `json:"transferSafeAmount"`
	} `json:"balance"`
}
type Instrument struct {
	Symbol        string `json:"symbol"`
	BaseCoin      string `json:"baseCoin"`
	QuoteCoin     string `json:"quoteCoin"`
	Status        string `json:"status"`
	LotSizeFilter struct {
		BasePrecision     string `json:"basePrecision"`
		QuotePrecision    string `json:"quotePrecision"`
		MinOrderAmt       string `json:"minOrderAmt"`
		MaxMarketOrderQty string `json:"maxMarketOrderQty"`
	} `json:"lotSizeFilter"`
	PriceFilter struct {
		TickSize string `json:"tickSize"`
	} `json:"priceFilter"`
}
type Ticker struct {
	Symbol    string `json:"symbol"`
	LastPrice string `json:"lastPrice"`
	Bid1Price string `json:"bid1Price"`
	Ask1Price string `json:"ask1Price"`
}
type FeeRate struct {
	Symbol       string `json:"symbol"`
	TakerFeeRate string `json:"takerFeeRate"`
	MakerFeeRate string `json:"makerFeeRate"`
}

// SpotOrder is intentionally limited to unleveraged spot market orders.
// Buy spends quoteCoin; Sell spends baseCoin. Slippage is a percentage.
type SpotOrder struct {
	Category              string `json:"category"`
	Symbol                string `json:"symbol"`
	Side                  string `json:"side"`
	OrderType             string `json:"orderType"`
	Qty                   string `json:"qty"`
	MarketUnit            string `json:"marketUnit"`
	IsLeverage            int    `json:"isLeverage"`
	SlippageToleranceType string `json:"slippageToleranceType"`
	SlippageTolerance     string `json:"slippageTolerance"`
	OrderLinkID           string `json:"orderLinkId"`
	// Native-only bounded IOC policy for Binance/KuCoin. Empty for Bybit.
	LimitPrice string `json:"limitPrice,omitempty"`
	BaseQty    string `json:"baseQty,omitempty"`
}
type OrderAcknowledgement struct {
	OrderID     string `json:"orderId"`
	OrderLinkID string `json:"orderLinkId"`
}
type Order struct {
	OrderID      string `json:"orderId"`
	OrderLinkID  string `json:"orderLinkId"`
	Symbol       string `json:"symbol"`
	Side         string `json:"side"`
	OrderType    string `json:"orderType"`
	Qty          string `json:"qty"`
	MarketUnit   string `json:"marketUnit"`
	IsLeverage   string `json:"isLeverage"`
	OrderStatus  string `json:"orderStatus"`
	CumExecQty   string `json:"cumExecQty"`
	CumExecValue string `json:"cumExecValue"`
	CumExecFee   string `json:"cumExecFee"`
	AvgPrice     string `json:"avgPrice"`
	RejectReason string `json:"rejectReason"`
	CreatedTime  string `json:"createdTime"`
	UpdatedTime  string `json:"updatedTime"`
	BaseQty      string `json:"baseQty,omitempty"`
	LimitPrice   string `json:"limitPrice,omitempty"`
	TimeInForce  string `json:"timeInForce,omitempty"`
}
type Execution struct {
	Symbol      string          `json:"symbol"`
	OrderID     string          `json:"orderId"`
	OrderLinkID string          `json:"orderLinkId"`
	Side        string          `json:"side"`
	OrderQty    string          `json:"orderQty"`
	OrderType   string          `json:"orderType"`
	ExecID      string          `json:"execId"`
	ExecPrice   string          `json:"execPrice"`
	ExecQty     string          `json:"execQty"`
	ExecValue   string          `json:"execValue"`
	ExecFee     string          `json:"execFee"`
	FeeCurrency string          `json:"feeCurrency"`
	ExecTime    string          `json:"execTime"`
	ExtraFees   json.RawMessage `json:"extraFees"`
}
type ExecutionPage struct {
	Category       string      `json:"category"`
	List           []Execution `json:"list"`
	NextPageCursor string      `json:"nextPageCursor"`
}
type TransferRequest struct {
	TransferID      string `json:"transferId"`
	Coin            string `json:"coin"`
	Amount          string `json:"amount"`
	FromAccountType string `json:"fromAccountType"`
	ToAccountType   string `json:"toAccountType"`
}
type TransferAcknowledgement struct {
	TransferID string `json:"transferId"`
	Status     string `json:"status"`
}
type TransferRecord struct {
	TransferID      string `json:"transferId"`
	Coin            string `json:"coin"`
	Amount          string `json:"amount"`
	FromAccountType string `json:"fromAccountType"`
	ToAccountType   string `json:"toAccountType"`
	Timestamp       string `json:"timestamp"`
	Status          string `json:"status"`
}
type WithdrawalRequest struct {
	Coin        string          `json:"coin"`
	Chain       string          `json:"chain"`
	Address     string          `json:"address"`
	Tag         string          `json:"tag,omitempty"`
	Amount      string          `json:"amount"`
	Timestamp   int64           `json:"timestamp"`
	ForceChain  int             `json:"forceChain"`
	AccountType string          `json:"accountType"`
	FeeType     int             `json:"feeType"`
	RequestID   string          `json:"requestId"`
	Beneficiary *UAEBeneficiary `json:"beneficiary,omitempty"`
	// Native provider fee contract; never accepted by the Bybit validator.
	FeeMode string `json:"feeMode,omitempty"`
}
type WithdrawalAcknowledgement struct {
	ID string `json:"id"`
}
type WithdrawalRecord struct {
	RequestID    string `json:"requestId,omitempty"`
	TxID         string `json:"txID"`
	Coin         string `json:"coin"`
	Chain        string `json:"chain"`
	Amount       string `json:"amount"`
	WithdrawFee  string `json:"withdrawFee"`
	Status       string `json:"status"`
	ToAddress    string `json:"toAddress"`
	Tag          string `json:"tag"`
	CreateTime   string `json:"createTime"`
	UpdateTime   string `json:"updateTime"`
	WithdrawID   string `json:"withdrawId"`
	WithdrawType int    `json:"withdrawType"`
}
type WithdrawalPage struct {
	Rows           []WithdrawalRecord `json:"rows"`
	NextPageCursor string             `json:"nextPageCursor"`
}
type DepositRecord struct {
	ID               string `json:"id"`
	Coin             string `json:"coin"`
	Chain            string `json:"chain"`
	Amount           string `json:"amount"`
	TxID             string `json:"txID"`
	Status           int    `json:"status"`
	ToAddress        string `json:"toAddress"`
	Tag              string `json:"tag"`
	SuccessAt        string `json:"successAt"`
	Confirmations    string `json:"confirmations"`
	DepositFee       string `json:"depositFee"`
	DepositType      string `json:"depositType"`
	TravelRuleStatus int    `json:"travelRuleStatus"`
}
type DepositPage struct {
	Rows           []DepositRecord `json:"rows"`
	NextPageCursor string          `json:"nextPageCursor"`
}
