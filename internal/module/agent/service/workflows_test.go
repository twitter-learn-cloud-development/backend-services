package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/testsuite"

	"twitter-clone/internal/events"
)

type UnitTestSuite struct {
	suite.Suite
	testsuite.WorkflowTestSuite
}

func TestUnitTestSuite(t *testing.T) {
	suite.Run(t, new(UnitTestSuite))
}

// Test_TweetRiskControlWorkflow_SpamFrequency 验证频率风控命中时，直接触发影子封禁，且不执行语义相似度检查
func (s *UnitTestSuite) Test_TweetRiskControlWorkflow_SpamFrequency() {
	env := s.NewTestWorkflowEnvironment()

	var activities *AgentActivities

	// 1. Mock 频率检查命中
	env.OnActivity(activities.CheckSpamFrequencyActivity, mock.Anything, uint64(10)).Return(true, nil)
	// 2. Mock 影子封禁执行成功
	env.OnActivity(activities.ExecuteShadowbanActivity, mock.Anything, uint64(1001), uint64(10)).Return(nil)

	event := events.TweetCreatedEvent{
		TweetID:  1001,
		AuthorID: 10,
		Content:  "spam frequency text",
	}

	env.ExecuteWorkflow(TweetRiskControlWorkflow, event)

	s.True(env.IsWorkflowCompleted())
	s.NoError(env.GetWorkflowError())
	env.AssertExpectations(s.T())
}

// Test_TweetRiskControlWorkflow_SpamSimilarity 验证频率风控安全但语义相似度风控命中时，触发影子封禁
func (s *UnitTestSuite) Test_TweetRiskControlWorkflow_SpamSimilarity() {
	env := s.NewTestWorkflowEnvironment()

	var activities *AgentActivities

	// 1. Mock 频率检查通过
	env.OnActivity(activities.CheckSpamFrequencyActivity, mock.Anything, uint64(10)).Return(false, nil)
	// 2. Mock 相似度检查命中
	env.OnActivity(activities.QdrantSearchSimilarityActivity, mock.Anything, "spam similarity text", uint64(10)).Return(true, nil)
	// 3. Mock 影子封禁执行成功
	env.OnActivity(activities.ExecuteShadowbanActivity, mock.Anything, uint64(1002), uint64(10)).Return(nil)

	event := events.TweetCreatedEvent{
		TweetID:  1002,
		AuthorID: 10,
		Content:  "spam similarity text",
	}

	env.ExecuteWorkflow(TweetRiskControlWorkflow, event)

	s.True(env.IsWorkflowCompleted())
	s.NoError(env.GetWorkflowError())
	env.AssertExpectations(s.T())
}

// Test_TweetRiskControlWorkflow_Safe 验证完全放行逻辑
func (s *UnitTestSuite) Test_TweetRiskControlWorkflow_Safe() {
	env := s.NewTestWorkflowEnvironment()

	var activities *AgentActivities

	// 1. Mock 频率与相似度检查全部通过
	env.OnActivity(activities.CheckSpamFrequencyActivity, mock.Anything, uint64(10)).Return(false, nil)
	env.OnActivity(activities.QdrantSearchSimilarityActivity, mock.Anything, "safe text", uint64(10)).Return(false, nil)

	event := events.TweetCreatedEvent{
		TweetID:  1003,
		AuthorID: 10,
		Content:  "safe text",
	}

	env.ExecuteWorkflow(TweetRiskControlWorkflow, event)

	s.True(env.IsWorkflowCompleted())
	s.NoError(env.GetWorkflowError())
	env.AssertExpectations(s.T())
}

// Test_TrendingReporterWorkflow_SingleCycle 验证舆情监控工作流的单次完整迭代和 ContinueAsNew 触发
func (s *UnitTestSuite) Test_TrendingReporterWorkflow_SingleCycle() {
	env := s.NewTestWorkflowEnvironment()

	var activities *AgentActivities

	// 1. Mock 所有的 Activity 行为
	env.OnActivity(activities.GetHottestTopicActivity, mock.Anything).Return("AI", nil)
	env.OnActivity(activities.ParallelRetrieveActivity, mock.Anything, "AI").Return([]string{"AI is changing the world", "AI agent mesh"}, nil)
	env.OnActivity(activities.GenerateSummaryActivity, mock.Anything, "AI", []string{"AI is changing the world", "AI agent mesh"}).Return("【🔥 舆情快报】关于 #AI 最新动态：AI 正在重构网格化开发。", nil)
	env.OnActivity(activities.PublishTweetActivity, mock.Anything, "【🔥 舆情快报】关于 #AI 最新动态：AI 正在重构网格化开发。").Return(uint64(9999), nil)

	env.ExecuteWorkflow(TrendingReporterWorkflow, 10*time.Second)

	s.True(env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	s.Error(err)
	// Continue-As-New 在 Temporal 中会被包装为特殊的错误指示，测试套件会抛出此错误以验证它发起新迭代
	s.Contains(err.Error(), "continue as new")
	env.AssertExpectations(s.T())
}
