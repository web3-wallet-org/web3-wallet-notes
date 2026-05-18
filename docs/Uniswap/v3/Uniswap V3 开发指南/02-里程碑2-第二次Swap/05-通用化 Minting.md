# 05 通用化 Minting

这一节要做的事很明确：

> 把 `mint` 从“写死 token 数量”，升级成“根据当前价格动态计算需要存入多少 token”。

## 之前的问题

里程碑 1 里，`mint` 里的 `amount0`、`amount1` 是提前算好的。  
这对学习很友好，但不是真实协议的写法。

真实情况应该是：

- 给定流动性 `L`
- 给定当前价格
- 给定上下边界 tick
- 合约自己算出要收多少 `token0` 和 `token1`

## 这一节的两个关键改动

### 1. 初始化 tick 时同步更新 bitmap

`ticks.update(...)` 现在不只更新流动性，还会返回一个 `flipped`：

- `true`：这个 tick 的初始化状态发生了变化
- `false`：状态没变

如果 `flipped` 为 `true`，就要同步调用 `tickBitmap.flipTick(...)`。

## 2. 动态计算 `amount0` 和 `amount1`

核心不再是硬编码，而是调用：

```solidity
Math.calcAmount0Delta(...)
Math.calcAmount1Delta(...)
```

这两个函数本质上是在做：

- 根据价格区间
- 根据流动性
- 反推出需要存入两种 token 各多少

## 这一节真正想教你的东西

### mint 不是“随便往池子里打钱”

而是：

- 你声明想放多少流动性
- 协议根据价格区间算出你该补多少 `token0/token1`

### V3 的核心输入是流动性，不是代币数量

这是很多人第一次学 V3 最容易混的点。

用户操作上看起来像“存 ETH 和 USDC”，但协议内部更关心的是：

> 这笔仓位对应多少流动性 `L`

## 记住这节的结论

- `mint` 要从当前价格动态推导代币数量。
- 初始化 tick 时，除了更新 tick 信息，还要更新 bitmap。
- `calcAmount0Delta/calcAmount1Delta` 是 V3 中非常基础的积木。
