package environment

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

type tweetWriteStateReaderStub struct {
	pages  []TweetWriteStatePage
	calls  int
	userID uint64
	limit  int
	err    error
}

func (stub *tweetWriteStateReaderStub) ListRecentTweetWriteState(
	_ context.Context,
	userID uint64,
	limit int,
) (TweetWriteStatePage, error) {
	stub.userID, stub.limit = userID, limit
	if stub.err != nil {
		return TweetWriteStatePage{}, stub.err
	}
	index := stub.calls
	stub.calls++
	if index >= len(stub.pages) {
		index = len(stub.pages) - 1
	}
	page := stub.pages[index]
	page.Tweets = append([]TweetWriteState(nil), page.Tweets...)
	return page, nil
}

func TestTweetWriteEnvironmentCapturesAuthoritativeStateTransition(t *testing.T) {
	catalog := &staticToolCatalog{tools: []agentRuntime.ToolDefinition{
		{Name: "web_search", Category: agentRuntime.ToolCategoryRead, InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: TweetPublishToolName, Category: agentRuntime.ToolCategoryWrite, RequiresApproval: true, InputSchema: json.RawMessage(`{"type":"object"}`)},
	}}
	reader := &tweetWriteStateReaderStub{pages: []TweetWriteStatePage{
		{Tweets: []TweetWriteState{{TweetID: 10, AuthorID: 7}}},
		{Tweets: []TweetWriteState{{TweetID: 11, AuthorID: 7}, {TweetID: 10, AuthorID: 7}}},
	}}
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	environment, err := NewTweetWriteEnvironment(catalog, reader, 7, WithTweetWriteClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	task := tweetWriteTask()
	tools, err := environment.Tools(context.Background(), task)
	if err != nil || len(tools) != 1 || tools[0].Name != TweetPublishToolName || !tools[0].ApprovalRequired() {
		t.Fatalf("Tools() = %+v, %v", tools, err)
	}
	tools[0].InputSchema[0] = '['
	if catalog.tools[1].InputSchema[0] == '[' {
		t.Fatal("Tools() leaked mutable catalog schema")
	}

	before, err := environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{Task: task, Phase: agentRuntime.SnapshotPhaseBefore})
	if err != nil {
		t.Fatal(err)
	}
	after, err := environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{Task: task, Phase: agentRuntime.SnapshotPhaseAfter})
	if err != nil {
		t.Fatal(err)
	}
	beforeView, err := DecodeTweetWriteSnapshot(&before, agentRuntime.SnapshotPhaseBefore, 7)
	if err != nil {
		t.Fatal(err)
	}
	afterView, err := DecodeTweetWriteSnapshot(&after, agentRuntime.SnapshotPhaseAfter, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(beforeView.TweetReferences) != 1 || beforeView.TweetReferences[0] != "/tweets/10" ||
		len(afterView.TweetReferences) != 2 || afterView.TweetReferences[1] != "/tweets/11" {
		t.Fatalf("views = %+v / %+v", beforeView, afterView)
	}
	if before.Digest == after.Digest || reader.userID != 7 || reader.limit != maxTweetWriteStateItems {
		t.Fatalf("snapshot/reader = %s/%s user=%d limit=%d", before.Digest, after.Digest, reader.userID, reader.limit)
	}
	if strings.Contains(string(after.Metadata), "content") || strings.Contains(string(after.Metadata), `"user_id"`) {
		t.Fatalf("snapshot leaked sensitive state: %s", after.Metadata)
	}
}

func TestTweetWriteEnvironmentFailsClosedOnUnsafeCatalogAndState(t *testing.T) {
	tests := []struct {
		name   string
		tool   agentRuntime.ToolDefinition
		states []TweetWriteState
	}{
		{name: "read category", tool: tweetWriteTool(agentRuntime.ToolCategoryRead, true)},
		{name: "invalid schema", tool: agentRuntime.ToolDefinition{Name: TweetPublishToolName, Category: agentRuntime.ToolCategoryWrite, RequiresApproval: true, InputSchema: json.RawMessage(`{`)}},
		{name: "foreign author", tool: tweetWriteTool(agentRuntime.ToolCategoryWrite, true), states: []TweetWriteState{{TweetID: 10, AuthorID: 8}}},
		{name: "duplicate tweet", tool: tweetWriteTool(agentRuntime.ToolCategoryWrite, true), states: []TweetWriteState{{TweetID: 10, AuthorID: 7}, {TweetID: 10, AuthorID: 7}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			environment, err := NewTweetWriteEnvironment(
				&staticToolCatalog{tools: []agentRuntime.ToolDefinition{test.tool}},
				&tweetWriteStateReaderStub{pages: []TweetWriteStatePage{{Tweets: test.states}}},
				7,
			)
			if err != nil {
				t.Fatal(err)
			}
			_, err = environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{Task: tweetWriteTask(), Phase: agentRuntime.SnapshotPhaseBefore})
			if err == nil {
				t.Fatal("Snapshot() error = nil")
			}
		})
	}
}

func TestDecodeTweetWriteSnapshotRejectsTamperingAndWrongActor(t *testing.T) {
	environment, err := NewTweetWriteEnvironment(
		&staticToolCatalog{tools: []agentRuntime.ToolDefinition{tweetWriteTool(agentRuntime.ToolCategoryWrite, true)}},
		&tweetWriteStateReaderStub{pages: []TweetWriteStatePage{{Tweets: []TweetWriteState{{TweetID: 10, AuthorID: 7}}}}},
		7,
	)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{Task: tweetWriteTask(), Phase: agentRuntime.SnapshotPhaseBefore})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeTweetWriteSnapshot(&snapshot, agentRuntime.SnapshotPhaseBefore, 8); err == nil {
		t.Fatal("wrong actor snapshot was accepted")
	}
	var metadata map[string]any
	if err = json.Unmarshal(snapshot.Metadata, &metadata); err != nil {
		t.Fatal(err)
	}
	metadata["tweet_references"] = []string{"/tweets/99"}
	snapshot.Metadata, _ = json.Marshal(metadata)
	if _, err = DecodeTweetWriteSnapshot(&snapshot, agentRuntime.SnapshotPhaseBefore, 7); err == nil {
		t.Fatal("tampered snapshot was accepted")
	}
}

func tweetWriteTask() agentRuntime.TaskSpec {
	return agentRuntime.TaskSpec{
		Goal: "publish one tweet", AllowedTools: []string{TweetPublishToolName},
		CompletionCriteria: []agentRuntime.CompletionCriterion{{
			ID: "tweet_publish_committed", Description: "the tweet exists in authoritative state", Required: true,
		}},
	}
}

func tweetWriteTool(category agentRuntime.ToolCategory, approval bool) agentRuntime.ToolDefinition {
	return agentRuntime.ToolDefinition{
		Name: TweetPublishToolName, Category: category, RequiresApproval: approval,
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
}
