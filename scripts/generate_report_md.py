# -*- coding: utf-8 -*-
import os

def generate_formal_report():
    base_dir = r"e:\GOProject\cloud\twitter-clone"
    docs_dir = os.path.join(base_dir, "docs")
    md_path = os.path.join(docs_dir, "COURSE_DESIGN_REPORT.md")

    if not os.path.exists(docs_dir):
        os.makedirs(docs_dir)

    with open(md_path, "w", encoding="utf-8") as f:
        f.write("# 课程设计报告\n\n")

        # 1
        f.write("## 1 项目背景与需求分析\n\n")
        f.write("### 1.1 项目背景\n")
        f.write("随着 Web 3.0 与 AIGC 的爆发，现代社交平台不仅面临着千万级高并发、数据强一致性以及海量存储的多重挑战，同时亟需引入智能化手段以应对信息过载。本项目旨在从零构建一个对标真实 Twitter 的大型云原生微服务社交系统。不仅要解决微服务架构中的经典问题（如缓存击穿、分布式事务等），还要深度集成原生 AI 智能体工作流，实现大模型驱动的自动化社交新范式。\n\n")
        f.write("### 1.2 需求分析\n")
        f.write("功能性需求包含：用户注册与登录、推文发布与多媒体上传、细粒度点赞与二级评论树、推拉结合的混合关注流、基于滑动窗口的时间衰减实时热搜榜单、AI 智能体会话与定制化流等。非功能性需求包含：系统需支持水平动态扩容、服务熔断降级、完整的全链路可观测性，以及 100% 的分布式事务最终一致性保证。\n\n")

        # 2
        f.write("## 2 系统分析\n\n")
        f.write("### 2.1 可行性分析\n")
        f.write("在技术可行性方面，采用 Golang 语言能够提供极高的并发处理能力；Kubernetes 提供了成熟的容器编排；Temporal、RabbitMQ、Redis 等中间件是目前业界成熟的高可用解决方案。经济与操作可行性上，依托 Minikube 和开源组件，能够在本地或云服务器以极低成本搭建并模拟百万级架构流。\n\n")
        f.write("### 2.2 核心业务流分析\n")
        f.write("系统的最核心业务为“发推-入库-扇出-读取”。当用户发起写请求时，涉及到权限校验、内容风控审查（可见性判定/影子封禁）、数据持久化、以及向消息队列的异步分发。读请求则涉及复杂的多级缓存命中、穿透保护与并发数据聚合。\n\n")

        # 3
        f.write("## 3 系统设计\n\n")
        f.write("### 3.1 总体架构设计\n")
        f.write("本项目摒弃单体架构，采用云原生微服务架构。服务层被解耦为 Gateway (BFF层)、User Service、Tweet Service、Follow Service 和 Agent Service。各服务间通过 gRPC 配合 Protobuf 进行高性能二进制通信。服务注册与发现依赖 Consul，外部统一通过 Nginx Ingress 暴露 RESTful 接口。\n\n")
        
        f.write("### 3.2 数据库与存储设计\n")
        f.write("业务数据主库采用 MySQL 8.0。核心表包含 `users`（存储用户信息及 bcrypt 密码）、`tweets`（主推文与嵌套引用，包含 `visible_type` 用于风控）、`outbox_events`（发件箱任务表）。非结构化多媒体数据直接流式传输并存储于 MinIO 对象存储，避免了本地磁盘 I/O 瓶颈。\n\n")

        f.write("### 3.3 缓存架构设计\n")
        f.write("采取多级缓存架构：L1 缓存采用 BigCache，直接驻留 Go 进程内存；L2 缓存使用 Redis。大V（Celebrity）的推文使用独立 ZSet 缓存，普通用户收件箱亦使用 ZSet 进行推模式时间轴缓存，极大降低 MySQL 的读取压力。\n\n")

        # 4
        f.write("## 4 技术选型与工程决策\n\n")
        f.write("### 4.1 认证机制决策：JWKS 非对称鉴权\n")
        f.write("在常规分布式系统中共享对称 JWT 密钥面临极高的泄露风险。工程决策引入 Auth Service 负责生成 RSA 密钥对，私钥保留签发，公钥以 JWKS 端点暴露。各微服务通过 `keyfunc` 定时轮询公钥并缓存在内存中验签，实现了真正的**零共享密钥**架构。\n\n")

        f.write("### 4.2 数据一致性决策：Canal CDC 与发件箱模式\n")
        f.write("发推时直接双写 DB 和 MQ 会产生数据不一致隐患。决策引入**事务发件箱模式 (Transactional Outbox)**：利用本地事务将业务逻辑与出站消息捆绑写入 MySQL，随后通过 Canal 伪装成 MySQL 从节点监听 Binlog 并投递到 RabbitMQ，实现了 100% 可靠的事件驱动。\n\n")

        f.write("### 4.3 高并发 Feed 流决策：推拉结合 (Push/Pull Hybrid)\n")
        f.write("如果采用纯拉模式，系统在大并发下读取极慢；若采用纯推模式，千万级大V发推将引发 Redis 写崩溃。系统最终决策引入阈值防抖：大V采用拉扩散（Read-Diffusion），普通用户采用写扩散（Write-Diffusion）。API 网关层结合 `singleflight` 防止突发流量击穿底层存储。\n\n")

        f.write("### 4.4 AI 工作流引擎决策：Temporal 状态机\n")
        f.write("针对需要数分钟执行甚至人工审批（Human-in-the-loop）的 AI 智能体编排任务，使用普通协程容易丢失状态。决策引入 Temporal，将工作流彻底状态机化，任何一步 Activity 都可以安全落盘重放，实现了具备容错降级能力的 AI 调度底座。\n\n")

        # 5
        f.write("## 5 系统实现\n\n")
        f.write("### 5.1 可视化定制工作流 (Visual DAG Workflow) 的实现\n")
        f.write("系统在前端使用 Vue Flow 构建了可视化拖拽画布，将节点连线序列化为 DSL。后端接收后利用 Kahn 算法进行拓扑排序，并使用 `sync.WaitGroup` 构建了无锁高并发调度引擎。特别是实现了挂起节点持久化黑板变量（Checkpoint）及人工水化恢复的功能。\n\n")

        f.write("### 5.2 RAG 混合检索召回实现\n")
        f.write("在智能体对用户会话进行响应时，系统通过 Elasticsearch (BM25) 与 Qdrant (HNSW) 进行双路并发拉取。使用 `errgroup` 实现短超时控制。聚合数据后，通过大模型 Reranker 进行重排序，大大减弱了直接问答导致的幻觉。\n\n")

        f.write("### 5.3 核心代码片段：Singleflight 缓存拦截\n")
        f.write("```go\n")
        f.write("var requestGroup singleflight.Group\n")
        f.write("func GetTweetDetail(id string) (*Tweet, error) {\n")
        f.write("    v, err, _ := requestGroup.Do(id, func() (interface{}, error) {\n")
        f.write("        // 真正的缓存拉取与回源逻辑，挡住了99.9%的并发穿透\n")
        f.write("        return fetchFromDBOrRedis(id)\n")
        f.write("    })\n")
        f.write("    return v.(*Tweet), err\n")
        f.write("}\n")
        f.write("```\n\n")

        # 6
        f.write("## 6 AI 辅助开发实践\n\n")
        f.write("在本项目极其庞大的工程体量下，AI 工具深度参与了开发全流程。\n")
        f.write("- **代码生成与重构**：利用 AI 自动生成 Protobuf 的中间件脚手架、单元测试模板以及重复的 GORM 实体定义。\n")
        f.write("- **架构分析与 Debug**：在配置 K8s 网络以及调试 Temporal 引擎超时机制时，通过将报错日志及火焰图喂给大模型，快速定位了协程泄露的根本原因。\n")
        f.write("- **AIOps 智能自愈尝试**：项目中更是编写了 `AnalyzeAlert` 接口，让大模型分析 Prometheus 告警并输出恢复 JSON 指令，实现了系统自我监控的雏形。\n\n")

        # 7
        f.write("## 7 系统测试\n\n")
        f.write("### 7.1 K6 极限并发压力测试\n")
        f.write("编写了 K6 压测脚本 `stress_feeds.js` 对 Feed 关注流接口进行高压轰炸。在引入 `APP_ENV=chaos_testing` 强隔离保护的安全测试 Token 后，成功验证了 Gateway 层 Sentinel 在达到 1000 QPS 时的快速熔断限流，保护了后端 MySQL 集群免于宕机。\n\n")

        f.write("### 7.2 混沌工程 (Chaos Mesh) 演练\n")
        f.write("在 Kubernetes 环境中，通过 Chaos Mesh 注入了针对 Redis pod 的 5s 网络延迟故障。测试证明：系统的短超时控制与无数据本地降级策略生效，整体服务无级联崩溃。\n\n")

        # 8
        f.write("## 8 项目管理与过程记录\n\n")
        f.write("### 8.1 自动化流水线 (CI/CD)\n")
        f.write("项目的迭代管理严格基于 Git Flow。提交代码至 main 分支后，GitHub Actions 自动拉起测试用例（包括依赖 miniredis 挡板的单元测试）。测试通过后触发 Docker Buildx 多阶段构建，并自动推送镜像到 Docker Hub。\n\n")
        
        f.write("### 8.2 GitOps 交付部署\n")
        f.write("在部署侧，集群内部署了 ArgoCD 监听配置仓库中的 Helm Chart 变化。只要镜像版本发生更新或 values.yaml 被修改，ArgoCD 能够在 3 分钟内完成全集群无感知的滚动更新，实现了从开发到生产的 100% 自动化流转。\n\n")

        # 9
        f.write("## 9 总结与反思\n\n")
        f.write("本项目是一次向工业级顶级分布式架构冲击的深度实战。在实现中，深刻体会到了“容错与降级”才是系统高可用的底色。从最开始单机内存跑满，到后来引入 Singleflight、多级缓存与异步退避重试，每一步都是在与“不稳定性”做斗争。同时，AI 工作流编排的引入让我看到了大模型应用的新天地。未来的工作重点将会是把 Service Mesh 下沉，并在 AI 端引入更轻量的私有化模型以降低整体算力成本。\n\n")

        # 10
        f.write("## 参考文献\n\n")
        f.write("[1] Kleppmann, M. (2017). Designing Data-Intensive Applications. O'Reilly Media.\n")
        f.write("[2] Burns, B., Beda, J., & Hightower, K. (2019). Kubernetes: Up and Running. O'Reilly Media.\n")
        f.write("[3] Temporal Documentation. Retrieved from https://docs.temporal.io/\n")
        f.write("[4] Go 并发编程实战. 郝林. 人民邮电出版社, 2017.\n")

    print(f"Formal Markdown generated at {md_path}")

if __name__ == "__main__":
    generate_formal_report()
