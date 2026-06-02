package swap

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeRPC struct {
	allowance string
}

func (f fakeRPC) GetAllowance(ctx context.Context, chainID int64, token, owner, spender string) (string, error) {
	if f.allowance == "" {
		return "0", nil
	}
	return f.allowance, nil
}
func (f fakeRPC) GetBalance(ctx context.Context, chainID int64, address string) (string, error) {
	return "0", nil
}
func (f fakeRPC) EstimateGas(ctx context.Context, chainID int64, tx map[string]any) (string, error) {
	return "0", nil
}
func (f fakeRPC) GasPrice(ctx context.Context, chainID int64) (string, error) { return "0", nil }
func (f fakeRPC) GetTransaction(ctx context.Context, chainID int64, hash string) (*rpcTx, error) {
	return nil, nil
}
func (f fakeRPC) ChainGasType(chainID int64) GasType { return GasTypeEIP1559 }

type fakeProvider struct {
	name      string
	quote     NormalizedQuote
	approveTx InternalEvmTxEnvelope
	swapTx    InternalEvmTxEnvelope
}

func (f fakeProvider) Name() string             { return f.name }
func (f fakeProvider) SupportedChains() []int64 { return []int64{1} }
func (f fakeProvider) GetQuote(input QuoteInput) (NormalizedQuote, error) {
	q := f.quote
	q.QuoteInput = input
	q.Provider = f.name
	q.ChainID = input.ChainID
	q.FromToken = TokenInfo{Address: input.FromToken, Symbol: "A", Decimals: 18}
	q.ToToken = TokenInfo{Address: input.ToToken, Symbol: "B", Decimals: 18}
	q.AmountIn = input.AmountIn
	return q, nil
}
func (f fakeProvider) GetApprovalTarget(quote NormalizedQuote) (string, error) {
	return quote.Spender, nil
}
func (f fakeProvider) BuildApproveTx(quote NormalizedQuote, taker string) (InternalEvmTxEnvelope, error) {
	return f.approveTx, nil
}
func (f fakeProvider) BuildSwapTx(quote NormalizedQuote, taker string) (InternalEvmTxEnvelope, error) {
	return f.swapTx, nil
}

func newTestService(t *testing.T, providers []QuoteProvider) *Service {
	t.Helper()
	cfg := DefaultConfig()
	now := func() time.Time { return time.Unix(0, 0) }
	return NewService(cfg, NewMemoryRepository(now), fakeRPC{allowance: "1000000000000000000"}, providers, now)
}

func TestSelectBestQuote(t *testing.T) {
	svc := newTestService(t, nil)
	q1 := NormalizedQuote{Provider: "0x", AmountOut: "100", MinAmountOut: "90"}
	q2 := NormalizedQuote{Provider: "1inch", AmountOut: "101", MinAmountOut: "90"}
	best, err := svc.selectBestQuote([]NormalizedQuote{q1, q2})
	if err != nil {
		t.Fatal(err)
	}
	if best.Provider != "1inch" {
		t.Fatalf("expected 1inch, got %s", best.Provider)
	}
}

func TestAllowanceSkipsNativeToken(t *testing.T) {
	svc := newTestService(t, nil)
	quote, err := svc.repo.SaveQuote(NormalizedQuote{
		Provider:  "0x",
		ChainID:   1,
		FromToken: TokenInfo{Address: NativeTokenAddress, Symbol: "ETH", Decimals: 18},
		ToToken:   TokenInfo{Address: "0xToken", Symbol: "T", Decimals: 18},
		AmountIn:  "100",
		Spender:   "0xspender",
		ExpiresAt: time.Unix(60, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.Allowance(context.Background(), quote.ID, "0xwallet")
	if err != nil {
		t.Fatal(err)
	}
	if !out.AllowanceEnough {
		t.Fatal("expected native token allowance to be enough")
	}
}

func TestExecuteReusesSigningOrder(t *testing.T) {
	provider := fakeProvider{
		name: "0x",
		quote: NormalizedQuote{
			Spender:        "0xspender",
			TransactionTo:  "0xrouter",
			MinAmountOut:   "90",
			AmountOut:      "100",
			PriceImpactBps: 10,
			ExpiresAt:      time.Unix(60, 0),
		},
		swapTx: InternalEvmTxEnvelope{
			GasType:  GasTypeEIP1559,
			ChainID:  1,
			To:       "0xrouter",
			Data:     "0xabc",
			Value:    "0",
			GasLimit: "185000",
		},
	}
	svc := newTestService(t, []QuoteProvider{provider})
	q, err := svc.Quote(context.Background(), QuoteInput{
		UserID: "u1", ChainID: 1, FromToken: "0xA", ToToken: "0xB", AmountIn: "100", SlippageBps: 50, WalletAddress: "0xwallet",
	})
	if err != nil {
		t.Fatal(err)
	}
	exec1, err := svc.Execute(context.Background(), q.QuoteID, "0xwallet", WalletTypeExternal)
	if err != nil {
		t.Fatal(err)
	}
	exec2, err := svc.Execute(context.Background(), q.QuoteID, "0xwallet", WalletTypeExternal)
	if err != nil {
		t.Fatal(err)
	}
	if exec1.OrderID != exec2.OrderID {
		t.Fatalf("expected same order id, got %s and %s", exec1.OrderID, exec2.OrderID)
	}
}

func TestSubmitHashExternalOnly(t *testing.T) {
	provider := fakeProvider{
		name: "0x",
		quote: NormalizedQuote{
			Spender:        "0xspender",
			TransactionTo:  "0xrouter",
			MinAmountOut:   "90",
			AmountOut:      "100",
			PriceImpactBps: 10,
			ExpiresAt:      time.Unix(60, 0),
		},
		swapTx: InternalEvmTxEnvelope{GasType: GasTypeEIP1559, ChainID: 1, To: "0xrouter", Data: "0xabc", Value: "0", GasLimit: "185000"},
	}
	svc := newTestService(t, []QuoteProvider{provider})
	q, err := svc.Quote(context.Background(), QuoteInput{
		UserID: "u1", ChainID: 1, FromToken: "0xA", ToToken: "0xB", AmountIn: "100", SlippageBps: 50, WalletAddress: "0xwallet",
	})
	if err != nil {
		t.Fatal(err)
	}
	exec, err := svc.Execute(context.Background(), q.QuoteID, "0xwallet", WalletTypeCustody)
	if err != nil {
		t.Fatal(err)
	}
	err = svc.SubmitHash(context.Background(), SubmitHashRequest{OrderID: exec.OrderID, TxHash: "0x123"})
	if err == nil {
		t.Fatal("expected submit-hash to reject custody wallet")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}
