rag.skill (检索与向量召回技能)
职责：保证十亿级数据下语义搜索的低延迟与高召回。
规范：
1. 掌控 ES 混合检索（BM25 关键词权重 + HNSW 向量余弦相似度）
2. 向量数据冷热分区存储
3. 多模态语义检索治理
4. 解决 Embedding 模型迭代造成的向量空间漂移问题（Embedding Drift Model Migration）