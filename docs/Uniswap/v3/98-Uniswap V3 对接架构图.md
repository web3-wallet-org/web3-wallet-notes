# Uniswap V3 对接架构图

这篇讲的是**对接真实 Uniswap V3**，不是重新开发一个 Uniswap V3。

你前面学 Pool、tick、swap、NFT 仓位，是为了看懂真实协议在做什么。

真正接入时，你要做的是：

```text
读官方已部署合约
调用官方 Router / Position Manager
用 SDK 帮你组装参数和 calldata
让用户钱包签名并发送交易
```

## 总架构

```text
你的前端 / 后端
  |
  | 读链上数据
  v
Factory --------> Pool
  |                |
  | getPool        | slot0 / liquidity / token0 / token1 / fee
  v                v
Pool 地址       当前价格、当前 tick、当前活跃流动性


你的前端 / 后端
  |
  | 查询报价
  v
QuoterV2
  |
  v
预估 amountOut / gas / swap 后价格


用户钱包
  |
  | swap
  v
Universal Router 或 SwapRouter02
  |
  v
Pool


用户钱包
  |
  | 添加 / 减少 / 收取流动性
  v
NonfungiblePositionManager
  |
  v
Pool
```

最重要的分工：

```text
Factory                    = 查 Pool 地址
Pool                       = 真实资金池和价格状态
QuoterV2                   = 模拟报价，不执行交易
Universal Router / Router  = 执行 swap
NonfungiblePositionManager = 管理 V3 LP NFT 仓位
```

## 地址关系

真实 Uniswap V3 已经部署在很多链上。

对接时第一件事不是写合约，而是确认当前链上的官方地址：

```text
chainId
Factory
QuoterV2
Universal Router 或 SwapRouter02
NonfungiblePositionManager
WETH / wrapped native token
```

不要假设所有链地址都一样。

官方部署页也明确提醒：不同链上的部署地址需要分别确认。

以 Ethereum mainnet 为例，常用地址包括：

```text
UniswapV3Factory
0x1F98431c8aD98523631AE4a59f267346ea31F984

NonfungiblePositionManager
0xC36442b4a4522E871399CD717aBDD847Ab11FE88

QuoterV2
0x61fFE014bA17989E743c5F6cB21bF9697530B21e

SwapRouter02
0x68b3465833fb72A70ecDF485E0e4C7bD8665Fc45

UniversalRouter
0x66a9893cc07d91d95644aedd05d03f95e1dba8af

WETH
0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2
```

实际项目里应该把它们放到链配置里：

```text
chainId -> contract addresses
```

## 查询 Pool 的地图

用户选择：

```text
tokenA
tokenB
fee tier
```

你要找到对应 Pool：

```text
Factory.getPool(tokenA, tokenB, fee)
```

如果返回：

```text
0x0000000000000000000000000000000000000000
```

说明这个 fee 档位下的 Pool 不存在。

如果存在，再读 Pool：

```text
token0()
token1()
fee()
liquidity()
slot0()
```

其中 `slot0()` 里最关键的是：

```text
sqrtPriceX96
tick
```

这一步解决的是：

```text
这个池子现在是什么价格？
当前活跃流动性是多少？
```

## swap 地图

swap 对接分两步：

```text
先 quote
再 swap
```

### 1. 先 quote

```text
你的前端
  |
  v
QuoterV2.quoteExactInputSingle(...)
  |
  v
预计能拿到多少 tokenOut
```

QuoterV2 是模拟，不是真正交易。

它的作用是告诉你：

```text
输入 amountIn
预计输出 amountOut
交易后价格大概到哪里
可能跨过多少 initialized tick
```

注意：

```text
Quoter 用来链下模拟，不应该在你的合约里当普通 view 调用。
```

### 2. 再 swap

```text
用户钱包
  |
  | approve tokenIn
  v
Router
  |
  | exactInputSingle / exactInput
  v
Pool
  |
  v
用户收到 tokenOut
```

真实项目里必须设置：

```text
amountOutMinimum
deadline
recipient
```

其中最重要的是：

```text
amountOutMinimum = 滑点保护
```

如果你把它设成 0，就等于告诉市场：

```text
我接受任何差价格成交。
```

这很危险。

## 添加流动性地图

添加 V3 流动性不是直接给 Pool 打 token。

实际入口通常是：

```text
NonfungiblePositionManager.mint(...)
```

流程：

```text
用户选择 token0 / token1 / fee
  |
  v
用户选择价格范围
  |
  v
前端把价格转成 tickLower / tickUpper
  |
  v
前端计算 amount0Desired / amount1Desired
  |
  v
用户 approve token0 / token1 给 Position Manager
  |
  v
NonfungiblePositionManager.mint(...)
  |
  v
Pool.mint(...)
  |
  v
用户收到一个 LP NFT tokenId
```

这一步结束后，用户拿到的不是 ERC20 LP Token，而是：

```text
V3 Position NFT
```

## 查询用户仓位地图

用户的钱包里可能有多个 V3 Position NFT。

查询流程：

```text
NonfungiblePositionManager.balanceOf(user)
  |
  v
用户有多少个 Position NFT

NonfungiblePositionManager.tokenOfOwnerByIndex(user, index)
  |
  v
拿到 tokenId

NonfungiblePositionManager.positions(tokenId)
  |
  v
拿到仓位详情
```

`positions(tokenId)` 会返回类似这些信息：

```text
token0
token1
fee
tickLower
tickUpper
liquidity
feeGrowthInside0LastX128
feeGrowthInside1LastX128
tokensOwed0
tokensOwed1
```

这一步解决的是：

```text
这个 tokenId 背后到底是哪一个 LP 仓位？
```

## 减少流动性和收取费用地图

减少流动性和提款仍然要分开：

```text
decreaseLiquidity = 结算
collect           = 提款
```

流程：

```text
用户选择 tokenId
  |
  v
NonfungiblePositionManager.decreaseLiquidity(...)
  |
  v
底层 Pool.burn(...)
  |
  v
流动性减少，可领取 token 增加

用户再调用 collect(...)
  |
  v
token0 / token1 转回用户钱包
```

如果只是 `decreaseLiquidity`，用户钱包不会自动收到 token。

必须 `collect`。

## burn NFT 地图

burn NFT 是销毁仓位凭证，不是提款。

正常顺序：

```text
1. decreaseLiquidity 到 liquidity = 0
2. collect 到 tokensOwed0 = 0 且 tokensOwed1 = 0
3. burn tokenId
```

如果仓位还有流动性或还有未领取 token，就不应该 burn。

一句话：

NFT burn <span style="color: red;">只销毁仓位凭证，不负责提款</span>。

## 你真正要记住的调用入口

### 读数据

```text
Factory.getPool(...)
Pool.slot0()
Pool.liquidity()
Pool.token0()
Pool.token1()
Pool.fee()
NonfungiblePositionManager.positions(tokenId)
```

### swap

```text
QuoterV2.quoteExactInputSingle(...)
Router.exactInputSingle(...)
Router.exactInput(...)
```

### 管理流动性

```text
NonfungiblePositionManager.mint(...)
NonfungiblePositionManager.increaseLiquidity(...)
NonfungiblePositionManager.decreaseLiquidity(...)
NonfungiblePositionManager.collect(...)
NonfungiblePositionManager.burn(...)
```

## 推荐对接方式

如果你是在做普通前端：

```text
v3 SDK + ethers / viem + 用户钱包
```

如果你想最快做 swap：

```text
Uniswap API 或 Universal Router
```

如果你要做 LP 仓位管理：

```text
NonfungiblePositionManager
```

如果你是合约项目要在链上调用：

```text
直接接 Router / Position Manager
并且自己处理授权、滑点、deadline、回调和资金安全。
```

## 参考

- Uniswap v3 部署地址：<https://developers.uniswap.org/docs/protocols/v3/deployments>
- Ethereum v3 部署地址：<https://developers.uniswap.org/docs/protocols/v3/deployments/v3-ethereum-deployments>
- Uniswap v3 SDK：<https://developers.uniswap.org/docs/sdks/v3/overview>
- v3 Quote 指南：<https://developers.uniswap.org/docs/sdks/v3/guides/swapping/quoting>
- v3 Swap 指南：<https://developers.uniswap.org/docs/sdks/v3/guides/swapping/swapping>
- v3 Position 查询：<https://developers.uniswap.org/docs/sdks/v3/guides/managing-liquidity/position-fetching>
