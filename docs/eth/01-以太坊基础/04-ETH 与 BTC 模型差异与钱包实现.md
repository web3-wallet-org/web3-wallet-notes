# 04 ETH 与 BTC 模型差异与钱包实现

## 目录
- [总览图](#sec-1)
- [1. 先看结论](#sec-2)
- [2. 数据模型差异](#sec-3)
  - [2.1 BTC（UTXO）](#sec-3-1)
  - [2.2 ETH（Account）](#sec-3-2)
- [3. 交易构造差异](#sec-6)
  - [3.1 BTC 钱包重点](#sec-6-1)
  - [3.2 ETH 钱包重点](#sec-6-2)
- [4. 费用机制差异](#sec-9)
  - [4.1 BTC](#sec-9-1)
  - [4.2 ETH](#sec-9-2)
- [5. 状态追踪差异](#sec-12)
  - [5.1 BTC](#sec-12-1)
  - [5.2 ETH](#sec-12-2)
- [6. 智能合约能力带来的钱包复杂度](#sec-15)
- [7. 对钱包工程师的能力要求对比](#sec-16)
- [8. 系统化学习建议（4 周）](#sec-17)
  - [8.1 第 1 周：基础交易闭环](#sec-17-1)
  - [8.2 第 2 周：签名与安全](#sec-17-2)
  - [8.3 第 3 周：工程化](#sec-17-3)
  - [8.4 第 4 周：进阶能力](#sec-17-4)
- [9. 本章你要做到的能力](#sec-22)

<a id="sec-1"></a>
## 总览图
```mermaid
flowchart LR

    subgraph BTC[BTC: UTXO 模型]
        direction TB
        B0[输入: 花费旧 UTXO]
        B1[输出: 新 UTXO]
        B2[余额: UTXO 集合求和]
        B3[重点: 选币 / 找零 / 费率 sat-vB]
        B0 --> B1 --> B2 --> B3
    end

    subgraph ETH[ETH: Account 模型]
        direction TB
        E0[输入: 账户交易]
        E1[执行: EVM 状态迁移]
        E2[余额: 账户字段]
        E3[重点: nonce / gas / receipt / logs]
        E0 --> E1 --> E2 --> E3
    end

    B3 --> C[钱包工程关注点差异]
    E3 --> C

    classDef title fill:#e8f0ff,stroke:#5b8def,color:#111;
    classDef btc fill:#fff7e6,stroke:#f0a202,color:#111;
    classDef eth fill:#e8fff1,stroke:#20a464,color:#111;

    class C title;
    class B0,B1,B2,B3 btc;
    class E0,E1,E2,E3 eth;
```

<a id="sec-2"></a>
## 1. 先看结论
- BTC：UTXO 模型，交易是“花费旧输出，创建新输出”
- ETH：Account 模型，交易是“修改全局账户状态”

这决定了钱包在“余额计算、交易构造、费用策略、状态追踪”上的实现方式完全不同。

<a id="sec-3"></a>
## 2. 数据模型差异
<a id="sec-3-1"></a>
### 2.1 BTC（UTXO）
- 余额来自多个未花费输出聚合
- 发送时要做选币（coin selection）
- 常见有找零地址管理问题

<a id="sec-3-2"></a>
### 2.2 ETH（Account）
- 余额是账户字段，直接读取
- 无需选币，但需要严格管理 nonce
- 合约调用可携带任意数据并改变复杂状态

<a id="sec-6"></a>
## 3. 交易构造差异
<a id="sec-6-1"></a>
### 3.1 BTC 钱包重点
- 选择 UTXO 集合
- 估算字节大小与费率（sat/vB）
- 构造找零输出

<a id="sec-6-2"></a>
### 3.2 ETH 钱包重点
- 生成 `to/value/data`
- 估算 `gasLimit`
- 计算 EIP-1559 费用参数
- 管理 nonce 与替换交易

<a id="sec-9"></a>
## 4. 费用机制差异
<a id="sec-9-1"></a>
### 4.1 BTC
- 费用由交易体积决定，通常 `fee = vsize * feerate`

<a id="sec-9-2"></a>
### 4.2 ETH
- 费用由执行复杂度决定，`fee = gasUsed * gasPrice`
- 在 EIP-1559 下，近似为 `gasUsed * (baseFee + priorityFee)`

对钱包工程来说，ETH 的“可预测性”通常比 BTC 更依赖执行估算和模拟。

<a id="sec-12"></a>
## 5. 状态追踪差异
<a id="sec-12-1"></a>
### 5.1 BTC
- 更关注 UTXO 是否确认、是否双花

<a id="sec-12-2"></a>
### 5.2 ETH
- 更关注交易执行是否成功（`receipt.status`）
- 还要关注事件日志是否符合业务预期

因此 ETH 钱包后端通常会实现“交易 + 事件”双轨索引。

<a id="sec-15"></a>
## 6. 智能合约能力带来的钱包复杂度
ETH 钱包必须处理：

- Token 标准差异（ERC-20/721/1155）
- 授权风险（`approve` 无限额度）
- 合约升级与代理模式
- 多调用组合（multicall/batch）

这部分复杂度在 BTC 原生钱包中相对较少。

<a id="sec-16"></a>
## 7. 对钱包工程师的能力要求对比
如果你做 ETH 钱包，除了账户与交易，还需要：

- ABI 解码能力
- 合约安全常识
- 签名标准（EIP-712 等）
- 多链 RPC 异常与兼容性治理

<a id="sec-17"></a>
## 8. 系统化学习建议（4 周）
<a id="sec-17-1"></a>
### 8.1 第 1 周：基础交易闭环
- ETH 转账
- ERC-20 转账
- 交易状态跟踪

<a id="sec-17-2"></a>
### 8.2 第 2 周：签名与安全
- `personal_sign` 与 EIP-712
- 授权风险提示
- 钓鱼场景演练

<a id="sec-17-3"></a>
### 8.3 第 3 周：工程化
- nonce 管理器
- 费用策略与加速取消
- RPC 容灾和重试

<a id="sec-17-4"></a>
### 8.4 第 4 周：进阶能力
- 交易模拟
- 事件索引
- 风控规则引擎（地址、方法、额度）

<a id="sec-22"></a>
## 9. 本章你要做到的能力
- 清楚解释 BTC 与 ETH 钱包为何设计不同
- 识别 Account 模型下的工程核心难点（nonce、gas、执行结果）
- 制定一条面向生产的钱包能力建设路线
