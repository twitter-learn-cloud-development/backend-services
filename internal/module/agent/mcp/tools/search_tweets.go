package tools

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	tweetv1 "twitter-clone/api/tweet/v1"
	"twitter-clone/pkg/ai"
	"twitter-clone/pkg/es"
	"twitter-clone/pkg/qdrant"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"golang.org/x/sync/errgroup"
)

// RegisterSearchTweets 注册语义搜索推文工具 (升级加固版，包含 Reranker 精排与回表延迟加载)
func RegisterSearchTweets(srv *server.MCPServer, aiClient *ai.Client, esClient *es.Client, qdrantClient *qdrant.Client, reranker ai.Reranker, tweetClient tweetv1.TweetServiceClient, model string) {
	tool := mcp.NewTool("search_tweets_by_semantic",
		mcp.WithDescription("根据用户输入的语义描述，搜索最相关的推文列表"),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("用户的搜索描述，例如：最近很火 of 健身博主、关于 AI 的推文"),
		),
		mcp.WithNumber("size",
			mcp.Description("返回结果数量，默认 5，最大 20"),
		),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := req.Params.Arguments.(map[string]any)
		if !ok {
			return mcp.NewToolResultError("参数解析失败"), nil
		}

		query, ok := args["query"].(string)
		if !ok || query == "" {
			return mcp.NewToolResultError("query 参数不能为空"), nil
		}

		size := 5
		if s, ok := args["size"].(float64); ok && s > 0 {
			size = int(s)
			if size > 20 {
				size = 20
			}
		}

		// 1. 把用户问题向量化
		vector, err := aiClient.GetEmbedding(ctx, query, model)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("embedding failed: %v", err)), nil
		}

		// 🎯 优化：初筛漏斗数量放大 3 倍，避免内存和网络 I/O 暴涨
		initialSize := size * 3

		var tweets []es.TweetDocument
		var searchErr error

		// 2. Qdrant 向量检索粗筛 (首选的高级向量检索)
		if qdrantClient != nil {
			var qdrantResults []qdrant.SearchResult
			qdrantResults, searchErr = qdrantClient.Search(ctx, "tweets", vector, initialSize)
			if searchErr == nil {
				for _, r := range qdrantResults {
					content := ""
					if c, ok := r.Payload["content"].(string); ok {
						content = c
					}
					userID := ""
					if uid, ok := r.Payload["user_id"].(string); ok {
						userID = uid
					}
					createdAt := int64(0)
					if ca, ok := r.Payload["created_at"].(float64); ok {
						createdAt = int64(ca)
					}
					tweets = append(tweets, es.TweetDocument{
						ID:        r.ID,
						UserID:    userID,
						Content:   content,
						CreatedAt: createdAt,
					})
				}
			} else {
				log.Printf("⚠️ [RAG Degradation] Qdrant search failed: %v. Trying ES fallback.", searchErr)
			}
		} else {
			log.Println("⚠️ [RAG Degradation] Qdrant client is nil. Trying ES fallback.")
		}

		// 3. 触发优雅降级：回退至传统的 Elasticsearch 关键词召回 (BM25)
		if len(tweets) == 0 && esClient != nil {
			esDocs, err := esClient.SearchTweets(ctx, query, 1, initialSize)
			if err == nil {
				for _, doc := range esDocs {
					tweets = append(tweets, doc)
				}
				log.Printf("🛡️ [RAG Fallback] ES successfully retrieved fallback data for '%s', count: %d", query, len(tweets))
			} else {
				log.Printf("⚠️ [RAG Degradation] ES fallback also failed for '%s': %v", query, err)
			}
		}

		// 4. 终极防御：返回文本而非 Error，防止 LLM 对话树崩溃
		if len(tweets) == 0 {
			return mcp.NewToolResultText("系统知识库中目前没有找到与该主题相关的推文，请尝试更换关键词搜索，或结合已有知识进行回答。"), nil
		}

		// 🎯 优化：精排重排序阶段
		var finalTweets []es.TweetDocument
		if len(tweets) > size && reranker != nil {
			var rDocs []ai.Document
			for _, t := range tweets {
				rDocs = append(rDocs, ai.Document{
					ID:   t.ID,
					Text: t.Content,
				})
			}

			reranked, err := reranker.Rerank(ctx, query, rDocs)
			if err != nil {
				// 🛡️ 优雅降级：精排超时(1.5s)或出错时，平滑退化为直接截取初筛前 size 个
				log.Printf("⚠️  [Reranker Warning] Rerank failed: %v. Fallback to initial rank.", err)
				limit := min(size, len(tweets))
				finalTweets = tweets[:limit]
			} else {
				limit := min(size, len(reranked))
				tweetMap := make(map[string]es.TweetDocument)
				for _, t := range tweets {
					tweetMap[t.ID] = t
				}
				for i := 0; i < limit; i++ {
					if originTweet, ok := tweetMap[reranked[i].Document.ID]; ok {
						finalTweets = append(finalTweets, originTweet)
					}
				}
			}
		} else {
			limit := min(size, len(tweets))
			finalTweets = tweets[:limit]
		}

		// 🎯 优化：延迟回表 (Late Materialization)
		// 拿着胜出的 top-size 元素，通过 gRPC 批量去 tweet-service 捞取全量实时数据
		var enrichedTweets []*tweetv1.Tweet
		for _, t := range finalTweets {
			tweetID, err := strconv.ParseUint(t.ID, 10, 64)
			if err != nil {
				continue
			}

			resp, err := tweetClient.GetTweet(ctx, &tweetv1.GetTweetRequest{
				TweetId: tweetID,
			})
			if err != nil {
				// 🛡️ 回表失败优雅降级：直接以 ES 字段封装返回，确保主检索可用性
				enrichedTweets = append(enrichedTweets, &tweetv1.Tweet{
					Id:        tweetID,
					UserId:    parseUintOrDefault(t.UserID, 0),
					Content:   t.Content,
					CreatedAt: t.CreatedAt,
					LikeCount: int32(t.LikeCount),
				})
			} else {
				enrichedTweets = append(enrichedTweets, resp.Tweet)
			}
		}

		// 3. 格式化结果给 LLM
		result := fmt.Sprintf("找到 %d 条最相关的精排推文：\n\n", len(enrichedTweets))
		for i, t := range enrichedTweets {
			result += fmt.Sprintf("%d. [推文ID: %d] [用户ID: %d]\n内容: %s\n发布时间: %d\n点赞数: %d\n\n",
				i+1, t.Id, t.UserId, t.Content, t.CreatedAt, t.LikeCount)
		}

		return mcp.NewToolResultText(result), nil
	})
}

// RegisterHybridSearchTweets 注册混合搜索推文工具 (升级加固版)
func RegisterHybridSearchTweets(srv *server.MCPServer, aiClient *ai.Client, esClient *es.Client, qdrantClient *qdrant.Client, reranker ai.Reranker, tweetClient tweetv1.TweetServiceClient, model string) {
	tool := mcp.NewTool("hybrid_search_tweets",
		mcp.WithDescription("混合搜索：同时基于关键词和语义向量搜索推文，结果更精准"),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("搜索关键词或语义描述"),
		),
		mcp.WithNumber("size",
			mcp.Description("返回结果数量，默认 5，最大 20"),
		),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := req.Params.Arguments.(map[string]any)
		if !ok {
			return mcp.NewToolResultError("参数解析失败"), nil
		}

		query, ok := args["query"].(string)
		if !ok || query == "" {
			return mcp.NewToolResultError("query 参数不能为空"), nil
		}

		size := 5
		if s, ok := args["size"].(float64); ok && s > 0 {
			size = int(s)
			if size > 20 {
				size = 20
			}
		}

		// 1. 向量化
		vector, err := aiClient.GetEmbedding(ctx, query, model)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("embedding failed: %v", err)), nil
		}

		// 🎯 优化：初筛漏斗数量放大 3 倍
		initialSize := size * 3

		// 2. 混合搜索双路并发召回 (ES 文本倒排 + Qdrant 向量检索)
		// 为整个 RAG 初筛链路设置强制短超时，防止下游拖垮上游
		searchCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		defer cancel()

		g, gCtx := errgroup.WithContext(searchCtx)

		var mu sync.Mutex
		candidateMap := make(map[string]es.TweetDocument)

		// 🚀 路径一：并发调用 Elasticsearch 进行传统文本倒排召回 (BM25)
		g.Go(func() error {
			if esClient == nil {
				log.Println("⚠️  Elasticsearch client is nil, skip ES path")
				return nil
			}
			esDocs, err := esClient.SearchTweets(gCtx, query, 1, initialSize)
			if err != nil {
				log.Printf("⚠️ [Hybrid Search] ES Path failed: %v", err)
				return nil // 降级容错
			}
			mu.Lock()
			for _, doc := range esDocs {
				candidateMap[doc.ID] = doc
			}
			mu.Unlock()
			return nil
		})

		// 🚀 路径二：并发调用 Qdrant 进行高维稠密向量语义召回 (HNSW)
		g.Go(func() error {
			if qdrantClient == nil {
				log.Println("⚠️  Qdrant client is nil, skip Qdrant path")
				return nil
			}
			qdrantDocs, err := qdrantClient.Search(gCtx, "tweets", vector, initialSize)
			if err != nil {
				log.Printf("⚠️ [Hybrid Search] Qdrant Path failed: %v", err)
				return nil // 降级容错
			}
			mu.Lock()
			for _, r := range qdrantDocs {
				userID := ""
				if uid, ok := r.Payload["user_id"].(string); ok {
					userID = uid
				}
				content := ""
				if c, ok := r.Payload["content"].(string); ok {
					content = c
				}
				createdAt := int64(0)
				if ca, ok := r.Payload["created_at"].(float64); ok {
					createdAt = int64(ca)
				}
				candidateMap[r.ID] = es.TweetDocument{
					ID:        r.ID,
					UserID:    userID,
					Content:   content,
					CreatedAt: createdAt,
				}
			}
			mu.Unlock()
			return nil
		})

		// 等待两路召回协同完毕（或触发超时）
		_ = g.Wait()

		// 转换为线性切片，准备送入 Reranker
		mu.Lock()
		tweets := make([]es.TweetDocument, 0, len(candidateMap))
		for _, doc := range candidateMap {
			tweets = append(tweets, doc)
		}
		mu.Unlock()

		if len(tweets) == 0 {
			return mcp.NewToolResultText("没有找到相关推文"), nil
		}

		// 🎯 优化：精排重排序阶段
		var finalTweets []es.TweetDocument
		if len(tweets) > size && reranker != nil {
			var rDocs []ai.Document
			for _, t := range tweets {
				rDocs = append(rDocs, ai.Document{
					ID:   t.ID,
					Text: t.Content,
				})
			}

			reranked, err := reranker.Rerank(ctx, query, rDocs)
			if err != nil {
				// 🛡️ 优雅降级：回退至直接截取粗筛结果的前 size 个
				log.Printf("⚠️  [Reranker Warning] Rerank failed: %v. Fallback to initial rank.", err)
				limit := min(size, len(tweets))
				finalTweets = tweets[:limit]
			} else {
				limit := min(size, len(reranked))
				tweetMap := make(map[string]es.TweetDocument)
				for _, t := range tweets {
					tweetMap[t.ID] = t
				}
				for i := 0; i < limit; i++ {
					if originTweet, ok := tweetMap[reranked[i].Document.ID]; ok {
						finalTweets = append(finalTweets, originTweet)
					}
				}
			}
		} else {
			limit := min(size, len(tweets))
			finalTweets = tweets[:limit]
		}

		// 🎯 优化：延迟回表 (Late Materialization)
		var enrichedTweets []*tweetv1.Tweet
		for _, t := range finalTweets {
			tweetID, err := strconv.ParseUint(t.ID, 10, 64)
			if err != nil {
				continue
			}

			resp, err := tweetClient.GetTweet(ctx, &tweetv1.GetTweetRequest{
				TweetId: tweetID,
			})
			if err != nil {
				// 🛡️ 回表降级兜底
				enrichedTweets = append(enrichedTweets, &tweetv1.Tweet{
					Id:        tweetID,
					UserId:    parseUintOrDefault(t.UserID, 0),
					Content:   t.Content,
					CreatedAt: t.CreatedAt,
					LikeCount: int32(t.LikeCount),
				})
			} else {
				enrichedTweets = append(enrichedTweets, resp.Tweet)
			}
		}

		result := fmt.Sprintf("混合搜索精排找到 %d 条推文：\n\n", len(enrichedTweets))
		for i, t := range enrichedTweets {
			result += fmt.Sprintf("%d. [推文ID: %d] [用户ID: %d]\n内容: %s\n发布时间: %d\n点赞数: %d\n\n",
				i+1, t.Id, t.UserId, t.Content, t.CreatedAt, t.LikeCount)
		}

		return mcp.NewToolResultText(result), nil
	})
}

// 辅助方法：把字符串解析为 uint64
func parseUintOrDefault(s string, fallback uint64) uint64 {
	val, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return fallback
	}
	return val
}

// min 内置类型比较辅助
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
