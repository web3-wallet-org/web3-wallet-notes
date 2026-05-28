# Uniswap V3 总复盘

这篇不是重新讲一遍所有细节，而是把整套 Uniswap V3 串成一张全局地图。

学完前面的里程碑后，最重要的是能回答一个问题：

```text
用户的一次操作，最后到底改了哪些合约里的哪些状态？
```

只要这条线清楚，V3 的概念就不会散。

## 一句话理解 Uniswap V3

Uniswap V3 的核心变化是：

```text
LP 不再把资金平均放在所有价格上，而是把流动性集中放在自己选择的价格区间里。
```

所以 V3 比 V2 多了几个关键概念：

- 价格区间；
- tick；
- 集中流动性；
- 手续费增长记录；
- NFT 仓位。

这些概念看起来多，但它们都围绕一个核心目标：

```text
让 LP 的资金只在指定价格范围内参与交易。
```

## 全局架构

可以先用这张图理解：

```text
用户
  |
  v
Router / NFT Position Manager
  |
  v
Pool
  |
  v
token0 / token1

NFT Position Manager
  |
  v
Renderer
  |
  v
tokenURI / metadata / SVG
```

最重要的分工是：

```text
Pool    = 资金和价格的核心账本
Manager = 用户管理仓位的入口
Router  = 用户做 swap 的入口
Renderer = NFT 展示层
```

其中资金安全主要在 `Pool` 和 `Manager`。

`Renderer` 只是展示，不负责转账、不改变流动性。

## Pool 是核心账本

`Pool` 是 Uniswap V3 最核心的合约。

它负责记录：

```text
当前价格 sqrtPriceX96
当前 tick
当前活跃流动性 liquidity
每个 tick 的状态
每个 position 的状态
费用增长记录
预言机观察值
```

用户看到的是“交易、加仓、减仓、收手续费”。

但从 Pool 角度看，本质都是在更新账本：

```text
swap    -> 改价格、改流动性、记手续费
mint    -> 增加某个价格区间的流动性
burn    -> 减少某个价格区间的流动性并结算
collect -> 把已经结算出来的 token 转走
```

## tick 是价格刻度

V3 不直接用任意价格做边界，而是用 tick 表示离散价格点。

可以理解成：

```text
tick = 价格尺子上的刻度
```

LP 选择价格范围时，最后都会变成：

```text
lowerTick
upperTick
```

这两个 tick 决定了流动性在哪个价格区间内生效。

如果当前价格在区间内：

```text
仓位活跃，可以赚手续费。
```

如果价格离开区间：

```text
仓位不活跃，不再赚新的 swap 手续费。
```

## sqrtPriceX96 是价格表示法

Solidity 没有浮点数，所以 V3 用定点数表示价格。

核心变量是：

```text
sqrtPriceX96
```

它表示的是：

```text
sqrt(price) * 2^96
```

为什么不用普通价格？

因为 V3 的流动性计算大量使用平方根价格。用 `sqrtPriceX96` 可以让计算更稳定，也更适合 Solidity 整数运算。

你不需要死记所有数学公式，但要记住：

```text
Pool 里的真实价格状态主要由 sqrtPriceX96 和 tick 表示。
```

## mint 是添加流动性

添加流动性时，用户不是简单地往池子里存两个 token。

用户要指定：

```text
哪个 Pool
哪个价格范围 lowerTick / upperTick
提供多少流动性
```

然后 Pool 会记录这个价格范围内新增了一份流动性。

在课程前半部分，前端和 Manager 需要通过这些信息定位仓位：

```text
owner
Pool
lowerTick
upperTick
```

后面引入 NFT 后，这些信息会被包装成：

```text
tokenId -> Position
```

## swap 是沿着 tick 改价格

V2 的 swap 可以理解成只改一个全局储备比例。

V3 的 swap 更复杂，因为流动性分布在不同价格区间。

交易发生时，Pool 会：

```text
1. 根据输入 token 计算价格移动
2. 在当前价格区间内消耗流动性
3. 如果价格跨过 tick，就切换到下一个流动性区间
4. 记录手续费
5. 更新 sqrtPriceX96、tick、liquidity
```

所以 V3 的 swap 本质是：

```text
价格沿着 tick 往前走，边走边使用当前区间的流动性。
```

## 费用是通过增长值记账

V3 不会在每次 swap 时逐个给 LP 转账。

这样太贵。

它采用的是记账方式：

```text
全局记录 feeGrowthGlobal
每个 tick 记录 feeGrowthOutside
每个 position 记录 feeGrowthInsideLast
```

当用户更新仓位时，合约再根据这些增长值算出：

```text
这个仓位这段时间应该拿多少手续费。
```

这些可领取的费用会累积到：

```text
tokensOwed0
tokensOwed1
```

## burn 和 collect 必须分开理解

这是 V3 里很容易混淆的点。

`burn` 不是提款。

`collect` 才是提款。

关系是：

```text
burn    = 减少流动性，并把能取多少 token 结算出来
collect = 把已经结算出来的 token 真正转出去
```

所以完整的移除流动性流程通常是：

```text
1. burn / decreaseLiquidity
2. 结算出 amount0 / amount1 或 tokensOwed
3. collect
4. 用户钱包收到 token0 / token1
```

如果只 `burn` 不 `collect`，token 还没有真正回到用户钱包。

## 预言机是价格历史记录

V3 的预言机不是外部喂价系统。

它是 Pool 自己记录的价格历史。

Pool 会保存 observation，用来查询过去一段时间的累计 tick。

外部可以通过这些数据计算 TWAP：

```text
Time Weighted Average Price
时间加权平均价格
```

它的作用是让其他合约能读取更稳定的价格，而不是只看当前瞬间价格。

## NFT 仓位解决的是管理入口

V3 仓位不是 ERC20，因为每个仓位都可能不同：

```text
Pool 不同
价格范围不同
liquidity 不同
费用状态不同
```

所以 V3 用 ERC721 NFT 表示仓位。

核心关系是：

```text
tokenId -> Position
```

用户不再需要记住：

```text
Pool
lowerTick
upperTick
owner
```

而是直接管理自己钱包里的 LP NFT。

## NFT Position Manager 做什么

NFT Position Manager 是用户管理 LP NFT 的入口。

它负责：

```text
mint NFT 仓位
increaseLiquidity
decreaseLiquidity
collect
burn NFT
tokenURI
```

但要注意：

```text
真正的流动性状态仍然在 Pool。
```

Manager 做的是包装和协调：

```text
用户操作 tokenId
Manager 找到 Position
Manager 调用 Pool
Manager 更新自己的 Position 记录
```

## NFT burn 不是 Pool burn

这两个名字很像，但意思完全不同。

```text
Pool.burn = 减少流动性
NFT burn  = 销毁 tokenId
```

NFT burn 的前提通常是：

```text
liquidity = 0
tokensOwed0 = 0
tokensOwed1 = 0
```

也就是说，必须先：

```text
1. decreaseLiquidity
2. collect
3. burn NFT
```

NFT burn <span style="color: red;">只销毁仓位凭证，不负责提款</span>。

## Renderer 只是展示层

Renderer 负责把仓位数据变成：

```text
metadata
SVG image
attributes
```

通常流程是：

```text
Manager 读取仓位数据
Manager 把数据传给 Renderer
Renderer 返回 metadata
Manager 把 tokenURI 返回给外部
```

它不负责：

```text
增加流动性
减少流动性
收手续费
转 token
```

所以要分清：

```text
Pool / Manager 是资金逻辑
Renderer 是展示逻辑
```

## 用户完整操作流程

### 添加流动性

```text
1. 用户选择 token0 / token1
2. 用户选择价格范围
3. 前端把价格转换成 lowerTick / upperTick
4. 用户输入 token 数量
5. Manager 调用 Pool.mint
6. Pool 增加流动性
7. Manager 铸造 NFT
8. 用户拿到 tokenId
```

### 增加已有仓位流动性

```text
1. 用户选择已有 tokenId
2. Manager 检查权限
3. Manager 找到 Position
4. Manager 调用 Pool.mint
5. Position.liquidity 增加
```

### 减少流动性并提款

```text
1. 用户选择 tokenId
2. Manager 检查权限
3. Manager 调用 Pool.burn
4. Pool 结算 amount0 / amount1
5. Manager 记录 tokensOwed
6. 用户调用 collect
7. token0 / token1 转回用户钱包
```

### 销毁空仓位 NFT

```text
1. liquidity 已经是 0
2. tokensOwed0 / tokensOwed1 已经是 0
3. 用户调用 burn NFT
4. tokenId 被销毁
```

## 最容易混淆的概念

### fee 和 tickSpacing

```text
fee = 手续费等级
tickSpacing = tick 的间隔
```

真实 Uniswap V3 里通常是：

```text
fee tier -> tickSpacing
```

课程实现里有时会用 `tickSpacing` 简化区分 Pool，但概念上二者不是一回事。

### liquidity 和 token 数量

`liquidity` 不是 token0 数量，也不是 token1 数量。

它是 V3 内部表示流动性规模的数值。

同样的 liquidity，在不同价格位置下，对应的 token0 / token1 数量可能不同。

### active liquidity 和用户总仓位

Pool 里的当前 `liquidity` 通常指当前价格区间内的活跃流动性。

用户的某个 position 里的 liquidity，是这个用户在某个价格范围里的仓位流动性。

不要把它们混成一个概念。

### burn 和 collect

```text
burn    = 结算
collect = 提款
```

如果用户问“为什么 burn 后钱包没收到 token”，答案通常是：

```text
还没有 collect。
```

### NFT 和图片

NFT 本体不是图片。

NFT 本体是链上的：

```text
tokenId
owner
Position
```

图片只是 `tokenURI` 里的展示内容。

## 最后检查自己是否学会

如果你能回答这些问题，说明 Uniswap V3 的主线已经打通：

- 为什么 V3 要引入集中流动性？
- tick 和价格是什么关系？
- 为什么要用 `sqrtPriceX96`？
- 一个 LP 仓位由哪些信息决定？
- swap 为什么可能跨 tick？
- 手续费为什么用 feeGrowth 记账？
- `burn` 和 `collect` 为什么要分开？
- 为什么 V3 仓位适合用 NFT 表示？
- `tokenId` 和 Position 是什么关系？
- NFT burn 和 Pool burn 有什么区别？
- Renderer 为什么不影响资金安全？

## 一句话收尾

Uniswap V3 可以这样记：

```text
Pool 负责资金账本，
tick 负责价格区间，
swap 沿着 tick 改价格，
mint / burn 改流动性，
collect 才真正提款，
NFT Manager 用 tokenId 包装仓位，
Renderer 只负责把仓位展示出来。
```
