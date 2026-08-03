package tool

import (
	"context"
	"errors"
	"testing"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentWebSearch "twitter-clone/internal/module/agent/websearch"
	"twitter-clone/internal/module/agent/workflow/guardrails"
)

type pageReaderStub struct {
	request agentWebSearch.PageRequest
	result  agentEvidence.WebPageResult
	err     error
}

func (stub *pageReaderStub) Read(
	_ context.Context,
	request agentWebSearch.PageRequest,
) (agentEvidence.WebPageResult, error) {
	stub.request = request
	return stub.result, stub.err
}

func TestPageReadToolFailsClosedWithoutReader(t *testing.T) {
	t.Parallel()

	_, err := NewPageReadTool().Execute(context.Background(), map[string]interface{}{
		"url": "https://example.com/article",
	})
	if !errors.Is(err, agentWebSearch.ErrPageUnavailable) {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestPageReadToolReturnsStructuredResultWithTrustedSubject(t *testing.T) {
	t.Parallel()

	reader := &pageReaderStub{result: agentEvidence.WebPageResult{
		Schema:      agentEvidence.WebPageSchema,
		URL:         "https://example.com/article",
		Title:       "Example",
		ContentType: "text/html",
		Content:     "Visible page content.",
		Excerpt:     "Visible page content.",
	}}
	ctx := guardrails.InjectUserContext(context.Background(), 42)
	ctx = InjectExecutionMetadata(ctx, ExecutionMetadata{RunID: "run-page-1"})
	result, err := NewPageReadTool(reader).Execute(ctx, map[string]interface{}{
		"url":       "https://example.com/article",
		"max_runes": float64(1200),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if reader.request.Subject.UserID != 42 ||
		reader.request.Subject.RunID != "run-page-1" ||
		reader.request.MaxRunes != 1200 {
		t.Fatalf("reader request = %+v", reader.request)
	}
	if result["schema"] != agentEvidence.WebPageSchema ||
		result["url"] != "https://example.com/article" {
		t.Fatalf("result = %+v", result)
	}
	modelText, _ := result["results"].(string)
	if modelText == "" {
		t.Fatal("model-facing page text is empty")
	}
}
