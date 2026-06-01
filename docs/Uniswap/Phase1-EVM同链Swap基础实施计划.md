# Phase 1 - EVM 同链 Swap 基础实施计划

这篇只讲第一阶段怎么落地。

Phase 1 的目标不是做完整多链跨链 Swap，而是先把**EVM 同链 Swap**跑通。

```text
用户在同一条 EVM 链上，把 tokenA 换成 tokenB。
```

## 一、Phase 1 范围

### 本阶段要做

```text
EVM 同链 swap
1inch Adapter
0x Adapter
报价归一化
基础路由选择
基础风控
approve / allowance 检查
交易构造
签名边界
广播与状态追踪
订单表落库
```

### 本阶段不做

```text
跨链 swap
Solana / 非 EVM 链
LP 仓位管理
限价单
gasless swap
复杂 MEV 保护
自研 AMM
```

先把最小可用链路跑通，再扩展。

## 二、推荐支持链

Phase 1 不建议一开始铺太多链。

建议先选 3 条：

```text
Ethereum
BSC
Base
```

原因：

- Ethereum 是主场，流动性最深；
- BSC 用户多，PancakeSwap / 1inch / 0x 等覆盖有价值；
- Base 增长快，适合验证 L2 体验。

稳定后再扩：

```text
Arbitrum
Optimism
Polygon
Avalanche
```

## 三、Phase 1 主链路

```text
用户输入 swap 参数
  |
  v
/swap/quote
  |
  v
Quote Engine
  |
  |-- 1inch Adapter
  |-- 0x Adapter
  |
  v
NormalizedQuote[]
  |
  v
Basic Risk Filter        (黑名单 / spender 白名单 / chainId 校验)
  |
  v
Route Engine 选择最优报价
  |
  v
swap_quotes 落库
  |
  v
前端展示报价
  |
  v
用户确认
  |
  v
检查 allowance
  |
  |-- 不足：构造 approve tx
  |-- 足够：构造 swap tx
  |
  v
用户签名 / MPC 签名
  |
  v
广播交易
  |
  v
Monitor 监听交易状态
  |
  v
swap_transactions / swap_events 更新（swap_orders 在 /swap/execute 构造时已创建）
```

最重要的原则：

```text
quote 是预估。
execute 前必须检查 quote 是否过期。
swap 交易必须有 minAmountOut。
```

## 四、后端接口设计

### 4.1 获取报价

```http
POST /swap/quote
```

请求：

```json
{
  "userId": "123",
  "chainId": 1,
  "fromToken": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
  "toToken": "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2",
  "amountIn": "100000000",
  "slippageBps": 50,
  "walletAddress": "0x..."
}
```

返回：

```json
{
  "quoteId": "uuid",
  "expiresAt": 1730000000000,
  "deadline": null,
  "selectedProvider": "1inch",
  "fromToken": {
    "address": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
    "symbol": "USDC",
    "decimals": 6
  },
  "toToken": {
    "address": "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2",
    "symbol": "WETH",
    "decimals": 18
  },
  "amountOut": "123456789",
  "minAmountOut": "122839505",
  "gasUsd": "2.13",
  "feeUsd": "0.00",
  "priceImpactBps": 12,
  "route": []
}
```

说明：`deadline` 在 provider 支持时为 unix seconds 整数，否则为 `null`。

注意：

```text
amountIn / amountOut 都按 token 原始精度传输。
前端展示时再根据 decimals 格式化。
```

### 4.2 检查授权

```http
POST /swap/allowance
```

请求：

```json
{
  "quoteId": "uuid",
  "walletAddress": "0x..."
}
```

说明：

```text
tokenAddress、spender、amount 应该从 quoteId 对应的报价里取。
前端不应该自己传 spender，避免授权检查和真实交易使用不同 spender。
```

检查：

```text
用户是否已经授权足够 fromToken 给对应 spender。
注意：若 fromToken 为 native token（ETH / BNB，地址为
0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE），
直接返回 allowanceEnough: true，无需查链。
```

返回：

```json
{
  "allowanceEnough": false,
  "spender": "0x...",
  "requiredAmount": "100000000",
  "currentAllowance": "0"
}
```

### 4.3 构造授权交易

```http
POST /swap/approve-tx
```

请求：

```json
{
  "quoteId": "uuid",
  "walletAddress": "0x..."
}
```

说明：

```text
spender、tokenAddress、amount 应该从 quoteId 对应的报价里取。
前端不应该自己传 spender，避免被篡改成恶意授权地址。
```

返回：

```json
{
  "gasType": "eip1559",
  "chainId": 1,
  "to": "0xToken",
  "data": "0x...",
  "value": "0",
  "gasLimit": "60000",
  "maxFeePerGas": "15000000000",
  "maxPriorityFeePerGas": "1000000000"
}
```

BSC 等 legacy gas 链返回：

```json
{
  "gasType": "legacy",
  "chainId": 56,
  "to": "0xToken",
  "data": "0x...",
  "value": "0",
  "gasLimit": "60000",
  "gasPrice": "5000000000"
}
```

说明：

```text
Phase 1 先支持标准 approve。
Permit2 可以预留，但不要强制所有 provider 都走 Permit2。

gasType 只作为后端返回的元信息，前端据此判断用哪套 gas 字段。
不要把 gasType 传给钱包 RPC，钱包不认识这个字段。

不同链返回不同 gas 字段（gas 类型从 chain config 读取，不要硬编码）：
  EIP-1559 链（如 Ethereum / Base / opBNB）：maxFeePerGas + maxPriorityFeePerGas
  legacy gas 链（如 BSC mainnet）：gasPrice
```

### 4.4 构造 Swap 交易

```http
POST /swap/execute
```

请求：

```json
{
  "quoteId": "uuid",
  "walletAddress": "0x...",
  "walletType": "external"
}
```

`walletType` 用来区分签名和广播方式：

```text
external = 外部钱包，如 MetaMask / WalletConnect，用户钱包自己签名和广播
custody  = 托管钱包，平台签名并广播
mpc      = MPC 钱包，平台通过 MPC 流程签名并广播
```

服务端要做：

```text
1. 检查 quote 是否过期
2. 重新校验 provider quote 是否仍可执行
3. 检查 allowance
4. 检查基础风控
5. 构造 swap transaction (calldata)
6. eth_call simulation  (Week 3 增强版启用，Week 2 跳过；此时 allowance 已确认充足，模拟不会因授权失败误判)
7. 创建 swap_order
```

幂等规则：

```text
quoteId 在 swap_orders 表上必须有唯一约束，但不是简单地返回旧记录。
重复调用时，根据已有订单状态决定响应：

  SIGNING：订单已创建但未签名
    → 重新构造 gas（旧 gas 可能过期），返回新的交易体
    → 不重复创建 swap_order，复用 orderId

  BROADCASTING / TX_HASH_RECEIVED / TX_PENDING 及之后：
    → 返回错误，提示订单已在处理中，不允许重新提交

  SIGNING_TIMEOUT / AWAITING_TX_HASH_TIMEOUT / TX_FAILED / BROADCAST_FAILED：
    → 这些是可恢复终态，但必须重新 quote，不能复用旧 quoteId

  COMPLETED：
    → 返回错误，该 quote 已完成，须重新 quote
```

重新校验 provider quote 时：

```text
1. amountOut 校验：
   新 amountOut 相比原 quote 下跌超过 slippageBps → 拒绝，要求重新 quote。
   不能静默改用更差的新价格。

2. minAmountOut 保护：
   构造 swap tx 时，minAmountOut 必须使用用户确认报价里的值。
   不能用重新校验后 provider 返回的新 minAmountOut。
   防止用户的价格保护线被悄悄放宽。

3. 关键字段一致性校验（全部必须和原 quote 一致）：
   - chainId
   - fromToken / toToken
   - amountIn
   - taker（walletAddress）
   - spender（必须在白名单里）
   - transaction.to（最终路由合约地址，必须在白名单里）
   只看 amountOut 不够，上述任一字段偏离都要拒绝。
```

返回：

```json
{
  "orderId": "uuid",
  "gasType": "eip1559",
  "transaction": {
    "chainId": 1,
    "to": "0xRouter",
    "data": "0x...",
    "value": "0",
    "gasLimit": "185000",
    "maxFeePerGas": "15000000000",
    "maxPriorityFeePerGas": "1000000000"
  }
}
```

BSC 等 legacy gas 链返回：

```json
{
  "orderId": "uuid",
  "gasType": "legacy",
  "transaction": {
    "chainId": 56,
    "to": "0xRouter",
    "data": "0x...",
    "value": "0",
    "gasLimit": "185000",
    "gasPrice": "5000000000"
  }
}
```

说明：

```text
gasType 放在 transaction 对象外层，前端据此判断用哪套 gas 字段。
不要把 gasType 混入传给钱包 RPC 的交易对象，钱包不认识这个字段。

gasLimit 是后端内部字段名。前端传给钱包 RPC（eth_sendTransaction）时，
字段名通常是 gas（不是 gasLimit），需要在前端做一次转换：
  gas = gasLimit

外部钱包需要 gas 和 gas fee 字段来预填交易参数和展示费用。
这些字段由后端构造交易时通过 eth_estimateGas 和当前 gas price 计算得到。

不同链返回不同 gas 字段（链的 gas 能力从 chain config 读取，不要硬编码）：
  EIP-1559 链（如 Ethereum / Base / opBNB）：maxFeePerGas + maxPriorityFeePerGas
  legacy gas 链（如 BSC mainnet）：gasPrice
  注意：BNB 生态中 BSC mainnet 是 legacy gas，opBNB 是 EIP-1559，需要分别配置。
```

如果是托管钱包 / MPC，`/swap/execute` 可以直接触发签名和广播。
订单创建后直接进入 BROADCASTING，广播成功后进入 TX_PENDING，不走 /swap/submit-hash 流程。

如果是外部钱包，后端只返回交易体，由用户钱包签名，订单进入 SIGNING 等待。

### 4.5 外部钱包提交 txHash

外部钱包场景下，用户钱包签名并广播交易后，后端不会自动知道 `txHash`。

所以前端必须把 `txHash` 提交给后端：

```http
POST /swap/submit-hash
```

请求：

```json
{
  "orderId": "uuid",
  "txHash": "0x..."
}
```

服务端收到后：

```text
1. 校验 orderId 是否属于当前用户
2. 校验 wallet_type = 'external'（custody/MPC 不应调用此接口，应走内部广播流程）
3. 校验订单是否处于 SIGNING 或 BROADCASTING 状态
   （外部钱包模式下，/swap/execute 返回交易体后订单处于 SIGNING；
    用户在钱包里签名广播，后端未必有机会先切换到 BROADCASTING，
    所以两个状态都允许提交 txHash）
4. 校验 txHash 唯一性：同一 chainId + txHash 不能绑定多个订单
5. 记录 txHash，更新订单状态为 TX_HASH_RECEIVED，更新 swap_transactions.tx_hash
6. Monitor 开始轮询 eth_getTransaction：
   - 查到 tx → 更新为 TX_PENDING，继续监听确认数
   - 查不到 tx → 正常，RPC/mempool 传播有延迟，继续等待
   - 等待超过 60s 仍查不到 → 标记为 SUSPICIOUS，进入人工处理
7. Monitor 通过 eth_getTransaction 异步校验 from / to / data 是否和订单匹配
8. 如果字段不匹配，标记为 SUSPICIOUS，进入人工检查
```

如果用户签名后没有提交 `txHash`，订单会停留在等待广播结果的状态。

前端需要在钱包返回 hash 后立即调用这个接口。

### 4.6 查询订单状态

```http
GET /swap/status/{orderId}
```

返回：

```json
{
  "orderId": "uuid",
  "status": "TX_CONFIRMED",
  "txHash": "0x...",
  "actualOut": "123456789",
  "errorMessage": null,
  "nextAction": "wait",
  "retryable": false,
  "expiresAt": null,
  "events": []
}
```

`nextAction` 可能的值：

```text
wait             - 等待，无需操作（如 TX_PENDING 中）
submit_hash      - 外部钱包广播后需提交 txHash
retry_quote      - quote 已过期或订单异常，需重新报价
manual_review    - 人工处理中，等待客服
null             - 终态（COMPLETED / TX_FAILED）
```

说明：

```text
approve 阶段（APPROVAL_REQUIRED / APPROVAL_PENDING / APPROVAL_FAILED）
发生在订单创建之前，不在 /swap/status 覆盖范围内。
retry_approve 不在此接口返回，由前端根据 quote session 状态自行判断。
```

`retryable` 说明：

```text
retryable: true  时，前端可以展示"重试"按钮引导用户操作。
retryable: false 时，隐藏重试按钮，仅展示状态和 errorMessage。
```

`expiresAt` 说明：

```text
当 nextAction 为 retry_quote 时，返回当前 quote 的过期时间。
前端可用于展示倒计时，提示用户 quote 还剩多久可用。
其他状态下为 null。
```

## 五、Provider Adapter 设计

Phase 1 先接：

```text
1inch
0x
```

不要把 provider 原始响应直接传给主流程。

每个 Adapter 都输出统一结构：

```typescript
interface QuoteInput {
  chainId: number
  fromToken: string       // token 合约地址
  toToken: string         // token 合约地址
  amountIn: string        // 原始精度整数
  slippageBps: number     // 滑点，单位 bps（50 = 0.5%）
  walletAddress: string   // taker 地址
}

interface QuoteProvider {
  name: string
  supportedChains(): number[]

  // 获取报价，保存原始响应供后续构造交易用
  getQuote(input: QuoteInput): Promise<NormalizedQuote>

  // 获取 approve 目标地址（不同 provider / 模式可能不同）
  // 0x 区分 AllowanceHolder 和 Permit2 两种 spender
  // 1inch 有独立的 approve transaction 接口
  getApprovalTarget(quote: NormalizedQuote): Promise<string>

  // 构造 approve tx（由 provider API 生成，不要本地手写 calldata）
  buildApproveTx(quote: NormalizedQuote, taker: string): Promise<InternalEvmTxEnvelope>

  // 构造 swap tx（策略见下方说明）
  buildSwapTx(quote: NormalizedQuote, taker: string): Promise<InternalEvmTxEnvelope>
}
```

说明：

```text
不要把 buildTx 合并成一个方法，approve 和 swap 是两个独立的链上操作。

0x API v2：
  返回的 transaction 字段直接是可签名的 unsigned tx。
  spender 在 issues.allowance.spender 或 allowanceTarget，
  区分 AllowanceHolder 和 Permit2 两种模式。

1inch API v6：
  approve transaction 有独立接口：GET /approve/transaction
  swap tx 在 /swap 返回的 tx 字段里。

所以 getApprovalTarget 和 buildApproveTx 都要走 provider API，
不要在本地自己拼 approve(spender, amount) 的 calldata，
因为 spender 地址必须来自 provider 当前 response，不能硬编码。

buildSwapTx 的构造策略：
  情况 A - quote 未过期（rawQuote 仍可用）：
    直接使用 rawQuote 里 provider 返回的 transaction 字段（calldata 是 ABI 编码的，
    不能随意改内部字段）。
    必须验证 provider quote 中的 minAmountOut >= 用户原始确认的 minAmountOut。
    不满足则拒绝执行，要求用户重新 quote。

  情况 B - 需要重新向 provider 请求（quote 临近过期、provider 要求刷新）：
    重新请求后，同样验证新 quote 的 minAmountOut >= 用户原始确认值。
    验证通过后使用新 provider 返回的 transaction（不自行修改 calldata）。
    不满足则拒绝执行，要求用户重新 quote。

  两种情况下，spender 和 transaction.to 都必须通过白名单校验。
  不要自行 ABI 编码或修改 provider 返回的 calldata。
```

统一报价结构：

```typescript
interface TokenInfo {
  address: string
  symbol: string
  decimals: number
}

interface RouteStep {
  protocol: string      // 'uniswap_v3' / '1inch' / 'pancake' ...
  fromToken: TokenInfo
  toToken: TokenInfo
  amountIn: string
  amountOut: string
  poolAddress?: string
}

interface NormalizedQuote {
  provider: string
  chainId: number
  fromToken: TokenInfo
  toToken: TokenInfo
  amountIn: string
  amountOut: string
  minAmountOut: string
  gasUsd: string
  feeUsd: string
  priceImpactBps: number
  spender: string
  route: RouteStep[]
  rawQuote: unknown
  expiresAt: number       // 服务端判断报价过期用，unix ms
  deadline: number | null  // 写入 swap calldata 的链上截止时间，unix seconds
                           // 仅当 provider 支持 deadline 参数时有值，否则为 null
}
```

`expiresAt` 和 `deadline` 不是同一个东西：

```text
expiresAt = 业务有效期，服务端和前端判断 quote 是否还能用
deadline  = 链上兜底保护，写进 swap calldata 防止过期交易被执行

重要规则：
  expiresAt 过期后，即使链上 deadline 还没到，也不能继续 execute。
  deadline 只是最后一道防线，不是业务有效期的替代。
  不能用 deadline 还没过来绕过 expiresAt 的限制。
```

通常可以设置：

```text
expiresAt = now + 15s ~ 30s  （业务有效期，短）
deadline  = now + 20min       （链上兜底，长；仅 provider 支持时写入）
```

如果 provider 不暴露独立 deadline 参数，则依赖 expiresAt + minAmountOut + provider quote 有效性控制，不强制写 deadline。

交易请求也要区分 gas 模式：

```typescript
type InternalEvmTxEnvelope =
  | {
      gasType: "eip1559"
      chainId: number
      to: string
      data: string
      value: string
      gasLimit: string
      maxFeePerGas: string
      maxPriorityFeePerGas: string
    }
  | {
      gasType: "legacy"
      chainId: number
      to: string
      data: string
      value: string
      gasLimit: string
      gasPrice: string
    }
```

`InternalEvmTxEnvelope` 是后端内部结构，不是直接传给钱包 RPC 的交易对象。前端调用 `eth_sendTransaction` 时需去掉 `gasType`，只传钱包认识的字段。

### Provider 超时和降级策略

Provider 并发报价时，不能让某一个 Provider 拖慢整个 quote。

建议策略：

```text
单个 Provider 超时阈值：800ms - 1500ms

如果某个 Provider 超时：
  1. 不阻塞其他 Provider
  2. 记录 timeout 到 provider_quotes.error_message
  3. 继续使用其他 Provider 的报价

如果所有 Provider 都失败：
  1. 返回 QUOTE_FAILED
  2. 前端提示用户稍后重试
  3. 后台记录告警
```

Provider 质量也要进入监控：

```text
成功率
平均延迟
p95 延迟
超时次数
报价失败次数
成交失败次数
```

后续可以根据这些指标动态调整 Provider 权重。

## 六、报价排序规则

不要直接用：

```text
amountOut - gas
```

因为它们不是同一个单位。

推荐统一折算成 USD：

```text
netOutUsd =
  amountOutUsd
  - gasUsd
  - providerFeeUsd
  - platformFeeUsd
```

这里有一个前提：

```text
amountOutUsd 和 gasUsd 都需要可靠的 USD 价格来源。
```

价格来源建议：

```text
1. 优先使用 Provider quote 返回的价格或 USD 字段
2. 如果 Provider 没返回，再查内部 Price Service
3. Price Service 可以接 CoinGecko / CoinMarketCap / 自有行情源
4. 价格需要本地缓存，TTL 建议 15s - 30s
5. 如果目标 token 没有可靠价格：
   - 不参与 USD 净值排序
   - 只能按 amountOut 排序
   - 前端提示"价格数据不足，排序仅供参考"
   - 稳定币对之间可以直接按 amountOut 比较（decimals 需归一化）
   - 无价格的长尾币建议强制展示 price impact 警告
```

不要在每次 quote 时都实时请求外部价格 API。

否则会引入：

```text
额外延迟
外部 API 限流
报价不稳定
```

排序时还要考虑：

```text
provider 成功率
接口延迟
交易失败率
是否需要额外 approve
price impact
```

Phase 1 可以先用简化评分：

```text
score = netOutUsd - failurePenaltyUsd - latencyPenaltyUsd
```

惩罚项也必须折算成 USD，不能和美元净值混用单位：

```text
failurePenaltyUsd =
  providerFailureRate * amountOutUsd * failurePenaltyFactor

latencyPenaltyUsd =
  (latencyMs / 1000) * latencyPenaltyUsdPerSecond
```

示例：

```text
failurePenaltyFactor = 0.5
latencyPenaltyUsdPerSecond = 0.01
```

## 七、基础风控

Phase 1 的风控分两层。

基础版必须先做，因为 `/swap/execute` 依赖它：

```text
1. token 黑名单
2. spender 白名单（来自 provider API 返回，不硬编码）
3. chainId 校验
4. minAmountOut 不允许为 0（即链上 minimum received 不得为零）
5. slippage 上限：主流币 3%，长尾币按 token risk tier 单独配置
6. fee-on-transfer 高风险 token 默认 BLOCK
```

（quote 过期检查已在 /swap/execute step 1 处理，不在 risk engine 里重复）

Native token 处理：

```text
ETH / BNB 等 native token 作为 fromToken 时：
  - 不需要 allowance 检查，跳过 approve 流程
  - 交易的 value 字段 = amountIn（不为 0）
  - 0x 用占位地址 0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE 表示 native token
  - 1inch 同样使用该地址约定
  /swap/allowance 接口收到 native token 时应直接返回 allowanceEnough: true
```

增强版可以在第 3 周补：

```text
1. 交易前 eth_call 模拟
2. price impact 超 5% 前端强提示
3. price impact 超 15% 拒绝
4. 单笔 / 日累计额度检查
```

风控结果不要只有通过/失败。

建议：

```text
PASS
WARN
BLOCK
MANUAL_REVIEW
```

## 八、状态机

Phase 1 的状态分两层：**Quote Session 状态**（前端本地 / 对 quoteId 追踪）和**订单状态**（数据库 swap_order，从 /swap/execute 创建后开始）。

### 8.1 Quote Session 状态（前端本地，无 orderId）

```text
QUOTING ──────────────────────► QUOTE_FAILED
  |                              QUOTE_EXPIRED
  v
QUOTED
  |
  |-- allowance 充足 ──────────────► 直接进入 /swap/execute
  |
  |-- allowance 不足
        |
        v
    APPROVAL_REQUIRED
        |
        v
    APPROVAL_PENDING
        |
        |-- APPROVAL_FAILED   (用户拒绝 / gas 不足 / nonce 冲突 / 上链失败)
        |     |
        |     v
        |   quote 未过期 → 重新 APPROVAL_REQUIRED
        |   quote 已过期 → 重新 QUOTING
        |
        v
    APPROVAL_CONFIRMED
        |
        v
    检查 quote 是否过期 ──── 过期 → 重新 QUOTING
        |
        v
    /swap/execute
```

这几个状态不在 swap_order 里，不依赖 orderId，只追踪 quoteId。

### 8.2 订单状态（数据库 swap_order，从 SIGNING 开始）

swap_order 在 /swap/execute step 7 创建，订单从 SIGNING 开始存在于数据库。

```text
SIGNING
  |
  |-- USER_REJECTED
  |-- SIGNING_TIMEOUT          (超过 30 分钟未签名，订单关闭)
  v
BROADCASTING
  |
  |-- BROADCAST_FAILED
  |-- AWAITING_TX_HASH_TIMEOUT (外部钱包超过 10 分钟未提交 txHash，订单关闭)
  v
TX_HASH_RECEIVED               (外部钱包已提交 txHash，等待 RPC 传播)
  |
  v
TX_PENDING                     (链上已可查到该 tx)
  |
  |-- SUSPICIOUS
  |     txHash 内容和订单不匹配
  |       |
  |       v
  |   MANUAL_REVIEW
  |
  |-- TX_FAILED
  v
TX_CONFIRMED
  |
  v
COMPLETED
```

注意：

```text
TX_HASH_RECEIVED：
  外部钱包调 /swap/submit-hash 后立即进入此状态。
  Monitor 轮询 eth_getTransaction，RPC 传播有延迟，查不到不等于异常。
  等待窗口建议 60s，超时后再标 SUSPICIOUS。

SUSPICIOUS 不是终态，确认不匹配后转 MANUAL_REVIEW。

超时关闭的订单，用户须重新 quote。

以下四个状态仅适用于 external 钱包；custody/MPC 模式下不会发生：
  USER_REJECTED         - custody/MPC 是程序签名，不存在用户拒绝
  SIGNING_TIMEOUT       - custody/MPC 立即签名，不会等待超时
  TX_HASH_RECEIVED      - custody/MPC 广播后直接进 TX_PENDING，不经过此状态
  AWAITING_TX_HASH_TIMEOUT - custody/MPC 走内部广播，不经过 /submit-hash
```

## 九、数据库最小表

Phase 1 最少需要：

```text
quote_requests
provider_quotes
swap_quotes
swap_orders
swap_transactions
swap_events
token_risk_cache
```

表之间的生命周期关系：

```text
quote_requests = 用户每次请求报价都会创建，成功和失败都记录
provider_quotes = 每个 Provider 对这次请求给出的原始报价或错误
swap_quotes = 系统从 provider_quotes 中选出的最优报价，只有成功报价才会生成
swap_orders = 用户调用 /swap/execute 后在服务端 step 7 创建（calldata 构造完成之后）
```

关键约束：

```text
swap_orders.quote_id 必须唯一。
同一个 quote 只能生成一个订单，保证 /swap/execute 幂等。
```

并发安全：

```text
/swap/execute 并发场景下，两个请求可能同时通过 quote 检查并构造 calldata，
然后同时尝试 INSERT swap_order，导致唯一冲突。

推荐策略：
  方案 A（推荐）：INSERT ... ON CONFLICT (quote_id) DO NOTHING 后立即查询。
    先尝试插入占位行（状态 SIGNING），冲突则读取已有订单。
    不要先 SELECT 再 INSERT，会有竞态。

  方案 B：在 Redis 对 quote_id 加分布式锁，锁住后再执行 INSERT。

approve tx 状态追踪：
  approve 发生在 swap_order 创建之前，不依赖 order_id。
  Phase 1 选择：approve 只由前端本地追踪 txHash + 轮询链上状态，
  不写入 swap_transactions（swap_transactions 依赖 order_id）。
  如后续需要服务端追踪 approve，可新增 approval_attempts 表（按 quote_id）。
```

swap_orders / swap_transactions 关键字段（实现时必须包含）：

```text
swap_orders:
  wallet_type       varchar   -- 'external' / 'custody' / 'mpc'
  wallet_address    varchar   -- taker 地址
  spender           varchar   -- approve 的目标地址
  transaction_to    varchar   -- swap tx 的路由合约地址
  gas_type          varchar   -- 'eip1559' / 'legacy'

swap_transactions:
  tx_payload_json   jsonb     -- provider 返回的原始 transaction 体
                              -- 用于 /submit-hash 时校验 from/to/data 是否匹配
```

可以先不做：

```text
swap_route_steps
```

因为 Phase 1 只有同链 swap，route_steps 可以先用 `route_json` 存在 quote 里。

但如果你想一开始就方便分析每一跳，也可以保留。

## 十、前端页面最小功能

### Swap 表单

必须有：

```text
选择链
选择 fromToken
选择 toToken
输入 amountIn
设置 slippage
显示预计 amountOut
显示 minimum received
显示 gas fee
显示 price impact
显示 route/provider
```

### 授权提示

要明确告诉用户：

```text
当前需要 approve
approve 给哪个 spender
approve 数量是多少
```

不要只弹一个“请授权”。

### 交易状态

至少显示：

```text
等待签名
广播中
确认中
成功
失败
用户取消
人工处理中（联系客服）
```

轮询建议：

```text
Ethereum: 约 12s 出块，建议 6s 轮询一次
Base / BSC: 约 2s - 3s 出块，建议 2s 轮询一次
TX_CONFIRMED / COMPLETED / TX_FAILED 后停止轮询
```

## 十一、测试计划

### 单元测试

```text
Provider Adapter 响应归一化
报价排序规则
slippage 计算
minAmountOut 计算
风险规则
状态机流转
```

### 集成测试

```text
1inch quote
0x quote
allowance 检查
approve tx 构造
approve tx 追踪（前端本地追踪 txHash + 轮询链上状态，不写 swap_transactions）
swap tx 构造
submit-hash 外部钱包完整流程
Provider 全部超时的降级行为
/swap/execute 幂等（重复提交同一 quoteId 返回同一 orderId）
provider quote 重新校验价格下跌超过 slippageBps 时拒绝执行
minAmountOut 不被重新校验后的新 quote 悄悄降低
EIP-1559 链返回 maxFeePerGas，legacy 链返回 gasPrice
approve 失败后 quote session 进入 APPROVAL_FAILED，不创建 swap_order
native token（ETH/BNB）作为 fromToken 时跳过 approve，value = amountIn
SUSPICIOUS 标记 + 转 MANUAL_REVIEW 流程
SIGNING_TIMEOUT 超时后订单自动关闭
AWAITING_TX_HASH_TIMEOUT 超时后订单自动关闭
eth_call simulation 识别会 revert 的交易并拒绝（Week 3 启用）
订单状态更新
```

### 链上测试

建议使用：

```text
mainnet fork
小额真实链测试
测试网仅用于流程，不适合验证真实流动性
```

因为测试网 DEX 流动性不稳定，不能代表真实 swap 效果。

## 十二、验收标准

### 必须上线（Phase 1 MVP）

- [ ] 支持至少 3 条 EVM 链；
- [ ] 能同时请求 1inch 和 0x 报价；
- [ ] 能把不同 provider 响应归一化；
- [ ] 能选出最优 quote；
- [ ] 能检查 allowance，返回正确的 spender 地址；
- [ ] 能生成 approve tx（spender 来自 provider API，不硬编码）；
- [ ] 能生成 swap tx（calldata 来自 provider 原始响应）；
- [ ] 能按链返回 EIP-1559 或 legacy gas 字段；
- [ ] `NormalizedQuote` 包含 `deadline`（provider 支持时写入 calldata，否则依赖 expiresAt + minAmountOut 控制）；
- [ ] expiresAt 过期后无法 execute，即使链上 deadline 未到；
- [ ] 重新校验时 chainId / fromToken / toToken / amountIn / spender / transaction.to 全部一致才放行；
- [ ] 重新校验时价格下跌超过 slippageBps 能拒绝；
- [ ] 构造 swap tx 时使用原始 quote 的 minAmountOut；
- [ ] 能拒绝 `minAmountOut = 0`（minimum received 不得为零）；
- [ ] 能记录订单状态（从 SIGNING 开始）；
- [ ] 能监听交易成功 / 失败；
- [ ] 能在用户拒签时正确结束订单；
- [ ] 能在广播失败时重试或标记失败；
- [ ] 外部钱包提交 txHash 后订单进入 TX_HASH_RECEIVED，Monitor 等待传播；
- [ ] fee-on-transfer 高风险 token 默认 BLOCK；
- [ ] native token（ETH/BNB）作为 fromToken 时正确跳过 approve，交易 value = amountIn；
- [ ] 同一 quoteId 不创建重复 swap_order（基础幂等）；
- [ ] 能在后台查到每个 provider 的原始报价；

### 增强项（Phase 1 后期 / Phase 2）

- [ ] `txHash` 验证异常（字段不匹配或超时查不到）能标记 SUSPICIOUS 并进入人工处理；
- [ ] custody / MPC 模式下 nonce 分配不会并发冲突；
- [ ] 签名超时 (SIGNING_TIMEOUT) 后订单自动关闭；
- [ ] 外部钱包超时未提交 txHash (AWAITING_TX_HASH_TIMEOUT) 后订单自动关闭；
- [ ] Week 3 启用后，eth_call simulation 能拒绝会 revert 的交易；
- [ ] /swap/execute 幂等进阶：SIGNING 状态复用订单并重建 gas，其他状态细分拒绝；
- [ ] provider 质量评分（成功率 / 延迟）动态影响报价排序；

## 十三、推荐排期

```text
第 1 周:
  Chain / Token 配置
  RPC 节点配置
  1inch Adapter
  0x Adapter
  quote_requests / provider_quotes / swap_quotes
  token_risk_cache

第 2 周:
  Quote Engine
  Route Engine
  Risk Engine 基础版（黑名单 + spender 白名单 + chainId 校验）
  allowance 检查
  approve tx / swap tx 构造
  swap_transactions（swap tx 广播状态追踪需要此表；approve tx 由前端本地追踪，不写此表）

第 3 周:
  订单状态机
  Broadcaster / Monitor
  Risk Engine 增强版（eth_call 模拟 + price impact + 额度检查）
  swap_orders / swap_events

第 4 周:
  前端联调
  mainnet fork 测试
  小额真实链测试
  监控和告警
```

## 十四、Phase 1 最容易踩坑

### quote 过期

用户 approve 花了 1 分钟，原 quote 可能已经过期。

解决：

```text
swap 前重新检查 quote TTL。
过期则重新 quote。
```

前端体验建议：

```text
1. quote 展示倒计时
2. quote 过期后禁用确认按钮
3. 如果输入参数没变，可以自动重新 quote
4. 如果用户刚完成 approve，execute 前后端必须再次校验 quote 是否过期
5. 过期 quote 不能继续执行
```

### token_risk_cache 刷新

Token 风险不是一次检测后永久有效。

建议刷新策略：

```text
普通 token: TTL 24 小时
新上线 token: TTL 1 小时

触发刷新:
  1. token 首次出现
  2. 用户发起 swap 前发现缓存过期
  3. 后台定时批量刷新
  4. 风控或运营手动标记复查
```

### spender 错误

不同 provider 的 spender 不一样。

解决：

```text
spender 必须来自 provider quote 返回结果。
spender 必须经过白名单校验。
```

### decimals 错误

USDC 是 6 位，WETH 是 18 位。

解决：

```text
数据库按原始整数存储。
展示时再格式化。
```

### 外部钱包广播

外部钱包可能自己广播，不经过你的 Broadcaster。

解决：

```text
external_wallet 模式下，平台只监听 txHash。
前端必须在钱包广播后调用 /swap/submit-hash 把 txHash 提交给后端。
custody / mpc 模式下，平台负责广播和 nonce。
```

### custody / MPC nonce 冲突

托管钱包或 MPC 钱包由平台统一广播交易。

多个用户同时发起 swap 时，如果同一个热钱包地址并发发交易，容易出现 nonce 冲突。

解决：

```text
1. Broadcaster 按 signer address 维度集中管理 nonce
2. nonce 分配要用单线程队列或分布式锁
3. 本地维护 pending nonce，不要每次都只查链上 nonce
4. 交易卡死时触发 RBF（Replace-By-Fee）提高手续费替换
5. 定期 reconcile 链上 nonce 和本地 pending 队列
```

### gas 价格过时

`/swap/approve-tx` 和 `/swap/execute` 返回的 gas 字段都只是构造时的快照。

如果用户等了几分钟才签名，链上 gas price 可能已经变化。

结果可能是：

```text
gas 太低，交易长时间 pending
gas 太高，用户过度支付手续费
```

解决：

```text
external_wallet 模式:
  approve-tx 和 swap tx 都可以让 MetaMask / 钱包自己重新估算 gas。
  后端返回的 gas 字段只作为建议值，钱包可以覆盖。

custody / mpc 模式:
  Broadcaster 在真正广播前重新获取当前 gas price。
  不使用 /swap/approve-tx 或 /swap/execute 里的旧值。
```

### fee-on-transfer token

有些 token 转账会扣税，quote 和实际到账可能不一致。

解决：

```text
Phase 1 可以先 BLOCK 高风险 fee-on-transfer token。
后续再做特殊支持。
```

## 十五、Phase 1 结束后的下一步

Phase 1 完成后，再进入：

```text
Phase 2: 风控 + 监控 + 失败恢复增强
Phase 3: 跨链聚合
Phase 4: Solana / 非 EVM
```

不要在 Phase 1 就做跨链。

先把同链 swap 做稳。

## 参考

- 0x Swap API：<https://docs.0x.org/docs/0x-swap-api/guides/swap-tokens-with-0x-swap-api>
- 0x API Reference：<https://docs.0x.org/api-reference/api-overview>
- 1inch Swap API：<https://business.1inch.com/portal/documentation/apis/swap/classic-swap/methods/v6.1/1/swap/method/get>
- Uniswap Permit2：<https://support.uniswap.org/hc/en-us/articles/39683402190733>
