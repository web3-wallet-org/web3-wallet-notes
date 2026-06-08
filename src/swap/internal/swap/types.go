package swap

import "time"

const NativeTokenAddress = "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE"

type GasType string

const (
	GasTypeEIP1559 GasType = "eip1559"
	GasTypeLegacy  GasType = "legacy"
)

type WalletType string

const (
	WalletTypeExternal WalletType = "external"
	WalletTypeCustody  WalletType = "custody"
	WalletTypeMPC      WalletType = "mpc"
)

type OrderStatus string

const (
	OrderStatusSigning               OrderStatus = "SIGNING"
	OrderStatusBroadcasting          OrderStatus = "BROADCASTING"
	OrderStatusTxHashReceived        OrderStatus = "TX_HASH_RECEIVED"
	OrderStatusTxPending             OrderStatus = "TX_PENDING"
	OrderStatusSuspicious            OrderStatus = "SUSPICIOUS"
	OrderStatusManualReview          OrderStatus = "MANUAL_REVIEW"
	OrderStatusTxFailed              OrderStatus = "TX_FAILED"
	OrderStatusTxConfirmed           OrderStatus = "TX_CONFIRMED"
	OrderStatusCompleted             OrderStatus = "COMPLETED"
	OrderStatusUserRejected          OrderStatus = "USER_REJECTED"
	OrderStatusSigningTimeout        OrderStatus = "SIGNING_TIMEOUT"
	OrderStatusBroadcastFailed       OrderStatus = "BROADCAST_FAILED"
	OrderStatusAwaitingTxHashTimeout OrderStatus = "AWAITING_TX_HASH_TIMEOUT"
)

type RiskDecision string

const (
	RiskPass         RiskDecision = "PASS"
	RiskWarn         RiskDecision = "WARN"
	RiskBlock        RiskDecision = "BLOCK"
	RiskManualReview RiskDecision = "MANUAL_REVIEW"
)

type TokenInfo struct {
	Address  string `json:"address"`
	Symbol   string `json:"symbol"`
	Decimals int    `json:"decimals"`
}

func (t TokenConfig) Info() TokenInfo {
	return TokenInfo{
		Address:  t.Address,
		Symbol:   t.Symbol,
		Decimals: t.Decimals,
	}
}

type RouteStep struct {
	Protocol    string    `json:"protocol"`
	FromToken   TokenInfo `json:"fromToken"`
	ToToken     TokenInfo `json:"toToken"`
	AmountIn    string    `json:"amountIn"`
	AmountOut   string    `json:"amountOut"`
	PoolAddress string    `json:"poolAddress,omitempty"`
}

type QuoteInput struct {
	UserID        string `json:"userId"`
	ChainID       int64  `json:"chainId"`
	FromToken     string `json:"fromToken"`
	ToToken       string `json:"toToken"`
	AmountIn      string `json:"amountIn"`
	SlippageBps   int64  `json:"slippageBps"`
	WalletAddress string `json:"walletAddress"`
	Provider      string `json:"provider,omitempty"`
}

type NormalizedQuote struct {
	ID             string      `json:"quoteId,omitempty"`
	Provider       string      `json:"provider"`
	ChainID        int64       `json:"chainId"`
	FromToken      TokenInfo   `json:"fromToken"`
	ToToken        TokenInfo   `json:"toToken"`
	AmountIn       string      `json:"amountIn"`
	AmountOut      string      `json:"amountOut"`
	MinAmountOut   string      `json:"minAmountOut"`
	GasUSD         string      `json:"gasUsd"`
	FeeUSD         string      `json:"feeUsd"`
	PriceImpactBps int64       `json:"priceImpactBps"`
	Spender        string      `json:"spender"`
	TransactionTo  string      `json:"transactionTo"`
	Route          []RouteStep `json:"route"`
	RawQuote       any         `json:"rawQuote,omitempty"`
	ExpiresAt      time.Time   `json:"expiresAt"`
	Deadline       *int64      `json:"deadline"`
	QuoteInput     QuoteInput  `json:"-"`
}

type InternalEvmTxEnvelope struct {
	GasType              GasType `json:"gasType"`
	ChainID              int64   `json:"chainId"`
	To                   string  `json:"to"`
	Data                 string  `json:"data"`
	Value                string  `json:"value"`
	GasLimit             string  `json:"gasLimit"`
	GasPrice             string  `json:"gasPrice,omitempty"`
	MaxFeePerGas         string  `json:"maxFeePerGas,omitempty"`
	MaxPriorityFeePerGas string  `json:"maxPriorityFeePerGas,omitempty"`
}

type QuoteProvider interface {
	Name() string
	SupportedChains() []int64
	GetQuote(input QuoteInput) (NormalizedQuote, error)
	GetApprovalTarget(quote NormalizedQuote) (string, error)
	BuildApproveTx(quote NormalizedQuote, taker string) (InternalEvmTxEnvelope, error)
	BuildSwapTx(quote NormalizedQuote, taker string) (InternalEvmTxEnvelope, error)
}

type StoredOrder struct {
	ID            string
	QuoteID       string
	UserID        string
	WalletType    WalletType
	WalletAddress string
	ChainID       int64
	Status        OrderStatus
	Spender       string
	TransactionTo string
	GasType       GasType
	TxPayload     InternalEvmTxEnvelope
	TxHash        string
	ErrorMessage  string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type SwapEvent struct {
	OrderID string      `json:"orderId"`
	Status  OrderStatus `json:"status"`
	Message string      `json:"message,omitempty"`
	At      time.Time   `json:"at"`
}
