package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	agentObservability "twitter-clone/internal/module/agent/observability"
)

const (
	CollectionAgentRunTraces      = "agent_run_traces"
	CollectionAgentStepTraces     = "agent_step_traces"
	CollectionAgentLLMCallTraces  = "agent_llm_call_traces"
	CollectionAgentToolCallTraces = "agent_tool_call_traces"
	maxTraceRecordsPerKind        = int64(1000)
)

type MongoExecutionTraceRepository struct {
	runColl      *mongo.Collection
	stepColl     *mongo.Collection
	llmCallColl  *mongo.Collection
	toolCallColl *mongo.Collection
}

func NewMongoExecutionTraceRepository(db *mongo.Database) *MongoExecutionTraceRepository {
	return &MongoExecutionTraceRepository{
		runColl: db.Collection(CollectionAgentRunTraces), stepColl: db.Collection(CollectionAgentStepTraces),
		llmCallColl: db.Collection(CollectionAgentLLMCallTraces), toolCallColl: db.Collection(CollectionAgentToolCallTraces),
	}
}

func (r *MongoExecutionTraceRepository) EnsureIndexes(ctx context.Context) error {
	if r == nil {
		return errors.New("execution trace repository is nil")
	}
	models := []mongo.IndexModel{
		{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "record_id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "run_id", Value: 1}, {Key: "sequence", Value: 1}, {Key: "record_id", Value: 1}}},
	}
	for name, collection := range map[string]*mongo.Collection{
		"run": r.runColl, "step": r.stepColl, "llm call": r.llmCallColl, "tool call": r.toolCallColl,
	} {
		if _, err := collection.Indexes().CreateMany(ctx, models); err != nil {
			return fmt.Errorf("create %s trace indexes failed: %w", name, err)
		}
	}
	return nil
}

func (r *MongoExecutionTraceRepository) RecordRun(ctx context.Context, record agentObservability.RunRecord) error {
	return upsertExecutionTrace(ctx, r.runColl, record.RecordID, record.RunID, record.UserID, record)
}

func (r *MongoExecutionTraceRepository) RecordStep(ctx context.Context, record agentObservability.StepRecord) error {
	return upsertExecutionTrace(ctx, r.stepColl, record.RecordID, record.RunID, record.UserID, record)
}

func (r *MongoExecutionTraceRepository) RecordLLMCall(ctx context.Context, record agentObservability.LLMCallRecord) error {
	return upsertExecutionTrace(ctx, r.llmCallColl, record.RecordID, record.RunID, record.UserID, record)
}

func (r *MongoExecutionTraceRepository) RecordToolCall(ctx context.Context, record agentObservability.ToolCallRecord) error {
	return upsertExecutionTrace(ctx, r.toolCallColl, record.RecordID, record.RunID, record.UserID, record)
}

func upsertExecutionTrace(ctx context.Context, collection *mongo.Collection, recordID, runID string, userID uint64, record interface{}) error {
	if collection == nil {
		return errors.New("execution trace collection is not configured")
	}
	if strings.TrimSpace(recordID) == "" || strings.TrimSpace(runID) == "" || userID == 0 {
		return errors.New("execution trace identity is incomplete")
	}
	_, err := collection.UpdateOne(ctx, bson.M{"record_id": recordID, "user_id": userID}, bson.M{"$set": record}, options.Update().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("upsert execution trace failed: %w", err)
	}
	return nil
}

func (r *MongoExecutionTraceRepository) GetTraceBundle(ctx context.Context, userID uint64, runID string) (*agentObservability.TraceBundle, error) {
	if userID == 0 || strings.TrimSpace(runID) == "" {
		return nil, errors.New("trace query identity is incomplete")
	}
	bundle := &agentObservability.TraceBundle{Steps: []agentObservability.StepRecord{}, LLMCalls: []agentObservability.LLMCallRecord{}, ToolCalls: []agentObservability.ToolCallRecord{}}
	filter := bson.M{"user_id": userID, "run_id": runID}
	var run agentObservability.RunRecord
	if err := r.runColl.FindOne(ctx, filter).Decode(&run); err == nil {
		bundle.Run = &run
	} else if err != mongo.ErrNoDocuments {
		return nil, fmt.Errorf("find run trace failed: %w", err)
	}
	var err error
	if bundle.Steps, err = findExecutionTraces[agentObservability.StepRecord](ctx, r.stepColl, filter); err != nil {
		return nil, fmt.Errorf("find step traces failed: %w", err)
	}
	if bundle.LLMCalls, err = findExecutionTraces[agentObservability.LLMCallRecord](ctx, r.llmCallColl, filter); err != nil {
		return nil, fmt.Errorf("find LLM call traces failed: %w", err)
	}
	if bundle.ToolCalls, err = findExecutionTraces[agentObservability.ToolCallRecord](ctx, r.toolCallColl, filter); err != nil {
		return nil, fmt.Errorf("find tool call traces failed: %w", err)
	}
	return bundle, nil
}

func findExecutionTraces[T any](ctx context.Context, collection *mongo.Collection, filter bson.M) ([]T, error) {
	cursor, err := collection.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "sequence", Value: 1}, {Key: "record_id", Value: 1}}).SetLimit(maxTraceRecordsPerKind))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	result := []T{}
	if err := cursor.All(ctx, &result); err != nil {
		return nil, err
	}
	return result, nil
}
