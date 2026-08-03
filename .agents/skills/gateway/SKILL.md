---
name: gateway
description: 保持 Gateway 为薄接入层，确保认证、契约转换、超时和服务发现边界清晰。用于 HTTP API、WebSocket、JWT、中间件、限流、Consul 或 gRPC Client 任务。
---

# Gateway Skill

## 先读

- `internal/gateway/router/`
- `internal/gateway/handler/`
- `internal/gateway/client/`
- `internal/gateway/middleware/`
- 对应 `api/<domain>/v1/*.proto`

## 执行步骤

1. 从 Route 确认 HTTP 方法、路径、鉴权和 Handler。
2. 从 Handler 追到 gRPC Client 与 Proto，不在 Handler 新增 SQL/GORM。
3. 定义状态码、错误映射、超时、分页/Cursor 和空结果语义。
4. 检查 JWT user_id 是否由可信 Context 注入，禁止信任客户端自报身份。
5. 检查 Consul 发现、连接复用、服务不可用降级和 WebSocket 生命周期。
6. 同步 Web/Mobile API 类型与接口文档。

## 项目不变量

- Gateway 不拥有业务数据，不新增 Repository/DB 写路径。
- Proto 是内部契约，HTTP DTO 不应无意暴露内部字段或密钥。
- 所有下游调用必须有 Context timeout；重试仅用于幂等请求。
- 分页使用稳定 Cursor；禁止为了“无限滚动”一次取全量。
- 认证、限流、Tracing 在 Middleware/Interceptor 统一实现。

## 反模式

- Handler 直接 `db.Find/Create`。
- 一个 HTTP 请求串行扇出多个服务且没有超时/并发上限。
- 用 HTTP 200 包装所有业务错误。
- WebSocket goroutine 无取消、无心跳、无背压。
- 在日志输出 JWT、密码或完整请求密钥。

## 验证

- Router/Handler 单测、gRPC Client Fake、鉴权失败和下游超时。
- 修改 Proto 时执行 API Contract Skill。
- 检查 Gateway 启动、Consul fallback 和 `go vet`。
