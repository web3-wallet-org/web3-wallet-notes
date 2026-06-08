package swap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestKyberSwapProviderQuoteAndBuildSwapTx(t *testing.T) {
	const (
		router = "0x1111111111111111111111111111111111111111"
		usdc   = "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
		dai    = "0x6B175474E89094C44Da98b954EedeAC495271d0F"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Client-Id") != "unit-test" {
			t.Fatalf("expected X-Client-Id header, got %q", r.Header.Get("X-Client-Id"))
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/ethereum/api/v1/routes":
			if got := r.URL.Query().Get("tokenIn"); got != usdc {
				t.Fatalf("expected tokenIn %s, got %s", usdc, got)
			}
			if got := r.URL.Query().Get("tokenOut"); got != dai {
				t.Fatalf("expected tokenOut %s, got %s", dai, got)
			}
			if got := r.URL.Query().Get("amountIn"); got != "1000000" {
				t.Fatalf("expected amountIn 1000000, got %s", got)
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"code":    0,
				"message": "OK",
				"data": map[string]any{
					"routerAddress": router,
					"routeSummary": map[string]any{
						"tokenIn":   usdc,
						"tokenOut":  dai,
						"amountIn":  "1000000",
						"amountOut": "997000000000000000",
						"gas":       "180000",
						"gasPrice":  "20000000000",
						"gasUsd":    "3.20",
						"route": [][]map[string]string{{
							{
								"pool":       "0x2222222222222222222222222222222222222222",
								"exchange":   "uniswapv3",
								"tokenIn":    usdc,
								"tokenOut":   dai,
								"swapAmount": "1000000",
								"amountOut":  "997000000000000000",
							},
						}},
					},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/ethereum/api/v1/route/build":
			var req struct {
				RouteSummary      json.RawMessage `json:"routeSummary"`
				Sender            string          `json:"sender"`
				Recipient         string          `json:"recipient"`
				SlippageTolerance int64           `json:"slippageTolerance"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode build request: %v", err)
			}
			if len(req.RouteSummary) == 0 {
				t.Fatal("expected routeSummary in build request")
			}
			if req.Sender != "0xwallet" || req.Recipient != "0xwallet" {
				t.Fatalf("unexpected sender/recipient: %+v", req)
			}
			if req.SlippageTolerance != 100 {
				t.Fatalf("expected slippageTolerance 100, got %d", req.SlippageTolerance)
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"code":    0,
				"message": "OK",
				"data": map[string]any{
					"routerAddress":    router,
					"data":             "0xswap",
					"transactionValue": "0",
					"gas":              "190000",
				},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.Provider.KyberSwapBaseURL = server.URL
	cfg.Provider.KyberSwapClientID = "unit-test"
	cfg.QuoteTTL = time.Minute
	provider := NewKyberSwapProvider(server.Client(), cfg)

	input := QuoteInput{
		UserID:        "u1",
		ChainID:       1,
		FromToken:     usdc,
		ToToken:       dai,
		AmountIn:      "1000000",
		SlippageBps:   100,
		WalletAddress: "0xwallet",
	}
	quote, err := provider.GetQuote(input)
	if err != nil {
		t.Fatal(err)
	}
	if quote.Provider != "kyberswap" {
		t.Fatalf("expected kyberswap provider, got %s", quote.Provider)
	}
	if quote.Spender != router || quote.TransactionTo != router {
		t.Fatalf("expected router %s, got spender=%s transactionTo=%s", router, quote.Spender, quote.TransactionTo)
	}
	if quote.AmountOut != "997000000000000000" || quote.MinAmountOut != "987030000000000000" {
		t.Fatalf("unexpected amounts: out=%s min=%s", quote.AmountOut, quote.MinAmountOut)
	}
	if len(quote.Route) != 1 || quote.Route[0].Protocol != "uniswapv3" {
		t.Fatalf("unexpected route: %+v", quote.Route)
	}

	tx, err := provider.BuildSwapTx(quote, "0xwallet")
	if err != nil {
		t.Fatal(err)
	}
	if tx.To != router || tx.Data != "0xswap" || tx.Value != "0" || tx.GasLimit != "190000" {
		t.Fatalf("unexpected tx: %+v", tx)
	}
	if tx.MaxFeePerGas != "20000000000" || tx.MaxPriorityFeePerGas != "20000000000" {
		t.Fatalf("expected eip1559 gas fields from route summary, got %+v", tx)
	}
}

func TestServiceQuoteSelectsKyberSwap(t *testing.T) {
	now := func() time.Time { return time.Unix(0, 0) }
	cfg := DefaultConfig()
	provider := fakeProvider{
		name: "kyberswap",
		quote: NormalizedQuote{
			Spender:        "0xspender",
			TransactionTo:  "0xrouter",
			MinAmountOut:   "90",
			AmountOut:      "120",
			PriceImpactBps: 10,
			ExpiresAt:      time.Unix(60, 0),
		},
	}
	svc := NewService(cfg, NewMemoryRepository(now), fakeRPC{allowance: "1000"}, []QuoteProvider{provider}, now)
	out, err := svc.Quote(context.Background(), QuoteInput{
		UserID:        "u1",
		ChainID:       1,
		FromToken:     "0xA",
		ToToken:       "0xB",
		AmountIn:      "100",
		SlippageBps:   50,
		WalletAddress: "0xwallet",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.SelectedProvider != "kyberswap" {
		t.Fatalf("expected kyberswap selected, got %s", out.SelectedProvider)
	}
}
