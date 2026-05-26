# 03 NFT ManagerContract

原文：<https://learnblockchain.cn/article/23820?course_id=100>

这一节把原来的 Manager 改造成 NFT Position Manager。

核心一句话：

```text
用户不再用 owner + tick 范围直接管理仓位，而是用 NFT tokenId 管理仓位。
```

这就是 Uniswap V3 用户体验真正变顺的地方。

## 这节在解决什么

前面的仓位定位方式是：

```text
owner
token0
token1
fee
lowerTick
upperTick
```

这些参数都要记住，才能找到一个仓位。

这对用户和前端都不友好。

NFT Manager 把这些参数包成一个 `tokenId`：

```text
tokenId -> Position
```

以后用户只要拿着这个 NFT，就能管理对应仓位。

## Position 里存什么

NFT Manager 会维护一个 `positions` 映射。

每个 `tokenId` 对应一个仓位结构。

里面通常会存：

```text
pool
lowerTick
upperTick
liquidity
```

在真实 Uniswap 里还会有更多字段，比如：

```text
nonce
operator
token0
token1
fee
feeGrowthInside0LastX128
feeGrowthInside1LastX128
tokensOwed0
tokensOwed1
```

课程实现会简化，但核心关系不变：

```text
tokenId 是仓位入口。
Position 是仓位数据。
Pool 里仍然保存底层流动性状态。
```

## mint 做了什么

用户添加流动性时，现在不只是调用 Pool `mint`。

流程变成：

```text
1. 用户调用 NFT Manager.mint
2. Manager 找到或创建对应 Pool
3. Manager 调用 Pool.mint 添加流动性
4. Pool 回调 Manager 收 token
5. Manager 铸造一个 ERC721 NFT 给用户
6. Manager 保存 tokenId -> Position
```

这一步之后，用户钱包里就能看到一个 LP NFT。

## increaseLiquidity 做什么

如果用户想给已有仓位加流动性，不应该再 mint 一个新 NFT。

而是调用：

```text
increaseLiquidity(tokenId, ...)
```

Manager 会：

```text
1. 检查调用者是否有权操作这个 tokenId
2. 找到对应 Position
3. 调用 Pool.mint 给同一个 tick 范围增加流动性
4. 更新 Position.liquidity
```

这样同一个 NFT 代表的仓位变大了。

## decreaseLiquidity 做什么

减少流动性对应 Pool 的 `burn`。

流程是：

```text
1. 检查调用者有权操作 tokenId
2. 找到 Position
3. 调用 Pool.burn(lowerTick, upperTick, liquidity)
4. Pool 结算出 amount0 / amount1
5. Manager 更新 Position.liquidity
6. 把可领取数量记下来
```

注意，减少流动性和真正把 token 转给用户，仍然是两个概念。

`decreaseLiquidity` 更像结算。

`collect` 才是提款。

## collect 做什么

`collect(tokenId, recipient, amount0Max, amount1Max)` 用来把仓位里可领取的 token 转走。

它会：

```text
1. 检查权限
2. 找到仓位对应 Pool
3. 调用 Pool.collect
4. 把 token 转给 recipient
```

费用和移除流动性释放出来的 token，都可能通过 collect 取走。

## burn NFT 做什么

当一个仓位已经没有流动性，也没有可领取 token 时，用户可以 burn 掉 NFT。

要求通常是：

```text
liquidity = 0
tokensOwed0 = 0
tokensOwed1 = 0
```

这样可以防止用户烧掉一个还带资产的仓位。

NFT 被 burn 后，`tokenId` 不再代表有效仓位。

## 为什么每个操作都要检查权限

因为 NFT 代表仓位所有权。

能操作仓位的人应该是：

```text
NFT owner
被 approve 的地址
被 setApprovalForAll 的 operator
```

所以 Manager 在 `increaseLiquidity`、`decreaseLiquidity`、`collect`、`burn` 这些操作里都要检查：

```text
调用者是否被授权操作 tokenId
```

否则别人就能偷你的 LP 仓位。

## 人话版理解

NFT Manager 像一个仓位管家。

以前你要管理仓位，必须说清楚：

```text
哪个池子
哪个价格范围
哪个 owner
多少流动性
```

现在你只要说：

```text
我要操作 tokenId = 7 这个仓位
```

Manager 会拿着 tokenId 去查底层信息，再帮你调用 Pool。

NFT 让复杂仓位变成用户能理解、钱包能展示、市场能转让的资产。

## 你要记住

- NFT Manager 把复杂 LP 仓位包装成 ERC721。
- `tokenId` 是用户管理仓位的入口。
- `positions[tokenId]` 保存仓位对应的 Pool、tick 范围和流动性。
- `mint` 会添加流动性并铸造 NFT。
- `increaseLiquidity` 给已有 NFT 仓位加仓。
- `decreaseLiquidity` 减少流动性并结算。
- `collect` 把可领取 token 转给用户。
- `burn` 只能在仓位清空后销毁 NFT。
- 所有仓位操作都必须检查 NFT 权限。

## 学完这节要能回答

- 为什么有了 NFT Manager 后，用户不需要手动记住 tick 范围？
- `tokenId` 和 Pool 里的底层仓位是什么关系？
- `decreaseLiquidity` 和 `collect` 为什么要分开？
- 为什么不能随便 burn 一个还有资产的 NFT？
- NFT 权限检查保护的是什么？
