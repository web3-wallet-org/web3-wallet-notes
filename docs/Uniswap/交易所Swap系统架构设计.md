# 交易所多链 Swap 系统架构设计

> **核心定位**: 交易所 Swap 功能不是自建 AMM，而是**聚合**链上现有 DEX 和跨链桥的最优路径，为用户提供最优价格、最低手续费的兑换体验。

---

## 一、整体架构分层

```
┌─────────────────────────────────────────────────────┐
│                    API Gateway                      │  -- 对外接口，鉴权 / 限流 / 路由
│         /swap/quote  /swap/execute  /swap/status    │
└──────────────────────────┬──────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────┐
│                  Swap Orchestrator                  │  -- 主流程协调，串联下方所有模块
└──────────────────────────┬──────────────────────────┘
                           │
             ┌─────────────▼─────────────┐
             │        Quote Engine       │  -- 并发拉取各 Provider 报价，按净收益排序
             └─────────────┬─────────────┘
                           │
             ┌─────────────▼─────────────┐
             │        Route Engine       │  -- 决策同链 / 跨链 / 多段组合路径
             └─────────────┬─────────────┘
                           │
             ┌─────────────▼─────────────┐
             │    Risk & Simulation      │  -- 黑名单 / 制裁 / honeypot / 交易模拟
             └─────────────┬─────────────┘
                           │
             ┌─────────────▼─────────────┐
             │    Transaction Builder    │  -- 只构造 calldata，不接触私钥
             └─────────────┬─────────────┘
                           │
             ┌─────────────▼─────────────┐
             │   Signer / Wallet / MPC   │  -- 托管钱包 / MPC / 用户外部钱包
             └─────────────┬─────────────┘
                           │
             ┌─────────────▼─────────────┐
             │        Broadcaster        │  -- 广播上链，集中管理 nonce
             └─────────────┬─────────────┘
                           │
             ┌─────────────▼─────────────┐
             │     Order State Machine   │  -- 持久化订单状态，支持断点恢复
             └─────────────┬─────────────┘
                           │
             ┌─────────────▼─────────────┐
             │  Monitor / Retry / Refund │  -- 确认上链 / 超时重试 / 跨链退款
             └───────────────────────────┘
```

---

## 二、Provider Adapter 层

主系统**只认统一格式**，避免每接一个聚合器或链都污染主逻辑。

```
Provider Adapter Layer
  ├── EVM DEX Adapters
  │     ├── 1inch Adapter
  │     ├── 0x Adapter
  │     ├── ParaSwap Adapter
  │     └── Uniswap V3 Adapter
  ├── Cross-Chain Adapters
  │     ├── LI.FI Adapter       (跨链聚合器，非只是状态追踪)
  │     ├── Socket Adapter      (跨链聚合器)
  │     ├── CCTP Adapter        (USDC 官方跨链)
  │     └── Stargate Adapter    (通用流动性桥)
  └── Non-EVM Adapters
        └── Jupiter Adapter     (Solana，单独走，不混入 EVM 流程)
```

每个 Adapter 输出统一接口：

```typescript
interface NormalizedQuote {
  provider: string        // "1inch" | "0x" | "lifi" ...
  fromToken: TokenInfo
  toToken: TokenInfo
  amountIn: bigint
  amountOut: bigint       // 预估输出
  priceImpact: number     // 价格影响 %
  gasEstimate: bigint
  feesUsd: number
  route: RouteStep[]      // 每一跳的路径
  expiresAt: number       // 报价有效期 (unix ms)
  rawQuote: unknown       // 原始响应，供调试
}

interface NormalizedRoute {
  steps: RouteStep[]      // 每一步：swap / bridge / approve
  estimatedTime: number   // 预计完成时间 (秒)
  totalFeesUsd: number
}

interface TransactionRequest {
  chainId: number
  to: string
  data: string
  value: bigint
  gasLimit: bigint
  maxFeePerGas: bigint
  maxPriorityFeePerGas: bigint
}
```

---

## 三、核心模块设计

### 3.1 Quote Engine

```
输入: fromToken, toToken, amount, fromChain, toChain

同链: 并发请求多个 DEX Adapter，按 (amountOut - gasCost) 排序取最优
      - 1inch API v6
      - 0x API
      - Paraswap
      - Uniswap V3 Quoter

跨链: 拆两段路径并发比较
      路径 A: src 链 swap → 桥接稳定币 → dst 链 swap
      路径 B: 桥接原生资产 → dst 链 swap
      路径 C: 跨链聚合器 (LI.FI / Socket) 一站式报价

比较维度: amountOut / totalFees / estimatedTime
```

### 3.2 Route Engine

| 场景 | 路由策略 |
|------|---------|
| fromChain == toChain, 同 Token | 直接转账，不走 Swap |
| fromChain == toChain, 不同 Token | DEX Adapter 聚合 |
| 跨链 USDC | CCTP 优先（零滑点） |
| 跨链 USDT / 通用稳定币 | Stargate |
| 跨链不同 Token (EVM) | LI.FI / Socket 聚合路由 |
| Solana | Jupiter Adapter，独立流程 |
| 目标链无 gas | 桥接时附带 gas drop |

### 3.3 Risk & Simulation Engine

风控是主链路的一环，**不是外挂**：

```
检查项 (按顺序，任一失败即拒绝):

1. Token 黑名单           - 内部维护 + 第三方情报
2. 合约地址校验           - 非 EOA，且在白名单/已验证合约列表
3. Spender 校验           - approve 对象必须是已知聚合器合约
4. 制裁地址检查           - OFAC / Chainalysis screener
5. Honeypot 检测          - 检查 sell tax / 不可转移 token
6. Fee-on-transfer 检查   - 检测转账扣税，更新实际 amountOut
7. 交易模拟               - eth_call / tenderly simulate，验证不 revert
8. Price Impact 检查      - 超 5% 警告，超 15% 拒绝
9. 滑点上限               - 用户设置最大 3%，默认 0.5%
10. 额度检查              - 单笔 / 日累计限额
```

### 3.4 签名边界（重要）

交易所平台同时存在多种钱包类型，签名边界必须清晰：

```
Transaction Builder  →  只构造 calldata，不触碰私钥
         │
         ▼
  Signer Interface  (统一抽象层)
    ├── 托管钱包 Signer    (内部 KMS / HSM)
    ├── MPC Signer        (多方计算，无单点私钥)
    └── 外部钱包 Signer   (WalletConnect / Metamask，用户自签)
         │
         ▼
    Broadcaster           →  只负责广播 + nonce 管理
         │
         ▼
    Monitor               →  只负责确认 + 重试 + 退款
```

### 3.5 订单状态机

```
CREATED
  │
  ▼
QUOTING ──────────────────────────► QUOTE_FAILED
  │                                  QUOTE_EXPIRED  (超有效期未确认)
  ▼
APPROVAL_REQUIRED ─► APPROVAL_PENDING ─► APPROVAL_CONFIRMED
  │                                              │
  └──────────────────────────────────────────────┘
                                                 │
                                                 ▼
                                             SIGNING
                                                 │
                                    ┌────────────▼────────────┐
                                    │                         │
                              [同链路径]                 [跨链路径]
                                    │                         │
                                    ▼                         ▼
                              TX_PENDING              SRC_PENDING
                                    │                         │
                              TX_CONFIRMED            SRC_CONFIRMED
                                    │                         │
                              COMPLETED           BRIDGE_PENDING
                                                       │
                                                  DST_PENDING
                                                       │
                                          ┌────────────┴────────────┐
                                          │                         │
                                   DST_CONFIRMED            DST_SWAP_FAILED
                                          │                  (桥成功但目标链
                                    COMPLETED                swap 失败，
                                                             用户收到中间 token)
                                                                     │
                                                             PARTIAL_COMPLETED

失败路径:
  BROADCAST_FAILED   → 广播失败，可重试
  BRIDGE_FAILED      → 桥超时或失败，触发退款
  REFUNDED           → 已退款到源链
  MANUAL_REVIEW      → 大额 / 异常，人工介入
```

---

## 四、数据库表设计

```sql
-- 一次报价请求的主记录。
-- 用户打开 swap 页面、输入 token 和数量后，会先产生 quote_request。
-- 此时用户还没有确认交易，所以不应该直接生成 swap_order。
quote_requests (
  id              uuid PRIMARY KEY,  -- 报价请求 ID，贯穿本次 quote 流程
  user_id         bigint,            -- 用户 ID；游客或外部钱包场景可为空
  from_chain      int,               -- 源链 chainId
  to_chain        int,               -- 目标链 chainId；同链 swap 时等于 from_chain
  from_token      varchar,           -- 用户支付的 token 地址；native token 可用约定地址表示
  to_token        varchar,           -- 用户想收到的 token 地址
  amount_in       numeric,           -- 用户输入数量，按 token 原始精度存储
  slippage_bps    int,               -- 用户设置的滑点，单位 bps；50 = 0.5%
  status          varchar,           -- 报价请求状态：quoting / quoted / failed / expired
  created_at      timestamp,         -- 创建时间
  expires_at      timestamp          -- 本次报价请求整体过期时间
)

-- 各 Provider 原始报价（用于对比分析）
provider_quotes (
  id                uuid PRIMARY KEY,  -- Provider 报价 ID
  quote_request_id  uuid,              -- 关联 quote_requests.id，表示属于哪次报价请求
  provider          varchar,           -- 报价来源：1inch / 0x / lifi / socket / jupiter 等
  provider_quote_id varchar,           -- Provider 自己返回的 quoteId / routeId，便于后续执行或排查
  amount_out        numeric,           -- 该 Provider 预估输出数量，按目标 token 原始精度存储
  min_amount_out    numeric,           -- 按滑点计算后的最小可接受输出
  fees_usd          numeric,           -- 预估总费用，包含 gas / bridge fee / provider fee
  gas_estimate      numeric,           -- 预估 gas 数量；非 EVM 链可存等价计算结果
  gas_usd           numeric,           -- 预估 gas 折算 USD，方便不同链报价比较
  price_impact_bps  int,               -- 价格影响，单位 bps
  estimated_time    int,               -- 预计完成时间，单位秒；跨链场景尤其重要
  risk_level        varchar,           -- 风险等级：safe / warning / blocked / manual_review
  route_json        jsonb,             -- Provider 原始响应，完整保留用于排查
  latency_ms        int,               -- Provider 接口响应耗时，监控 Provider 质量
  error_message     text,              -- Provider 报价失败时的错误信息
  created_at        timestamp          -- 创建时间
)

-- 报价（用户看到的最优报价）
swap_quotes (
  id                    uuid PRIMARY KEY,  -- 系统选出的最终报价 ID
  quote_request_id      uuid,              -- 关联 quote_requests.id
  selected_provider     varchar,           -- 最终选择的 Provider
  selected_provider_quote_id uuid,         -- 关联 provider_quotes.id，表示最终采用哪条报价
  from_chain            int,               -- 源链 chainId，冗余存储便于查询
  to_chain              int,               -- 目标链 chainId，冗余存储便于查询
  from_token            varchar,           -- 源 token 地址
  to_token              varchar,           -- 目标 token 地址
  amount_in             numeric,           -- 用户支付数量，按源 token 原始精度存储
  amount_out            numeric,           -- 预估输出数量，按目标 token 原始精度存储
  min_amount_out        numeric,           -- 最小可接受输出，交易执行时用于滑点保护
  fee_usd               numeric,           -- 预估总费用 USD
  gas_usd               numeric,           -- 预估 gas USD
  platform_fee_usd      numeric,           -- 平台服务费 USD，没有则为 0
  net_amount_out_usd    numeric,           -- 扣除 gas / bridge / 平台费后的净输出价值
  route_json            jsonb,             -- 归一化后的最终路由，用于交易构造
  risk_result_json      jsonb,             -- 风控结果快照，避免执行时无法复盘
  expires_at            timestamp,         -- 报价过期时间；过期后必须重新 quote
  created_at            timestamp          -- 创建时间
)

-- 订单主表
swap_orders (
  id              uuid PRIMARY KEY,  -- Swap 订单 ID，用户确认报价后生成
  user_id         bigint,            -- 用户 ID
  quote_id        uuid,              -- 用户确认使用的 swap_quotes.id
  status          varchar,           -- 订单状态，见状态机
  from_chain      int,               -- 源链 chainId，冗余存储便于查询
  to_chain        int,               -- 目标链 chainId
  from_token      varchar,           -- 源 token 地址
  to_token        varchar,           -- 目标 token 地址
  amount_in       numeric,           -- 用户实际支付数量
  expected_out    numeric,           -- 下单时预估输出
  min_amount_out  numeric,           -- 最小可接受输出
  actual_out      numeric,           -- 实际到账数量，完成后更新
  failure_reason  text,              -- 失败原因，给客服和排查使用
  created_at      timestamp,         -- 创建时间
  updated_at      timestamp          -- 最近更新时间
)

-- 每一步交易（一个订单可能有多步：approve + swap + bridge + swap）
swap_transactions (
  id              uuid PRIMARY KEY,  -- 交易步骤 ID
  order_id        uuid,              -- 关联 swap_orders.id
  step_index      int,               -- 第几步，从 0 或 1 开始统一约定
  step_type       varchar,           -- 步骤类型：approve / permit / swap / bridge / claim / refund
  chain_id        int,               -- 该步骤发生在哪条链
  signer_type     varchar,           -- 签名方式：custody / mpc / external_wallet
  tx_to           varchar,           -- 交易发送目标合约地址
  tx_value        numeric,           -- native token value
  tx_data         text,              -- calldata 或非 EVM 交易序列化数据
  tx_hash         varchar,           -- 链上交易 hash；未广播或用户取消时为空
  nonce           numeric,           -- EVM nonce；外部钱包或非 EVM 场景可为空
  status          varchar,           -- 步骤状态：pending / confirmed / failed / cancelled
  gas_limit       numeric,           -- 预估或设置的 gas limit
  gas_used        numeric,           -- 实际消耗 gas，确认后更新
  block_number    numeric,           -- 确认所在区块高度
  error_message   text,              -- 失败或 revert 原因
  created_at      timestamp,         -- 创建时间
  updated_at      timestamp          -- 最近更新时间
)

-- 路由步骤明细（每一跳的详情）
swap_route_steps (
  id              uuid PRIMARY KEY,  -- 路由步骤 ID
  order_id        uuid,              -- 关联 swap_orders.id
  step_index      int,               -- 第几段路由
  action          varchar,           -- 动作类型：swap / bridge / transfer / gas_drop
  protocol        varchar,           -- 协议名：uniswap_v3 / stargate / cctp / 1inch / jupiter
  from_chain      int,               -- 该步骤源链
  to_chain        int,               -- 该步骤目标链；同链 swap 时等于 from_chain
  from_token      varchar,           -- 该步骤输入 token
  to_token        varchar,           -- 该步骤输出 token
  amount_in       numeric,           -- 该步骤输入数量
  expected_out    numeric,           -- 该步骤预估输出
  actual_out      numeric,           -- 该步骤实际输出，完成后更新
  bridge_msg_id   varchar,           -- 跨链消息 ID；非跨链步骤为空
  status          varchar,           -- 路由步骤状态：pending / completed / failed / partial
  created_at      timestamp,         -- 创建时间
  updated_at      timestamp          -- 最近更新时间
)

-- Token 风控缓存（避免重复检测）
token_risk_cache (
  chain_id        int,               -- token 所在链 chainId
  token_addr      varchar,           -- token 合约地址
  symbol          varchar,           -- token symbol，仅展示和排查用，不作为唯一标识
  decimals        int,               -- token decimals，报价和展示都依赖它
  is_honeypot     boolean,           -- 是否疑似只能买不能卖
  buy_tax_bps     int,               -- 买入税率，单位 bps
  sell_tax_bps    int,               -- 卖出税率，单位 bps
  is_blacklist    boolean,           -- 是否在内部或第三方黑名单
  is_verified     boolean,           -- 是否为官方/已验证 token
  risk_level      varchar,           -- 风险等级：safe / warning / blocked / manual_review
  risk_source     varchar,           -- 风险数据来源：internal / goplus / chainalysis 等
  raw_result      jsonb,             -- 第三方原始检测结果，便于复盘
  checked_at      timestamp,         -- 最近检测时间
  PRIMARY KEY (chain_id, token_addr)
)

-- 状态流转日志
swap_events (
  id              bigserial PRIMARY KEY, -- 自增日志 ID
  order_id        uuid,                  -- 关联 swap_orders.id
  event           varchar,               -- 事件名：QUOTE_CREATED / TX_CONFIRMED / BRIDGE_FAILED 等
  previous_status varchar,               -- 事件发生前订单状态
  next_status     varchar,               -- 事件发生后订单状态
  data            jsonb,                 -- 事件附加数据，如 txHash、blockNumber、错误信息
  chain_id        int,                   -- 事件关联链；纯业务事件可为空
  created_at      timestamp              -- 事件时间
)
```

---

## 五、关键工程问题

### 5.1 Gas 估算

```
同链:
  estimatedGas = eth_estimateGas x 1.2 buffer
  maxFeePerGas = 实时 baseFee + priority tip

跨链:
  totalGasCost = srcChain gas
               + bridge fee  (Stargate: quoteLayerZeroFee)
               + dstChain execution gas
```

### 5.2 Token 授权

```
EVM 链优先使用 Permit2 (Uniswap 通用授权合约):
  用户首次: approve(Permit2, MaxUint256)  [仅 1 次]
  每次 swap: 签名 permit，无需额外 approve tx

注意:
  - 非所有链/聚合器都支持 Permit2，需按 Adapter 判断
  - 非 EVM 链 (Solana 等) 有各自的授权机制，不能复用
```

### 5.3 跨链失败处理

```
场景 1: src tx 失败
  → 资产未动，直接提示重试

场景 2: bridge 消息超时
  → LayerZero: lzRetry 重执行
  → CCTP: 重新 attestation
  → Stargate: 协议层自动退款
  → 超时 30min 触发告警，通知用户

场景 3: dst swap 失败 (桥成功但 swap revert)
  → 状态置为 PARTIAL_COMPLETED
  → 用户在目标链收到中间 token (如 USDC)
  → 前端提示并引导手动完成后半段
```

### 5.4 MEV 保护

| 方案 | 说明 | 适用场景 |
|------|------|---------|
| 1inch Fusion | RFQ 链下撮合，绕开 mempool | 默认方案 |
| Flashbots Protect | 私有交易，不经公开 mempool | 大额 EVM 交易 |
| 滑点上限 | 默认 0.5%，最大 3% | 全部 |
| 拆单 | 单笔 > $50K 拆分执行 | 大额 |
| Deadline | 20 分钟有效期 | 全部 |

---

## 六、技术选型

| 组件 | 推荐方案 | 说明 |
|------|---------|------|
| EVM DEX 聚合 | 1inch API v6 + 0x API | 1inch 覆盖链最广作主路由，0x 作备用 |
| 跨链聚合器 | LI.FI + Socket | 两者均是跨链聚合器，非单纯状态追踪 |
| USDC 跨链 | CCTP | Circle 官方，零滑点，安全性最高 |
| USDT / 通用桥 | Stargate | 流动性深 |
| OFT 资产跨链 | LayerZero OFT | 仅适用于已发行 OFT 版本的资产，非通用兜底 |
| Solana Swap | Jupiter | 独立 Adapter，不混入 EVM 流程 |
| MEV 保护 | 1inch Fusion + Flashbots | 大额必用 |
| EVM Token 授权 | Permit2 | 支持 Permit2 的链优先，否则走标准 approve |
| 风控模拟 | Tenderly Simulation API | 交易模拟，防止 revert |
| Token 风险检测 | GoPlus Security API | Honeypot / tax 检测 |
| 报价缓存 | Redis (TTL 15s) | 降低 Provider API 调用成本 |

---

## 七、分阶段实施路线

```
Phase 1 ── EVM 同链 Swap 基础（3~4 周）
  ├─ 接入 1inch + 0x 两个 Adapter
  ├─ Quote Engine + Route Engine 基础版
  ├─ Risk Engine 基础版（黑名单 + 滑点 + 交易模拟）
  ├─ Tx Builder + Signer + Broadcaster 分层
  └─ 订单状态机 + swap_orders / swap_events 表

Phase 2 ── 风控 + 监控 + 失败恢复（2~3 周）
  ├─ 完善状态机（APPROVAL 系列 / BROADCAST_FAILED / MANUAL_REVIEW）
  ├─ Monitor / Retry / Refund 模块
  ├─ provider_quotes + swap_transactions 表上线
  ├─ Token 风险缓存（token_risk_cache）
  └─ 告警 + 监控指标体系

Phase 3 ── 跨链聚合（3~4 周）
  ├─ 接入 LI.FI / Socket 跨链聚合器 Adapter
  ├─ CCTP Adapter（USDC 跨链）
  ├─ 跨链状态机（BRIDGE_PENDING / PARTIAL_COMPLETED）
  ├─ swap_route_steps 表 + 跨链失败退款引导
  └─ Gas Drop 功能

Phase 4 ── Solana / 非 EVM 链（3~4 周）
  ├─ Jupiter Adapter（独立流程，不复用 EVM Signer）
  ├─ Solana 订单状态机适配
  └─ 非 EVM 链风控适配

Phase 5 ── 体验优化与商业化（持续）
  ├─ MEV 保护（1inch Fusion / Flashbots）
  ├─ 限价 Swap（挂单）
  ├─ 自营手续费 + 会员费率
  └─ Swap 数据分析 + Provider 质量报表
```

---

## 八、核心设计原则

> 1. **Adapter 隔离**：每个 Provider 变更不影响主流程，通过统一接口对接
> 2. **签名边界清晰**：Tx Builder / Signer / Broadcaster 三层严格分离，支持多种钱包类型
> 3. **风控在主链路**：Risk Engine 是必经环节，不是外挂
> 4. **资产安全兜底**：跨链任意步骤失败必须有明确的退款或人工处理路径
> 5. **可观测性**：每一步报价、每一笔 tx、每一次状态变更全部持久化，支持全链路排查
> 6. **非 EVM 独立**：Solana 等非 EVM 链单独走 Adapter，不强行复用 EVM 流程

---

## 相关文档

- [[05-钱包系统架构|钱包系统架构]] — 签名服务、Nonce 管理
- [[06-钱包工程实践|钱包工程实践]] — 节点多活、交易卡死处理
- [[06-钱包工程实践/充值系统/跨链桥充值识别|跨链桥充值识别]] — 跨链入账逻辑
- [Uniswap V3 核心概念](v3/01-V3核心概念.md) — DEX 底层原理
