package swap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type KyberSwapProvider struct {
	httpClient *http.Client
	cfg        Config
}

func NewKyberSwapProvider(httpClient *http.Client, cfg Config) *KyberSwapProvider {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &KyberSwapProvider{httpClient: httpClient, cfg: cfg}
}

func (p *KyberSwapProvider) Name() string { return "kyberswap" }

func (p *KyberSwapProvider) SupportedChains() []int64 { return []int64{1, 56, 8453} }

type kyberSwapRouteResponse struct {
	Code    int                `json:"code"`
	Message string             `json:"message"`
	Data    kyberSwapRouteData `json:"data"`
}

type kyberSwapRouteData struct {
	RouteSummary  json.RawMessage `json:"routeSummary"`
	RouterAddress string          `json:"routerAddress"`
}

type kyberSwapRouteSummary struct {
	TokenIn       string                 `json:"tokenIn"`
	TokenOut      string                 `json:"tokenOut"`
	AmountIn      string                 `json:"amountIn"`
	AmountOut     string                 `json:"amountOut"`
	Gas           string                 `json:"gas"`
	GasPrice      string                 `json:"gasPrice"`
	GasUsd        string                 `json:"gasUsd"`
	RouterAddress string                 `json:"routerAddress"`
	Route         [][]kyberSwapRouteStep `json:"route"`
}

type kyberSwapRouteStep struct {
	Pool       string `json:"pool"`
	Exchange   string `json:"exchange"`
	PoolType   string `json:"poolType"`
	TokenIn    string `json:"tokenIn"`
	TokenOut   string `json:"tokenOut"`
	SwapAmount string `json:"swapAmount"`
	AmountOut  string `json:"amountOut"`
}

type kyberSwapRawQuote struct {
	RouteSummary  json.RawMessage `json:"routeSummary"`
	RouterAddress string          `json:"routerAddress"`
}

type kyberSwapBuildRequest struct {
	RouteSummary      json.RawMessage `json:"routeSummary"`
	Sender            string          `json:"sender"`
	Recipient         string          `json:"recipient"`
	Origin            string          `json:"origin,omitempty"`
	SlippageTolerance int64           `json:"slippageTolerance"`
	Deadline          *int64          `json:"deadline,omitempty"`
	Source            string          `json:"source,omitempty"`
}

type kyberSwapBuildResponse struct {
	Code    int                `json:"code"`
	Message string             `json:"message"`
	Data    kyberSwapBuildData `json:"data"`
}

type kyberSwapBuildData struct {
	AmountIn         string `json:"amountIn"`
	AmountOut        string `json:"amountOut"`
	Gas              string `json:"gas"`
	GasUsd           string `json:"gasUsd"`
	Data             string `json:"data"`
	RouterAddress    string `json:"routerAddress"`
	TransactionValue string `json:"transactionValue"`
}

func (p *KyberSwapProvider) GetQuote(input QuoteInput) (NormalizedQuote, error) {
	if !containsInt64(p.SupportedChains(), input.ChainID) {
		return NormalizedQuote{}, ErrUnsupportedChain
	}
	chainName, err := kyberSwapChainName(input.ChainID)
	if err != nil {
		return NormalizedQuote{}, err
	}

	u, _ := url.Parse(strings.TrimRight(p.kyberSwapBaseURL(), "/"))
	u.Path = fmt.Sprintf("/%s/api/v1/routes", chainName)
	q := u.Query()
	q.Set("tokenIn", input.FromToken)
	q.Set("tokenOut", input.ToToken)
	q.Set("amountIn", input.AmountIn)
	q.Set("gasInclude", "true")
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
		return NormalizedQuote{}, fmt.Errorf("kyberswap quote status %d", resp.StatusCode)
	}

	var raw kyberSwapRouteResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return NormalizedQuote{}, err
	}
	if raw.Code != 0 {
		return NormalizedQuote{}, fmt.Errorf("kyberswap quote code %d: %s", raw.Code, raw.Message)
	}
	if len(raw.Data.RouteSummary) == 0 {
		return NormalizedQuote{}, fmt.Errorf("kyberswap quote missing route summary")
	}

	summary, err := decodeKyberSwapRouteSummary(raw.Data.RouteSummary)
	if err != nil {
		return NormalizedQuote{}, err
	}
	routerAddress := raw.Data.RouterAddress
	if routerAddress == "" {
		routerAddress = summary.RouterAddress
	}
	if routerAddress == "" {
		return NormalizedQuote{}, fmt.Errorf("kyberswap quote missing router address")
	}
	minOut, err := minAmountOut(summary.AmountOut, input.SlippageBps)
	if err != nil {
		return NormalizedQuote{}, err
	}

	fromToken := p.cfg.Token(input.ChainID, input.FromToken)
	toToken := p.cfg.Token(input.ChainID, input.ToToken)
	deadline := time.Now().Add(20 * time.Minute).Unix()
	return NormalizedQuote{
		Provider:       p.Name(),
		ChainID:        input.ChainID,
		FromToken:      fromToken.Info(),
		ToToken:        toToken.Info(),
		AmountIn:       input.AmountIn,
		AmountOut:      summary.AmountOut,
		MinAmountOut:   minOut,
		GasUSD:         summary.GasUsd,
		FeeUSD:         "",
		PriceImpactBps: 0,
		Spender:        routerAddress,
		TransactionTo:  routerAddress,
		Route:          p.normalizeRoute(input.ChainID, fromToken.Info(), toToken.Info(), summary),
		RawQuote:       kyberSwapRawQuote{RouteSummary: raw.Data.RouteSummary, RouterAddress: routerAddress},
		ExpiresAt:      time.Now().Add(p.cfg.QuoteTTL),
		Deadline:       &deadline,
		QuoteInput:     input,
	}, nil
}

func (p *KyberSwapProvider) GetApprovalTarget(quote NormalizedQuote) (string, error) {
	if quote.Spender != "" {
		return quote.Spender, nil
	}
	raw, err := kyberSwapRawQuoteFromAny(quote.RawQuote)
	if err != nil {
		return "", err
	}
	if raw.RouterAddress == "" {
		return "", fmt.Errorf("kyberswap approval target missing")
	}
	return raw.RouterAddress, nil
}

func (p *KyberSwapProvider) BuildApproveTx(quote NormalizedQuote, taker string) (InternalEvmTxEnvelope, error) {
	chain, ok := p.cfg.Chain(quote.ChainID)
	if !ok {
		return InternalEvmTxEnvelope{}, ErrUnsupportedChain
	}
	spender, err := p.GetApprovalTarget(quote)
	if err != nil {
		return InternalEvmTxEnvelope{}, err
	}
	return InternalEvmTxEnvelope{
		GasType:  chain.GasType,
		ChainID:  quote.ChainID,
		To:       quote.FromToken.Address,
		Data:     encodeApproveCalldata(spender, quote.AmountIn),
		Value:    "0",
		GasLimit: "60000",
	}, nil
}

func (p *KyberSwapProvider) BuildSwapTx(quote NormalizedQuote, taker string) (InternalEvmTxEnvelope, error) {
	chain, ok := p.cfg.Chain(quote.ChainID)
	if !ok {
		return InternalEvmTxEnvelope{}, ErrUnsupportedChain
	}
	chainName, err := kyberSwapChainName(quote.ChainID)
	if err != nil {
		return InternalEvmTxEnvelope{}, err
	}
	raw, err := kyberSwapRawQuoteFromAny(quote.RawQuote)
	if err != nil {
		return InternalEvmTxEnvelope{}, err
	}
	summary, err := decodeKyberSwapRouteSummary(raw.RouteSummary)
	if err != nil {
		return InternalEvmTxEnvelope{}, err
	}

	body := kyberSwapBuildRequest{
		RouteSummary:      raw.RouteSummary,
		Sender:            taker,
		Recipient:         taker,
		Origin:            taker,
		SlippageTolerance: quote.QuoteInput.SlippageBps,
		Deadline:          quote.Deadline,
		Source:            p.kyberSwapClientID(),
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return InternalEvmTxEnvelope{}, err
	}

	u, _ := url.Parse(strings.TrimRight(p.kyberSwapBaseURL(), "/"))
	u.Path = fmt.Sprintf("/%s/api/v1/route/build", chainName)
	req, err := http.NewRequest(http.MethodPost, u.String(), bytes.NewReader(buf))
	if err != nil {
		return InternalEvmTxEnvelope{}, err
	}
	p.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.Provider.RequestTimeout)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return InternalEvmTxEnvelope{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return InternalEvmTxEnvelope{}, fmt.Errorf("kyberswap build status %d", resp.StatusCode)
	}

	var built kyberSwapBuildResponse
	if err := json.NewDecoder(resp.Body).Decode(&built); err != nil {
		return InternalEvmTxEnvelope{}, err
	}
	if built.Code != 0 {
		return InternalEvmTxEnvelope{}, fmt.Errorf("kyberswap build code %d: %s", built.Code, built.Message)
	}

	to := built.Data.RouterAddress
	if to == "" {
		to = raw.RouterAddress
	}
	if to == "" {
		to = quote.TransactionTo
	}
	if to == "" || built.Data.Data == "" {
		return InternalEvmTxEnvelope{}, fmt.Errorf("kyberswap build missing transaction")
	}
	value := built.Data.TransactionValue
	if value == "" {
		value = "0"
	}
	gas := built.Data.Gas
	if gas == "" {
		gas = summary.Gas
	}
	if gas == "" {
		gas = "185000"
	}

	envelope := InternalEvmTxEnvelope{
		GasType:  chain.GasType,
		ChainID:  quote.ChainID,
		To:       to,
		Data:     built.Data.Data,
		Value:    value,
		GasLimit: gas,
	}
	if chain.GasType == GasTypeLegacy {
		envelope.GasPrice = summary.GasPrice
	} else {
		envelope.MaxFeePerGas = summary.GasPrice
		envelope.MaxPriorityFeePerGas = summary.GasPrice
	}
	return envelope, nil
}

func (p *KyberSwapProvider) normalizeRoute(chainID int64, fromToken, toToken TokenInfo, summary kyberSwapRouteSummary) []RouteStep {
	var route []RouteStep
	for _, path := range summary.Route {
		for _, hop := range path {
			protocol := hop.Exchange
			if protocol == "" {
				protocol = hop.PoolType
			}
			if protocol == "" {
				protocol = "KyberSwap"
			}
			route = append(route, RouteStep{
				Protocol:    protocol,
				FromToken:   p.cfg.Token(chainID, hop.TokenIn).Info(),
				ToToken:     p.cfg.Token(chainID, hop.TokenOut).Info(),
				AmountIn:    hop.SwapAmount,
				AmountOut:   hop.AmountOut,
				PoolAddress: hop.Pool,
			})
		}
	}
	if len(route) == 0 {
		route = []RouteStep{{Protocol: "KyberSwap", FromToken: fromToken, ToToken: toToken, AmountIn: summary.AmountIn, AmountOut: summary.AmountOut}}
	}
	return route
}

func (p *KyberSwapProvider) setAuth(req *http.Request) {
	if clientID := p.kyberSwapClientID(); clientID != "" {
		req.Header.Set("X-Client-Id", clientID)
	}
}

func (p *KyberSwapProvider) kyberSwapBaseURL() string {
	base := strings.TrimRight(p.cfg.Provider.KyberSwapBaseURL, "/")
	if base == "" {
		return "https://aggregator-api.kyberswap.com"
	}
	return base
}

func (p *KyberSwapProvider) kyberSwapClientID() string {
	clientID := strings.TrimSpace(p.cfg.Provider.KyberSwapClientID)
	if clientID == "" {
		return "web3-wallet"
	}
	return clientID
}

func decodeKyberSwapRouteSummary(raw json.RawMessage) (kyberSwapRouteSummary, error) {
	var summary kyberSwapRouteSummary
	if err := json.Unmarshal(raw, &summary); err != nil {
		return kyberSwapRouteSummary{}, err
	}
	if summary.AmountOut == "" {
		return kyberSwapRouteSummary{}, fmt.Errorf("kyberswap route summary missing amountOut")
	}
	return summary, nil
}

func kyberSwapRawQuoteFromAny(raw any) (kyberSwapRawQuote, error) {
	switch value := raw.(type) {
	case kyberSwapRawQuote:
		if len(value.RouteSummary) == 0 {
			return kyberSwapRawQuote{}, fmt.Errorf("kyberswap raw quote missing route summary")
		}
		return value, nil
	case map[string]any:
		buf, err := json.Marshal(value)
		if err != nil {
			return kyberSwapRawQuote{}, err
		}
		var out kyberSwapRawQuote
		if err := json.Unmarshal(buf, &out); err != nil {
			return kyberSwapRawQuote{}, err
		}
		if len(out.RouteSummary) == 0 {
			return kyberSwapRawQuote{}, fmt.Errorf("kyberswap raw quote missing route summary")
		}
		return out, nil
	default:
		buf, err := json.Marshal(value)
		if err != nil {
			return kyberSwapRawQuote{}, err
		}
		var out kyberSwapRawQuote
		if err := json.Unmarshal(buf, &out); err != nil {
			return kyberSwapRawQuote{}, err
		}
		if len(out.RouteSummary) == 0 {
			return kyberSwapRawQuote{}, fmt.Errorf("kyberswap raw quote missing route summary")
		}
		return out, nil
	}
}

func kyberSwapChainName(chainID int64) (string, error) {
	switch chainID {
	case 1:
		return "ethereum", nil
	case 56:
		return "bsc", nil
	case 8453:
		return "base", nil
	default:
		return "", ErrUnsupportedChain
	}
}
