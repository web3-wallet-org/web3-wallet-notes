# Uniswap V3 对接实战清单

这篇是给真实项目用的。

目标不是开发一个新的 Uniswap V3，而是：

```text
把你的前端、后端或合约接到真实 Uniswap V3 上。
```

你真正要完成的是这些能力：

```text
查 Pool
查价格
查报价
执行 swap
查询用户 NFT 仓位
添加流动性
减少流动性
collect 手续费和已结算 token
burn 空仓位 NFT
```

## 第 0 步：确定你要接哪条链

先确定：

```text
Ethereum mainnet
Arbitrum
Optimism
Base
Polygon
其他链
```

然后准备链配置：

```text
chainId
RPC URL
block explorer
native token
wrapped native token
Uniswap v3 contract addresses
```

不要把 mainnet 地址直接复制到所有链。

每条链都要查官方部署地址。

## 第 1 步：准备官方合约地址

至少需要这些地址：

```text
UniswapV3Factory
QuoterV2
UniversalRouter 或 SwapRouter02
NonfungiblePositionManager
WETH / wrapped native token
```

前端项目里建议这样组织：

```ts
export const UNISWAP_V3_ADDRESSES = {
  1: {
    factory: "...",
    quoterV2: "...",
    swapRouter02: "...",
    universalRouter: "...",
    positionManager: "...",
    weth: "...",
  },
  8453: {
    factory: "...",
    quoterV2: "...",
    swapRouter02: "...",
    universalRouter: "...",
    positionManager: "...",
    weth: "...",
  },
}
```

检查点：

- 当前钱包 `chainId` 是否支持；
- 合约地址是否来自官方部署页；
- token 地址是否是当前链的地址；
- native token 是否要先 wrap 成 WETH / WETH-like token。

## 第 2 步：准备依赖

常见前端依赖：

```text
ethers 或 viem
wagmi
@uniswap/v3-sdk
@uniswap/sdk-core
@uniswap/smart-order-router
jsbi
```

其中：

```text
@uniswap/sdk-core = Token、CurrencyAmount、Percent 等基础类型
@uniswap/v3-sdk   = Pool、Route、Trade、Position、tick 工具
smart-order-router = 自动找路由和报价
ethers / viem      = 读链和发交易
```

如果只是简单 swap，可以先不用把所有 SDK 都上齐。

## 第 3 步：查询 Pool 是否存在

用户选择：

```text
tokenA
tokenB
fee tier
```

调用：

```text
Factory.getPool(tokenA, tokenB, fee)
```

常见 fee tier：

```text
500   = 0.05%
3000  = 0.3%
10000 = 1%
```

返回零地址表示：

```text
这个交易对在这个 fee tier 下没有 Pool。
```

检查点：

- token 顺序不用自己乱猜，Pool 里有 `token0()` 和 `token1()`；
- 同一个交易对可能有多个 fee tier；
- 不要把 `fee` 和 `tickSpacing` 混成一个东西。

## 第 4 步：读取 Pool 当前状态

拿到 Pool 地址后，读取：

```text
slot0()
liquidity()
token0()
token1()
fee()
```

`slot0()` 里最重要的是：

```text
sqrtPriceX96
tick
```

你可以用这些数据展示：

```text
当前价格
当前 tick
当前活跃流动性
当前池子 fee tier
```

检查点：

- token 小数位不同，价格展示要处理 decimals；
- Pool 价格方向是 token1 / token0；
- 前端展示 ETH/USDC 和 USDC/ETH 时要注意倒数。

## 第 5 步：做 swap 报价

swap 之前先 quote。

常见方式：

```text
QuoterV2.quoteExactInputSingle(...)
```

你要准备：

```text
tokenIn
tokenOut
fee
amountIn
sqrtPriceLimitX96
```

如果不限制价格，`sqrtPriceLimitX96` 通常可以传 0。

报价结果用于展示：

```text
预计收到 amountOut
价格影响
交易后价格
预估 gas
```

检查点：

- QuoterV2 是模拟，不是真交易；
- quote 结果不是最终成交保证；
- 真正交易时还要设置滑点保护。

## 第 6 步：执行 swap

swap 前，用户通常要先：

```text
approve tokenIn 给 Router
```

然后调用 Router：

```text
exactInputSingle(...)
```

或多跳：

```text
exactInput(...)
```

必须处理：

```text
amountIn
amountOutMinimum
recipient
deadline
path
```

最重要的是：

```text
amountOutMinimum = 滑点保护
```

不要为了省事设成 0。

检查点：

- 先检查 allowance；
- allowance 不够才让用户 approve；
- approve 和 swap 是两笔交易，除非用 permit 或其他聚合方式；
- deadline 不要太长；
- swap 失败时要把 revert 信息展示清楚。

## 第 7 步：查询用户 V3 NFT 仓位

V3 LP 仓位是 NFT。

先查用户有多少个仓位：

```text
NonfungiblePositionManager.balanceOf(user)
```

再遍历 tokenId：

```text
tokenOfOwnerByIndex(user, index)
```

然后查仓位详情：

```text
positions(tokenId)
```

你会拿到类似：

```text
token0
token1
fee
tickLower
tickUpper
liquidity
tokensOwed0
tokensOwed1
```

检查点：

- `balanceOf` 只告诉你数量，不告诉你 tokenId；
- 遍历 tokenId 适合普通钱包仓位数量少的场景；
- 如果仓位很多，应该考虑索引服务；
- tokenURI 是展示入口，不是仓位真实资金数据。

## 第 8 步：添加流动性

添加流动性入口：

```text
NonfungiblePositionManager.mint(...)
```

你要准备：

```text
token0
token1
fee
tickLower
tickUpper
amount0Desired
amount1Desired
amount0Min
amount1Min
recipient
deadline
```

前端要做：

```text
1. 用户选择交易对和 fee tier
2. 用户选择价格范围
3. 把价格转换成 tickLower / tickUpper
4. 用 tickSpacing 对齐 tick
5. 根据输入金额计算另一边需要多少
6. approve token0 / token1 给 Position Manager
7. 调用 mint
8. 得到 tokenId
```

检查点：

- tick 必须符合该 fee tier 对应的 tickSpacing；
- amount0Min / amount1Min 是添加流动性的滑点保护；
- 价格范围如果在当前价格两边，需要两种 token；
- 如果区间完全在当前价格一侧，可能只需要一种 token；
- mint 后用户拿到的是 NFT，不是 ERC20 LP Token。

## 第 9 步：增加已有仓位流动性

入口：

```text
increaseLiquidity(...)
```

用户选择已有 `tokenId`，你读取它的：

```text
token0
token1
fee
tickLower
tickUpper
```

然后只让用户补充 token 数量。

检查点：

- 不要新建 NFT，除非用户选择的是新价格范围；
- 先检查用户是否拥有或被授权操作该 tokenId；
- 仍然需要 approve token0 / token1；
- 仍然需要 amount0Min / amount1Min。

## 第 10 步：减少流动性

入口：

```text
decreaseLiquidity(...)
```

它做的是：

```text
减少 liquidity
结算能取回的 token0 / token1
```

但它不是提款。

检查点：

- 用户要输入减少比例或 liquidity 数量；
- 不能减少超过 position.liquidity；
- decrease 后 token 还没真正到钱包；
- 后面必须 collect。

一句话：

```text
decreaseLiquidity = 结算
collect           = 提款
```

## 第 11 步：collect

入口：

```text
collect(...)
```

它负责：

```text
把已经结算出来的 token0 / token1 真正转给用户。
```

collect 可以领取：

```text
手续费
decreaseLiquidity 后释放出来的 token
```

检查点：

- `amount0Max` / `amount1Max` 决定最多领取多少；
- 如果只想领取手续费，也要看当前实现怎么处理 tokensOwed；
- collect 后再重新读取 `positions(tokenId)`；
- 确认 `tokensOwed0` / `tokensOwed1` 是否归零。

## 第 12 步：burn 空仓位 NFT

入口：

```text
burn(tokenId)
```

前提通常是：

```text
liquidity = 0
tokensOwed0 = 0
tokensOwed1 = 0
```

正常顺序：

```text
1. decreaseLiquidity 到 liquidity = 0
2. collect 到 tokensOwed0 / tokensOwed1 = 0
3. burn tokenId
```

检查点：

- burn NFT 不会帮你取 token；
- 如果还有 liquidity，不能 burn；
- 如果还有 tokensOwed，不能 burn；
- burn 后 tokenId 不再代表有效仓位。

## 第 13 步：前端页面建议

一个完整的 V3 对接前端，至少可以分成这些页面：

```text
Swap 页面
Pool 查询页面
添加流动性页面
我的仓位页面
仓位详情页面
减少流动性 / collect 页面
```

### Swap 页面

展示：

```text
tokenIn / tokenOut
amountIn
预计 amountOut
price impact
slippage
minimum received
route
```

### 添加流动性页面

展示：

```text
交易对
fee tier
当前价格
价格范围
tickLower / tickUpper
需要的 token0 / token1
滑点保护
```

### 我的仓位页面

展示：

```text
tokenId
交易对
fee tier
价格范围
当前价格是否在区间内
liquidity
未领取费用
```

## 第 14 步：安全和产品检查

上线前至少检查：

- 合约地址是否按 chainId 配置；
- token decimals 是否正确；
- quote 和 swap 的 token 顺序是否一致；
- slippage 是否生效；
- deadline 是否合理；
- approve 目标是否是正确 Router / Position Manager；
- 用户切链后是否刷新地址和数据；
- 交易失败是否有清晰提示；
- 是否处理了 Pool 不存在的情况；
- 是否处理了用户没有仓位的情况；
- 是否避免把 `amountOutMinimum` 设置成 0；
- 是否避免把 `amount0Min` / `amount1Min` 设置成 0。

## 最小完成标准

如果你能完成下面这些，就算完成了 Uniswap V3 对接基础版：

- [ ] 能根据 tokenA / tokenB / fee 查询 Pool；
- [ ] 能读取 Pool 当前价格和 tick；
- [ ] 能用 QuoterV2 获取 swap 报价；
- [ ] 能执行一次 exactInputSingle swap；
- [ ] 能查询用户持有的 V3 Position NFT；
- [ ] 能展示某个 tokenId 的仓位详情；
- [ ] 能添加一个 V3 流动性仓位；
- [ ] 能 decreaseLiquidity；
- [ ] 能 collect；
- [ ] 能 burn 一个空仓位 NFT。

## 推荐学习顺序

```text
1. 先做只读：查 Pool、查价格、查仓位
2. 再做 swap：quote、approve、swap
3. 再做 LP：mint、increase、decrease、collect、burn
4. 最后优化体验：路由、滑点、错误提示、索引服务
```

不要一上来就做完整 LP 管理。

先把只读和 swap 跑通，最容易验证。

## 参考

- Uniswap v3 部署地址：<https://developers.uniswap.org/docs/protocols/v3/deployments>
- Ethereum v3 部署地址：<https://developers.uniswap.org/docs/protocols/v3/deployments/v3-ethereum-deployments>
- Uniswap v3 SDK：<https://developers.uniswap.org/docs/sdks/v3/overview>
- v3 Quote 指南：<https://developers.uniswap.org/docs/sdks/v3/guides/swapping/quoting>
- v3 Swap 指南：<https://developers.uniswap.org/docs/sdks/v3/guides/swapping/swapping>
- v3 Position 查询：<https://developers.uniswap.org/docs/sdks/v3/guides/managing-liquidity/position-fetching>
