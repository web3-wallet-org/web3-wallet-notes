package swap

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Service struct {
	cfg       Config
	repo      Repository
	rpc       ChainClient
	providers map[string]QuoteProvider
	now       func() time.Time
}

func NewService(cfg Config, repo Repository, rpc ChainClient, providers []QuoteProvider, now func() time.Time) *Service {
	providerMap := map[string]QuoteProvider{}
	for _, provider := range providers {
		providerMap[strings.ToLower(provider.Name())] = provider
	}
	return &Service{
		cfg:       cfg,
		repo:      repo,
		rpc:       rpc,
		providers: providerMap,
		now:       now,
	}
}

type QuoteResponse struct {
	QuoteID          string      `json:"quoteId"`
	ExpiresAt        int64       `json:"expiresAt"`
	Deadline         *int64      `json:"deadline"`
	SelectedProvider string      `json:"selectedProvider"`
	FromToken        TokenInfo   `json:"fromToken"`
	ToToken          TokenInfo   `json:"toToken"`
	AmountOut        string      `json:"amountOut"`
	MinAmountOut     string      `json:"minAmountOut"`
	GasUsd           string      `json:"gasUsd"`
	FeeUsd           string      `json:"feeUsd"`
	PriceImpactBps   int64       `json:"priceImpactBps"`
	Route            []RouteStep `json:"route"`
}

type AllowanceResponse struct {
	AllowanceEnough  bool   `json:"allowanceEnough"`
	Spender          string `json:"spender"`
	RequiredAmount   string `json:"requiredAmount"`
	CurrentAllowance string `json:"currentAllowance"`
}

type ApproveTxResponse struct {
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

type ExecuteResponse struct {
	OrderID     string                `json:"orderId"`
	GasType     GasType               `json:"gasType"`
	Transaction InternalEvmTxEnvelope `json:"transaction"`
}

type StatusResponse struct {
	OrderID      string      `json:"orderId"`
	Status       OrderStatus `json:"status"`
	TxHash       string      `json:"txHash"`
	ActualOut    string      `json:"actualOut"`
	ErrorMessage string      `json:"errorMessage"`
	NextAction   *string     `json:"nextAction"`
	Retryable    bool        `json:"retryable"`
	ExpiresAt    *int64      `json:"expiresAt"`
	Events       []SwapEvent `json:"events"`
}

type SubmitHashRequest struct {
	OrderID string `json:"orderId"`
	TxHash  string `json:"txHash"`
}

func (s *Service) provider(name string) (QuoteProvider, error) {
	p, ok := s.providers[strings.ToLower(name)]
	if !ok {
		return nil, ErrProviderDisabled
	}
	return p, nil
}

func (s *Service) Quote(ctx context.Context, input QuoteInput) (QuoteResponse, error) {
	if err := s.validateQuoteInput(input); err != nil {
		return QuoteResponse{}, err
	}
	var rawQuotes []NormalizedQuote
	var errs []string
	for _, provider := range s.providers {
		if !containsInt64(provider.SupportedChains(), input.ChainID) {
			continue
		}
		quote, err := provider.GetQuote(input)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", provider.Name(), err))
			continue
		}
		quote.Provider = provider.Name()
		rawQuotes = append(rawQuotes, quote)
		if _, err := s.repo.SaveQuote(quote); err != nil {
			return QuoteResponse{}, err
		}
	}
	if len(rawQuotes) == 0 {
		if len(errs) > 0 {
			return QuoteResponse{}, fmt.Errorf("all providers failed: %s", strings.Join(errs, "; "))
		}
		return QuoteResponse{}, ErrUnsupportedChain
	}
	best, err := s.selectBestQuote(rawQuotes)
	if err != nil {
		return QuoteResponse{}, err
	}
	saved, err := s.repo.SaveQuote(best)
	if err != nil {
		return QuoteResponse{}, err
	}
	return QuoteResponse{
		QuoteID:          saved.ID,
		ExpiresAt:        saved.ExpiresAt.UnixMilli(),
		Deadline:         saved.Deadline,
		SelectedProvider: saved.Provider,
		FromToken:        saved.FromToken,
		ToToken:          saved.ToToken,
		AmountOut:        saved.AmountOut,
		MinAmountOut:     saved.MinAmountOut,
		GasUsd:           saved.GasUSD,
		FeeUsd:           saved.FeeUSD,
		PriceImpactBps:   saved.PriceImpactBps,
		Route:            saved.Route,
	}, nil
}

func (s *Service) Allowance(ctx context.Context, quoteID, walletAddress string) (AllowanceResponse, error) {
	quote, err := s.repo.GetQuote(quoteID)
	if err != nil {
		return AllowanceResponse{}, err
	}
	if s.now().After(quote.ExpiresAt) {
		return AllowanceResponse{}, ErrQuoteExpired
	}
	if isNativeToken(quote.FromToken.Address) {
		return AllowanceResponse{AllowanceEnough: true, Spender: quote.Spender, RequiredAmount: quote.AmountIn, CurrentAllowance: quote.AmountIn}, nil
	}
	current, err := s.rpc.GetAllowance(ctx, quote.ChainID, quote.FromToken.Address, walletAddress, quote.Spender)
	if err != nil {
		return AllowanceResponse{}, err
	}
	enough := decimalStringGreaterOrEqual(current, quote.AmountIn)
	return AllowanceResponse{
		AllowanceEnough:  enough,
		Spender:          quote.Spender,
		RequiredAmount:   quote.AmountIn,
		CurrentAllowance: current,
	}, nil
}

func (s *Service) ApproveTx(ctx context.Context, quoteID, walletAddress string) (ApproveTxResponse, error) {
	quote, err := s.repo.GetQuote(quoteID)
	if err != nil {
		return ApproveTxResponse{}, err
	}
	if s.now().After(quote.ExpiresAt) {
		return ApproveTxResponse{}, ErrQuoteExpired
	}
	provider, err := s.provider(quote.Provider)
	if err != nil {
		return ApproveTxResponse{}, err
	}
	env, err := provider.BuildApproveTx(quote, walletAddress)
	if err != nil {
		return ApproveTxResponse{}, err
	}
	return ApproveTxResponse{
		GasType:              env.GasType,
		ChainID:              env.ChainID,
		To:                   env.To,
		Data:                 env.Data,
		Value:                env.Value,
		GasLimit:             env.GasLimit,
		GasPrice:             env.GasPrice,
		MaxFeePerGas:         env.MaxFeePerGas,
		MaxPriorityFeePerGas: env.MaxPriorityFeePerGas,
	}, nil
}

func (s *Service) Execute(ctx context.Context, quoteID, walletAddress string, walletType WalletType) (ExecuteResponse, error) {
	quote, err := s.repo.GetQuote(quoteID)
	if err != nil {
		return ExecuteResponse{}, err
	}
	if s.now().After(quote.ExpiresAt) {
		return ExecuteResponse{}, ErrQuoteExpired
	}
	provider, err := s.provider(quote.Provider)
	if err != nil {
		return ExecuteResponse{}, err
	}
	if isNativeToken(quote.FromToken.Address) == false {
		allowance, err := s.rpc.GetAllowance(ctx, quote.ChainID, quote.FromToken.Address, walletAddress, quote.Spender)
		if err != nil {
			return ExecuteResponse{}, err
		}
		if !decimalStringGreaterOrEqual(allowance, quote.AmountIn) {
			return ExecuteResponse{}, fmt.Errorf("%w: allowance不足", ErrInvalidArgument)
		}
	}
	if !s.validateRisk(quote) {
		return ExecuteResponse{}, ErrRiskBlocked
	}
	existingOrder, err := s.repo.GetOrderByQuoteID(quote.ID)
	if err == nil {
		switch existingOrder.Status {
		case OrderStatusSigning:
			swapTx, err := provider.BuildSwapTx(quote, walletAddress)
			if err != nil {
				return ExecuteResponse{}, err
			}
			existingOrder.TxPayload = swapTx
			existingOrder.GasType = swapTx.GasType
			existingOrder.WalletType = walletType
			existingOrder.WalletAddress = walletAddress
			if err := s.repo.UpdateOrder(existingOrder); err != nil {
				return ExecuteResponse{}, err
			}
			if err := s.repo.AddEvent(SwapEvent{OrderID: existingOrder.ID, Status: OrderStatusSigning, Message: "order rebuilt"}); err != nil {
				return ExecuteResponse{}, err
			}
			return ExecuteResponse{OrderID: existingOrder.ID, GasType: swapTx.GasType, Transaction: swapTx}, nil
		case OrderStatusBroadcasting, OrderStatusTxHashReceived, OrderStatusTxPending:
			return ExecuteResponse{}, fmt.Errorf("%w: order already in progress", ErrConflict)
		case OrderStatusSigningTimeout, OrderStatusAwaitingTxHashTimeout, OrderStatusTxFailed, OrderStatusBroadcastFailed:
			return ExecuteResponse{}, fmt.Errorf("%w: order already failed, re-quote required", ErrConflict)
		case OrderStatusCompleted, OrderStatusTxConfirmed:
			return ExecuteResponse{}, fmt.Errorf("%w: order already completed", ErrConflict)
		}
	}

	swapTx, err := provider.BuildSwapTx(quote, walletAddress)
	if err != nil {
		return ExecuteResponse{}, err
	}
	order, created, err := s.repo.CreateOrder(StoredOrder{
		QuoteID:       quote.ID,
		UserID:        quote.QuoteInput.UserID,
		WalletType:    walletType,
		WalletAddress: walletAddress,
		ChainID:       quote.ChainID,
		Status:        OrderStatusSigning,
		Spender:       quote.Spender,
		TransactionTo: quote.TransactionTo,
		GasType:       swapTx.GasType,
		TxPayload:     swapTx,
	})
	if err != nil {
		return ExecuteResponse{}, err
	}
	if !created {
		return ExecuteResponse{}, fmt.Errorf("%w: order already exists", ErrConflict)
	}
	if err := s.repo.AddEvent(SwapEvent{OrderID: order.ID, Status: OrderStatusSigning, Message: "order created"}); err != nil {
		return ExecuteResponse{}, err
	}
	return ExecuteResponse{
		OrderID:     order.ID,
		GasType:     swapTx.GasType,
		Transaction: swapTx,
	}, nil
}

func (s *Service) SubmitHash(ctx context.Context, req SubmitHashRequest) error {
	order, err := s.repo.GetOrder(req.OrderID)
	if err != nil {
		return err
	}
	if order.WalletType != WalletTypeExternal {
		return fmt.Errorf("%w: submit-hash only for external wallets", ErrInvalidArgument)
	}
	if order.Status != OrderStatusSigning && order.Status != OrderStatusBroadcasting && order.Status != OrderStatusTxHashReceived {
		return fmt.Errorf("%w: order not in submit-hash allowed state", ErrConflict)
	}
	if req.TxHash == "" {
		return fmt.Errorf("%w: txHash required", ErrInvalidArgument)
	}
	if sameAddress(req.TxHash, order.TxHash) && order.TxHash != "" {
		return nil
	}
	order.TxHash = req.TxHash
	order.Status = OrderStatusTxHashReceived
	if err := s.repo.UpdateOrder(order); err != nil {
		return err
	}
	if err := s.repo.AddEvent(SwapEvent{OrderID: order.ID, Status: OrderStatusTxHashReceived, Message: "tx hash received"}); err != nil {
		return err
	}
	return nil
}

func (s *Service) Status(ctx context.Context, orderID string) (StatusResponse, error) {
	order, err := s.repo.GetOrder(orderID)
	if err != nil {
		return StatusResponse{}, err
	}
	events, _ := s.repo.ListEvents(order.ID)
	var nextAction *string
	retryable := false
	var expiresAt *int64
	switch order.Status {
	case OrderStatusSigning, OrderStatusBroadcasting, OrderStatusTxHashReceived, OrderStatusTxPending:
		action := "wait"
		nextAction = &action
	case OrderStatusSigningTimeout, OrderStatusAwaitingTxHashTimeout, OrderStatusTxFailed, OrderStatusBroadcastFailed:
		action := "retry_quote"
		nextAction = &action
		retryable = true
	case OrderStatusSuspicious, OrderStatusManualReview:
		action := "manual_review"
		nextAction = &action
	case OrderStatusCompleted, OrderStatusTxConfirmed:
		nextAction = nil
	default:
		nextAction = nil
	}
	if !order.UpdatedAt.IsZero() && (order.Status == OrderStatusSigning || order.Status == OrderStatusBroadcasting) {
		if order.UpdatedAt.Add(30 * time.Minute).After(s.now()) {
			exp := order.UpdatedAt.Add(30 * time.Minute).UnixMilli()
			expiresAt = &exp
		}
	}
	return StatusResponse{
		OrderID:      order.ID,
		Status:       order.Status,
		TxHash:       order.TxHash,
		ActualOut:    "",
		ErrorMessage: order.ErrorMessage,
		NextAction:   nextAction,
		Retryable:    retryable,
		ExpiresAt:    expiresAt,
		Events:       events,
	}, nil
}

func (s *Service) selectBestQuote(quotes []NormalizedQuote) (NormalizedQuote, error) {
	if len(quotes) == 0 {
		return NormalizedQuote{}, ErrNotFound
	}
	sort.SliceStable(quotes, func(i, j int) bool {
		cmp := decimalStringCmp(quotes[i].AmountOut, quotes[j].AmountOut)
		if cmp == 0 {
			return quotes[i].Provider < quotes[j].Provider
		}
		return cmp > 0
	})
	best := quotes[0]
	if !decimalStringGreaterOrEqual(best.MinAmountOut, "0") {
		return NormalizedQuote{}, ErrInvalidArgument
	}
	return best, nil
}

func (s *Service) validateQuoteInput(input QuoteInput) error {
	if input.ChainID == 0 {
		return invalidArgument("chainId is required")
	}
	if input.FromToken == "" || input.ToToken == "" {
		return invalidArgument("fromToken and toToken are required")
	}
	if input.AmountIn == "" || !validUnsignedDecimal(input.AmountIn) {
		return invalidArgument("amountIn must be unsigned decimal string")
	}
	if input.SlippageBps < 0 {
		return invalidArgument("slippageBps must be non-negative")
	}
	if input.WalletAddress == "" {
		return invalidArgument("walletAddress is required")
	}
	if sameAddress(input.FromToken, input.ToToken) {
		return invalidArgument("fromToken and toToken must differ")
	}
	return nil
}

func (s *Service) validateRisk(quote NormalizedQuote) bool {
	if quote.MinAmountOut == "0" {
		return false
	}
	if chain, ok := s.cfg.Chain(quote.ChainID); ok {
		_ = chain
	}
	if _, blocked := s.cfg.TokenBlacklist[quote.ChainID][normalizeAddress(quote.FromToken.Address)]; blocked {
		return false
	}
	if _, blocked := s.cfg.TokenBlacklist[quote.ChainID][normalizeAddress(quote.ToToken.Address)]; blocked {
		return false
	}
	return true
}
