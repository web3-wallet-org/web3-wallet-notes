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

type ZeroXProvider struct {
	httpClient *http.Client
	cfg        Config
}

func NewZeroXProvider(httpClient *http.Client, cfg Config) *ZeroXProvider {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &ZeroXProvider{httpClient: httpClient, cfg: cfg}
}

func (p *ZeroXProvider) Name() string { return "0x" }

func (p *ZeroXProvider) SupportedChains() []int64 { return []int64{1, 56, 8453} }

type zeroXQuoteResponse struct {
	Price                string           `json:"price"`
	GuaranteedPrice      string           `json:"guaranteedPrice"`
	To                   string           `json:"to"`
	Data                 string           `json:"data"`
	Value                string           `json:"value"`
	BuyAmount            string           `json:"buyAmount"`
	SellAmount           string           `json:"sellAmount"`
	AllowanceTarget      string           `json:"allowanceTarget"`
	ToToken              string           `json:"buyToken"`
	FromToken            string           `json:"sellToken"`
	Gas                  string           `json:"gas"`
	EstimatedGas         string           `json:"estimatedGas"`
	GasPrice             string           `json:"gasPrice"`
	TotalNetworkFee      string           `json:"totalNetworkFee"`
	EstimatedPriceImpact string           `json:"estimatedPriceImpact"`
	Protocols            any              `json:"protocols"`
	Router               string           `json:"router"`
	Orders               []map[string]any `json:"orders"`
	Fees                 map[string]any   `json:"fees"`
	Transaction          map[string]any   `json:"transaction"`
	MinBuyAmount         string           `json:"minBuyAmount"`
	Route                struct {
		Fills []struct {
			From           string `json:"from"`
			To             string `json:"to"`
			Source         string `json:"source"`
			ProportionBps  string `json:"proportionBps"`
		} `json:"fills"`
		Tokens []struct {
			Address string `json:"address"`
			Symbol  string `json:"symbol"`
		} `json:"tokens"`
	} `json:"route"`
	Issues struct {
		Allowance struct {
			Spender string `json:"spender"`
		} `json:"allowance"`
	} `json:"issues"`
}

func (p *ZeroXProvider) GetQuote(input QuoteInput) (NormalizedQuote, error) {
	if !containsInt64(p.SupportedChains(), input.ChainID) {
		return NormalizedQuote{}, ErrUnsupportedChain
	}
	chain, ok := p.cfg.Chain(input.ChainID)
	if !ok {
		return NormalizedQuote{}, ErrUnsupportedChain
	}

	u, _ := url.Parse(strings.TrimRight(p.cfg.Provider.ZeroXBaseURL, "/"))
	u.Path = "/swap/allowance-holder/quote"
	q := u.Query()
	q.Set("chainId", strconv.FormatInt(input.ChainID, 10))
	q.Set("sellToken", input.FromToken)
	q.Set("buyToken", input.ToToken)
	q.Set("sellAmount", input.AmountIn)
	q.Set("taker", input.WalletAddress)
	q.Set("slippageBps", strconv.FormatInt(input.SlippageBps, 10))
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return NormalizedQuote{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("0x-version", "v2")
	if p.cfg.Provider.ZeroXAPIKey != "" {
		req.Header.Set("0x-api-key", p.cfg.Provider.ZeroXAPIKey)
	}

	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.Provider.RequestTimeout)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return NormalizedQuote{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return NormalizedQuote{}, fmt.Errorf("0x quote status %d", resp.StatusCode)
	}

	var raw zeroXQuoteResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return NormalizedQuote{}, err
	}

	spender := raw.AllowanceTarget
	if spender == "" {
		spender = raw.Issues.Allowance.Spender
	}
	if spender == "" {
		return NormalizedQuote{}, fmt.Errorf("0x quote missing allowance target")
	}
	transactionTo := raw.To
	if transactionTo == "" {
		if v, ok := raw.Transaction["to"].(string); ok {
			transactionTo = v
		}
	}
	fromToken := p.cfg.Token(input.ChainID, input.FromToken)
	toToken := p.cfg.Token(input.ChainID, input.ToToken)
	minOut, err := minAmountOut(raw.BuyAmount, input.SlippageBps)
	if err != nil {
		return NormalizedQuote{}, err
	}

	// 把 0x route.fills 转成统一的 RouteStep，每个 fill 是一跳 DEX
	var route []RouteStep
	fills := raw.Route.Fills
	for i, fill := range fills {
		step := RouteStep{
			Protocol:  fill.Source,
			FromToken: p.cfg.Token(input.ChainID, fill.From).Info(),
			ToToken:   p.cfg.Token(input.ChainID, fill.To).Info(),
		}
		if len(fills) == 1 {
			// 单跳：直接用顶层金额
			step.AmountIn = raw.SellAmount
			step.AmountOut = raw.BuyAmount
		} else if fill.ProportionBps != "" && fill.ProportionBps != "0" {
			// 多跳并行拆分：按比例推算每跳金额（串行多跳中间金额 provider 未给，留空）
			if i == 0 {
				step.AmountIn = proportionOf(raw.SellAmount, fill.ProportionBps)
			}
			if i == len(fills)-1 {
				step.AmountOut = proportionOf(raw.BuyAmount, fill.ProportionBps)
			}
		}
		route = append(route, step)
	}

	deadline := time.Now().Add(20 * time.Minute).Unix()
	expiresAt := time.Now().Add(p.cfg.QuoteTTL)
	quote := NormalizedQuote{
		Provider:       p.Name(),
		ChainID:        input.ChainID,
		FromToken:      fromToken.Info(),
		ToToken:        toToken.Info(),
		AmountIn:       input.AmountIn,
		AmountOut:      raw.BuyAmount,
		MinAmountOut:   minOut,
		GasUSD:         "",
		FeeUSD:         "",
		PriceImpactBps: 0,
		Spender:        spender,
		TransactionTo:  transactionTo,
		Route:          route,
		RawQuote:       raw,
		ExpiresAt:      expiresAt,
		Deadline:       &deadline,
		QuoteInput:     input,
	}
	if raw.Gas != "" {
		quote.GasUSD = raw.Gas
	}
	if raw.EstimatedPriceImpact != "" {
		quote.RawQuote = raw
	}
	_ = chain
	return quote, nil
}

func (p *ZeroXProvider) GetApprovalTarget(quote NormalizedQuote) (string, error) {
	return quote.Spender, nil
}

func (p *ZeroXProvider) BuildApproveTx(quote NormalizedQuote, taker string) (InternalEvmTxEnvelope, error) {
	chain, ok := p.cfg.Chain(quote.ChainID)
	if !ok {
		return InternalEvmTxEnvelope{}, ErrUnsupportedChain
	}
	return InternalEvmTxEnvelope{
		GasType:              chain.GasType,
		ChainID:              quote.ChainID,
		To:                   quote.FromToken.Address,
		Data:                 encodeApproveCalldata(quote.Spender, quote.AmountIn),
		Value:                "0",
		GasLimit:             "60000",
		GasPrice:             "",
		MaxFeePerGas:         "",
		MaxPriorityFeePerGas: "",
	}, nil
}

func (p *ZeroXProvider) BuildSwapTx(quote NormalizedQuote, taker string) (InternalEvmTxEnvelope, error) {
	raw, ok := quote.RawQuote.(zeroXQuoteResponse)
	if !ok {
		if decoded, ok := quote.RawQuote.(map[string]any); ok {
			buf, _ := json.Marshal(decoded)
			_ = json.Unmarshal(buf, &raw)
		}
	}
	chain, ok := p.cfg.Chain(quote.ChainID)
	if !ok {
		return InternalEvmTxEnvelope{}, ErrUnsupportedChain
	}
	to := quote.TransactionTo
	data := raw.Data
	value := raw.Value
	gas := raw.Gas
	gasPrice := raw.GasPrice
	if raw.Transaction != nil {
		if v, ok := raw.Transaction["to"].(string); ok {
			to = v
		}
		if v, ok := raw.Transaction["data"].(string); ok {
			data = v
		}
		if v, ok := raw.Transaction["value"].(string); ok {
			value = v
		}
		if v, ok := raw.Transaction["gas"].(string); ok {
			gas = v
		}
		if v, ok := raw.Transaction["gasPrice"].(string); ok {
			gasPrice = v
		}
	}
	if value == "" {
		value = "0"
	}
	if gas == "" {
		gas = raw.EstimatedGas
	}
	envelope := InternalEvmTxEnvelope{
		GasType:  chain.GasType,
		ChainID:  quote.ChainID,
		To:       to,
		Data:     data,
		Value:    value,
		GasLimit: gas,
	}
	if chain.GasType == GasTypeLegacy {
		envelope.GasPrice = gasPrice
	} else {
		envelope.MaxFeePerGas = gasPrice
		envelope.MaxPriorityFeePerGas = gasPrice
	}
	return envelope, nil
}
