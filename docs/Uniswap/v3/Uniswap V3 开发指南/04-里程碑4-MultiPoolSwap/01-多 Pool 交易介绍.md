# 01 多 Pool 交易介绍

原文：<https://learnblockchain.cn/article/23809?course_id=100>

这一章的目标可以先压成一句话：

```text
把“只能在一个池子里换币”的交易，升级成“可以穿过多个池子完成一次换币”。
```

前面几章已经把单个 Pool 做得越来越像 Uniswap V3 了：有价格区间、有 tick、有跨 tick swap、有滑点保护。

但还有一个很现实的问题：

```text
如果没有 WETH/WBTC 这个池子，用户是不是就不能把 WETH 换成 WBTC？
```

真实 DEX 当然不是这样。它可以走中间资产，比如：

```text
WETH -> USDC -> USDT -> WBTC
```

用户看到的是一次兑换，但底层其实做了多次 swap。

## 这章在解决什么

之前的交易模型是：

```text
用户 -> 一个 Pool -> 输出 token
```

这一章要变成：

```text
用户 -> Pool A -> Pool B -> Pool C -> 最终输出 token
```

每个 Pool 只负责自己那一小段兑换。上一段 Pool 的输出，会变成下一段 Pool 的输入。

这就是多 Pool 交易，也可以理解成链式交易。

## 为什么需要多 Pool

因为现实里不可能每两个 token 之间都有一个流动性很好的直接池。

比如用户想换：

```text
WETH -> WBTC
```

如果没有直接池，但有下面这些池：

```text
WETH/USDC
USDC/USDT
USDT/WBTC
```

那就可以绕路完成交易。

这和现实生活里换汇有点像：你手上没有人民币兑某种小币种的市场，可能要先换美元，再换目标币种。用户关心的是最终能不能换到，路由系统关心的是中间怎么走。

## 这一章会补哪些能力

这一章不是改 Pool 的数学核心，而是围绕 Pool 外面加一层“组织能力”。

主要有几块：

1. `Factory`：统一创建和登记 Pool。
2. `Path`：把多段交易路线编码成一个字节路径。
3. `Manager`：按路径一段一段执行 swap。
4. `Quoter`：按路径一段一段模拟报价。
5. 前端 Router：帮用户找到两个 token 之间能走的路径。
6. `tickSpacing`：让不同 Pool 可以使用不同的 tick 间距。

## 人话版理解

把每个 Pool 想成一段路。

以前的系统只会问：

```text
有没有一条直达路？
```

如果没有，就结束。

这一章要让系统学会问：

```text
没有直达路的话，能不能中转？
```

比如：

```text
WETH 到 WBTC 没直达
WETH 到 USDC 有路
USDC 到 USDT 有路
USDT 到 WBTC 有路
那就可以走 WETH -> USDC -> USDT -> WBTC
```

Pool 本身不用知道整条路。它只负责当前这一段。真正管理路线的是 Manager、Path、Router 这些外层模块。

## 这一章最核心的变化

最核心的变化不是“多调用几次 swap”这么简单，而是系统边界变了：

```text
Pool 负责单池交易
Manager 负责串联多个 Pool
Router 负责找路径
Factory 负责让 Pool 可发现
Path 负责描述路线
```

这就是 Uniswap 真实架构里很重要的一层分工。

如果把多 Pool 逻辑塞进 Pool 合约，Pool 会变得很臃肿。更合理的方式是：Pool 保持专注，只做自己的数学；外面的合约负责组合多个 Pool。

## 你要记住

- 多 Pool 交易的本质是：上一池输出，下一池输入。
- 用户看到的是一次 swap，底层可能执行了多次 swap。
- Pool 不负责找路，也不负责理解整条路径。
- `Factory` 让 Pool 可以被统一创建、登记和计算地址。
- `Path` 是多池交易的路线说明书。
- `tickSpacing` 也属于 Pool 身份的一部分，同一对 token 可以有不同 tickSpacing 的池。

## 学完这节要能回答

- 为什么只有单个 Pool 还不算完整 DEX？
- 为什么没有直接池时，还可以通过中间 token 完成交易？
- 多 Pool 交易里，Pool、Manager、Router 分别负责什么？
- 为什么这一章主要改外层合约，而不是继续改 Pool 核心？
