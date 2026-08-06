# RAG Eval 基线与对比 Runner

## 当前能力

- 数据集：`internal/module/agent/eval/testdata/rag_cases.json`，当前 51 条。
- 覆盖：中文、英文、混合语种、错别字、无答案、时态记忆和用户画像。
- 指标：Recall@K、MRR、NDCG@K、EmptyRate、NoiseRate、P50/P95 延迟。
- Runner：`internal/module/agent/eval/runner.go`，只依赖 `Retriever` 小接口，不依赖 ES、Qdrant 或具体模型 SDK。
- 在线适配命令：`cmd/agent-rag-eval`，按固定顺序运行 BM25、Vector、RRF 和 RRF+Rerank。
- 命令支持 `--strategies bm25,vector,rrf,rrf_rerank` 子集执行，并按依赖闭包懒初始化 Provider；只跑 BM25 不会初始化 Qdrant/Embedding，只跑 Vector 不会连接 ES。
- 结果报告记录环境、数据集版本、Embedding 模型/版本、K、随机种子、逐 Case 结果与错误，以及 ES/Qdrant/Embedding/Reranker 的 Endpoint、模型、请求数、失败数和失败率。
- Router 数据集：`internal/module/agent/eval/testdata/router_cases.json`，当前 34 条，覆盖 L1 Persona、L2 Episodic、L3 Global、直接动作、混合意图和模糊表达。
- Router Runner：按意图、认知层和实际命中 Stage 统计 Accuracy、MisrouteRate、ErrorRate、P50/P95，并保留逐 Case 决策。

离线指标与 Runner 测试不会连接真实模型或存储。`agent-rag-eval` 默认拒绝 live 连接，只有显式传入 `--allow-live` 才会连接 ES、Qdrant 和 Embedding/Reranker Provider；命令只执行检索并写报告，不写入或删除业务数据。出站 HTTP 复用 Endpoint Policy、禁用 Redirect，并在 DNS 解析后再次校验目标地址。

## 指标口径

- `Recall@K`：前 K 个结果命中的相关文档数，占该样本全部相关文档数的比例。
- `MRR`：第一个相关结果排名的倒数，未命中记为 0。
- `NDCG@K`：二值相关性下的归一化折损累计增益。
- `EmptyRate`：前 K 个结果为空的样本比例。
- `NoiseRate`：所有样本前 K 个去重结果中非相关结果的比例。
- `P50/P95`：策略逐 Case 端到端检索耗时的最近秩百分位。

Provider 或存储失败会保留在对应 Case 的 `error` 字段，并按空召回进入指标；Runner 不会把部分失败伪装成完整成功。

## 离线验证

```powershell
$env:GOCACHE=(Join-Path $PWD '.gocache')
go test ./internal/module/agent/eval ./pkg/qdrant -count=1
```

Qdrant Fake 集成契约会用同一共享 Collection 模拟两个用户，并验证服务端 `user_id` Filter 不会返回跨用户结果。

## Router 离线基线

`agent-router-eval` 只运行当前 Cascade Router 的确定性词典层与无模型默认层，不连接 Embedding 或 LLM：

```powershell
go run ./cmd/agent-router-eval `
  --dataset internal/module/agent/eval/testdata/router_cases.json `
  --out tmp/router-eval/lexical-baseline.json
```

`router-cases-v1` 的当前基线：

| 范围 | Accuracy | MisrouteRate |
|------|----------|--------------|
| Overall | 91.18% (31/34) | 8.82% (3/34) |
| L1 Persona | 77.78% (7/9) | 22.22% (2/9) |
| L2 Episodic | 88.89% (8/9) | 11.11% (1/9) |
| L3 Global | 100% (11/11) | 0% |
| Direct Action | 100% (5/5) | 0% |

词典命中 26 条且全部正确；8 条进入无模型默认层，其中 5 条正确。剩余三条都是缺少显式关键词的画像/历史表达，应由 Semantic Router 或 LLM Fallback 处理，不能把这份离线结果描述为完整三级 Router 的线上准确率。

### Router Provider 对照

同一命令支持四种严格区分的模式：

- `lexical`：词典 + Default，默认模式，不连接 Provider。
- `semantic`：词典 + Semantic + Default，只连接 Embedding Provider。
- `llm`：词典 + LLM Fallback，不连接 Embedding Provider。
- `full`：词典 + Semantic + LLM Fallback，完整三级级联。

后三种模式必须显式传入 `--allow-live`，否则命令在创建 Provider Client 前直接拒绝：

```powershell
go run ./cmd/agent-router-eval --mode semantic --allow-live --out tmp/router-eval/semantic.json
go run ./cmd/agent-router-eval --mode llm --allow-live --out tmp/router-eval/llm.json
go run ./cmd/agent-router-eval --mode full --allow-live --out tmp/router-eval/full.json
```

Provider Base URL、模型和标签可通过 `--embedding-*`、`--llm-*` 参数固定；API Key 不接受命令行参数，只读取 `AGENT_ROUTER_EVAL_EMBEDDING_API_KEY`、`AGENT_ROUTER_EVAL_LLM_API_KEY`，并兼容现有 LM Studio/DashScope 环境变量。出站连接复用 Endpoint Policy、禁用 Redirect 并执行 DNS Rebinding 检查。

报告额外记录模式、Semantic 阈值、单 Case 超时、Provider、Endpoint、模型、请求/失败数、输入/输出 Token、估算成本、Pricing Version、Semantic/LLM 错误和 ProviderErrorRate。Semantic 报告的 Embedding 请求数包含四个意图锚点初始化请求。Provider 失败可以继续触发级联降级，但不再被报告伪装成普通 `global` 分类。

## 真实检索对比

先启动并配置 ES、Qdrant 和 Embedding Provider，然后执行。`--allow-live` 是有意的安全门，不能省略：

```powershell
go run ./cmd/agent-rag-eval `
  --allow-live `
  --strategies bm25,vector,rrf,rrf_rerank `
  --dataset internal/module/agent/eval/testdata/rag_cases.json `
  --collection tweets `
  --k 10 `
  --out tmp/rag-eval/local-report.json
```

相关环境变量：

- `ES_ADDRESSES`、`ES_USERNAME`、`ES_PASSWORD`
- `QDRANT_URL`
- `LM_STUDIO_API_URL`、`LM_STUDIO_MODEL_EMBEDDING`
- `AGENT_RAG_EVAL_ALLOWED_HOSTS`、`AGENT_RAG_EVAL_COLLECTION`、`AGENT_RAG_EVAL_DATASET_VERSION`
- `AGENT_RAG_EVAL_STRATEGIES`；可选值为 `bm25`、`vector`、`rrf`、`rrf_rerank`，输入顺序不会改变固定报告顺序，RRF/RRF+Rerank 自动补齐 BM25/Vector 依赖
- `AGENT_RAG_EVAL_EMBEDDING_PROVIDER`、`AGENT_RAG_EVAL_EMBEDDING_BASE_URL`、`AGENT_RAG_EVAL_EMBEDDING_MODEL`、`AGENT_RAG_EVAL_EMBEDDING_API_KEY`
- `AGENT_RAG_EVAL_CASE_TIMEOUT`、`AGENT_RAG_EVAL_TIMEOUT`
- `AGENT_EPISODIC_EMBEDDING_VERSION`、`APP_ENV`
- `RERANKER_TYPE`、`RERANKER_API_URL`、`RERANKER_MODEL`、`RERANKER_API_KEY`

API Key 不接受命令行参数。`AGENT_RAG_EVAL_ALLOWED_HOSTS` 默认为 `localhost,127.0.0.1,::1`；ES、Qdrant、Embedding 或远程 Reranker 使用其他域名时必须显式加入 allowlist。报告中的请求计数是检索执行期间的观察值，ES 初始化 Ping 不计入检索请求；成本/Token 不会被伪造，当前 RAG Runner 只记录可观察的请求失败率。

同一份报告包含 `bm25`、`vector`、`rrf`、`rrf_rerank` 四个 Strategy。为了控制压测强度和保证基线可比较，Runner 默认按策略和数据集顺序串行执行。`RERANKER_TYPE=local` 使用本地 Bigram Jaccard 降级器；云端重排只有在显式配置后才会连接 Provider。

## Episodic 迁移

共享 Episodic 迁移只允许显式指定用户集合，并支持先 dry-run：

```powershell
go run ./cmd/agent-memory-migrate --user-ids 1001,1002 --dry-run
go run ./cmd/agent-memory-migrate --user-ids 1001,1002 --batch-size 100

# 迁移后只读验收；不会写入或删除任何集合
go run ./cmd/agent-memory-migrate --user-ids 1001,1002 --verify-only --report tmp/rag-eval/migration-verify.json
```

`--delete-legacy` 只能在非 dry-run 迁移成功后使用。命令不会枚举用户 Collection，也不会重新生成 Embedding；它使用 Scroll 读取向量和 Payload，再以原始 Point ID 写入 `agent_episodic_memory`。

`--verify-only` 是删除旧集合前的人工验收门：它按显式用户 ID 扫描旧集合，使用共享集合的服务端 `user_id` Filter 读取目标点，比较有效 Point ID 集合，并检查租户字段、共享 Payload Schema、缺失点和意外点。报告只读写到显式 `--report` 路径；验证失败时命令以非零状态退出，并明确保证不会删除旧集合。

## Session End

`POST /api/v1/agent/dialogues/:id/end` 会保留对话，停止该会话的空闲 Timer，取消并等待在途摘要 Job 释放租约，再同步强制结晶尚未处理的消息。重复或并发 End 由 Mongo 摘要租约与游标保证最多写入一份新版本。

## 剩余验收

- 在受控测试环境执行真实旧集合回填，再运行 `--verify-only` 核对迁移前后数量、Payload 和跨租户查询结果；确认报告后才允许人工执行旧集合删除。
- 运行真实 BM25/Vector/RRF 报告并保存基线；当前代码没有伪造一份无真实依赖的性能报告。
- 使用已具备 live 护栏的 Router Runner，在固定 Embedding/LLM Provider、模型版本和超时预算下执行并保存 Semantic/LLM Fallback 对照报告，再与当前词典基线比较收益、延迟和成本。
