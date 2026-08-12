package service

import (
	"context"
	"strings"
	"testing"
	"twitter-clone/pkg/ai"
)

type recordingAIOpsReportSink struct {
	reports []AIOpsReport
}

func (sink *recordingAIOpsReportSink) AppendAIOpsReport(
	_ context.Context,
	report AIOpsReport,
) error {
	sink.reports = append(sink.reports, report)
	return nil
}

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
	t.Setenv("MOCK_EMBEDDING", "true")

	sink := &recordingAIOpsReportSink{}
	aiClient := ai.NewClient("")
	s := NewAgentService("http://localhost:1234/v1", "test-key", "test-model", "localhost:9200", nil, aiClient, nil, WithAIOpsReportSink(sink))
	defer s.Close()

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

	if len(sink.reports) != 1 || sink.reports[0].Report == "" ||
		!strings.Contains(sink.reports[0].StructuredRCA, "RedisDown") ||
		sink.reports[0].CreatedAt.IsZero() {
		t.Fatalf("persisted reports = %+v", sink.reports)
	}
}
