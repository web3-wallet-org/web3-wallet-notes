# Provider 适配图

这张图展示聚合器 provider 的统一接口。新增聚合器时，核心是实现 `QuoteProvider` 的四个业务方法。

```mermaid
classDiagram
    class QuoteProvider {
        <<interface>>
        Name() string
        SupportedChains() []int64
        GetQuote(input QuoteInput) NormalizedQuote
        GetApprovalTarget(quote NormalizedQuote) string
        BuildApproveTx(quote NormalizedQuote, taker string) InternalEvmTxEnvelope
        BuildSwapTx(quote NormalizedQuote, taker string) InternalEvmTxEnvelope
    }

    class ZeroXProvider {
        GetQuote()
        GetApprovalTarget()
        BuildApproveTx()
        BuildSwapTx()
    }

    class OneInchProvider {
        GetQuote()
        GetApprovalTarget()
        BuildApproveTx()
        BuildSwapTx()
    }

    class KyberSwapProvider {
        GetQuote()
        GetApprovalTarget()
        BuildApproveTx()
        BuildSwapTx()
    }

    QuoteProvider <|.. ZeroXProvider
    QuoteProvider <|.. OneInchProvider
    QuoteProvider <|.. KyberSwapProvider
```

```mermaid
flowchart TD
    service["Service"]
    getQuote["GetQuote<br/>provider quote API"]
    approval["GetApprovalTarget<br/>spender / approval target"]
    approve["BuildApproveTx<br/>approve tx envelope"]
    swap["BuildSwapTx<br/>swap tx envelope"]

    service --> getQuote
    service --> approval
    service --> approve
    service --> swap

    getQuote --> normalized["NormalizedQuote"]
    approval --> spender["spender"]
    approve --> approveTx["InternalEvmTxEnvelope"]
    swap --> swapTx["InternalEvmTxEnvelope"]
```

## 当前实现要点

- 0x 和 KyberSwap 的 approve tx 当前由本地编码 ERC20 `approve(spender, amountIn)`。
- 1inch 的 approve tx 当前调用 1inch `/approve/transaction`。
- quote 阶段会解析 `spender`，并保存到 `NormalizedQuote`。
- 后续 allowance、approve、execute 仍通过 `quoteId` 读取保存的 quote，不信任前端传 spender。

## 对应代码入口

- `src/swap/internal/swap/types.go`
- `src/swap/internal/swap/provider_0x.go`
- `src/swap/internal/swap/provider_1inch.go`
- `src/swap/internal/swap/provider_kyberswap.go`
- `src/swap/internal/swap/service.go`

