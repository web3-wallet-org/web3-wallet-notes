# 06 Tick Rounding

原文：<https://learnblockchain.cn/article/23812?course_id=100>

这一节讲一个很细但很重要的问题：tick 取整。

先记住一句话：

```text
有了 tickSpacing 以后，用户选择的 tick 不一定能直接用，必须对齐到合法 tick。
```

比如 `tickSpacing = 60`，合法 tick 只能是：

```text
..., -120, -60, 0, 60, 120, ...
```

如果用户通过价格算出来的 tick 是 `73`，那它不能直接作为流动性边界。系统要把它四舍五入到附近可用的 tick，比如 `60`。

## 这节在解决什么

前面引入了 `tickSpacing`。

这带来一个约束：

```text
LP 的 lowerTick 和 upperTick 必须是 tickSpacing 的倍数。
```

但用户在前端输入的是价格，不是 tick。

价格转 tick 后，很可能不是合法边界。

所以前端和测试里都需要一个工具函数：

```text
nearestUsableTick
```

它的作用是：

```text
把任意 tick 调整成离它最近的合法 tick。
```

## JavaScript 里的逻辑

核心公式是：

```text
Math.round(tick / tickSpacing) * tickSpacing
```

比如：

```text
tick = 73
tickSpacing = 60
73 / 60 = 1.216...
round 后是 1
1 * 60 = 60
```

所以 `73` 会变成 `60`。

再比如：

```text
tick = 95
tickSpacing = 60
95 / 60 = 1.583...
round 后是 2
2 * 60 = 120
```

所以 `95` 会变成 `120`。

## 为什么要处理最大最小 tick

tick 有全局边界：

```text
MIN_TICK
MAX_TICK
```

四舍五入以后，结果可能越界。

如果向下越过最小 tick，就加一个 `tickSpacing` 拉回来。

如果向上超过最大 tick，就减一个 `tickSpacing` 拉回来。

目的只有一个：

```text
最终 tick 既要对齐 tickSpacing，又不能超过 V3 允许的 tick 范围。
```

## 前端什么时候用它

用户添加流动性时会输入价格范围。

前端大概会经历：

```text
用户输入价格
价格转 sqrtPriceX96
sqrtPriceX96 转 tick
tick 用 nearestUsableTick 对齐
把 lowerTick / upperTick 传给合约
```

如果不对齐，合约里初始化 tick 或 mint 流动性时就可能失败。

更好的用户体验是：用户调整价格时，前端就马上展示对齐后的真实价格范围。这样用户不会以为自己选的是 `73`，实际上链上用的是 `60`。

## Solidity 测试里为什么也需要

合约测试里也会构造价格范围。

如果测试代码直接拿一个未对齐的 tick 去 mint，测试会因为 tickSpacing 校验失败。

所以测试里也要有类似 JavaScript 的 `nearestUsableTick`。

麻烦点在于 Solidity 里的定点数库不一定提供 JavaScript `Math.round` 那样的函数，所以课程里自己写了一个 `divRound`。

它做的事情就是：

```text
先做除法
再看小数部分是否 >= 0.5
如果是，就往上进一位
```

然后再乘回 `tickSpacing`。

## 人话版理解

`tickSpacing` 像网格间距。

如果网格线只画在：

```text
0, 60, 120, 180
```

那用户点在 `73` 的位置时，系统不能真的把边界放在 `73`。

它只能把这个点吸附到最近的网格线上：

```text
73 -> 60
95 -> 120
```

这就是 Tick Rounding。

## 容易踩的点

第一，当前价格 tick 和流动性边界 tick 不是一回事。

当前价格可以很精细，不一定是 `tickSpacing` 的倍数。

但 LP 设置的 `lowerTick` 和 `upperTick` 必须对齐。

第二，取整会改变用户实际提供流动性的价格范围。

所以前端最好显示取整后的结果，否则用户会有误解。

第三，负数 tick 也要小心。

不同语言里的 round、除法、取余对负数的行为可能有差异。测试和前端最好用同一套规则，避免边界 case 出现不一致。

## 你要记住

- `tickSpacing > 1` 时，流动性边界 tick 不能随便选。
- `nearestUsableTick` 用来把 tick 对齐到最近的合法 tick。
- 核心思路是：`round(tick / tickSpacing) * tickSpacing`。
- 对齐后还要保证没有超过 `MIN_TICK` / `MAX_TICK`。
- 前端和测试都需要这个逻辑。
- 当前价格可以不对齐，但流动性范围边界必须对齐。

## 学完这节要能回答

- 为什么引入 `tickSpacing` 后需要 tick rounding？
- 为什么用户输入价格后不能直接拿转换出的 tick 去 mint？
- `nearestUsableTick` 的核心公式是什么？
- 当前价格 tick 和流动性边界 tick 有什么区别？
- 为什么前端应该显示对齐后的真实价格范围？
