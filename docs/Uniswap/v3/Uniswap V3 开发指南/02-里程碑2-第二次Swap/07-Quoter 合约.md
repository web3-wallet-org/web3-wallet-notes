# 07 Quoter 合约

`Quoter` 的意义非常大：

> 它让前端在不真正成交的情况下，先拿到本次 swap 的预计输出。

## 为什么 V3 不能像 V2 那样简单报价

因为 V3 的流动性分散在很多价格区间里。  
一笔交易的最终输出量，依赖的是 pool 内部真实的 swap 过程。

所以最可靠的报价方式不是自己手算，而是：

> 直接调用真实 `swap` 逻辑做一次模拟。

## Quoter 的做法

### 第一步

调用 pool 的 `swap(...)`。

### 第二步

在 `uniswapV3SwapCallback(...)` 里拿到：

- `amountOut`
- `sqrtPriceX96After`
- `tickAfter`

### 第三步

立刻 `revert`，把这些值作为 revert data 带出去。

### 第四步

在 `quote(...)` 里 `catch` 这次回滚，再 decode 出结果并返回。

## 为什么它要这么绕

因为它想复用真实的成交路径，又不想真的改状态。

所以你可以把 Quoter 理解成：

> 借用 swap 的计算过程，但在最后一刻撤销整笔交易。

## 为什么 `quote` 不是 `view`

因为它内部调用了 `swap`，而 `swap` 不是 `view`。  
从 EVM 规则看，`quote` 也不能被标成 `view`。

但因为最后会 `revert`，所以链上状态最终不会留下变化。

## 前端调用时最重要的一点

必须用：

```js
quoter.callStatic.quote(...)
```

否则前端库会把它当成真实交易去发起。

## 这一节你应该带走的结论

- Quoter 不是算公式，而是模拟真实 swap。
- 它的套路是：`swap -> callback -> revert -> catch -> decode`。
- 它虽然不是 `view`，但本质上承担“只读报价”的职责。
