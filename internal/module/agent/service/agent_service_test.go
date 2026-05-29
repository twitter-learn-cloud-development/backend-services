package service

import (
	"context"
	"os"
	"strings"
	"testing"
	"twitter-clone/pkg/ai"
)

func TestSanitizeMarkdownTable(t *testing.T) {
	s := &AgentService{}

	tests := []struct {
		name      string
		rawReport string
		expected  string
	}{
		{
			name:      "Normal Table",
			rawReport: "| 草稿一 | 🟢 98% |",
			expected:  "| 草稿一 | 🟢 98% |",
		},
		{
			name:      "Table with Markdown Fenced Blocks",
			rawReport: "```markdown\n| 草稿一 | 🟢 98% |\n| 草稿二 | 🔴 40% |\n```",
			expected:  "| 草稿一 | 🟢 98% |\n| 草稿二 | 🔴 40% |",
		},
		{
			name:      "Invalid Table (No Pipes)",
			rawReport: "This is a raw text feedback from LLM without any markdown table.",
			expected:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.sanitizeMarkdownTable(tt.rawReport)
			if got != tt.expected {
				t.Errorf("sanitizeMarkdownTable() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestAgentService_AnalyzeAlert(t *testing.T) {
	// 1. 设置 Mock 挡板环境变量
	os.Setenv("MOCK_EMBEDDING", "true")
	defer os.Unsetenv("MOCK_EMBEDDING")

	// 2. 初始化 AI 客户端和 AgentService
	aiClient := ai.NewClient("")
	s := NewAgentService("http://localhost:1234/v1", "test-key", "test-model", "localhost:9200", nil, aiClient, nil)

	// 3. 执行 AnalyzeAlert
	ctx := context.Background()
	alertPayload := `{"status":"firing","alerts":[{"labels":{"alertname":"RedisDown"}}],"groupKey":"redis-error-group"}`
	errorLogs := []string{
		"ERROR: redis connection refused",
		"ERROR: failed to get timeline cache",
	}

	report, structuredRCA, err := s.AnalyzeAlert(ctx, alertPayload, errorLogs)
	if err != nil {
		t.Fatalf("AnalyzeAlert failed: %v", err)
	}

	// 4. 验证返回的报告内容是否包含了 Mock 挡板的输出
	if !strings.Contains(report, "告警现状与影响评估") {
		t.Errorf("expected report to contain mock text '告警现状与影响评估', got %q", report)
	}

	// 4.1 验证结构化元数据是否被成功解析 (Mock 挡板包含 RedisDown 标签)
	if !strings.Contains(structuredRCA, "RedisDown") {
		t.Errorf("expected structuredRCA to contain 'RedisDown', got %q", structuredRCA)
	}

	// 5. 验证本地文件是否被创建
	reportPath := "C:/Users/郭丰硕/.gemini/antigravity-ide/brain/63d49437-9d83-40a2-a7d2-33758c3e0a03/scratch/alert_rca_reports.md"
	info, err := os.Stat(reportPath)
	if os.IsNotExist(err) {
		t.Fatalf("expected report file at %s to be created, but it does not exist", reportPath)
	}
	if info.Size() == 0 {
		t.Errorf("expected report file to be non-empty, but size is 0")
	}

	// 6. 清理：为了避免多次运行测试使 rca reports 文件无限增长，我们在测试后截断或删除该文件
	// 这里由于是追加模式，我们可以将它删掉，或者清空
	_ = os.Remove(reportPath)
}
