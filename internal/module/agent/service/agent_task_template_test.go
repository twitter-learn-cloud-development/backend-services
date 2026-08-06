package service

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type memoryAgentTaskTemplateStore struct {
	mu        sync.Mutex
	templates map[primitive.ObjectID]*repository.AgentTaskTemplate
	byKey     map[string]primitive.ObjectID
	creates   int
}

func newMemoryAgentTaskTemplateStore() *memoryAgentTaskTemplateStore {
	return &memoryAgentTaskTemplateStore{
		templates: make(map[primitive.ObjectID]*repository.AgentTaskTemplate),
		byKey:     make(map[string]primitive.ObjectID),
	}
}

func (s *memoryAgentTaskTemplateStore) CreateAgentTaskTemplate(
	_ context.Context,
	template *repository.AgentTaskTemplate,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := templateStoreKey(template.UserID, template.IdempotencyKey)
	if _, exists := s.byKey[key]; exists {
		return repository.ErrAgentTaskTemplateConflict
	}
	s.creates++
	if template.ID.IsZero() {
		template.ID = primitive.NewObjectID()
	}
	if template.Revision == 0 {
		template.Revision = 1
	}
	if template.Status == "" {
		template.Status = repository.AgentTaskTemplateActive
	}
	if template.CreatedAt.IsZero() {
		template.CreatedAt = time.Now()
	}
	if template.UpdatedAt.IsZero() {
		template.UpdatedAt = template.CreatedAt
	}
	s.templates[template.ID] = cloneTaskTemplate(template)
	s.byKey[key] = template.ID
	return nil
}

func (s *memoryAgentTaskTemplateStore) GetAgentTaskTemplate(
	_ context.Context,
	templateID primitive.ObjectID,
	userID uint64,
) (*repository.AgentTaskTemplate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	template := s.templates[templateID]
	if template == nil || template.UserID != userID {
		return nil, repository.ErrAgentTaskTemplateNotFound
	}
	return cloneTaskTemplate(template), nil
}

func (s *memoryAgentTaskTemplateStore) GetAgentTaskTemplateByIdempotencyKey(
	_ context.Context,
	userID uint64,
	idempotencyKey string,
) (*repository.AgentTaskTemplate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	templateID, exists := s.byKey[templateStoreKey(userID, idempotencyKey)]
	if !exists {
		return nil, repository.ErrAgentTaskTemplateNotFound
	}
	return cloneTaskTemplate(s.templates[templateID]), nil
}

func (s *memoryAgentTaskTemplateStore) ListActiveAgentTaskTemplates(
	_ context.Context,
	userID uint64,
	limit int,
) ([]*repository.AgentTaskTemplate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]*repository.AgentTaskTemplate, 0, limit)
	for _, template := range s.templates {
		if template.UserID == userID && template.Status == repository.AgentTaskTemplateActive {
			result = append(result, cloneTaskTemplate(template))
			if len(result) == limit {
				break
			}
		}
	}
	return result, nil
}

func (s *memoryAgentTaskTemplateStore) ArchiveAgentTaskTemplate(
	_ context.Context,
	templateID primitive.ObjectID,
	userID uint64,
	expectedRevision int64,
	now time.Time,
) (*repository.AgentTaskTemplate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	template := s.templates[templateID]
	if template == nil || template.UserID != userID {
		return nil, repository.ErrAgentTaskTemplateNotFound
	}
	if template.Status != repository.AgentTaskTemplateActive ||
		template.Revision != expectedRevision {
		return nil, repository.ErrAgentTaskTemplateConflict
	}
	template.Status = repository.AgentTaskTemplateArchived
	template.Revision++
	template.UpdatedAt = now
	template.ArchivedAt = now
	return cloneTaskTemplate(template), nil
}

func TestCreateAgentTaskTemplateRequiresCompletedAuthoritativeRun(t *testing.T) {
	runStore := &memoryAgentExecutionRunStore{run: &repository.AgentExecutionRun{
		ID: "source-run", UserID: 42, Revision: 2,
		Status:       repository.AgentExecutionRunFailed,
		ResultDigest: "digest", ExecutionProfile: ExecutionProfileRuntimeChat,
		CapabilityIDs: []string{CapabilityConversationReply},
	}}
	templateStore := newMemoryAgentTaskTemplateStore()
	svc := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		&assistRuntimeRepository{}, nil, nil,
		WithAgentExecutionRunStore(runStore),
		WithRecoverableAgentRuns(true),
		WithAgentTaskTemplates(templateStore, true, 20),
	)
	defer svc.Close()

	_, err := svc.CreateAgentTaskTemplate(context.Background(), validCreateTaskTemplateRequest())
	if !errors.Is(err, ErrAgentTaskTemplateSourceIncomplete) {
		t.Fatalf("CreateAgentTaskTemplate() error = %v", err)
	}
	if templateStore.creates != 0 {
		t.Fatalf("creates = %d, want 0", templateStore.creates)
	}
}

func TestCreateAgentTaskTemplateIsIdempotentAndStoresOnlySourceEvidence(t *testing.T) {
	runStore := completedTemplateSourceRunStore()
	templateStore := newMemoryAgentTaskTemplateStore()
	svc := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		&assistRuntimeRepository{}, nil, nil,
		WithAgentExecutionRunStore(runStore),
		WithRecoverableAgentRuns(true),
		WithAgentTaskTemplates(templateStore, true, 20),
	)
	defer svc.Close()

	request := validCreateTaskTemplateRequest()
	first, err := svc.CreateAgentTaskTemplate(context.Background(), request)
	if err != nil {
		t.Fatalf("CreateAgentTaskTemplate() error = %v", err)
	}
	second, err := svc.CreateAgentTaskTemplate(context.Background(), request)
	if err != nil {
		t.Fatalf("idempotent CreateAgentTaskTemplate() error = %v", err)
	}
	if first.TemplateID == "" || second.TemplateID != first.TemplateID ||
		templateStore.creates != 1 {
		t.Fatalf("first = %+v, second = %+v, creates = %d", first, second, templateStore.creates)
	}
	if first.SourceRunID != "source-run" || first.SourceResultDigest != "result-digest" ||
		first.InstructionTemplate != "Summarize this input: {{input}}" {
		t.Fatalf("template view = %+v", first)
	}
}

func TestRunAgentTaskTemplateRevalidatesEvidenceAndAuditsNewRun(t *testing.T) {
	runStore := completedTemplateSourceRunStore()
	templateStore := newMemoryAgentTaskTemplateStore()
	templateID := primitive.NewObjectID()
	template := &repository.AgentTaskTemplate{
		ID: templateID, ContractVersion: repository.AgentTaskTemplateContractVersion,
		UserID: 42, Name: "Summary", InstructionTemplate: "Summarize this input: {{input}}",
		Status: repository.AgentTaskTemplateActive, Revision: 1,
		IdempotencyKey: "template-existing", SourceRunID: "source-run",
		SourceRunRevision: 2, SourceResultDigest: "result-digest",
		SourceExecutionProfile: ExecutionProfileRuntimeChat,
		CapabilityIDs:          []string{CapabilityConversationReply},
		SourceModel:            "source-model",
		AgentProfileID:         "profile-1",
		AgentProfileVersion:    "3",
		PromptTemplateID:       "prompt-1",
		PromptTemplateVersion:  "7",
		CreatedAt:              time.Now(), UpdatedAt: time.Now(),
	}
	templateStore.templates[templateID] = cloneTaskTemplate(template)
	templateStore.byKey[templateStoreKey(42, template.IdempotencyKey)] = templateID
	dialogueRepo := &assistRuntimeRepository{}
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status: agentRuntime.RunStatusCompleted, FinalAnswer: "summary result",
		Steps: []agentRuntime.Step{{Index: 1}},
	}}
	svc := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		dialogueRepo, nil, nil,
		WithAgentRunner(runner),
		WithAgentExecutionRunStore(runStore),
		WithRecoverableAgentRuns(true),
		WithAgentTaskTemplates(templateStore, true, 20),
	)
	defer svc.Close()

	result, err := svc.RunAgentTaskTemplate(context.Background(), RunAgentTaskTemplateRequest{
		UserID: 42, TemplateID: templateID.Hex(), ExpectedTemplateRevision: 1,
		Input: "Go Agent Runtime",
	})
	if err != nil {
		t.Fatalf("RunAgentTaskTemplate() error = %v", err)
	}
	if result.Response != "summary result" ||
		result.SelectedTaskTemplateID != templateID.Hex() ||
		result.SelectedTaskTemplateRevision != 1 {
		t.Fatalf("result = %+v", result)
	}
	if len(dialogueRepo.saved) != 2 ||
		dialogueRepo.saved[0].Content != "Summarize this input: Go Agent Runtime" {
		t.Fatalf("saved messages = %+v", dialogueRepo.saved)
	}
	if runStore.run == nil || runStore.run.TaskTemplateID != templateID.Hex() ||
		runStore.run.TaskTemplateRevision != 1 ||
		runStore.run.Status != repository.AgentExecutionRunCompleted {
		t.Fatalf("persisted run = %+v", runStore.run)
	}
}

func TestRunAgentTaskTemplateRejectsSourceEvidenceDrift(t *testing.T) {
	runStore := completedTemplateSourceRunStore()
	templateStore := newMemoryAgentTaskTemplateStore()
	templateID := primitive.NewObjectID()
	templateStore.templates[templateID] = &repository.AgentTaskTemplate{
		ID: templateID, ContractVersion: repository.AgentTaskTemplateContractVersion,
		UserID: 42, Name: "Summary", InstructionTemplate: "Summarize: {{input}}",
		Status: repository.AgentTaskTemplateActive, Revision: 1,
		IdempotencyKey: "template-drift", SourceRunID: "source-run",
		SourceRunRevision: 2, SourceResultDigest: "result-digest",
		SourceExecutionProfile: ExecutionProfileRuntimeChat,
		CapabilityIDs:          []string{CapabilityConversationReply},
		SourceModel:            "source-model",
		AgentProfileID:         "profile-1",
		AgentProfileVersion:    "different-version",
		PromptTemplateID:       "prompt-1",
		PromptTemplateVersion:  "7",
		CreatedAt:              time.Now(), UpdatedAt: time.Now(),
	}
	templateStore.byKey[templateStoreKey(42, "template-drift")] = templateID
	runner := &capturingRuntimeRunner{}
	svc := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		&assistRuntimeRepository{}, nil, nil,
		WithAgentRunner(runner),
		WithAgentExecutionRunStore(runStore),
		WithRecoverableAgentRuns(true),
		WithAgentTaskTemplates(templateStore, true, 20),
	)
	defer svc.Close()

	_, err := svc.RunAgentTaskTemplate(context.Background(), RunAgentTaskTemplateRequest{
		UserID: 42, TemplateID: templateID.Hex(), ExpectedTemplateRevision: 1,
		Input: "Go Agent Runtime",
	})
	if !errors.Is(err, ErrAgentTaskTemplateSourceIncomplete) {
		t.Fatalf("RunAgentTaskTemplate() error = %v", err)
	}
	if runner.calls != 0 {
		t.Fatalf("runner calls = %d, want 0", runner.calls)
	}
}

func validCreateTaskTemplateRequest() CreateAgentTaskTemplateRequest {
	return CreateAgentTaskTemplateRequest{
		UserID: 42, SourceRunID: "source-run", ExpectedSourceRunRevision: 2,
		Name: "Summary", Description: "Reusable summary",
		InstructionTemplate: "Summarize this input: {{input}}",
		IdempotencyKey:      "request-1",
	}
}

func completedTemplateSourceRunStore() *memoryAgentExecutionRunStore {
	return &memoryAgentExecutionRunStore{run: &repository.AgentExecutionRun{
		ID: "source-run", UserID: 42, Revision: 2,
		Status:       repository.AgentExecutionRunCompleted,
		ResultDigest: "result-digest", ExecutionProfile: ExecutionProfileRuntimeChat,
		CapabilityIDs: []string{CapabilityConversationReply},
		Model:         "source-model", AgentProfileID: "profile-1",
		AgentProfileVersion: "3", PromptTemplateID: "prompt-1",
		PromptTemplateVersion: "7",
	}}
}

func templateStoreKey(userID uint64, idempotencyKey string) string {
	return strconv.FormatUint(userID, 10) + ":" + idempotencyKey
}

func cloneTaskTemplate(template *repository.AgentTaskTemplate) *repository.AgentTaskTemplate {
	if template == nil {
		return nil
	}
	cloned := *template
	cloned.CapabilityIDs = append([]string(nil), template.CapabilityIDs...)
	return &cloned
}
