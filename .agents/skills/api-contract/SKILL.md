---
name: api-contract
description: 以向后兼容方式同步 Proto、服务端、Gateway、Web、Mobile 和接口文档。用于 Proto、gRPC、Gateway HTTP、DTO、字段、分页、错误码或跨端接口变更。
---

# API Contract Skill

## 先读

- 对应 `api/<domain>/v1/*.proto`
- 模块 `grpc/` 实现
- `internal/gateway/client/handler/router/`
- `web/src/api/` 与 Mobile 对应 datasource/model
- `docs/API_REFERENCE.md`

## 执行步骤

1. 写清消费者、生产者、字段语义、默认值、权限和错误。
2. Proto 只追加字段/方法；不复用已发布 Field Number，不随意重命名语义。
3. 重新生成 `*.pb.go`/`*_grpc.pb.go`，禁止手改生成文件。
4. Service 校验身份/所有权；Gateway 只转换协议与错误。
5. 前端类型、空值、Cursor/has_more、时间和 uint64/string ID 同步适配。
6. 更新 API_REFERENCE；有迁移时记录双读/双写和回滚。

## 兼容检查

- 旧客户端省略新字段时行为是否保持。
- 新服务读取旧 Mongo/MySQL 记录时零值是否安全。
- `uint64` 经 JavaScript 是否改用 string，避免精度丢失。
- Enum 新值是否有 unknown/default 分支。
- 分页是否稳定，错误码是否可被客户端区分。
- 敏感字段是否被 Proto/JSON/日志意外暴露。

## 验证

- Proto 生成 + Go 编译。
- gRPC compatibility contract tests。
- Gateway Handler/Client Fake。
- Web `npm run build`；Mobile `flutter analyze/test`（受影响时）。
- 文档示例与实际字段一致。
