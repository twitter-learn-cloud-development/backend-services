package service

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"twitter-clone/internal/events"
)

// TweetRiskControlWorkflow 影子风控自愈工作流
func TweetRiskControlWorkflow(ctx workflow.Context, event events.TweetCreatedEvent) error {
	// 1. 设置 AI/大模型相关的重试策略，防止无限重试刷爆 API 账单（防破产配置）
	aiRetryPolicy := &temporal.RetryPolicy{
		InitialInterval:    time.Second,
		BackoffCoefficient: 2.0,
		MaximumInterval:    10 * time.Second,
		MaximumAttempts:    3, // ⚠️ 核心防线：大模型最多重试 3 次，防止破产
	}

	// 2. 设置通用基础设施的重试策略（如 Redis 瞬断），允许自动退避无限重试以达成事务最终一致性
	infraRetryPolicy := &temporal.RetryPolicy{
		InitialInterval:    time.Second,
		BackoffCoefficient: 2.0,
		MaximumInterval:    30 * time.Second,
	}

	var activities *AgentActivities

	// 3. 执行频率检查 Activity (查 DB，属于通用基础设施)
	aoDB := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy:         infraRetryPolicy,
	}
	ctxDB := workflow.WithActivityOptions(ctx, aoDB)

	var isSpamFrequency bool
	err := workflow.ExecuteActivity(ctxDB, activities.CheckSpamFrequencyActivity, event.AuthorID).Get(ctxDB, &isSpamFrequency)
	if err != nil {
		return err
	}

	if isSpamFrequency {
		// 命中高频发帖，执行影子封禁 (写 DB + 洗 Redis)
		aoBan := workflow.ActivityOptions{
			StartToCloseTimeout: 60 * time.Second, // 洗地可能粉丝较多，设置较长超时
			RetryPolicy:         infraRetryPolicy,
		}
		ctxBan := workflow.WithActivityOptions(ctx, aoBan)
		return workflow.ExecuteActivity(ctxBan, activities.ExecuteShadowbanActivity, event.TweetID, event.AuthorID).Get(ctxBan, nil)
	}

	// 4. 频率检查通过，继续进行 Qdrant 语义相似度检查 (涉及 AI，配置防破产重试策略)
	aoAI := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         aiRetryPolicy,
	}
	ctxAI := workflow.WithActivityOptions(ctx, aoAI)

	var isSpamSimilarity bool
	err = workflow.ExecuteActivity(ctxAI, activities.QdrantSearchSimilarityActivity, event.Content, event.AuthorID).Get(ctxAI, &isSpamSimilarity)
	if err != nil {
		// 语义识别失败，不阻断发推流程，但向上层返回错误
		return err
	}

	if isSpamSimilarity {
		// 语义相似度过高，判定为灌水内容，执行影子封禁
		aoBan := workflow.ActivityOptions{
			StartToCloseTimeout: 60 * time.Second,
			RetryPolicy:         infraRetryPolicy,
		}
		ctxBan := workflow.WithActivityOptions(ctx, aoBan)
		return workflow.ExecuteActivity(ctxBan, activities.ExecuteShadowbanActivity, event.TweetID, event.AuthorID).Get(ctxBan, nil)
	}

	return nil
}

// TrendingReporterWorkflow 周期性舆情巡逻哨兵工作流
func TrendingReporterWorkflow(ctx workflow.Context, interval time.Duration) error {
	// 配置大模型生成摘要的防破产重试策略
	aiRetryPolicy := &temporal.RetryPolicy{
		InitialInterval:    time.Second,
		BackoffCoefficient: 2.0,
		MaximumInterval:    10 * time.Second,
		MaximumAttempts:    3,
	}

	// 基础设施常规重试
	infraRetryPolicy := &temporal.RetryPolicy{
		InitialInterval:    time.Second,
		BackoffCoefficient: 2.0,
		MaximumInterval:    10 * time.Second,
	}

	aoAI := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute, // 宽裕的单次 LLM 响应超时，防长耗时误杀
		RetryPolicy:         aiRetryPolicy,
	}

	aoInfra := workflow.ActivityOptions{
		StartToCloseTimeout: 15 * time.Second,
		RetryPolicy:         infraRetryPolicy,
	}

	var activities *AgentActivities

	// 1. 周期休眠，必须使用 workflow.Sleep
	err := workflow.Sleep(ctx, interval)
	if err != nil {
		return err
	}

	// 2. 获取最高热度的话题
	ctxInfra := workflow.WithActivityOptions(ctx, aoInfra)
	var topic string
	err = workflow.ExecuteActivity(ctxInfra, activities.GetHottestTopicActivity).Get(ctxInfra, &topic)
	if err != nil {
		// 若获取失败，本轮静默退出，并在 Continue-As-New 后自动重新开始下一轮
		workflow.GetLogger(ctx).Error("Failed to get hottest topic", "error", err)
	} else if topic != "" {
		// 3. 并行检索相关推文
		var tweets []string
		err = workflow.ExecuteActivity(ctxInfra, activities.ParallelRetrieveActivity, topic).Get(ctxInfra, &tweets)
		if err != nil {
			workflow.GetLogger(ctx).Error("Failed to retrieve parallel tweets", "topic", topic, "error", err)
		} else if len(tweets) > 0 {
			// 4. 调用 AI 客户端生成摘要
			ctxAI := workflow.WithActivityOptions(ctx, aoAI)
			var summary string
			err = workflow.ExecuteActivity(ctxAI, activities.GenerateSummaryActivity, topic, tweets).Get(ctxAI, &summary)
			if err != nil {
				workflow.GetLogger(ctx).Error("Failed to generate summary via AI", "topic", topic, "error", err)
			} else if summary != "" {
				// 5. 播报姬发帖
				var tweetID uint64
				err = workflow.ExecuteActivity(ctxInfra, activities.PublishTweetActivity, summary).Get(ctxInfra, &tweetID)
				if err != nil {
					workflow.GetLogger(ctx).Error("Failed to publish trending report tweet", "error", err)
				}
			}
		}
	}

	// 6. 【核心防线】通过 Continue-As-New 清理整个执行历史，重新开启下一轮迭代，防止历史事件数超过 50,000 条上限
	return workflow.NewContinueAsNewError(ctx, TrendingReporterWorkflow, interval)
}
