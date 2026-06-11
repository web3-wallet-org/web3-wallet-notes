# Swap 模块图文档

这些文档用于辅助阅读 `src/swap/internal/swap` 代码，描述当前实现，不定义新的业务规则。

## 文档索引

- [01-整体架构图](./01-整体架构图.md)：看清 `HTTPHandler`、`Service`、`Repository`、`ChainClient`、`QuoteProvider` 的边界。
- [02-Swap主流程时序图](./02-Swap主流程时序图.md)：看前端从 quote 到 status 的接口调用顺序。
- [03-订单状态流转图](./03-订单状态流转图.md)：看 `StoredOrder.Status` 的主要状态和恢复规则。
- [04-Provider适配图](./04-Provider适配图.md)：看新增聚合器时要实现哪些 `QuoteProvider` 方法。
- [05-Allowance-Approve授权图](./05-Allowance-Approve授权图.md)：看 `spender`、`allowance`、`approve` 的关系。
- [06-数据模型关系图](./06-数据模型关系图.md)：看 `NormalizedQuote`、`StoredOrder`、`SwapEvent` 的关联。

## 代码入口

- HTTP 路由入口：`src/swap/internal/swap/http.go`
- 核心业务流程：`src/swap/internal/swap/service.go`
- Provider 接口与数据类型：`src/swap/internal/swap/types.go`
- 内存存储实现：`src/swap/internal/swap/repository.go`
- 链上 RPC 查询：`src/swap/internal/swap/rpc.go`

