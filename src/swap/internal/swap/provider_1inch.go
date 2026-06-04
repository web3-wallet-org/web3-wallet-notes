package swap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type OneInchProvider struct {
	httpClient *http.Client
	cfg        Config
}

func NewOneInchProvider(httpClient *http.Client, cfg Config) *OneInchProvider {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &OneInchProvider{httpClient: httpClient, cfg: cfg}
}

func (p *OneInchProvider) Name() string { return "1inch" }

func (p *OneInchProvider) SupportedChains() []int64 { return []int64{1, 56, 8453} }

type oneInchQuoteResponse struct {
	FromToken       map[string]any        `json:"fromToken"`
	ToToken         map[string]any        `json:"toToken"`
	FromTokenAmt    string                `json:"srcAmount"`
	ToTokenAmt      string                `json:"dstAmount"`
	EstimatedGas    string                `json:"estimatedGas"`
	Protocols       []oneInchProtocolHop  `json:"protocols"`
	Transaction     map[string]any        `json:"tx"`
	Router          string                `json:"router"`
	AllowanceTarget string                `json:"allowanceTarget"`
	PriceImpact     string                `json:"priceImpact"`
}

// oneInchProtocolHop 是 1inch protocols 的一层，表示经过某个中间 token 的一跳
type oneInchProtocolHop struct {
	Token string                    `json:"token"` // 该跳的 fromToken 地址
	Hops  []oneInchProtocolHopStep  `json:"hops"`
}

type oneInchProtocolHopStep struct {
	Part      int                      `json:"part"` // 资金占比 %
	Dst       string                   `json:"dst"`  // toToken 地址
	Protocols []oneInchProtocolDetail  `json:"protocols"`
}

type oneInchProtocolDetail struct {
	Name string `json:"name"` // DEX 名称，如 UNISWAP_V3、CURVE 等
	Part int    `json:"part"`
}

type oneInchApproveSpenderResponse struct {
	Address string `json:"address"`
}

type oneInchApproveTxResponse struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Data     string `json:"data"`
	Value    string `json:"value"`
	Gas      string `json:"gas"`
	GasPrice string `json:"gasPrice"`
}

func (p *OneInchProvider) apiBase(chainID int64) (string, error) {
	chain, ok := p.cfg.Chain(chainID)
	if !ok {
		return "", ErrUnsupportedChain
	}
	_ = chain
	base := strings.TrimRight(p.cfg.Provider.OneInchBaseURL, "/")
	if base == "" {
		base = "https://api.1inch.com"
	}
	return base, nil
}

func (p *OneInchProvider) setAuth(req *http.Request) {
	if p.cfg.Provider.OneInchAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.cfg.Provider.OneInchAPIKey)
	}
}

func (p *OneInchProvider) GetQuote(input QuoteInput) (NormalizedQuote, error) {
	if !containsInt64(p.SupportedChains(), input.ChainID) {
		return NormalizedQuote{}, ErrUnsupportedChain
	}
	base, _ := p.apiBase(input.ChainID)
	u, _ := url.Parse(base)
	u.Path = fmt.Sprintf("/swap/v6.1/%d/quote", input.ChainID)
	q := u.Query()
	q.Set("src", input.FromToken)
	q.Set("dst", input.ToToken)
	q.Set("amount", input.AmountIn)
	q.Set("from", input.WalletAddress)
	q.Set("includeProtocols", "true")
	// slippage 不传给 /quote，该端点只返回价格；slippage 在 BuildSwapTx 调 /swap 时才需要
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return NormalizedQuote{}, err
	}
	p.setAuth(req)
	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.Provider.RequestTimeout)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return NormalizedQuote{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return NormalizedQuote{}, fmt.Errorf("1inch quote status %d", resp.StatusCode)
	}
	var raw oneInchQuoteResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return NormalizedQuote{}, err
	}
	fromToken := p.cfg.Token(input.ChainID, input.FromToken)
	toToken := p.cfg.Token(input.ChainID, input.ToToken)
	minOut, err := minAmountOut(raw.ToTokenAmt, input.SlippageBps)
	if err != nil {
		return NormalizedQuote{}, err
	}
	// 解析 1inch protocols 路由：
	// - protocols[].token          = 这段的 fromToken
	// - protocols[].hops[].dst     = 这段的 toToken
	// - protocols[].hops[].part    = 这条路径占总资金的 %（用于并行拆单）
	// - hops[].protocols[].name    = 具体 DEX 名称
	// - hops[].protocols[].part    = 该 DEX 在这一步的 % （同一步多 DEX 拆分时有意义）
	// 实际金额 = 总金额 × hop.part% × dex.part%
	var route []RouteStep
	for _, segment := range raw.Protocols {
		for _, hop := range segment.Hops {
			for _, proto := range hop.Protocols {
				step := RouteStep{
					Protocol:  proto.Name,
					FromToken: p.cfg.Token(input.ChainID, segment.Token).Info(),
					ToToken:   p.cfg.Token(input.ChainID, hop.Dst).Info(),
				}
				// effectiveBps = hop.Part * proto.Part（两者均为 %，乘积即为 bps）
				effectiveBps := int64(hop.Part * proto.Part)
				// 第一跳起始 token = fromToken：用 input.AmountIn（1inch /quote 不返回 srcAmount）
				if normalizeAddress(segment.Token) == normalizeAddress(input.FromToken) {
					step.AmountIn = proportionOf(input.AmountIn, strconv.FormatInt(effectiveBps, 10))
				}
				// 最后一跳：目标 token 匹配，或者目标是 native ETH 而用户请求的是 WETH（两者等价）
				dstIsTarget := normalizeAddress(hop.Dst) == normalizeAddress(input.ToToken) ||
					(isNativeToken(hop.Dst) && p.isWrappedNative(input.ChainID, input.ToToken))
				if dstIsTarget {
					step.AmountOut = proportionOf(raw.ToTokenAmt, strconv.FormatInt(effectiveBps, 10))
				}
				route = append(route, step)
			}
		}
	}
	if len(route) == 0 {
		route = []RouteStep{{Protocol: "1inch", FromToken: fromToken.Info(), ToToken: toToken.Info(),
			AmountIn: input.AmountIn, AmountOut: raw.ToTokenAmt}}
	}
	deadline := time.Now().Add(20 * time.Minute).Unix()
	quote := NormalizedQuote{
		Provider:       p.Name(),
		ChainID:        input.ChainID,
		FromToken:      fromToken.Info(),
		ToToken:        toToken.Info(),
		AmountIn:       input.AmountIn,
		AmountOut:      raw.ToTokenAmt,
		MinAmountOut:   minOut,
		GasUSD:         raw.EstimatedGas,
		FeeUSD:         "",
		PriceImpactBps: 0,
		Spender:        "", // 延迟获取：spender 在 GetApprovalTarget 中按需获取，避免 GetQuote 阶段多一次 API 调用
		TransactionTo:  "",
		Route:          route,
		RawQuote:       raw,
		ExpiresAt:      time.Now().Add(p.cfg.QuoteTTL),
		Deadline:       &deadline,
		QuoteInput:     input,
	}
	return quote, nil
}

func (p *OneInchProvider) GetApprovalTarget(quote NormalizedQuote) (string, error) {
	raw, ok := quote.RawQuote.(oneInchQuoteResponse)
	if !ok {
		if decoded, ok := quote.RawQuote.(map[string]any); ok {
			buf, _ := json.Marshal(decoded)
			_ = json.Unmarshal(buf, &raw)
		}
	}
	if raw.AllowanceTarget != "" {
		return raw.AllowanceTarget, nil
	}
	base, err := p.apiBase(quote.ChainID)
	if err != nil {
		return "", err
	}
	u, _ := url.Parse(base)
	u.Path = fmt.Sprintf("/swap/v6.1/%d/approve/spender", quote.ChainID)
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	p.setAuth(req)
	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.Provider.RequestTimeout)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("1inch approve spender status %d", resp.StatusCode)
	}
	var spender oneInchApproveSpenderResponse
	if err := json.NewDecoder(resp.Body).Decode(&spender); err != nil {
		return "", err
	}
	if spender.Address == "" {
		return "", fmt.Errorf("1inch approval target missing")
	}
	return spender.Address, nil
}

func (p *OneInchProvider) BuildApproveTx(quote NormalizedQuote, taker string) (InternalEvmTxEnvelope, error) {
	chain, ok := p.cfg.Chain(quote.ChainID)
	if !ok {
		return InternalEvmTxEnvelope{}, ErrUnsupportedChain
	}
	raw, ok := quote.RawQuote.(oneInchQuoteResponse)
	if !ok {
		if decoded, ok := quote.RawQuote.(map[string]any); ok {
			buf, _ := json.Marshal(decoded)
			_ = json.Unmarshal(buf, &raw)
		}
	}
	u, _ := url.Parse(strings.TrimRight(p.cfg.Provider.OneInchBaseURL, "/"))
	u.Path = fmt.Sprintf("/swap/v6.1/%d/approve/transaction", quote.ChainID)
	q := u.Query()
	q.Set("tokenAddress", quote.FromToken.Address)
	q.Set("amount", quote.AmountIn)
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return InternalEvmTxEnvelope{}, err
	}
	p.setAuth(req)
	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.Provider.RequestTimeout)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return InternalEvmTxEnvelope{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return InternalEvmTxEnvelope{}, fmt.Errorf("1inch approve status %d", resp.StatusCode)
	}
	var approve oneInchApproveTxResponse
	if err := json.NewDecoder(resp.Body).Decode(&approve); err != nil {
		return InternalEvmTxEnvelope{}, err
	}
	gasLimit := approve.Gas
	if gasLimit == "" {
		gasLimit = "60000"
	}
	return InternalEvmTxEnvelope{
		GasType:              chain.GasType,
		ChainID:              quote.ChainID,
		To:                   approve.To,
		Data:                 approve.Data,
		Value:                approve.Value,
		GasLimit:             gasLimit,
		GasPrice:             approve.GasPrice,
		MaxFeePerGas:         approve.GasPrice,
		MaxPriorityFeePerGas: approve.GasPrice,
	}, nil
}

func (p *OneInchProvider) BuildSwapTx(quote NormalizedQuote, taker string) (InternalEvmTxEnvelope, error) {
	chain, ok := p.cfg.Chain(quote.ChainID)
	if !ok {
		return InternalEvmTxEnvelope{}, ErrUnsupportedChain
	}
	raw, ok := quote.RawQuote.(oneInchQuoteResponse)
	if !ok {
		if decoded, ok := quote.RawQuote.(map[string]any); ok {
			buf, _ := json.Marshal(decoded)
			_ = json.Unmarshal(buf, &raw)
		}
	}
	u, _ := url.Parse(strings.TrimRight(p.cfg.Provider.OneInchBaseURL, "/"))
	u.Path = fmt.Sprintf("/swap/v6.1/%d/swap", quote.ChainID)
	q := u.Query()
	q.Set("src", quote.FromToken.Address)
	q.Set("dst", quote.ToToken.Address)
	q.Set("amount", quote.AmountIn)
	q.Set("from", taker)
	q.Set("slippage", fmt.Sprintf("%.4f", float64(quote.QuoteInput.SlippageBps)/100))
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return InternalEvmTxEnvelope{}, err
	}
	p.setAuth(req)
	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.Provider.RequestTimeout)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return InternalEvmTxEnvelope{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return InternalEvmTxEnvelope{}, fmt.Errorf("1inch swap status %d", resp.StatusCode)
	}
	var swapResp struct {
		Tx map[string]any `json:"tx"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&swapResp); err != nil {
		return InternalEvmTxEnvelope{}, err
	}
	to := ""
	data := ""
	value := "0"
	gas := ""
	maxFee := ""
	maxPriority := ""
	if v, ok := swapResp.Tx["to"].(string); ok {
		to = v
	}
	if v, ok := swapResp.Tx["data"].(string); ok {
		data = v
	}
	if v, ok := swapResp.Tx["value"].(string); ok {
		value = v
	}
	if v, ok := swapResp.Tx["gas"].(string); ok {
		gas = v
	}
	if v, ok := swapResp.Tx["gasPrice"].(string); ok {
		if chain.GasType == GasTypeLegacy {
			// handled below as gasPrice
		} else {
			maxFee = v
			maxPriority = v
		}
	}
	if gas == "" {
		gas = "185000"
	}
	return InternalEvmTxEnvelope{
		GasType:              chain.GasType,
		ChainID:              quote.ChainID,
		To:                   to,
		Data:                 data,
		Value:                value,
		GasLimit:             gas,
		GasPrice:             legacyGasPrice(chain.GasType, swapResp.Tx),
		MaxFeePerGas:         maxFee,
		MaxPriorityFeePerGas: maxPriority,
	}, nil
}

func legacyGasPrice(gasType GasType, tx map[string]any) string {
	if gasType != GasTypeLegacy {
		return ""
	}
	if v, ok := tx["gasPrice"].(string); ok {
		return v
	}
	return ""
}

// isWrappedNative 判断 addr 是否是该链的 wrapped native token（WETH / WBNB 等）。
// 通过 token 符号是否等于 "W" + 链的 native symbol 来识别。
func (p *OneInchProvider) isWrappedNative(chainID int64, addr string) bool {
	chain, ok := p.cfg.Chain(chainID)
	if !ok {
		return false
	}
	tok := p.cfg.Token(chainID, addr)
	return tok.Symbol == "W"+chain.NativeSymbol
}
