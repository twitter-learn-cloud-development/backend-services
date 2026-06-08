# Twitter Clone V2 全局源码学习与架构拆解每日学习计划 (20天终极训练营)

欢迎来到 **Twitter Clone V2** 云原生架构与智能体治理深度学习训练营！本指南由您的专属架构导师设计，旨在帮助您从代码底层、并发控制、服务治理、AIOps 自愈以及 AI Agent Mesh 层面，彻底吃透这一生产级大型分布式社交平台的精髓。

整个训练营为期 **20 天**，分为三大核心阶段，所有内容均严格按照“请求链路、核心重点、技术亮点、避坑指南、进阶玩法、动手挑战”的生产级标准进行深度解析：

---

## 📅 20天学习日程总览

### 🛑 第一阶段：社交系统核心微服务拆解 (Day 1 - Day 10)
> 剖析 Twitter Clone 的底层核心业务架构，涵盖从 BFF 网关到用户、推文、点赞、关注、实时通知及 WebSocket 私信系统的完整微服务读写链路。
>
> 📖 详细拆解文档：[Part 1: 社交系统核心微服务拆解 (Day 1 - Day 10)](file:///e:/GOProject/云原生/twitter-clone/docs/learn/part1_social_base.md)

- **🗓️ Day 1: BFF API Gateway 网关路由转发与统一 JWT 鉴权隔离**
- **🗓️ Day 2: User Service 用户注册登录、密码哈希与雪花 ID 精度纠偏**
- **🗓️ Day 3: User Service 个人资料卡更新、头像媒体文件上传与安全边界**
- **🗓️ Day 4: Tweet Service 推文极速发布、富文本解析与事件解耦**
- **🗓️ Day 5: Tweet Service 评论系统架构设计（二级树形评论与总数统计）**
- **🗓️ Day 6: Like System 高并发点赞（Redis 读写缓存与 RabbitMQ 异步落盘）**
- **🗓️ Day 7: Bookmark System 书签系统与 Retweet 转发多态表映射**
- **🗓️ Day 8: Follow Service 关注系统底层的社交图谱关系与粉丝列表设计**
- **🗓️ Day 9: Notification System 实时互动通知网关设计（MQ + WebSocket）**
- **🗓️ Day 10: Messenger System 社交私信系统架构（WebSocket 单聊与离线消息拉取）**

---

### 🛑 第二阶段：百万高并发 Feed 流与 AI Agent Mesh 网络 (Day 11 - Day 15)
> 深入剖析如何构建 Zero-GC 读写扩散混合 Feed 流，以及基于 MCP 协议打造自愈、双路降级的多智能体协同网络。
>
> 📖 详细拆解文档：[Part 2: 百万高并发 Feed 流与 AI Agent Mesh 网络 (Day 11 - Day 15)](file:///e:/GOProject/云原生/twitter-clone/docs/learn/part2_high_concurrency.md)

- **🗓️ Day 11: Feed 流多级缓存引擎与防击穿 (GetFeeds 模块)**
- **🗓️ Day 12: 大V推特混合 Feed 流与防抖架构 (Celebrity Push/Pull Hybrid)**
- **🗓️ Day 13: ES/Qdrant 向量发件箱 Worker 双写异步同步器 (Transactional Outbox)**
- **🗓️ Day 14: AI Agent 模式二 RAG 搜索与双引擎优雅降级 (RAG Search & Fallback)**
- **🗓️ Day 15: AI Agent 模式三与模式四多智能体协同网络 (Agentic Mesh & SSE)**

---

### 🛑 第三阶段：分布式状态机编排与 AIOps 混沌网格自愈 (Day 16 - Day 20)
> 探索在高并发混沌故障注入下，如何通过 Temporal、Sentinel-Go、Istio 以及 K8s 控制面动态切流建立全自动自愈防线。
>
> 📖 详细拆解文档：[Part 3: 分布式状态机编排与 AIOps 混沌网格自愈 (Day 16 - Day 20)](file:///e:/GOProject/云原生/twitter-clone/docs/learn/part3_sagas_and_ops.md)

- **🗓️ Day 16: AIOps 持续性能剖析与配置自适应智能热调优 (Profiling & Tuning)**
- **🗓️ Day 17: Temporal 分布式 Saga 风控工作流编排与原子 Lua 洗地 (Temporal Saga)**
- **🗓️ Day 18: Sentinel-Go 流量网格熔断限流与静态/动态规则合并 (Sentinel-Go)**
- **🗓️ Day 19: OTel 全链路追踪跨协程延续与 PLG 日志级联 (Observability)**
- **🗓️ Day 20: 混沌测试、K8s 动态切流与 CI/CD 自动化 GitOps 大阅兵 (Chaos & CI/CD)**

---

## 💡 导师学习建议

1. **先读契约，后看逻辑**：在每一天开始前，建议先阅读相关的 `.proto` 文件，了解服务间的接口通信结构，再去深入阅读 Service 代码。
2. **注重课后挑战**：每一天都为您留有极富启发性的“动手挑战”任务。请务必在本地环境实际修改代码并利用回归脚本/日志进行验证，这能将理论直接转化为您的实战经验。
3. **保持全局观**：注意观察缓存一致性广播与 AIOps 自愈动态重载等跨服务的联动，这是构建高弹性、可自治系统的关键。
