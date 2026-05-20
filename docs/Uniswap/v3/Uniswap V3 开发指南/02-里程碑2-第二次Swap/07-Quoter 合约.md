# 07 Quoter 合约

这一节可以先用一句话理解：

> `Quoter` 用来在不真正成交的情况下，提前算出一次 swap 大概能换到多少。

也就是前端里常见的：

```text
你输入：1 ETH
页面显示：预计收到 2100 USDC
```

这个“预计收到多少”，就需要 `Quoter` 来算。

## 为什么需要 Quoter

在 Uniswap V2 里，报价相对简单。  
因为池子的流动性都在同一个价格曲线上，可以直接根据公式估算输出。

但 V3 不一样。

V3 的流动性被放在不同价格区间里：

```text
1800 - 2000 有一批流动性
2000 - 2200 有一批流动性
2200 - 2500 又有另一批流动性
```

一笔 swap 到底能换出多少，取决于它会不会跨过不同价格区间。  
所以最可靠的办法不是前端自己手算，而是：

> 让 Pool 按真实 swap 逻辑跑一遍，然后把结果拿出来。

## Quoter 做的事情

`Quoter` 本质上是一个辅助合约。

它的核心函数是：

```solidity
quote(params)
```

参数里主要有三个东西：

```text
pool        要询价的池子
amountIn    用户准备卖出的数量
zeroForOne  兑换方向
```

这三个参数刚好够模拟一次 swap：

```text
在哪个池子换
卖多少
从 token0 换 token1，还是从 token1 换 token0
```

## 它为什么要调用真实 swap

因为 V3 的报价就是一次“模拟成交”。

`Quoter` 会调用：

```solidity
pool.swap(...)
```

Pool 会像真实交易一样执行 swap 逻辑：

```text
读取当前价格
根据方向移动价格
计算输入消耗
计算输出数量
更新临时状态
触发 swap callback
```

到 callback 的时候，Pool 已经算出了这次 swap 的关键结果。

## callback 里拿到了什么

在 `uniswapV3SwapCallback(...)` 里，`Quoter` 可以拿到：

```text
amount0Delta
amount1Delta
```

这两个值表示：

```text
池子需要收到多少 token
池子会付出多少 token
```

从这里就能推导出 `amountOut`：

```text
如果 amount0Delta > 0
说明用户输入 token0，输出就是 -amount1Delta

否则
说明用户输入 token1，输出就是 -amount0Delta
```

它还会读取池子的 `slot0`，拿到 swap 后的：

```text
sqrtPriceX96After
tickAfter
```

也就是：

```text
这次 swap 后的新价格
这次 swap 后的新 tick
```

## 最绕的地方：为什么要 revert

这里是本节最重要的点。

`Quoter` 不是要真的完成交易，它只是要报价。

所以它在 callback 里拿到结果后，会立刻 `revert`。

这个 `revert` 同时做了两件事：

```text
1. 撤销前面 swap 对 Pool 状态的修改
2. 把 amountOut、sqrtPriceX96After、tickAfter 作为 revert data 带出去
```

所以这里的 `revert` 不是出错，而是一种技巧：

> 用真实 swap 算结果，再用 revert 撤销交易，并把结果带回给 quote。

## quote 里怎么拿到结果

`quote` 会用 `try/catch` 包住 `pool.swap(...)`：

```text
try 调用 pool.swap
swap 在 callback 里主动 revert
catch 捕获 revert data
decode 出 amountOut、sqrtPriceX96After、tickAfter
返回给前端
```

所以完整流程是：

```text
前端调用 quote
  -> Quoter 调用 pool.swap
  -> Pool 执行真实 swap 计算
  -> Pool 调用 Quoter 的 callback
  -> Quoter 在 callback 里拿到报价结果
  -> Quoter 主动 revert，并把结果塞进 revert data
  -> quote 捕获 revert data
  -> decode 后返回报价
```

## 为什么还用了 Yul

原文里 callback 最后用了 Yul 写 `revert` 数据。

你现在不用死磕 Yul，只要知道它在做一件事：

```text
把 amountOut、sqrtPriceX96After、tickAfter 按 32 字节一组写进内存
然后 revert(ptr, 96)
```

为什么是 `96`？

因为有 3 个值：

```text
amountOut           32 bytes
sqrtPriceX96After   32 bytes
tickAfter           32 bytes
总共                96 bytes
```

这样外层 `quote` 才能用 `abi.decode(...)` 把它们解出来。

## 为什么 quote 不是 view

这个地方也容易误解。

从结果看，`quote` 像是只读函数：

```text
只是问一下能换多少
不是真的成交
```

但从 EVM 规则看，它内部调用了 `pool.swap(...)`。  
而 `swap` 会修改 Pool 状态，所以它不是 `view`。

因此 `quote` 也不能声明成 `view`。

不过因为最后会 `revert`，所以状态修改会被撤销，最终不会真的改变链上状态。

## 前端调用时要注意什么

因为 `quote` 不是 `view`，前端库可能会把它当成一笔真实交易。

所以前端要用静态调用：

```js
quoter.callStatic.quote(...)
```

意思是：

```text
请节点帮我模拟执行一下
但不要真的发交易上链
```

这也是下一节 Web UI 会用到的关键点。

## 这一节你真正要带走什么

先记住这几个点就够了：

- `Quoter` 是给前端用的报价合约。
- V3 报价不能只靠简单公式，最可靠的是模拟真实 swap。
- `Quoter` 会调用真实的 `pool.swap(...)`。
- callback 里拿到 `amountOut`、新价格、新 tick。
- `revert` 用来撤销交易，同时把报价结果带出来。
- `quote` 不是 `view`，前端要用 `callStatic` 调用。

一句话总结：

> Quoter 的套路是：真实执行一次 swap 的计算过程，然后在 callback 里 revert，把报价结果带回来。
