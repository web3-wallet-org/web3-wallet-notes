
## 定义
BLS（Boneh-Lynn-Shacham）签名支持将 N 个签名聚合为 1 个，验证一次即可确认所有签名者。

## 原理
- 基于**双线性配对（pairing）**：e(aG, bG) = e(G, G)^ab
- 签名可以线性叠加：sig1 + sig2 = aggregate_sig
- 验证时只需一次 pairing 运算，与签名数量无关

## 聚合流程
1. 每个 validator 用自己的私钥对消息签名
2. aggregator 收集各 validator 的签名
3. 将所有签名相加，生成 aggregate signature
4. 用 bitlist 记录哪些 validator 参与了签名

## 数据结构
- **aggregate signature**：所有签名的叠加结果（96 字节）
- **bitlist**：标记哪些 validator 参与，用于聚合公钥的构造

## 安全问题：Rogue Key Attack
攻击者构造特殊公钥，使聚合后的签名实际只需攻击者自己签名即可通过验证。

解决方案：**Proof of Possession**，每个 validator 注册时需证明自己拥有对应私钥。

## 在 ETH 中的应用
- 用于 Beacon Chain 的 **attestation**（validator 投票），大幅减少链上数据量
- ETH 普通交易（EOA 转账、合约调用）仍使用 **ECDSA**，与 BLS 无关
