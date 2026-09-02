package swap

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	applog "github.com/web3-wallet-org/web3-wallet-notes/src/swap/pkg/log"
)

// Service 是业务逻辑核心，所有 swap 流程都经过这里。
// 它只依赖接口（Repository、ChainClient、QuoteProvider），不直接依赖具体实现，
// 方便在测试中替换为 fake/mock。
type Service struct {
	cfg       Config
	repo      Repository               // 数据存储（quote、order、event）
	rpc       ChainClient              // 区块链 RPC（查 allowance、估 gas 等）
	providers map[string]QuoteProvider // key 为小写 provider 名，如 "0x"、"1inch"
	now       func() time.Time         // 注入时间函数，方便测试时伪造当前时间
}

func NewService(cfg Config, repo Repository, rpc ChainClient, providers []QuoteProvider, now func() time.Time) *Service {
	// 转成 map 便于按名字快速查找，key 统一小写避免大小写问题
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

// 响应结构体

type QuoteResponse struct {
	QuoteID            string                `json:"quoteId"`
	RecommendedQuoteID string                `json:"recommendedQuoteId"`
	ExpiresAt          int64                 `json:"expiresAt"`
	Deadline           *int64                `json:"deadline"`
	SelectedProvider   string                `json:"selectedProvider"`
	Spender            string                `json:"spender"`
	FromToken          TokenInfo             `json:"fromToken"`
	ToToken            TokenInfo             `json:"toToken"`
	AmountOut          string                `json:"amountOut"`
	MinAmountOut       string                `json:"minAmountOut"`
	GasUsd             string                `json:"gasUsd"`
	FeeUsd             string                `json:"feeUsd"`
	PriceImpactBps     int64                 `json:"priceImpactBps"`
	Route              []RouteStep           `json:"route"`
	Routes             []QuoteOptionResponse `json:"routes"`
}

type QuoteOptionResponse struct {
	QuoteID        string      `json:"quoteId"`
	Provider       string      `json:"provider"`
	Spender        string      `json:"spender"`
	ChainID        int64       `json:"chainId"`
	ExpiresAt      int64       `json:"expiresAt"`
	Deadline       *int64      `json:"deadline"`
	FromToken      TokenInfo   `json:"fromToken"`
	ToToken        TokenInfo   `json:"toToken"`
	AmountIn       string      `json:"amountIn"`
	AmountOut      string      `json:"amountOut"`
	MinAmountOut   string      `json:"minAmountOut"`
	GasUsd         string      `json:"gasUsd"`
	FeeUsd         string      `json:"feeUsd"`
	PriceImpactBps int64       `json:"priceImpactBps"`
	Route          []RouteStep `json:"route"`
}

// AllowanceResponse 告知前端当前授权是否充足，以及差多少。
// 前端根据 AllowanceEnough 决定是否弹 approve 流程。
type AllowanceResponse struct {
	AllowanceEnough  bool   `json:"allowanceEnough"`
	Spender          string `json:"spender"`
	RequiredAmount   string `json:"requiredAmount"`
	CurrentAllowance string `json:"currentAllowance"`
}

// ApproveTxResponse 返回待签名的 approve 交易体。
// GasPrice / MaxFeePerGas 二选一，根据链的 gasType 决定（BSC 用 legacy，Ethereum/Base 用 EIP1559）。
// omitempty 确保不相关的字段不出现在 JSON 里，避免前端混淆。
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

// ExecuteResponse 返回已创建的订单 ID 和待签名的 swap 交易体。
// GasType 在 transaction 外层，前端据此判断用哪套 gas 字段；
// 不要把 GasType 混入传给钱包 RPC 的交易对象，钱包不认识这个字段。
type ExecuteResponse struct {
	OrderID     string                `json:"orderId"`
	GasType     GasType               `json:"gasType"`
	Transaction InternalEvmTxEnvelope `json:"transaction"`
}

// StatusResponse 是轮询接口的响应。
// NextAction 指导前端下一步操作（wait / submit_hash / retry_quote / manual_review / nil 终态）。
// ExpiresAt 仅在 nextAction=retry_quote 时非 nil，供前端展示剩余可用时间倒计时。
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

// 核心业务方法

// provider 按名字查找 QuoteProvider，name 统一小写匹配。
func (s *Service) provider(name string) (QuoteProvider, error) {
	p, ok := s.providers[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return nil, ErrProviderDisabled
	}
	return p, nil
}

// Quote 同时向所有支持该链的 provider 请求报价，选出 amountOut 最大的一个返回给前端。
// 每个 provider 的原始报价都会单独落库（SaveQuote），供后续 execute 时重新校验价格用。
// 只要有一个 provider 成功就不会报错；全部失败才返回错误。
func (s *Service) Quote(ctx context.Context, input QuoteInput) (QuoteResponse, error) {
	if err := s.validateQuoteInput(input); err != nil {
		return QuoteResponse{}, err
	}
	providers, err := s.quoteProviders(input)
	if err != nil {
		return QuoteResponse{}, err
	}
	fromToken := s.cfg.Token(input.ChainID, input.FromToken)
	toToken := s.cfg.Token(input.ChainID, input.ToToken)
	pair := quotePair(fromToken, toToken)
	applog.FromContext(ctx).Infow("QUOTE request",
		"chainId", input.ChainID, "providerFilter", quoteProviderFilter(input.Provider),
		"userId", input.UserID, "wallet", input.WalletAddress,
		"fromSymbol", fromToken.Symbol, "fromAddress", fromToken.Address,
		"toSymbol", toToken.Symbol, "toAddress", toToken.Address,
		"amountIn", input.AmountIn, "slippageBps", input.SlippageBps)
	var rawQuotes []NormalizedQuote
	var errs []string
	for _, provider := range providers {
		if !containsInt64(provider.SupportedChains(), input.ChainID) {
			continue // 该 provider 不支持此链，直接跳过
		}
		quote, err := provider.GetQuote(input)
		if err != nil {
			applog.FromContext(ctx).Warnw("QUOTE provider failed",
				"provider", provider.Name(), "chainId", input.ChainID, "pair", pair, "amountIn", input.AmountIn, "err", err)
			errs = append(errs, fmt.Sprintf("%s: %v", provider.Name(), err))
			continue // 单个 provider 失败不影响其他 provider
		}
		quote.Provider = provider.Name()
		if quote.ID == "" {
			quote.ID = newID()
		}
		quote, err = s.resolveApprovalTarget(quote, provider)
		if err != nil {
			applog.FromContext(ctx).Warnw("QUOTE resolve approval target failed",
				"provider", provider.Name(), "chainId", input.ChainID, "pair", pair, "amountIn", input.AmountIn, "err", err)
			errs = append(errs, fmt.Sprintf("%s: %v", provider.Name(), err))
			continue
		}
		applog.FromContext(ctx).Infow("QUOTE provider ok",
			"provider", provider.Name(), "chainId", input.ChainID, "pair", pair,
			"amountIn", input.AmountIn, "amountOut", quote.AmountOut, "minAmountOut", quote.MinAmountOut,
			"gasUsd", quote.GasUSD, "spender", quote.Spender, "quoteId", quote.ID)
		rawQuotes = append(rawQuotes, quote)
		// 每个 provider 的原始报价单独落库，key 为随机 ID（不是 "最优报价" 的 ID）
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
	if err := s.sortQuoteCandidates(rawQuotes); err != nil {
		return QuoteResponse{}, err
	}
	best := rawQuotes[0]
	applog.FromContext(ctx).Infow("QUOTE selected",
		"provider", best.Provider, "chainId", input.ChainID, "pair", pair,
		"amountIn", input.AmountIn, "amountOut", best.AmountOut, "minAmountOut", best.MinAmountOut,
		"spender", best.Spender, "quoteId", best.ID, "routes", len(rawQuotes))
	// best 已经是已落库的候选 quote；旧顶层 quoteId 直接复用它的 ID。
	saved := best
	return QuoteResponse{
		QuoteID:            saved.ID,
		RecommendedQuoteID: saved.ID,
		ExpiresAt:          saved.ExpiresAt.UnixMilli(),
		Deadline:           saved.Deadline,
		SelectedProvider:   saved.Provider,
		Spender:            saved.Spender,
		FromToken:          saved.FromToken,
		ToToken:            saved.ToToken,
		AmountOut:          saved.AmountOut,
		MinAmountOut:       saved.MinAmountOut,
		GasUsd:             saved.GasUSD,
		FeeUsd:             saved.FeeUSD,
		PriceImpactBps:     saved.PriceImpactBps,
		Route:              saved.Route,
		Routes:             quoteOptionsFromCandidates(rawQuotes),
	}, nil
}

// Allowance 检查用户对 fromToken 的授权额度是否足以执行该 quote。
// native token（ETH/BNB）无需 approve，直接返回充足。
// spender 来自 quote 本身（provider 报价时确定），前端不传 spender，防止被篡改。
func (s *Service) Allowance(ctx context.Context, quoteID, walletAddress string) (AllowanceResponse, error) {
	if strings.TrimSpace(quoteID) == "" {
		return AllowanceResponse{}, invalidArgument("quoteId is required")
	}
	if strings.TrimSpace(walletAddress) == "" {
		return AllowanceResponse{}, invalidArgument("walletAddress is required")
	}
	applog.FromContext(ctx).Infow("ALLOWANCE request", "quoteId", quoteID, "wallet", walletAddress)
	quote, err := s.repo.GetQuote(quoteID)
	if err != nil {
		applog.FromContext(ctx).Errorw("ALLOWANCE get quote failed", "quoteId", quoteID, "wallet", walletAddress, "err", err)
		return AllowanceResponse{}, err
	}
	pair := quoteTokenPair(quote.FromToken, quote.ToToken)
	if s.now().After(quote.ExpiresAt) {
		applog.FromContext(ctx).Warnw("ALLOWANCE quote expired",
			"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
			"amountIn", quote.AmountIn, "wallet", walletAddress, "quoteId", quote.ID, "err", ErrQuoteExpired)
		return AllowanceResponse{}, ErrQuoteExpired
	}
	// native token 无 ERC20 合约，不存在 allowance 概念，直接放行
	if isNativeToken(quote.FromToken.Address) {
		applog.FromContext(ctx).Infow("ALLOWANCE native token skip",
			"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
			"amountIn", quote.AmountIn, "wallet", walletAddress, "quoteId", quote.ID,
			"allowanceEnough", true, "native", true)
		return AllowanceResponse{AllowanceEnough: true, Spender: quote.Spender, RequiredAmount: quote.AmountIn, CurrentAllowance: quote.AmountIn}, nil
	}
	quote, err = s.ensureApprovalTarget(quote)
	if err != nil {
		applog.FromContext(ctx).Errorw("ALLOWANCE ensure approval target failed",
			"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
			"amountIn", quote.AmountIn, "wallet", walletAddress, "quoteId", quote.ID, "err", err)
		return AllowanceResponse{}, err
	}
	current, err := s.rpc.GetAllowance(ctx, quote.ChainID, quote.FromToken.Address, walletAddress, quote.Spender)
	if err != nil {
		applog.FromContext(ctx).Errorw("ALLOWANCE get allowance failed",
			"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
			"amountIn", quote.AmountIn, "wallet", walletAddress, "spender", quote.Spender, "quoteId", quote.ID, "err", err)
		return AllowanceResponse{}, err
	}
	enough := decimalStringGreaterOrEqual(current, quote.AmountIn) // current >= amountIn 才算充足
	applog.FromContext(ctx).Infow("ALLOWANCE checked",
		"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
		"amountIn", quote.AmountIn, "wallet", walletAddress, "spender", quote.Spender,
		"currentAllowance", current, "allowanceEnough", enough, "quoteId", quote.ID, "native", false)
	return AllowanceResponse{
		AllowanceEnough:  enough,
		Spender:          quote.Spender,
		RequiredAmount:   quote.AmountIn,
		CurrentAllowance: current,
	}, nil
}

// ApproveTx 由 provider 构造 approve calldata；spender 因 provider 而异（0x 有 AllowanceHolder/Permit2），不能硬编码。
func (s *Service) ApproveTx(ctx context.Context, quoteID, walletAddress string) (ApproveTxResponse, error) {
	// spender/token/amount 以后端 quote 为准，防前端篡改。
	if strings.TrimSpace(quoteID) == "" {
		return ApproveTxResponse{}, invalidArgument("quoteId is required")
	}
	if strings.TrimSpace(walletAddress) == "" {
		return ApproveTxResponse{}, invalidArgument("walletAddress is required")
	}
	applog.FromContext(ctx).Infow("APPROVE_TX request", "quoteId", quoteID, "wallet", walletAddress)

	quote, err := s.repo.GetQuote(quoteID)
	if err != nil {
		applog.FromContext(ctx).Errorw("APPROVE_TX get quote failed", "quoteId", quoteID, "wallet", walletAddress, "err", err)
		return ApproveTxResponse{}, err
	}
	pair := quoteTokenPair(quote.FromToken, quote.ToToken)

	// quote 过期则链上价格已偏移，approve 后无法 execute。
	if s.now().After(quote.ExpiresAt) {
		applog.FromContext(ctx).Warnw("APPROVE_TX quote expired",
			"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
			"amountIn", quote.AmountIn, "wallet", walletAddress, "quoteId", quote.ID, "err", ErrQuoteExpired)
		return ApproveTxResponse{}, ErrQuoteExpired
	}

	// native token 无 ERC20 合约，不存在 approve。
	if isNativeToken(quote.FromToken.Address) {
		err := invalidArgument("native token does not require approve")
		applog.FromContext(ctx).Warnw("APPROVE_TX native token rejected",
			"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
			"amountIn", quote.AmountIn, "wallet", walletAddress, "quoteId", quote.ID, "err", err)
		return ApproveTxResponse{}, err
	}

	// 1inch 等 provider 的 spender 在 quote 阶段可能未返回，此处懒加载并缓存。
	quote, err = s.ensureApprovalTarget(quote)
	if err != nil {
		applog.FromContext(ctx).Errorw("APPROVE_TX ensure approval target failed",
			"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
			"amountIn", quote.AmountIn, "wallet", walletAddress, "quoteId", quote.ID, "err", err)
		return ApproveTxResponse{}, err
	}

	provider, err := s.provider(quote.Provider)
	if err != nil {
		applog.FromContext(ctx).Errorw("APPROVE_TX provider not found",
			"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
			"amountIn", quote.AmountIn, "wallet", walletAddress, "spender", quote.Spender, "quoteId", quote.ID, "err", err)
		return ApproveTxResponse{}, err
	}

	// 只构造 calldata，不检查 allowance 也不落库；前端签名后自行广播。
	env, err := provider.BuildApproveTx(quote, walletAddress)
	if err != nil {
		applog.FromContext(ctx).Errorw("APPROVE_TX build failed",
			"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
			"amountIn", quote.AmountIn, "wallet", walletAddress, "spender", quote.Spender,
			"token", quote.FromToken.Address, "quoteId", quote.ID, "err", err)
		return ApproveTxResponse{}, err
	}
	applog.FromContext(ctx).Infow("APPROVE_TX ok",
		"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
		"amountIn", quote.AmountIn, "wallet", walletAddress, "spender", quote.Spender,
		"token", quote.FromToken.Address, "txTo", env.To, "gasType", env.GasType, "gasLimit", env.GasLimit, "quoteId", quote.ID)

	// to=token 合约地址，data=approve(spender,amountIn) calldata，value=0。
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

// Execute 是 swap 的核心执行入口，流程：
//  1. 检查 quote 是否过期（expiresAt）
//  2. 检查 allowance（非 native token 必须已足额授权）
//  3. 风控校验（黑名单、minAmountOut 等）
//  4. 幂等处理：同一 quoteId 若已有订单，根据订单状态决定是复用还是拒绝
//  5. 构造 swap 交易并创建订单（状态 SIGNING）
func (s *Service) Execute(ctx context.Context, quoteID, walletAddress string, walletType WalletType) (ExecuteResponse, error) {
	// 参数校验
	if strings.TrimSpace(quoteID) == "" {
		return ExecuteResponse{}, invalidArgument("quoteId is required")
	}
	if strings.TrimSpace(walletAddress) == "" {
		return ExecuteResponse{}, invalidArgument("walletAddress is required")
	}
	if !validWalletType(walletType) {
		return ExecuteResponse{}, invalidArgument("walletType must be external, custody, or mpc")
	}
	applog.FromContext(ctx).Infow("EXECUTE request", "quoteId", quoteID, "wallet", walletAddress, "walletType", walletType)

	// 1. 加载 quote，验过期
	quote, err := s.repo.GetQuote(quoteID)
	if err != nil {
		applog.FromContext(ctx).Errorw("EXECUTE get quote failed", "quoteId", quoteID, "wallet", walletAddress, "walletType", walletType, "err", err)
		return ExecuteResponse{}, err
	}
	pair := quoteTokenPair(quote.FromToken, quote.ToToken)
	if s.now().After(quote.ExpiresAt) {
		applog.FromContext(ctx).Warnw("EXECUTE quote expired",
			"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
			"amountIn", quote.AmountIn, "wallet", walletAddress, "walletType", walletType, "quoteId", quote.ID, "err", ErrQuoteExpired)
		return ExecuteResponse{}, ErrQuoteExpired
	}

	// 2. 验 provider（短路优于后续 RPC 调用）
	provider, err := s.provider(quote.Provider)
	if err != nil {
		applog.FromContext(ctx).Errorw("EXECUTE provider not found",
			"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
			"amountIn", quote.AmountIn, "wallet", walletAddress, "walletType", walletType, "quoteId", quote.ID, "err", err)
		return ExecuteResponse{}, err
	}

	// 3. 验 allowance（/execute 独立校验，不信任前端 /allowance 预检结果）
	quote, err = s.executeCheckAllowance(ctx, quote, walletAddress, walletType)
	if err != nil {
		return ExecuteResponse{}, err
	}

	// 4. 风控
	if !s.validateRisk(quote) {
		applog.FromContext(ctx).Warnw("EXECUTE risk blocked",
			"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
			"amountIn", quote.AmountIn, "wallet", walletAddress, "walletType", walletType,
			"spender", quote.Spender, "quoteId", quote.ID, "err", ErrRiskBlocked)
		return ExecuteResponse{}, ErrRiskBlocked
	}

	// 5. 幂等：同 quoteId 已有订单，按状态决定复用或拒绝
	if existingOrder, err := s.repo.GetOrderByQuoteID(quote.ID); err == nil {
		resp, handled, err := s.executeHandleExisting(ctx, provider, quote, existingOrder, walletAddress, walletType)
		if handled {
			return resp, err
		}
	}

	// 6. 构造 swap 交易，创建新订单
	return s.executeNewOrder(ctx, provider, quote, walletAddress, walletType)
}

// executeCheckAllowance 校验链上 allowance 是否充足。
// 返回更新后的 quote（Spender 可能在此首次解析并缓存到 repo）。
func (s *Service) executeCheckAllowance(ctx context.Context, quote NormalizedQuote, walletAddress string, walletType WalletType) (NormalizedQuote, error) {
	pair := quoteTokenPair(quote.FromToken, quote.ToToken)
	if isNativeToken(quote.FromToken.Address) {
		applog.FromContext(ctx).Infow("EXECUTE allowance skipped",
			"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
			"amountIn", quote.AmountIn, "wallet", walletAddress, "walletType", walletType,
			"quoteId", quote.ID, "native", true)
		return quote, nil
	}
	var err error
	quote, err = s.ensureApprovalTarget(quote)
	if err != nil {
		applog.FromContext(ctx).Errorw("EXECUTE ensure approval target failed",
			"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
			"amountIn", quote.AmountIn, "wallet", walletAddress, "walletType", walletType, "quoteId", quote.ID, "err", err)
		return NormalizedQuote{}, err
	}
	allowance, err := s.rpc.GetAllowance(ctx, quote.ChainID, quote.FromToken.Address, walletAddress, quote.Spender)
	if err != nil {
		applog.FromContext(ctx).Errorw("EXECUTE get allowance failed",
			"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
			"amountIn", quote.AmountIn, "wallet", walletAddress, "walletType", walletType,
			"spender", quote.Spender, "quoteId", quote.ID, "err", err)
		return NormalizedQuote{}, err
	}
	if !decimalStringGreaterOrEqual(allowance, quote.AmountIn) {
		err := fmt.Errorf("%w: allowance insufficient", ErrInvalidArgument)
		applog.FromContext(ctx).Warnw("EXECUTE allowance insufficient",
			"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
			"amountIn", quote.AmountIn, "wallet", walletAddress, "walletType", walletType,
			"spender", quote.Spender, "currentAllowance", allowance, "allowanceEnough", false, "quoteId", quote.ID, "err", err)
		return NormalizedQuote{}, err
	}
	applog.FromContext(ctx).Infow("EXECUTE allowance ok",
		"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
		"amountIn", quote.AmountIn, "wallet", walletAddress, "walletType", walletType,
		"spender", quote.Spender, "currentAllowance", allowance, "allowanceEnough", true, "quoteId", quote.ID, "native", false)
	return quote, nil
}

// executeHandleExisting 处理同一 quoteId 已存在订单时的幂等逻辑。
// handled=true 表示已处理完毕，调用方直接返回；false 表示继续走创建新订单流程。
func (s *Service) executeHandleExisting(ctx context.Context, provider QuoteProvider, quote NormalizedQuote, existingOrder StoredOrder, walletAddress string, walletType WalletType) (ExecuteResponse, bool, error) {
	pair := quoteTokenPair(quote.FromToken, quote.ToToken)
	switch existingOrder.Status {
	case OrderStatusSigning:
		// quote 未过期但订单已存在且还没签名 → 重新构造交易体（gas 刷新），复用 orderId
		swapTx, err := provider.BuildSwapTx(quote, walletAddress)
		if err != nil {
			applog.FromContext(ctx).Errorw("EXECUTE rebuild swap tx failed",
				"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
				"amountIn", quote.AmountIn, "wallet", walletAddress, "walletType", walletType,
				"spender", quote.Spender, "quoteId", quote.ID, "orderId", existingOrder.ID, "status", existingOrder.Status, "err", err)
			return ExecuteResponse{}, true, err
		}
		existingOrder.TxPayload = swapTx
		existingOrder.GasType = swapTx.GasType
		existingOrder.WalletType = walletType
		existingOrder.WalletAddress = walletAddress
		if err := s.repo.UpdateOrder(existingOrder); err != nil {
			applog.FromContext(ctx).Errorw("EXECUTE update order failed",
				"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
				"amountIn", quote.AmountIn, "wallet", walletAddress, "walletType", walletType,
				"spender", quote.Spender, "quoteId", quote.ID, "orderId", existingOrder.ID, "status", existingOrder.Status, "err", err)
			return ExecuteResponse{}, true, err
		}
		if err := s.repo.AddEvent(SwapEvent{OrderID: existingOrder.ID, Status: OrderStatusSigning, Message: "order rebuilt"}); err != nil {
			applog.FromContext(ctx).Errorw("EXECUTE add event failed",
				"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
				"amountIn", quote.AmountIn, "wallet", walletAddress, "walletType", walletType,
				"spender", quote.Spender, "quoteId", quote.ID, "orderId", existingOrder.ID, "status", existingOrder.Status, "err", err)
			return ExecuteResponse{}, true, err
		}
		applog.FromContext(ctx).Infow("EXECUTE rebuilt",
			"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
			"amountIn", quote.AmountIn, "amountOut", quote.AmountOut, "minAmountOut", quote.MinAmountOut,
			"wallet", walletAddress, "walletType", walletType, "spender", quote.Spender,
			"txTo", swapTx.To, "gasType", swapTx.GasType, "gasLimit", swapTx.GasLimit,
			"quoteId", quote.ID, "orderId", existingOrder.ID, "status", OrderStatusSigning, "rebuilt", true)
		return ExecuteResponse{OrderID: existingOrder.ID, GasType: swapTx.GasType, Transaction: swapTx}, true, nil
	case OrderStatusBroadcasting, OrderStatusTxHashReceived, OrderStatusTxPending:
		// 交易已在链上流转，拒绝重复提交
		err := fmt.Errorf("%w: order already in progress", ErrConflict)
		applog.FromContext(ctx).Warnw("EXECUTE order in progress",
			"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
			"amountIn", quote.AmountIn, "wallet", walletAddress, "walletType", walletType,
			"spender", quote.Spender, "quoteId", quote.ID, "orderId", existingOrder.ID, "status", existingOrder.Status, "err", err)
		return ExecuteResponse{}, true, err
	case OrderStatusSigningTimeout, OrderStatusAwaitingTxHashTimeout, OrderStatusTxFailed, OrderStatusBroadcastFailed:
		// 可恢复终态，但必须重新 quote，不能复用旧 quoteId
		err := fmt.Errorf("%w: order already failed, re-quote required", ErrConflict)
		applog.FromContext(ctx).Warnw("EXECUTE order failed state",
			"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
			"amountIn", quote.AmountIn, "wallet", walletAddress, "walletType", walletType,
			"spender", quote.Spender, "quoteId", quote.ID, "orderId", existingOrder.ID, "status", existingOrder.Status, "err", err)
		return ExecuteResponse{}, true, err
	case OrderStatusCompleted, OrderStatusTxConfirmed:
		// 已完成，同一 quote 不能再次执行
		err := fmt.Errorf("%w: order already completed", ErrConflict)
		applog.FromContext(ctx).Warnw("EXECUTE order completed",
			"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
			"amountIn", quote.AmountIn, "wallet", walletAddress, "walletType", walletType,
			"spender", quote.Spender, "quoteId", quote.ID, "orderId", existingOrder.ID, "status", existingOrder.Status, "err", err)
		return ExecuteResponse{}, true, err
	}
	return ExecuteResponse{}, false, nil
}

// executeNewOrder 构造 swap 交易并创建新订单。
// CreateOrder 内部做并发安全的幂等插入（INSERT ON CONFLICT DO NOTHING）；
// created=false 说明另一个并发请求已先插入，直接返回冲突。
func (s *Service) executeNewOrder(ctx context.Context, provider QuoteProvider, quote NormalizedQuote, walletAddress string, walletType WalletType) (ExecuteResponse, error) {
	pair := quoteTokenPair(quote.FromToken, quote.ToToken)
	swapTx, err := provider.BuildSwapTx(quote, walletAddress)
	if err != nil {
		applog.FromContext(ctx).Errorw("EXECUTE build swap tx failed",
			"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
			"amountIn", quote.AmountIn, "wallet", walletAddress, "walletType", walletType,
			"spender", quote.Spender, "quoteId", quote.ID, "err", err)
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
		applog.FromContext(ctx).Errorw("EXECUTE create order failed",
			"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
			"amountIn", quote.AmountIn, "wallet", walletAddress, "walletType", walletType,
			"spender", quote.Spender, "txTo", swapTx.To, "gasType", swapTx.GasType, "gasLimit", swapTx.GasLimit,
			"quoteId", quote.ID, "err", err)
		return ExecuteResponse{}, err
	}
	if !created {
		err := fmt.Errorf("%w: order already exists", ErrConflict)
		applog.FromContext(ctx).Warnw("EXECUTE order already exists",
			"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
			"amountIn", quote.AmountIn, "wallet", walletAddress, "walletType", walletType,
			"spender", quote.Spender, "txTo", swapTx.To, "gasType", swapTx.GasType, "gasLimit", swapTx.GasLimit,
			"quoteId", quote.ID, "orderId", order.ID, "status", order.Status, "err", err)
		return ExecuteResponse{}, err
	}
	if err := s.repo.AddEvent(SwapEvent{OrderID: order.ID, Status: OrderStatusSigning, Message: "order created"}); err != nil {
		applog.FromContext(ctx).Errorw("EXECUTE add event failed",
			"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
			"amountIn", quote.AmountIn, "wallet", walletAddress, "walletType", walletType,
			"spender", quote.Spender, "txTo", swapTx.To, "gasType", swapTx.GasType, "gasLimit", swapTx.GasLimit,
			"quoteId", quote.ID, "orderId", order.ID, "status", order.Status, "err", err)
		return ExecuteResponse{}, err
	}
	applog.FromContext(ctx).Infow("EXECUTE created",
		"provider", quote.Provider, "chainId", quote.ChainID, "pair", pair,
		"amountIn", quote.AmountIn, "amountOut", quote.AmountOut, "minAmountOut", quote.MinAmountOut,
		"wallet", walletAddress, "walletType", walletType, "spender", quote.Spender,
		"txTo", swapTx.To, "gasType", swapTx.GasType, "gasLimit", swapTx.GasLimit,
		"quoteId", quote.ID, "orderId", order.ID, "status", OrderStatusSigning, "created", true)
	return ExecuteResponse{
		OrderID:     order.ID,
		GasType:     swapTx.GasType,
		Transaction: swapTx,
	}, nil
}

// SubmitHash 仅适用于外部钱包（MetaMask 等）。
// 外部钱包在用户设备上签名并广播后，后端不会自动感知 txHash，
// 所以前端必须在广播成功后立即调用此接口把 txHash 交给后端，
// 后端才能启动 Monitor 去链上轮询交易状态。
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
	// 幂等：同一 txHash 重复提交直接返回成功，不重复写库
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

// Status 返回订单当前状态和历史事件，供前端轮询使用。
// nextAction 告知前端下一步该做什么，避免前端自己判断状态逻辑。
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
		action := "wait" // 正在处理中，前端等待即可
		nextAction = &action
	case OrderStatusSigningTimeout, OrderStatusAwaitingTxHashTimeout, OrderStatusTxFailed, OrderStatusBroadcastFailed:
		action := "retry_quote" // 失败且不可恢复，需要重新走 quote 流程
		nextAction = &action
		retryable = true
	case OrderStatusSuspicious, OrderStatusManualReview:
		action := "manual_review" // txHash 内容异常，进入人工处理
		nextAction = &action
	case OrderStatusCompleted, OrderStatusTxConfirmed:
		nextAction = nil // 终态，不需要操作
	default:
		nextAction = nil
	}
	// SIGNING/BROADCASTING 状态下，如果订单创建后超过 30 分钟还没签名，
	// 返回过期时间供前端展示倒计时（实际关闭由后台超时任务处理）
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

// 内部辅助方法

// quoteProviders resolves the provider set for one quote request.
func (s *Service) quoteProviders(input QuoteInput) ([]QuoteProvider, error) {
	if strings.TrimSpace(input.Provider) != "" {
		provider, err := s.provider(input.Provider)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrProviderDisabled, input.Provider)
		}
		if !containsInt64(provider.SupportedChains(), input.ChainID) {
			return nil, ErrUnsupportedChain
		}
		return []QuoteProvider{provider}, nil
	}
	providers := make([]QuoteProvider, 0, len(s.providers))
	for _, provider := range s.providers {
		if containsInt64(provider.SupportedChains(), input.ChainID) {
			providers = append(providers, provider)
		}
	}
	sort.SliceStable(providers, func(i, j int) bool {
		return providers[i].Name() < providers[j].Name()
	})
	if len(providers) == 0 {
		return nil, ErrUnsupportedChain
	}
	return providers, nil
}

func quoteOptionsFromCandidates(quotes []NormalizedQuote) []QuoteOptionResponse {
	options := make([]QuoteOptionResponse, 0, len(quotes))
	for _, quote := range quotes {
		options = append(options, QuoteOptionResponse{
			QuoteID:        quote.ID,
			Provider:       quote.Provider,
			Spender:        quote.Spender,
			ChainID:        quote.ChainID,
			ExpiresAt:      quote.ExpiresAt.UnixMilli(),
			Deadline:       quote.Deadline,
			FromToken:      quote.FromToken,
			ToToken:        quote.ToToken,
			AmountIn:       quote.AmountIn,
			AmountOut:      quote.AmountOut,
			MinAmountOut:   quote.MinAmountOut,
			GasUsd:         quote.GasUSD,
			FeeUsd:         quote.FeeUSD,
			PriceImpactBps: quote.PriceImpactBps,
			Route:          quote.Route,
		})
	}
	return options
}

func quotePair(fromToken, toToken TokenConfig) string {
	return fromToken.Symbol + "->" + toToken.Symbol
}

func quoteTokenPair(fromToken, toToken TokenInfo) string {
	return fromToken.Symbol + "->" + toToken.Symbol
}

func quoteProviderFilter(provider string) string {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return "all"
	}
	return strings.ToLower(provider)
}

func validWalletType(walletType WalletType) bool {
	switch walletType {
	case WalletTypeExternal, WalletTypeCustody, WalletTypeMPC:
		return true
	default:
		return false
	}
}

func (s *Service) resolveApprovalTarget(quote NormalizedQuote, provider QuoteProvider) (NormalizedQuote, error) {
	if isNativeToken(quote.FromToken.Address) {
		quote.Spender = ""
		return quote, nil
	}
	if spender := strings.TrimSpace(quote.Spender); spender != "" {
		quote.Spender = spender
		return quote, nil
	}
	if provider == nil {
		var err error
		provider, err = s.provider(quote.Provider)
		if err != nil {
			return NormalizedQuote{}, err
		}
	}
	if !containsInt64(provider.SupportedChains(), quote.ChainID) {
		return NormalizedQuote{}, ErrUnsupportedChain
	}
	spender, err := provider.GetApprovalTarget(quote)
	if err != nil {
		return NormalizedQuote{}, err
	}
	spender = strings.TrimSpace(spender)
	if spender == "" {
		return NormalizedQuote{}, invalidArgument("approval target missing for provider %s", quote.Provider)
	}
	quote.Spender = spender
	return quote, nil
}

func (s *Service) ensureApprovalTarget(quote NormalizedQuote) (NormalizedQuote, error) {
	resolved, err := s.resolveApprovalTarget(quote, nil)
	if err != nil {
		return NormalizedQuote{}, err
	}
	if resolved.Spender == quote.Spender {
		return resolved, nil
	}
	quote = resolved
	return s.repo.SaveQuote(quote)
}

func (s *Service) sortQuoteCandidates(quotes []NormalizedQuote) error {
	if len(quotes) == 0 {
		return ErrNotFound
	}
	sort.SliceStable(quotes, func(i, j int) bool {
		cmp := decimalStringCmp(quotes[i].AmountOut, quotes[j].AmountOut)
		if cmp == 0 {
			return quotes[i].Provider < quotes[j].Provider
		}
		return cmp > 0
	})
	for _, quote := range quotes {
		if !decimalStringGreaterOrEqual(quote.MinAmountOut, "0") {
			return ErrInvalidArgument
		}
	}
	return nil
}

// selectBestQuote 按 amountOut 降序选出最优报价。
// 相同 amountOut 时按 provider 名字字母序兜底，保证结果稳定（不随 map 遍历顺序变化）。
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
	// minAmountOut 不能为 0，否则用户在极端滑点下可能收到 0 个 token
	if !decimalStringGreaterOrEqual(best.MinAmountOut, "0") {
		return NormalizedQuote{}, ErrInvalidArgument
	}
	return best, nil
}

// validateQuoteInput 检查用户输入的基本合法性，在调用 provider 之前做快速拦截。
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

// validateRisk 是 Phase 1 基础版风控，仅检查：
//   - minAmountOut 不为 0（链上最低收到量不能为零）
//   - fromToken / toToken 不在黑名单
//
// 增强版（eth_call 模拟、price impact 阈值、额度检查）在 Phase 1 后期补充。
func (s *Service) validateRisk(quote NormalizedQuote) bool {
	if quote.MinAmountOut == "0" {
		return false
	}
	if chain, ok := s.cfg.Chain(quote.ChainID); ok {
		_ = chain // 预留：后续可在此按链做额外校验
	}
	if _, blocked := s.cfg.TokenBlacklist[quote.ChainID][normalizeAddress(quote.FromToken.Address)]; blocked {
		return false
	}
	if _, blocked := s.cfg.TokenBlacklist[quote.ChainID][normalizeAddress(quote.ToToken.Address)]; blocked {
		return false
	}
	return true
}
