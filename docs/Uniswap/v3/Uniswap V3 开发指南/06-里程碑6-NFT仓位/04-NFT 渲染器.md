# 04 NFT 渲染器

原文：<https://learnblockchain.cn/article/23821?course_id=100>

这一节讲 NFT 怎么显示出来。

先记住一句话：

```text
渲染器不负责仓位逻辑，它只负责把仓位数据变成 metadata 和图片。
```

也就是说，Renderer 不改变流动性，不收费用，不转 token。它只是展示层。

## 这节在解决什么

ERC721 有一个 `tokenURI(tokenId)`。

前端、钱包、NFT 市场会通过它读取 NFT 展示信息。

对于普通 NFT，`tokenURI` 可能指向 IPFS 上的 JSON。

但 Uniswap V3 仓位 NFT 比较特殊：

```text
它的图片和属性可以根据仓位状态动态生成。
```

比如：

```text
交易对
fee
lowerTick
upperTick
当前是否在区间内
```

这些都可以展示在 NFT 图片和 attributes 里。

## tokenURI 在哪里实现

通常 `tokenURI` 会在 NFT Manager 里暴露，因为 Manager 是 ERC721 合约。

但 Manager 不一定自己拼 SVG。

更干净的方式是：

```text
Manager 读取仓位数据
Manager 把数据传给 Renderer
Renderer 返回 metadata
Manager 把 tokenURI 返回给外部
```

这样业务逻辑和展示逻辑分开。

## metadata 长什么样

NFT metadata 通常是 JSON：

```json
{
  "name": "Uniswap V3 Position",
  "description": "...",
  "image": "data:image/svg+xml;base64,...",
  "attributes": []
}
```

这里的 `image` 可以是一个 base64 编码后的 SVG。

这样整个 NFT 展示数据可以完全链上生成，不依赖外部服务器。

## 为什么要 base64

base64 的目的，是把 JSON 和 SVG 包成一段安全的字符串，方便 `tokenURI` 返回。

`tokenURI` 返回的是字符串。

如果直接塞 JSON 和 SVG，会遇到很多特殊字符转义问题。

所以常见做法是：

```text
SVG -> base64
JSON 里放 base64 image
JSON -> base64
tokenURI 返回 data:application/json;base64,...
```

这样钱包或 NFT 市场拿到 `tokenURI` 后，可以直接解析出 metadata，再显示图片。

## Renderer 需要哪些仓位数据

渲染器通常需要：

```text
token0
token1
fee
lowerTick
upperTick
liquidity
currentTick
```

有了这些，它就能生成一张表达仓位特征的图。

比如：

- 显示交易对名称；
- 显示手续费等级；
- 显示价格范围；
- 显示当前价格是否在范围内；
- 用颜色或形状区分不同 token。

课程里的实现会比较简化，但目的和真实 Uniswap 一样：

```text
让仓位 NFT 不只是一个编号，而是能被人看懂。
```

## 为什么渲染器不应该影响核心逻辑

仓位 NFT 的真正价值来自：

```text
Pool 里的流动性和可领取费用
```

图片只是展示。

如果渲染器写错，最多影响钱包里显示不好看。

但如果 Manager 或 Pool 写错，可能影响资金安全。

所以学习时要分清主次：

```text
Pool / Manager 是资金逻辑
Renderer 是展示逻辑
```

不要把图片生成看得比仓位账本更重要。

## 人话版理解

NFT 渲染器像房产证的封面设计。

房产证真正重要的是：

```text
这套房属于谁
地址在哪里
面积多少
能不能交易
```

封面图片只是让人一眼看懂这张证对应什么资产。

V3 仓位 NFT 也是一样。

Renderer 只是把：

```text
哪个池子
哪个价格范围
多少手续费等级
当前价格状态
```

画出来。

## 你要记住

- `tokenURI` 是 NFT 展示入口。
- metadata 通常包含 name、description、image、attributes。
- 图片可以用链上 SVG 动态生成。
- base64 是为了把 JSON / SVG 安全放进字符串。
- Renderer 只负责展示，不负责资金逻辑。
- Manager 负责读取仓位并调用 Renderer。
- 仓位 NFT 的核心价值来自底层流动性，不是图片。

## 学完这节要能回答

- `tokenURI` 返回的是什么？
- 为什么仓位 NFT 可以动态生成图片？
- 为什么要把 SVG 和 JSON 做 base64 编码？
- Renderer 和 NFT Manager 的职责有什么区别？
- 为什么渲染器错误通常不等于资金逻辑错误？
