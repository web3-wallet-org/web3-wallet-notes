
## 定义
用质押 ETH 替代算力参与共识，validator 通过投票决定链的状态。

## 核心角色
- **Beacon Chain**：共识层，协调所有 validator
- **Validator**：质押 32 ETH 后参与出块和投票

## 时间结构
- **slot**：12 秒，每个 slot 选一个 proposer 出块
- **epoch**：32 个 slot，约 6.4 分钟，epoch 结束时进行 finality 判定

## 共识流程
1. 每个 slot 随机选一个 validator 作为 proposer，负责出块
2. 其余 validator 对区块进行投票（attestation）
3. 超过 2/3 的 validator 投票确认 -> checkpoint
4. 连续两个 checkpoint 被确认 -> finalize，不可回滚

## Finality
- 至少需要 2 个 epoch（约 13 分钟）才能最终确定
- finalized 的区块除非攻击者控制 1/3 以上质押量，否则不可回滚
- 钱包/交易所充值确认应以 finalized 为准，而非区块数

## 随机性
- 使用 **RANDAO**：validator 提交随机数，链上混合生成随机种子，用于选 proposer

## 惩罚机制
- **inactivity leak**：长期离线导致质押 ETH 缓慢减少
- **slashing**：双重投票等作恶行为，直接大额罚款并强制退出

## reorg 风险
- PoS 下 reorg 概率远低于 PoW，但在 finalize 前仍可能发生
- 实践中等待 1-2 个 epoch 后再入账更安全
