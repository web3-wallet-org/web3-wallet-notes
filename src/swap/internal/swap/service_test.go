package swap

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeRPC struct {
	allowance        string
	allowanceCalls   *int
	allowanceSpender *string
}

func (f fakeRPC) GetAllowance(ctx context.Context, chainID int64, token, owner, spender string) (string, error) {
	if f.allowanceCalls != nil {
		*f.allowanceCalls += 1
	}
	if f.allowanceSpender != nil {
		*f.allowanceSpender = spender
	}
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
	name            string
	quote           NormalizedQuote
	quoteErr        error
	supportedChains []int64
	quoteCalls      *int
	approvalTarget  string
	approvalErr     error
	approvalCalls   *int
	approveTx       InternalEvmTxEnvelope
	approveErr      error
	approveCalls    *int
	swapTx          InternalEvmTxEnvelope
	approveSpender  *string
	swapSpender     *string
}

func (f fakeProvider) Name() string { return f.name }
func (f fakeProvider) SupportedChains() []int64 {
	if f.supportedChains != nil {
		return f.supportedChains
	}
	return []int64{1}
}
func (f fakeProvider) GetQuote(input QuoteInput) (NormalizedQuote, error) {
	if f.quoteCalls != nil {
		*f.quoteCalls += 1
	}
	if f.quoteErr != nil {
		return NormalizedQuote{}, f.quoteErr
	}
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
	if f.approvalCalls != nil {
		*f.approvalCalls += 1
	}
	if f.approvalErr != nil {
		return "", f.approvalErr
	}
	if f.approvalTarget != "" {
		return f.approvalTarget, nil
	}
	return quote.Spender, nil
}
func (f fakeProvider) BuildApproveTx(quote NormalizedQuote, taker string) (InternalEvmTxEnvelope, error) {
	if f.approveCalls != nil {
		*f.approveCalls += 1
	}
	if f.approveSpender != nil {
		*f.approveSpender = quote.Spender
	}
	if f.approveErr != nil {
		return InternalEvmTxEnvelope{}, f.approveErr
	}
	return f.approveTx, nil
}
func (f fakeProvider) BuildSwapTx(quote NormalizedQuote, taker string) (InternalEvmTxEnvelope, error) {
	if f.swapSpender != nil {
		*f.swapSpender = quote.Spender
	}
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

func TestQuoteReturnsAllRoutesAndRecommended(t *testing.T) {
	providers := []QuoteProvider{
		fakeProvider{name: "0x", quote: NormalizedQuote{Spender: "0xspender0", TransactionTo: "0xrouter0", MinAmountOut: "90", AmountOut: "100", ExpiresAt: time.Unix(60, 0)}},
		fakeProvider{name: "kyberswap", quote: NormalizedQuote{Spender: "0xspender1", TransactionTo: "0xrouter1", MinAmountOut: "100", AmountOut: "120", ExpiresAt: time.Unix(60, 0)}},
	}
	svc := newTestService(t, providers)
	out, err := svc.Quote(context.Background(), QuoteInput{
		UserID: "u1", ChainID: 1, FromToken: "0xA", ToToken: "0xB", AmountIn: "100", SlippageBps: 50, WalletAddress: "0xwallet",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(out.Routes))
	}
	if out.QuoteID == "" || out.QuoteID != out.RecommendedQuoteID || out.QuoteID != out.Routes[0].QuoteID {
		t.Fatalf("expected top quote id to match recommended route, got top=%s recommended=%s route0=%s", out.QuoteID, out.RecommendedQuoteID, out.Routes[0].QuoteID)
	}
	if out.SelectedProvider != "kyberswap" || out.Routes[0].Provider != "kyberswap" {
		t.Fatalf("expected kyberswap recommendation, got selected=%s route0=%s", out.SelectedProvider, out.Routes[0].Provider)
	}
	if out.Spender != "0xspender1" || out.Spender != out.Routes[0].Spender || out.Routes[1].Spender != "0xspender0" {
		t.Fatalf("unexpected quote spenders: top=%s routes=%+v", out.Spender, out.Routes)
	}
	repo := svc.repo.(*MemoryRepository)
	if len(repo.quotes) != 2 {
		t.Fatalf("expected exactly 2 stored quotes, got %d", len(repo.quotes))
	}
}

func TestQuoteFiltersSpecifiedProvider(t *testing.T) {
	zeroXCalls := 0
	kyberCalls := 0
	providers := []QuoteProvider{
		fakeProvider{name: "0x", quoteCalls: &zeroXCalls, quote: NormalizedQuote{Spender: "0xspender0", TransactionTo: "0xrouter0", MinAmountOut: "90", AmountOut: "100", ExpiresAt: time.Unix(60, 0)}},
		fakeProvider{name: "kyberswap", quoteCalls: &kyberCalls, quote: NormalizedQuote{Spender: "0xspender1", TransactionTo: "0xrouter1", MinAmountOut: "100", AmountOut: "120", ExpiresAt: time.Unix(60, 0)}},
	}
	svc := newTestService(t, providers)
	out, err := svc.Quote(context.Background(), QuoteInput{
		UserID: "u1", ChainID: 1, FromToken: "0xA", ToToken: "0xB", AmountIn: "100", SlippageBps: 50, WalletAddress: "0xwallet", Provider: "KyberSwap",
	})
	if err != nil {
		t.Fatal(err)
	}
	if zeroXCalls != 0 || kyberCalls != 1 {
		t.Fatalf("expected only kyberswap to be called, got 0x=%d kyberswap=%d", zeroXCalls, kyberCalls)
	}
	if len(out.Routes) != 1 || out.Routes[0].Provider != "kyberswap" || out.SelectedProvider != "kyberswap" {
		t.Fatalf("unexpected filtered quote response: %+v", out)
	}
	if out.Spender != "0xspender1" || out.Routes[0].Spender != "0xspender1" {
		t.Fatalf("unexpected spender for filtered quote: top=%s route=%s", out.Spender, out.Routes[0].Spender)
	}
}

func TestQuoteRejectsUnknownProvider(t *testing.T) {
	svc := newTestService(t, []QuoteProvider{
		fakeProvider{name: "0x", quote: NormalizedQuote{Spender: "0xspender", TransactionTo: "0xrouter", MinAmountOut: "90", AmountOut: "100", ExpiresAt: time.Unix(60, 0)}},
	})
	_, err := svc.Quote(context.Background(), QuoteInput{
		UserID: "u1", ChainID: 1, FromToken: "0xA", ToToken: "0xB", AmountIn: "100", SlippageBps: 50, WalletAddress: "0xwallet", Provider: "missing",
	})
	if !errors.Is(err, ErrProviderDisabled) {
		t.Fatalf("expected provider disabled, got %v", err)
	}
}

func TestQuoteRejectsSpecifiedProviderUnsupportedChain(t *testing.T) {
	svc := newTestService(t, []QuoteProvider{
		fakeProvider{name: "kyberswap", supportedChains: []int64{56}, quote: NormalizedQuote{Spender: "0xspender", TransactionTo: "0xrouter", MinAmountOut: "90", AmountOut: "100", ExpiresAt: time.Unix(60, 0)}},
	})
	_, err := svc.Quote(context.Background(), QuoteInput{
		UserID: "u1", ChainID: 1, FromToken: "0xA", ToToken: "0xB", AmountIn: "100", SlippageBps: 50, WalletAddress: "0xwallet", Provider: "kyberswap",
	})
	if !errors.Is(err, ErrUnsupportedChain) {
		t.Fatalf("expected unsupported chain, got %v", err)
	}
}

func TestQuoteSkipsFailedProvider(t *testing.T) {
	providers := []QuoteProvider{
		fakeProvider{name: "0x", quoteErr: errors.New("quote failed")},
		fakeProvider{name: "kyberswap", quote: NormalizedQuote{Spender: "0xspender", TransactionTo: "0xrouter", MinAmountOut: "90", AmountOut: "100", ExpiresAt: time.Unix(60, 0)}},
	}
	svc := newTestService(t, providers)
	out, err := svc.Quote(context.Background(), QuoteInput{
		UserID: "u1", ChainID: 1, FromToken: "0xA", ToToken: "0xB", AmountIn: "100", SlippageBps: 50, WalletAddress: "0xwallet",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Routes) != 1 || out.Routes[0].Provider != "kyberswap" {
		t.Fatalf("expected only successful kyberswap route, got %+v", out.Routes)
	}
}

func TestQuoteResolvesAndReturnsProviderSpender(t *testing.T) {
	approvalCalls := 0
	provider := fakeProvider{
		name:           "1inch",
		approvalTarget: "0xapproval",
		approvalCalls:  &approvalCalls,
		quote:          NormalizedQuote{TransactionTo: "0xrouter", MinAmountOut: "90", AmountOut: "100", ExpiresAt: time.Unix(60, 0)},
	}
	svc := newTestService(t, []QuoteProvider{provider})
	out, err := svc.Quote(context.Background(), QuoteInput{
		UserID: "u1", ChainID: 1, FromToken: "0xA", ToToken: "0xB", AmountIn: "100", SlippageBps: 50, WalletAddress: "0xwallet",
	})
	if err != nil {
		t.Fatal(err)
	}
	if approvalCalls != 1 {
		t.Fatalf("expected one approval target lookup, got %d", approvalCalls)
	}
	if out.Spender != "0xapproval" || len(out.Routes) != 1 || out.Routes[0].Spender != "0xapproval" {
		t.Fatalf("expected resolved spender in quote response, got top=%s routes=%+v", out.Spender, out.Routes)
	}
	stored, err := svc.repo.GetQuote(out.QuoteID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Spender != "0xapproval" {
		t.Fatalf("expected resolved spender cached on quote, got %s", stored.Spender)
	}
}

func TestQuoteSkipsNativeTokenApprovalTarget(t *testing.T) {
	approvalCalls := 0
	provider := fakeProvider{
		name:           "0x",
		approvalTarget: "0xapproval",
		approvalCalls:  &approvalCalls,
		quote:          NormalizedQuote{Spender: "0xspender", TransactionTo: "0xrouter", MinAmountOut: "90", AmountOut: "100", ExpiresAt: time.Unix(60, 0)},
	}
	svc := newTestService(t, []QuoteProvider{provider})
	out, err := svc.Quote(context.Background(), QuoteInput{
		UserID: "u1", ChainID: 1, FromToken: NativeTokenAddress, ToToken: "0xB", AmountIn: "100", SlippageBps: 50, WalletAddress: "0xwallet",
	})
	if err != nil {
		t.Fatal(err)
	}
	if approvalCalls != 0 {
		t.Fatalf("expected native token to skip approval target lookup, got %d calls", approvalCalls)
	}
	if out.Spender != "" || len(out.Routes) != 1 || out.Routes[0].Spender != "" {
		t.Fatalf("expected native quote spender to be empty, got top=%s routes=%+v", out.Spender, out.Routes)
	}
	stored, err := svc.repo.GetQuote(out.QuoteID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Spender != "" {
		t.Fatalf("expected stored native quote spender to be empty, got %s", stored.Spender)
	}
}

func TestQuoteRejectsSpecifiedProviderApprovalTargetError(t *testing.T) {
	svc := newTestService(t, []QuoteProvider{
		fakeProvider{
			name:        "1inch",
			approvalErr: errors.New("approval failed"),
			quote:       NormalizedQuote{TransactionTo: "0xrouter", MinAmountOut: "90", AmountOut: "100", ExpiresAt: time.Unix(60, 0)},
		},
	})
	_, err := svc.Quote(context.Background(), QuoteInput{
		UserID: "u1", ChainID: 1, FromToken: "0xA", ToToken: "0xB", AmountIn: "100", SlippageBps: 50, WalletAddress: "0xwallet", Provider: "1inch",
	})
	if err == nil {
		t.Fatal("expected specified provider quote to fail when approval target lookup fails")
	}
}

func TestQuoteSkipsProviderWhenApprovalTargetFails(t *testing.T) {
	providers := []QuoteProvider{
		fakeProvider{
			name:        "1inch",
			approvalErr: errors.New("approval failed"),
			quote:       NormalizedQuote{TransactionTo: "0xrouter0", MinAmountOut: "90", AmountOut: "100", ExpiresAt: time.Unix(60, 0)},
		},
		fakeProvider{name: "kyberswap", quote: NormalizedQuote{Spender: "0xspender1", TransactionTo: "0xrouter1", MinAmountOut: "100", AmountOut: "120", ExpiresAt: time.Unix(60, 0)}},
	}
	svc := newTestService(t, providers)
	out, err := svc.Quote(context.Background(), QuoteInput{
		UserID: "u1", ChainID: 1, FromToken: "0xA", ToToken: "0xB", AmountIn: "100", SlippageBps: 50, WalletAddress: "0xwallet",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Routes) != 1 || out.Routes[0].Provider != "kyberswap" || out.Routes[0].Spender != "0xspender1" {
		t.Fatalf("expected only successful kyberswap route, got %+v", out.Routes)
	}
}

func TestExecuteUsesSelectedRouteQuoteID(t *testing.T) {
	providers := []QuoteProvider{
		fakeProvider{
			name:   "0x",
			quote:  NormalizedQuote{Spender: "0xspender0", TransactionTo: "0xrouter0", MinAmountOut: "90", AmountOut: "100", ExpiresAt: time.Unix(60, 0)},
			swapTx: InternalEvmTxEnvelope{GasType: GasTypeEIP1559, ChainID: 1, To: "0xrouter0", Data: "0xabc", Value: "0", GasLimit: "185000"},
		},
		fakeProvider{
			name:   "kyberswap",
			quote:  NormalizedQuote{Spender: "0xspender1", TransactionTo: "0xrouter1", MinAmountOut: "100", AmountOut: "120", ExpiresAt: time.Unix(60, 0)},
			swapTx: InternalEvmTxEnvelope{GasType: GasTypeEIP1559, ChainID: 1, To: "0xrouter1", Data: "0xdef", Value: "0", GasLimit: "190000"},
		},
	}
	svc := newTestService(t, providers)
	out, err := svc.Quote(context.Background(), QuoteInput{
		UserID: "u1", ChainID: 1, FromToken: "0xA", ToToken: "0xB", AmountIn: "100", SlippageBps: 50, WalletAddress: "0xwallet",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Routes) != 2 || out.Routes[1].Provider != "0x" {
		t.Fatalf("expected second route to be 0x, got %+v", out.Routes)
	}
	exec, err := svc.Execute(context.Background(), out.Routes[1].QuoteID, "0xwallet", WalletTypeExternal)
	if err != nil {
		t.Fatal(err)
	}
	if exec.Transaction.To != "0xrouter0" {
		t.Fatalf("expected selected 0x route tx, got %+v", exec.Transaction)
	}
}

func TestAllowanceSkipsNativeToken(t *testing.T) {
	now := func() time.Time { return time.Unix(0, 0) }
	repo := NewMemoryRepository(now)
	allowanceCalls := 0
	approvalCalls := 0
	svc := NewService(DefaultConfig(), repo, fakeRPC{allowanceCalls: &allowanceCalls}, []QuoteProvider{
		fakeProvider{name: "0x", approvalTarget: "0xapproval", approvalCalls: &approvalCalls},
	}, now)
	quote, err := repo.SaveQuote(NormalizedQuote{
		Provider:  "0x",
		ChainID:   1,
		FromToken: TokenInfo{Address: NativeTokenAddress, Symbol: "ETH", Decimals: 18},
		ToToken:   TokenInfo{Address: "0xToken", Symbol: "T", Decimals: 18},
		AmountIn:  "100",
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
	if allowanceCalls != 0 {
		t.Fatalf("expected native token to skip allowance rpc, got %d calls", allowanceCalls)
	}
	if approvalCalls != 0 {
		t.Fatalf("expected native token to skip approval target lookup, got %d calls", approvalCalls)
	}
}

func TestAllowanceResolvesAndCachesProviderSpender(t *testing.T) {
	now := func() time.Time { return time.Unix(0, 0) }
	repo := NewMemoryRepository(now)
	approvalCalls := 0
	allowanceCalls := 0
	var allowanceSpender string
	svc := NewService(DefaultConfig(), repo, fakeRPC{
		allowance:        "50",
		allowanceCalls:   &allowanceCalls,
		allowanceSpender: &allowanceSpender,
	}, []QuoteProvider{
		fakeProvider{name: "1inch", approvalTarget: "0xapproval", approvalCalls: &approvalCalls},
	}, now)
	quote, err := repo.SaveQuote(NormalizedQuote{
		Provider:  "1inch",
		ChainID:   1,
		FromToken: TokenInfo{Address: "0xToken", Symbol: "T", Decimals: 18},
		ToToken:   TokenInfo{Address: "0xOut", Symbol: "O", Decimals: 18},
		AmountIn:  "100",
		ExpiresAt: time.Unix(60, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.Allowance(context.Background(), quote.ID, "0xwallet")
	if err != nil {
		t.Fatal(err)
	}
	if out.Spender != "0xapproval" || allowanceSpender != "0xapproval" {
		t.Fatalf("expected resolved spender to be used, response=%s rpc=%s", out.Spender, allowanceSpender)
	}
	if out.AllowanceEnough {
		t.Fatal("expected allowance to be insufficient")
	}
	if approvalCalls != 1 || allowanceCalls != 1 {
		t.Fatalf("expected one approval lookup and one allowance call, got approval=%d allowance=%d", approvalCalls, allowanceCalls)
	}
	cached, err := repo.GetQuote(quote.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cached.Spender != "0xapproval" {
		t.Fatalf("expected spender cached on quote, got %s", cached.Spender)
	}
}

func TestAllowanceUsesExistingSpender(t *testing.T) {
	now := func() time.Time { return time.Unix(0, 0) }
	repo := NewMemoryRepository(now)
	approvalCalls := 0
	var allowanceSpender string
	svc := NewService(DefaultConfig(), repo, fakeRPC{
		allowance:        "1000",
		allowanceSpender: &allowanceSpender,
	}, []QuoteProvider{
		fakeProvider{name: "0x", approvalTarget: "0xapproval", approvalCalls: &approvalCalls},
	}, now)
	quote, err := repo.SaveQuote(NormalizedQuote{
		Provider:  "0x",
		ChainID:   1,
		FromToken: TokenInfo{Address: "0xToken", Symbol: "T", Decimals: 18},
		ToToken:   TokenInfo{Address: "0xOut", Symbol: "O", Decimals: 18},
		AmountIn:  "100",
		Spender:   "0xexisting",
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
		t.Fatal("expected allowance to be enough")
	}
	if out.Spender != "0xexisting" || allowanceSpender != "0xexisting" {
		t.Fatalf("expected existing spender to be used, response=%s rpc=%s", out.Spender, allowanceSpender)
	}
	if approvalCalls != 0 {
		t.Fatalf("expected existing spender to skip approval lookup, got %d calls", approvalCalls)
	}
}

func TestAllowanceRejectsRequiredFields(t *testing.T) {
	svc := newTestService(t, nil)
	if _, err := svc.Allowance(context.Background(), "", "0xwallet"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected quoteId invalid argument, got %v", err)
	}
	if _, err := svc.Allowance(context.Background(), "quote-id", ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected walletAddress invalid argument, got %v", err)
	}
}

func TestApproveTxRejectsRequiredFields(t *testing.T) {
	svc := newTestService(t, nil)
	if _, err := svc.ApproveTx(context.Background(), "", "0xwallet"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected quoteId invalid argument, got %v", err)
	}
	if _, err := svc.ApproveTx(context.Background(), "quote-id", ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected walletAddress invalid argument, got %v", err)
	}
}

func TestApproveTxRejectsExpiredQuote(t *testing.T) {
	svc := newTestService(t, []QuoteProvider{fakeProvider{name: "0x"}})
	quote, err := svc.repo.SaveQuote(NormalizedQuote{
		Provider:  "0x",
		ChainID:   1,
		FromToken: TokenInfo{Address: "0xToken", Symbol: "T", Decimals: 18},
		ToToken:   TokenInfo{Address: "0xOut", Symbol: "O", Decimals: 18},
		AmountIn:  "100",
		Spender:   "0xspender",
		ExpiresAt: time.Unix(-1, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApproveTx(context.Background(), quote.ID, "0xwallet"); !errors.Is(err, ErrQuoteExpired) {
		t.Fatalf("expected quote expired, got %v", err)
	}
}

func TestApproveTxRejectsNativeToken(t *testing.T) {
	now := func() time.Time { return time.Unix(0, 0) }
	repo := NewMemoryRepository(now)
	approvalCalls := 0
	approveCalls := 0
	svc := NewService(DefaultConfig(), repo, fakeRPC{}, []QuoteProvider{
		fakeProvider{name: "0x", approvalTarget: "0xapproval", approvalCalls: &approvalCalls, approveCalls: &approveCalls},
	}, now)
	quote, err := repo.SaveQuote(NormalizedQuote{
		Provider:  "0x",
		ChainID:   1,
		FromToken: TokenInfo{Address: NativeTokenAddress, Symbol: "ETH", Decimals: 18},
		ToToken:   TokenInfo{Address: "0xOut", Symbol: "O", Decimals: 18},
		AmountIn:  "100",
		ExpiresAt: time.Unix(60, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApproveTx(context.Background(), quote.ID, "0xwallet"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected invalid argument for native approve, got %v", err)
	}
	if approvalCalls != 0 || approveCalls != 0 {
		t.Fatalf("expected native approve to skip provider calls, got approval=%d approve=%d", approvalCalls, approveCalls)
	}
}

func TestApproveTxUsesExistingSpender(t *testing.T) {
	now := func() time.Time { return time.Unix(0, 0) }
	repo := NewMemoryRepository(now)
	approvalCalls := 0
	approveCalls := 0
	var approveSpender string
	expected := InternalEvmTxEnvelope{
		GasType:              GasTypeEIP1559,
		ChainID:              1,
		To:                   "0xToken",
		Data:                 "0xapprove",
		Value:                "0",
		GasLimit:             "60000",
		MaxFeePerGas:         "100",
		MaxPriorityFeePerGas: "10",
	}
	svc := NewService(DefaultConfig(), repo, fakeRPC{}, []QuoteProvider{
		fakeProvider{
			name:           "0x",
			approvalTarget: "0xapproval",
			approvalCalls:  &approvalCalls,
			approveCalls:   &approveCalls,
			approveSpender: &approveSpender,
			approveTx:      expected,
		},
	}, now)
	quote, err := repo.SaveQuote(NormalizedQuote{
		Provider:  "0x",
		ChainID:   1,
		FromToken: TokenInfo{Address: "0xToken", Symbol: "T", Decimals: 18},
		ToToken:   TokenInfo{Address: "0xOut", Symbol: "O", Decimals: 18},
		AmountIn:  "100",
		Spender:   "0xexisting",
		ExpiresAt: time.Unix(60, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.ApproveTx(context.Background(), quote.ID, "0xwallet")
	if err != nil {
		t.Fatal(err)
	}
	if approvalCalls != 0 || approveCalls != 1 {
		t.Fatalf("expected existing spender to skip approval lookup and build once, got approval=%d approve=%d", approvalCalls, approveCalls)
	}
	if approveSpender != "0xexisting" {
		t.Fatalf("expected existing spender in approve builder, got %s", approveSpender)
	}
	if out.GasType != expected.GasType || out.ChainID != expected.ChainID || out.To != expected.To || out.Data != expected.Data ||
		out.Value != expected.Value || out.GasLimit != expected.GasLimit || out.MaxFeePerGas != expected.MaxFeePerGas ||
		out.MaxPriorityFeePerGas != expected.MaxPriorityFeePerGas {
		t.Fatalf("unexpected approve response: %+v", out)
	}
}

func TestApproveTxResolvesAndCachesProviderSpender(t *testing.T) {
	now := func() time.Time { return time.Unix(0, 0) }
	repo := NewMemoryRepository(now)
	approvalCalls := 0
	approveCalls := 0
	var approveSpender string
	svc := NewService(DefaultConfig(), repo, fakeRPC{}, []QuoteProvider{
		fakeProvider{
			name:           "1inch",
			approvalTarget: "0xapproval",
			approvalCalls:  &approvalCalls,
			approveCalls:   &approveCalls,
			approveSpender: &approveSpender,
			approveTx:      InternalEvmTxEnvelope{GasType: GasTypeEIP1559, ChainID: 1, To: "0xToken", Data: "0xapprove", Value: "0", GasLimit: "60000"},
		},
	}, now)
	quote, err := repo.SaveQuote(NormalizedQuote{
		Provider:  "1inch",
		ChainID:   1,
		FromToken: TokenInfo{Address: "0xToken", Symbol: "T", Decimals: 18},
		ToToken:   TokenInfo{Address: "0xOut", Symbol: "O", Decimals: 18},
		AmountIn:  "100",
		ExpiresAt: time.Unix(60, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.ApproveTx(context.Background(), quote.ID, "0xwallet")
	if err != nil {
		t.Fatal(err)
	}
	if out.To != "0xToken" || out.Data != "0xapprove" {
		t.Fatalf("unexpected approve response: %+v", out)
	}
	if approvalCalls != 1 || approveCalls != 1 {
		t.Fatalf("expected one approval lookup and one approve build, got approval=%d approve=%d", approvalCalls, approveCalls)
	}
	if approveSpender != "0xapproval" {
		t.Fatalf("expected resolved spender in approve builder, got %s", approveSpender)
	}
	cached, err := repo.GetQuote(quote.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cached.Spender != "0xapproval" {
		t.Fatalf("expected resolved spender cached on quote, got %s", cached.Spender)
	}
}

func TestApproveTxReturnsProviderError(t *testing.T) {
	approveErr := errors.New("approve failed")
	svc := newTestService(t, []QuoteProvider{
		fakeProvider{name: "0x", approveErr: approveErr},
	})
	quote, err := svc.repo.SaveQuote(NormalizedQuote{
		Provider:  "0x",
		ChainID:   1,
		FromToken: TokenInfo{Address: "0xToken", Symbol: "T", Decimals: 18},
		ToToken:   TokenInfo{Address: "0xOut", Symbol: "O", Decimals: 18},
		AmountIn:  "100",
		Spender:   "0xspender",
		ExpiresAt: time.Unix(60, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApproveTx(context.Background(), quote.ID, "0xwallet"); !errors.Is(err, approveErr) {
		t.Fatalf("expected provider approve error, got %v", err)
	}
}

func TestExecuteResolvesProviderSpender(t *testing.T) {
	now := func() time.Time { return time.Unix(0, 0) }
	repo := NewMemoryRepository(now)
	approvalCalls := 0
	allowanceCalls := 0
	var allowanceSpender string
	var swapSpender string
	svc := NewService(DefaultConfig(), repo, fakeRPC{
		allowance:        "1000",
		allowanceCalls:   &allowanceCalls,
		allowanceSpender: &allowanceSpender,
	}, []QuoteProvider{
		fakeProvider{
			name:           "1inch",
			approvalTarget: "0xapproval",
			approvalCalls:  &approvalCalls,
			swapSpender:    &swapSpender,
			swapTx:         InternalEvmTxEnvelope{GasType: GasTypeEIP1559, ChainID: 1, To: "0xrouter", Data: "0xabc", Value: "0", GasLimit: "185000"},
		},
	}, now)
	quote, err := repo.SaveQuote(NormalizedQuote{
		Provider:     "1inch",
		ChainID:      1,
		FromToken:    TokenInfo{Address: "0xToken", Symbol: "T", Decimals: 18},
		ToToken:      TokenInfo{Address: "0xOut", Symbol: "O", Decimals: 18},
		AmountIn:     "100",
		AmountOut:    "200",
		MinAmountOut: "1",
		ExpiresAt:    time.Unix(60, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	exec, err := svc.Execute(context.Background(), quote.ID, "0xwallet", WalletTypeExternal)
	if err != nil {
		t.Fatal(err)
	}
	if exec.Transaction.To != "0xrouter" {
		t.Fatalf("unexpected swap tx: %+v", exec.Transaction)
	}
	if approvalCalls != 1 || allowanceCalls != 1 {
		t.Fatalf("expected one approval lookup and one allowance call, got approval=%d allowance=%d", approvalCalls, allowanceCalls)
	}
	if allowanceSpender != "0xapproval" || swapSpender != "0xapproval" {
		t.Fatalf("expected resolved spender in allowance and swap build, allowance=%s swap=%s", allowanceSpender, swapSpender)
	}
	order, err := repo.GetOrder(exec.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if order.Spender != "0xapproval" {
		t.Fatalf("expected order spender to be resolved, got %s", order.Spender)
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
