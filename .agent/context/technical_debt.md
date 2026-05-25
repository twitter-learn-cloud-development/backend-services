# Technical Debt (已知技术债与防踩坑指南)

在开发、维护与重构本项目时，必须注意以下已知的技术债与架构缺陷。请避免在后续的代码生成和业务开发中继续引入类似的设计。

---

## 1. Gateway 职责泄漏与 DB 直接耦合
* **现状描述**：网关层（Gateway）不仅仅做路由转发与认证，还依赖了 `*gorm.DB` 并且在 Handler 层直接编写了 SQL 查询和业务逻辑。
* **物理位置**：
  * [tweet_handler.go](file:///e:/GOProject/云原生/twitter-clone/internal/gateway/handler/tweet_handler.go) 中的 `NewTweetHandler`、`db *gorm.DB` 成员以及下面的 SQL 聚合统计逻辑。
  * [bookmark_handler.go](file:///e:/GOProject/云原生/twitter-clone/internal/gateway/handler/bookmark_handler.go) 中直接访问 DB 进行书签增删改查。
  * [notification_handler.go](file:///e:/GOProject/云原生/twitter-clone/internal/gateway/handler/notification_handler.go) 直接读写通知表。
* **防踩坑指南**：
  * 当需要为推文、书签、通知等新增接口或修改字段时，**禁止**在 Gateway 的 Handler 中直接使用 `db.Find()` 或 `db.Create()`。
  * 应当在对应的 gRPC 接口定义中添加契约，将数据读取下沉到具体的微服务（如 `Tweet-Service`、`Notification-Service`）中，Gateway 仅充当 RPC Client 代理。

---

## 2. Timeline 纯写扩散（Push）模型的“大V崩点”
* **现状描述**：当前 Timeline 系统采用单一的写扩散模型。每当用户发布推文，`Timeline-Consumer` 会遍历该用户的所有关注者（Followers）并依次写入 Redis ZSet。对于拥有百万级粉丝的“大V”（KOL），这会产生瞬间的高并发 Redis 写入压力，导致消费队列严重阻塞或 Redis CPU 飙升。
* **物理位置**：
  * [timeline_consumer.go](file:///e:/GOProject/云原生/twitter-clone/internal/mq/consumer/timeline_consumer.go) 中的写扩散核心逻辑。
* **防踩坑指南**：
  * 对粉丝量极大的用户，未来开发需要引入 **Hybrid（推拉结合）模型**。
  * 当大V发布推文时，不进行写扩散（不推送到 Followers 的 ZSet），而是仅写入大V个人的 Outbox ZSet。
  * 普通用户读取 Timeline 时，结合读取自己的 Inbox ZSet 和所关注大V的 Outbox ZSet 进行内存多路归并。

---

## 3. RabbitMQ 消费失败的“重试风暴”
* **现状描述**：在消息消费失败时，直接调用了 `msg.Nack(false, true)` 重新放回队列头部。如果错误是持久性错误（如格式非法）或下游服务瞬时宕机，这会导致消息瞬间被无限循环重复拉取消费，产生重试风暴（CPU 跑满，日志暴涨）。
* **物理位置**：
  * [timeline_consumer.go](file:///e:/GOProject/云原生/twitter-clone/internal/mq/consumer/timeline_consumer.go) 中的 `msg.Nack(false, true)` 逻辑。
* **防踩坑指南**：
  * 遇到业务逻辑错误（如参数错误）时，应直接丢弃 `msg.Nack(false, false)`。
  * 遇到网络或下游临时不可用时，**禁止**使用 `Nack(true)` 直接重回队列头部。应该将其投递至**死信队列 (DLX)**，或使用 RabbitMQ 延迟插件进行指数退避（Exponential Backoff）延迟重试。

---

## 4. Agent 服务未实现沙箱隔离与连接池化
* **现状描述**：`Agent-Service` 运行用户自定义的代码或执行插件时，缺乏严格的容器化/微沙箱隔离。同时，在大模型交互过程中，可能存在未进行池化管理的 LangChain/网络连接，高并发下会导致资源泄露或端口耗尽。
* **物理位置**：
  * [cmd/agent-service](file:///e:/GOProject/云原生/twitter-clone/cmd/agent-service) 与 [internal/module/agent/](file:///e:/GOProject/云原生/twitter-clone/internal/module/agent/)。
* **防踩坑指南**：
  * 在扩展 Agent 功能（如调用 Shell、运行 Python 脚本、操作本地数据库）时，必须通过隔离沙箱运行，绝对不可直接在宿主机进程执行。
  * 针对外部 LLM/Embedding API 接口的 HTTP 客户端，必须配置 `IdleConnTimeout` 与 `MaxIdleConnsPerHost`，建立连接池。

---

## 5. Elasticsearch 同步保障缺失
* **现状描述**：推文持久化写入 MySQL 之后，同步写入 Elasticsearch 的逻辑是异步通过 MQ 分发的。如果 MQ 发生消息丢失，或者 ES 写入失败，系统没有任何数据对账或补偿机制，会导致用户搜索不到已发布的推文。
* **物理位置**：
  * [timeline_consumer.go](file:///e:/GOProject/云原生/twitter-clone/internal/mq/consumer/timeline_consumer.go)。
* **防踩坑指南**：
  * 必须建立定时对账任务，或者使用 CDC (如 Debezium/Canal) 捕获 MySQL binlog 来保证最终一致性。
  * ES 写入失败时，应当有本地错误重试队列，不要丢失更新。
