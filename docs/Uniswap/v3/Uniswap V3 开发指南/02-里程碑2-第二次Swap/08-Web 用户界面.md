# 08 Web 用户界面

这一节的目标是把前端补成一个真正能用的 swap 面板。

## 这次 UI 的 3 个升级

1. 用户可以输入任意卖出数量。
2. 用户可以切换 swap 方向。
3. 页面会实时显示预计输出。

## 页面结构应该怎么理解

表单其实只有三块：

- 上方输入框：用户输入卖出数量
- 中间切换按钮：切换 `zeroForOne`
- 下方输入框：展示预计收到多少

其中下方输入框必须是只读的，因为它来自报价，不是用户输入。

## 前端最核心的状态

```js
zeroForOne
amount0
amount1
loading
```

你只要理解这 4 个状态，就能看懂整个页面：

- `zeroForOne`: 当前交易方向
- `amount0/amount1`: 两个输入框显示的值
- `loading`: 正在请求报价

## 输入时为什么要请求 Quoter

因为真实 DEX 不会让用户自己估 output。  
用户输入 `amountIn` 后，前端应该立刻向 Quoter 要报价。

核心调用：

```js
quoter.callStatic.quote({
  pool,
  amountIn,
  zeroForOne
})
```

拿到 `amountOut` 后，再更新只读输入框。

## 为什么要加 `debounce`

用户输入时会连续触发很多次 `onChange`。  
如果每次都请求一次链上报价，会很卡。

所以要做防抖：

- 用户停一下再请求
- 只取最后一次输入的报价

## 点击 Swap 时真正要做什么

不是直接调 pool，而是调 manager：

```js
manager.swap(poolAddress, zeroForOne, amountInWei, extra)
```

并且在这之前要：

- 判断当前输入 token 是哪一个
- 检查 allowance
- 不够就先 `approve`

## 这节最容易错的点

方向切换时，下面这些都要一起切：

- 顶部输入框显示哪种 token
- 底部输出框显示哪种 token
- 本次 `amountIn` 用的是 `amount0` 还是 `amount1`
- 本次需要 `approve` 的 token 是哪一个

## 一句话总结

这一节真正教你的不是 React，而是：

> DEX 前端必须围绕链上报价、交易方向和 allowance 来组织交互。
