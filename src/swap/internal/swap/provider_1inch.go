package swap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	FromToken       map[string]any `json:"fromToken"`
	ToToken         map[string]any `json:"toToken"`
	FromTokenAmt    string         `json:"fromTokenAmount"`
	ToTokenAmt      string         `json:"toTokenAmount"`
	EstimatedGas    string         `json:"estimatedGas"`
	Protocols       any            `json:"protocols"`
	Transaction     map[string]any `json:"tx"`
	Router          string         `json:"router"`
	AllowanceTarget string         `json:"allowanceTarget"`
	PriceImpact     string         `json:"priceImpact"`
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
	q.Set("slippage", fmt.Sprintf("%.4f", float64(input.SlippageBps)/100))
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
	spender, err := p.GetApprovalTarget(NormalizedQuote{ChainID: input.ChainID, RawQuote: raw})
	if err != nil {
		return NormalizedQuote{}, err
	}
	fromToken := p.cfg.Token(input.ChainID, input.FromToken)
	toToken := p.cfg.Token(input.ChainID, input.ToToken)
	minOut, err := minAmountOut(raw.ToTokenAmt, input.SlippageBps)
	if err != nil {
		return NormalizedQuote{}, err
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
		Spender:        spender,
		TransactionTo:  "",
		Route:          nil,
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
