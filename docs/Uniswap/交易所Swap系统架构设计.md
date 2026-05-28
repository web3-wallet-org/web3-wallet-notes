# 交易所多链 Swap 系统架构设计

> **核心定位**: 交易所 Swap 功能不是自建 AMM，而是**聚合**链上现有 DEX 和跨链桥的最优路径，为用户提供最优价格、最低手续费的兑换体验。

---

## 一、整体架构分层

```
┌─────────────────────────────────────────────────────┐
│                    API Gateway                       │
│         /swap/quote  /swap/execute  /swap/status     │
└────────────────────────┬────────────────────────────┘
                         │
┌────────────────────────▼────────────────────────────┐
│                  Swap Orchestrator                   │
│   路由决策：同链 Swap ？跨链 Swap ？聚合路径？         │
└──────┬───────────────────────────────────┬──────────┘
       │                                   │
┌──────▼──────────┐              ┌─────────▼──────────┐
│  Same-Chain DEX │              │  Cross-Chain Bridge │
│  聚合器层        │              │  跨链协议层          │
│  1inch/0x/Para  │              │  Stargate/LayerZero │
└──────┬──────────┘              └─────────┬──────────┘
       │                                   │
┌──────▼───────────────────────────────────▼──────────┐
│              Transaction Builder                     │
│         构造交易 → 签名服务 → 广播 → 监控             │
└─────────────────────────────────────────────────────┘
```

---

## 二、核心模块设计

### 2.1 Quote Engine（报价引擎）

#### 同链 Swap：聚合多个 DEX 报价取最优

```
输入: fromToken, toToken, amount, chainId
并发请求:
  - 1inch API        (覆盖 EVM 主流链，市场份额最大)
  - 0x API (Matcha)  (机构级 RFQ 模式)
  - Paraswap         (欧洲用户为主)
  - Uniswap V3 Quoter 合约 (直接链上查询)

比较: 输出数量 - gas 成本 = 净收益最大路径
```

#### 跨链 Swap：拆解为两段路径

```
示例: ETH(Ethereum) → USDC(Arbitrum)

路径 A:  ETH → USDC(Ethereum)  [same-chain swap]
         + USDC 桥接 Eth→Arb   [CCTP / Stargate]

路径 B:  ETH 桥接 Eth→Arb      [ETH native bridge]
         + ETH → USDC(Arbitrum) [same-chain swap]

Quote Engine 对比两条路径: 最终金额 + 确认时间 + 总费用
```

### 2.2 Route Engine（路由引擎）

| 场景 | 判断条件 | 路由策略 |
|------|----------|---------|
| 同链同 Token | fromChain == toChain, fromToken == toToken | 直接转账，不走 Swap |
| 同链不同 Token | fromChain == toChain | DEX 聚合器 |
| 跨链同 Token (稳定币) | fromChain != toChain, USDC/USDT | CCTP 或 Stargate |
| 跨链同 Token (原生) | fromChain != toChain, ETH/BNB | 官方 canonical bridge |
| 跨链不同 Token | fromChain != toChain | 拆分两段路径取最优 |
| 目标链 Gas 不足 | 用户目标链无 native gas | 桥接时附带 gas drop |

### 2.3 跨链桥选型策略

```
稳定币跨链:
  USDC → 优先 CCTP        (Circle 官方，零滑点，最安全)
  USDT → Stargate         (流动性最深)

原生资产跨链:
  ETH L1 ↔ L2 → 官方 Canonical Bridge  (Arbitrum/OP bridge)
  ETH 非官方链  → Wormhole / deBridge

通用 ERC20:
  Stargate / LayerZero OFT / Across Protocol

选择权重: 安全性 > 滑点 > 速度 > 费用
```

### 2.4 订单状态机

```
CREATED
  │
  ▼
QUOTING ──────────────────────► QUOTE_FAILED
  │
  ▼
APPROVED (用户确认报价)
  │
  ▼
EXECUTING
  │
  ├─[同链]──► TX_PENDING ──► TX_CONFIRMED ──► COMPLETED
  │
  └─[跨链]──► SRC_PENDING ──► SRC_CONFIRMED
                                    │
                                    ▼
                               BRIDGE_PENDING  (等待跨链消息，5~20 min)
                                    │
                                    ▼
                               DST_PENDING ──► DST_CONFIRMED ──► COMPLETED
                                    │
                               BRIDGE_FAILED
                                    │
                                    ▼
                                REFUNDED      (桥协议超时自动退款)
```

### 2.5 MEV 保护

> **问题**: 大额 Swap 会被机器人插入三明治攻击（抢先买入 + 抢后卖出），用户实际成交价变差。

| 方案 | 说明 | 适用场景 |
|------|------|---------|
| 1inch Fusion | RFQ 模式，链下撮合，绕开公共 mempool | 推荐默认方案 |
| Flashbots Protect RPC | 交易直接发给矿工/validator，不经 mempool | 大额交易 |
| 合理滑点上限 | 默认 0.5%，最大 3%，超限拒绝执行 | 全部交易 |
| 拆单执行 | 大额 Swap（> $50K）拆分多笔 | 大额交易 |
| Deadline 设置 | 通常 20 分钟，防止长时间悬挂的交易被攻击 | 全部交易 |

---

## 三、关键工程问题

### 3.1 Gas 估算策略

```
同链:
  estimatedGas = eth_estimateGas × 1.2 buffer
  maxFeePerGas = 实时 baseFee + priority tip
  实时监听 baseFee 变化，动态调整

跨链:
  totalGasCost = srcChain gas
               + bridge protocol fee    (通过 API 查询, 如 Stargate: quoteLayerZeroFee)
               + dstChain execution gas  (估算目标链执行成本)
```

### 3.2 Token 授权（Approve）管理

> **问题**: ERC20 Swap 前需先发 approve 交易，导致两笔 tx，用户体验差。

**推荐方案：接入 Permit2（Uniswap 通用授权合约）**

```
传统流程: approve(DEX, amount) → swap(...)  [需要 2 笔交易]

Permit2 流程:
  用户首次: approve(Permit2 合约, MaxUint256)  [仅 1 次，永久有效]
  每次 swap:  用户对 Permit2 消息签名（链下，无 gas）
              → 合约验证签名并执行转账

优点: 后续每次 swap 只需 1 笔链上 tx
注意: 需向用户说明 MaxUint256 授权的含义和风险
```

### 3.3 失败重试与退款

```
跨链失败的三种场景:

1. src 链 tx 失败
   → 用户资产未动，直接提示重试，无需处理

2. src 成功 + bridge 消息超时
   → LayerZero: 支持手动重执行消息 (lzRetry)
   → Stargate:  协议层自动退款到源链地址
   → 设置超时告警 (> 30 min)，主动通知用户

3. dst 链执行失败 (如滑点超限)
   → 用户在目标链收到中间 token（如 USDC）
   → DEX 部分失败，不影响已桥接的资产
   → 引导用户在目标链手动完成后半段 Swap

设计原则: 每笔跨链单独持久化状态，前端定期轮询，超时后提供手动处理入口
```

### 3.4 与现有钱包系统的衔接

| 现有模块 | Swap 层复用方式 |
|---------|--------------|
| 签名服务 | Swap tx 也走签名服务，不绕过 |
| Nonce 管理 | 同链 Swap tx 进入统一 nonce 队列 |
| 风控系统 | Swap 金额纳入日累计限额检查 |
| 节点多活 | Quote 查询和 tx 广播复用多节点方案 |
| 监控指标 | 新增 Swap 成功率、跨链确认时长等指标 |

---

## 四、数据库核心表设计

```sql
-- 报价缓存（TTL 30 秒，可存 Redis）
swap_quotes (
  id          uuid PRIMARY KEY,
  from_chain  int,
  to_chain    int,
  from_token  varchar,
  to_token    varchar,
  amount_in   numeric,
  amount_out  numeric,     -- 预估输出
  route_json  jsonb,       -- 完整路由信息（序列化的 calldata、路径等）
  fee_usd     numeric,     -- 总费用（gas + 协议费）
  expires_at  timestamp
)

-- Swap 订单主表
swap_orders (
  id           uuid PRIMARY KEY,
  user_id      bigint,
  quote_id     uuid,
  status       varchar,    -- 见状态机
  src_tx_hash  varchar,
  dst_tx_hash  varchar,
  bridge_tx_id varchar,    -- 跨链桥的消息 ID
  amount_in    numeric,
  amount_out   numeric,    -- 实际到账（完成后更新）
  created_at   timestamp,
  updated_at   timestamp
)

-- 状态流转日志（用于排查和审计）
swap_events (
  id          bigserial PRIMARY KEY,
  order_id    uuid,
  event       varchar,     -- 'SRC_CONFIRMED' / 'BRIDGE_PENDING' 等
  data        jsonb,       -- 附加数据（block_number、tx_hash 等）
  chain_id    int,
  created_at  timestamp
)
```

---

## 五、技术选型汇总

| 组件 | 推荐方案 | 备注 |
|------|---------|------|
| 同链聚合 | **1inch API v6** + 0x API | 1inch 覆盖链最广，作为主路由 |
| 跨链稳定币 | **CCTP** (USDC) + **Stargate** (USDT) | CCTP 零滑点，最安全 |
| 跨链通用 | **LayerZero OFT** / Across Protocol | 支持链多，生态完善 |
| 跨链状态追踪 | **LI.FI SDK** / Socket | 现成的多桥聚合 + 状态追踪方案 |
| MEV 保护 | **1inch Fusion** + Flashbots Protect | 大额必用 |
| Token 授权 | **Permit2** | Uniswap 维护，安全可靠 |
| 报价缓存 | **Redis** (TTL 15s) | 避免频繁打 DEX API，降成本 |
| 跨链桥监控 | LI.FI / Squid Router | 聚合监控，避免单桥依赖 |

---

## 六、分阶段实施路线

```
Phase 1 ── 同链 Swap（2~3 周）
  ├─ 接入 1inch API，支持 EVM 主流链（ETH/BSC/Polygon/Arbitrum/OP）
  ├─ 实现 Permit2 授权流程
  ├─ 接入 Flashbots Protect RPC（MEV 保护）
  └─ 完成 Swap 订单状态追踪

Phase 2 ── 稳定币跨链（2~3 周）
  ├─ 接入 CCTP（USDC 跨链，零滑点）
  ├─ 接入 Stargate（USDT 及其他稳定币）
  └─ 实现跨链订单状态机 + 超时告警

Phase 3 ── 通用跨链（3~4 周）
  ├─ 接入 LI.FI SDK（聚合多桥协议，简化集成）
  ├─ 实现跨链失败退款引导
  └─ Gas Drop 功能（目标链 gas 不足时自动补充）

Phase 4 ── 优化与体验（持续）
  ├─ 大额拆单执行
  ├─ 限价 Swap（挂单等待最优价）
  ├─ 历史记录与 Swap 数据分析
  └─ 手续费分层（会员优惠费率）
```

---

## 七、核心设计原则

> 1. **不自建 AMM**：接入现有 DEX 聚合器，专注业务层，避免流动性冷启动问题
> 2. **不自建跨链桥**：使用 CCTP/Stargate/LayerZero 等成熟协议，安全由专业团队保障
> 3. **复用现有基础设施**：签名服务、Nonce 管理、风控、节点多活全部复用现有钱包系统
> 4. **资产安全优先**：跨链失败必须有明确退款路径，用户资产不能卡在中间状态
> 5. **用户体验优先**：Permit2 授权、Gas 估算兜底、滑点保护，减少交易失败

---

## 相关文档

- [[05-钱包系统架构|钱包系统架构]] — 签名服务、Nonce 管理
- [[06-钱包工程实践|钱包工程实践]] — 节点多活、交易卡死处理
- [[06-钱包工程实践/充值系统/跨链桥充值识别|跨链桥充值识别]] — 跨链入账逻辑
- [Uniswap V3 核心概念](v3/01-V3核心概念.md) — DEX 底层原理
