---
name: rag
description: 维护 ES BM25 + Qdrant Vector 的分层认知检索，并控制噪声、Token 与模型迁移风险。用于搜索、Embedding、Qdrant、Elasticsearch、Persona、Episodic Memory、Rerank 或上下文召回任务。
---

# RAG Skill

## 先读

- `.agents/context/agent_runtime_context.md`
- `internal/module/agent/workflow/rag/`
- `internal/module/agent/service/cognitive_context.go`
- `internal/module/agent/service/session_summary.go`
- `pkg/es/`、`pkg/qdrant/`、`pkg/ai/`

## 当前数据流

```text
Query -> Cascade Router
      -> L1 Persona (MySQL direct injection)
      -> L2 Episodic (shared Qdrant summary vector + mandatory user_id filter)
      -> L3 Knowledge (ES BM25 + Qdrant vector)
      -> RRF/Similarity/TimeDecay/Persona boost
      -> Token Budget assembly
```

## 执行步骤

1. 明确改的是路由、Embedding、召回、融合、重排、预算还是记忆生命周期。
2. 明确事实源与派生索引；ES/Qdrant 失败必须允许降级或重放。
3. 固定 Embedding 模型 ID、维度、版本和 Collection/Index 兼容策略。
4. 保留 `user_id` Payload 隔离和相似度噪声阈值。
5. RAG Chunk 整块装配；放不下跳过，不做请求内摘要截断。
6. 用查询集验证 Recall/NDCG/MRR、空结果、低相似度和跨用户隔离。

## 项目不变量

- Elasticsearch 负责 BM25/全文；Qdrant 负责向量，不混写职责描述。
- Embedding 不应同步阻塞 Tweet 主事务；派生索引必须可重建。
- Session Summary 只处理租约声明的增量消息区间，失败不推进游标。
- Persona 不进入公共知识库；Episodic 不跨用户泄露。
- Provider 连接失败必须返回真实失败，禁止编造检索已发起或结果即将返回。
- Episodic 新写入固定使用 `agent_episodic_memory` 共享 Collection；Payload 至少包含字符串 `user_id`、`embedding_model`、`embedding_dimension`、`embedding_version`。
- 共享 Episodic 检索必须使用 Qdrant 服务端 Filter；不能先取公共 Top-K 再在应用层按用户丢弃。
- 旧 `episodic_user_<id>` 只允许有界双读兼容；迁移由 `cmd/agent-memory-migrate` 离线执行，不得在请求路径枚举用户 Collection。
- 迁移删除前必须运行 `cmd/agent-memory-migrate --verify-only --user-ids ...`；验收使用共享集合服务端 `user_id` Filter，比对有效 Point ID 并检查 `shared_user_payload_v1`，报告失败时禁止删除旧集合。
- 显式 Session End 使用 `POST /api/v1/agent/dialogues/:id/end`：保留对话，取消并等待旧摘要 Job 释放租约，再强制结晶；重复调用必须由 Mongo 游标/租约幂等处理。

## Embedding 迁移检查

- 新旧模型向量维度是否一致。
- 是否需要双写 Collection/Version Payload。
- 回填是否限速、可暂停、可重试、可观测。
- 查询何时切读、如何灰度、如何回滚。
- 旧向量何时清理，是否有离线质量对照。

## 验证

- 纯函数：评分、RRF、Token 截断、摘要 JSON 降级。
- Adapter：Fake ES/Qdrant/AI，不把真实外部服务作为单测前提。
- 记忆调度/并发：Service + Repository + RAG `-race`。
- 变更检索行为时记录离线数据集、阈值和指标，而不只验证“能返回文本”。
- 离线评测数据集位于 `internal/module/agent/eval/testdata/rag_cases.json`；使用 `internal/module/agent/eval` 的纯函数/Runner 计算 Recall@K、MRR、NDCG@K、空召回率、噪声率和 P50/P95。`cmd/agent-rag-eval` 才允许显式连接 ES/Qdrant/Embedding/Reranker，按固定顺序生成 BM25、Vector、RRF、RRF+Rerank JSON 报告。
- Router 数据集位于 `internal/module/agent/eval/testdata/router_cases.json`；`cmd/agent-router-eval` 默认只评估确定性 Lexical/Default 层并输出意图、认知层、Stage、错误和延迟。Semantic/LLM Fallback 的结果必须记录 Provider、模型版本和超时，禁止与纯离线基线混称。
- Router live 对照必须使用 `semantic`、`llm` 或 `full` 模式并显式 `--allow-live`；API Key 仅从环境读取，报告必须保留请求失败、ProviderErrorRate、Token、成本与 Pricing Version。未实际运行 Provider 时只能声明 Runner 已具备，不能声明线上准确率。
