# Allowance / Approve 授权图

这张图展示 ERC20 授权相关的核心关系。`spender` 是 provider 决定的 approval target，前端不传，后端通过 `quoteId` 读取。

```mermaid
sequenceDiagram
    participant UI as Application UI
    participant API as Swap API
    participant Service as Service
    participant Repo as Repository
    participant Provider as QuoteProvider
    participant RPC as ChainClient
    participant ERC20 as ERC20 Token Contract
    participant Wallet as Wallet

    UI->>API: POST /swap/quote
    API->>Service: Quote(input)
    Service->>Provider: GetQuote(input)
    Provider-->>Service: NormalizedQuote
    Service->>Provider: GetApprovalTarget(quote)
    Provider-->>Service: spender
    Service->>Repo: SaveQuote(quote with spender)
    Service-->>API: QuoteResponse with spender
    API-->>UI: quoteId and spender

    UI->>API: POST /swap/allowance with quoteId
    API->>Service: Allowance(quoteId, wallet)
    Service->>Repo: GetQuote(quoteId)
    Service->>RPC: eth_call allowance(owner, spender)
    RPC->>ERC20: allowance(wallet, spender)
    ERC20-->>RPC: currentAllowance
    RPC-->>Service: currentAllowance
    Service-->>API: AllowanceResponse
    API-->>UI: allowanceEnough

    alt allowanceEnough is false
        UI->>API: POST /swap/approve-tx with quoteId
        API->>Service: ApproveTx(quoteId, wallet)
        Service->>Repo: GetQuote(quoteId)
        Service->>Provider: BuildApproveTx(quote, wallet)
        Provider-->>Service: approve tx
        Service-->>API: EvmTxEnvelope
        API-->>UI: approve tx
        UI->>Wallet: Sign and broadcast approve tx
        Wallet->>ERC20: approve(spender, amountIn)
    end
```

```mermaid
flowchart LR
    wallet["owner<br/>walletAddress"]
    token["fromToken<br/>ERC20 contract"]
    spender["spender<br/>provider approval target"]
    amount["amountIn<br/>quote amount"]

    wallet -->|"allowance(owner, spender)"| token
    token -->|"returns currentAllowance"| wallet
    wallet -->|"approve(spender, amountIn)"| token
    spender -.->|"authorized to spend"| token
    amount -.->|"requiredAmount"| token
```

## 当前实现要点

- native token 不需要 ERC20 approve。
- `/swap/allowance` 查询的是 `ERC20.allowance(walletAddress, quote.Spender)`。
- `/swap/approve-tx` 构造的是 `ERC20.approve(quote.Spender, quote.AmountIn)`。
- `/swap/approve-tx` 不检查 allowance 是否足够，也不广播交易。
- approve 金额当前使用本次 quote 的精确 `amountIn`，不是无限授权。

## 对应代码入口

- `src/swap/internal/swap/service.go`
- `src/swap/internal/swap/rpc.go`
- `src/swap/internal/swap/util.go`
- `src/swap/internal/swap/provider_0x.go`
- `src/swap/internal/swap/provider_1inch.go`
- `src/swap/internal/swap/provider_kyberswap.go`

