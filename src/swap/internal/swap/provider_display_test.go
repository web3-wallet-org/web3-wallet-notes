package swap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOneInchQuoteDisplaysWrappedNativeTarget(t *testing.T) {
	const (
		usdc = "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
		weth = "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/swap/v6.1/1/quote" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"srcAmount":    "100000000",
			"dstAmount":    "59389421339828781",
			"estimatedGas": "185000",
			"protocols": []map[string]any{
				{
					"token": usdc,
					"hops": []map[string]any{
						{
							"part": 100,
							"dst":  NativeTokenAddress,
							"protocols": []map[string]any{
								{"name": "UNISWAP_V3", "part": 100},
							},
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.Provider.OneInchBaseURL = server.URL
	cfg.QuoteTTL = time.Minute
	provider := NewOneInchProvider(server.Client(), cfg)

	quote, err := provider.GetQuote(QuoteInput{
		ChainID:       1,
		FromToken:     usdc,
		ToToken:       weth,
		AmountIn:      "100000000",
		SlippageBps:   50,
		WalletAddress: "0xwallet",
	})
	if err != nil {
		t.Fatal(err)
	}
	if quote.ToToken.Address != weth {
		t.Fatalf("expected top-level WETH target, got %s", quote.ToToken.Address)
	}
	if len(quote.Route) != 1 {
		t.Fatalf("expected 1 route step, got %d", len(quote.Route))
	}
	if quote.Route[0].ToToken.Address != weth || quote.Route[0].ToToken.Symbol != "WETH" {
		t.Fatalf("expected route target WETH, got %+v", quote.Route[0].ToToken)
	}
	if quote.Route[0].AmountOut != "59389421339828781" {
		t.Fatalf("unexpected amount out %s", quote.Route[0].AmountOut)
	}
}

func TestZeroXQuoteDoesNotUseGasAsGasUSD(t *testing.T) {
	const (
		usdc    = "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
		weth    = "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
		spender = "0x1111111111111111111111111111111111111111"
		router  = "0x2222222222222222222222222222222222222222"
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/swap/allowance-holder/quote" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("0x-version") != "v2" {
			t.Fatalf("expected 0x-version header")
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"buyAmount":       "59276567648698126",
			"sellAmount":      "100000000",
			"minBuyAmount":    "58980184810454635",
			"allowanceTarget": spender,
			"to":              router,
			"data":            "0xswap",
			"value":           "0",
			"gas":             "185000",
			"estimatedGas":    "184000",
			"gasPrice":        "1000000000",
			"route": map[string]any{
				"fills": []map[string]string{
					{"from": usdc, "to": weth, "source": "Uniswap_V3", "proportionBps": "10000"},
				},
			},
		})
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.Provider.ZeroXBaseURL = server.URL
	cfg.QuoteTTL = time.Minute
	provider := NewZeroXProvider(server.Client(), cfg)

	quote, err := provider.GetQuote(QuoteInput{
		ChainID:       1,
		FromToken:     usdc,
		ToToken:       weth,
		AmountIn:      "100000000",
		SlippageBps:   50,
		WalletAddress: "0xwallet",
	})
	if err != nil {
		t.Fatal(err)
	}
	if quote.GasUSD != "" {
		raw, _ := json.Marshal(quote.RawQuote)
		t.Fatalf("expected empty GasUSD when provider has no USD gas, got %q raw=%s", quote.GasUSD, string(raw))
	}
	if quote.AmountOut != "59276567648698126" {
		t.Fatalf("unexpected amount out %s", quote.AmountOut)
	}
}
