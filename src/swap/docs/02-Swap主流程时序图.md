# Swap 主流程时序图

这张图展示前端按当前实现调用 swap 接口的主流程。用户选择某个候选 route 后，后续接口都使用该候选自己的 `quoteId`。

```mermaid
sequenceDiagram
    participant User as User
    participant UI as Application UI
    participant API as Swap API
    participant Service as Service
    participant Provider as QuoteProvider
    participant Repo as Repository
    participant RPC as ChainClient
    participant Wallet as Wallet
    participant Chain as EVM Chain

    User->>UI: Input swap params
    UI->>API: POST /swap/quote
    API->>Service: Quote(input)
    Service->>Provider: GetQuote(input)
    Provider-->>Service: NormalizedQuote
    Service->>Provider: GetApprovalTarget(quote)
    Provider-->>Service: spender
    Service->>Repo: SaveQuote(each candidate)
    Service-->>API: QuoteResponse with routes and spender
    API-->>UI: Display quote candidates

    User->>UI: Select route
    UI->>API: POST /swap/allowance with quoteId
    API->>Service: Allowance(quoteId, wallet)
    Service->>Repo: GetQuote(quoteId)
    Service->>RPC: ERC20.allowance(owner, spender)
    RPC-->>Service: currentAllowance
    Service-->>API: AllowanceResponse
    API-->>UI: allowanceEnough

    alt allowanceEnough is false
        UI->>API: POST /swap/approve-tx
        API->>Service: ApproveTx(quoteId, wallet)
        Service->>Repo: GetQuote(quoteId)
        Service->>Provider: BuildApproveTx(quote, wallet)
        Provider-->>Service: approve tx envelope
        Service-->>API: EvmTxEnvelope
        API-->>UI: approve tx
        UI->>Wallet: Request approve signature
        Wallet->>Chain: Broadcast approve tx
    end

    UI->>API: POST /swap/execute
    API->>Service: Execute(quoteId, wallet, walletType)
    Service->>Repo: GetQuote(quoteId)
    Service->>RPC: ERC20.allowance(owner, spender)
    RPC-->>Service: currentAllowance
    Service->>Provider: BuildSwapTx(quote, wallet)
    Provider-->>Service: swap tx envelope
    Service->>Repo: CreateOrder(SIGNING)
    Service->>Repo: AddEvent(order created)
    Service-->>API: ExecuteResponse
    API-->>UI: swap tx and orderId

    UI->>Wallet: Request swap signature
    Wallet->>Chain: Broadcast swap tx
    UI->>API: POST /swap/submit-hash
    API->>Service: SubmitHash(orderId, txHash)
    Service->>Repo: UpdateOrder(TX_HASH_RECEIVED)
    Service->>Repo: AddEvent(tx hash received)

    UI->>API: GET /swap/status/{orderId}
    API->>Service: Status(orderId)
    Service->>Repo: GetOrder and ListEvents
    Service-->>API: StatusResponse
    API-->>UI: status and nextAction
```

## 当前实现要点

- `/swap/quote` 返回所有成功 provider 候选，默认推荐是 `routes[0]`。
- `spender` 在 quote 阶段解析并保存到 `NormalizedQuote`。
- `/swap/allowance` 不接收 spender，只通过 `quoteId` 读取保存的 quote。
- `/swap/approve-tx` 只构造 approve 交易，不签名、不广播、不创建订单。
- `/swap/execute` 创建 `StoredOrder`，初始状态是 `SIGNING`。
- `/swap/submit-hash` 适用于外部钱包广播后提交 txHash。

## 对应代码入口

- `src/swap/internal/swap/http.go`
- `src/swap/internal/swap/service.go`
- `src/swap/internal/swap/provider_0x.go`
- `src/swap/internal/swap/provider_1inch.go`
- `src/swap/internal/swap/provider_kyberswap.go`

