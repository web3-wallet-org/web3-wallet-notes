# 01 NFT 仓位

原文：<https://learnblockchain.cn/article/23822?course_id=100>

这一章讲 Uniswap V3 最容易被初学者低估的一件事：

```text
V3 的 LP 仓位不是 ERC20 份额，而是 NFT。
```

这不是为了赶 NFT 热点，而是因为 V3 的每个 LP 仓位都可能完全不同。

## 这节在解决什么

在 Uniswap V2 里，LP 仓位很好理解。

如果你给 ETH/USDC 池子提供流动性，你拿到的是 LP Token。

这些 LP Token 是 ERC20，因为同一个池子里的 LP 份额是同质化的：

```text
你的 1 个 LP Token 和我的 1 个 LP Token 没有本质区别。
```

但 V3 不一样。

V3 的 LP 不只是提供两个 token，还要选择价格范围：

```text
lowerTick
upperTick
liquidity
fee tier
token0
token1
```

两个 LP 即使都给 ETH/USDC 提供流动性，只要价格范围不同，它们承担的风险和赚到的费用都不同。

所以这些仓位不是同质化资产。

## 为什么 V3 仓位适合 NFT

ERC20 适合表示“每一份都一样”的东西。

比如：

```text
USDC
V2 LP Token
治理代币
```

ERC721 适合表示“每一个都独一无二”的东西。

V3 仓位就是这样。

每个仓位都有自己的参数：

```text
token0 / token1
fee
lowerTick / upperTick
liquidity
tokensOwed
feeGrowthInsideLast
```

所以把它做成 NFT，意思是：

```text
每个 tokenId 代表一个独立的 LP 仓位。
```

## NFT 仓位带来什么好处

第一，所有权清晰。

谁拥有这个 NFT，谁就拥有这个 LP 仓位。

第二，转让方便。

用户可以把仓位像 NFT 一样转给别人。

第三，前端展示方便。

每个仓位可以有自己的图片、属性和描述。

第四，Manager 可以统一管理复杂操作。

用户不用直接和 Pool 打交道，而是通过 NFT Manager：

```text
mint 仓位
increase liquidity
decrease liquidity
collect fees
burn NFT
```

## 这章要做什么

这一章会把前面的 Manager 升级成类似真实 Uniswap 的 `NonfungiblePositionManager`。

它会做几件事：

1. 用户 mint 流动性时，Manager 铸造一个 NFT。
2. NFT 的 `tokenId` 对应一个仓位结构。
3. 以后增加、减少流动性，都通过这个 `tokenId` 找仓位。
4. 收取费用也通过这个 `tokenId`。
5. 当仓位完全清空后，可以 burn NFT。
6. Renderer 根据仓位信息生成 NFT metadata 和 SVG 图片。

## 人话版理解

V2 的 LP 像银行里的同一种理财份额。

大家买的是同一个产品，只是数量不同，所以可以用 ERC20 表示。

V3 的 LP 更像一张张私人定制合同。

每张合同都写着：

```text
你在哪个交易对做市
价格范围是多少
手续费等级是多少
现在还有多少流动性
赚了多少费用
```

这些合同彼此不一样，所以用 NFT 表示更自然。

## 你要记住

- V2 LP Token 是 ERC20，因为份额同质化。
- V3 LP 仓位是 NFT，因为每个仓位都有不同价格范围和状态。
- NFT 的 `tokenId` 对应一个独立仓位。
- ERC721 主要解决所有权和转让问题。
- 真正的流动性操作仍然要由 Position Manager 和 Pool 完成。
- 仓位 NFT 不是图片本身，图片只是这个仓位的展示层。

## 学完这节要能回答

- 为什么 V3 不能像 V2 一样用 ERC20 表示 LP 仓位？
- 一个 V3 仓位由哪些关键参数决定？
- NFT 在这里解决的是艺术品问题，还是所有权问题？
- `tokenId` 和 LP 仓位是什么关系？
