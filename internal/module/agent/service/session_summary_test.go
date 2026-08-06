package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"twitter-clone/internal/module/agent/repository"
	"twitter-clone/internal/module/agent/workflow/rag"
)

type sessionSummaryRepositoryFake struct {
	repository.AgentRepository
	mu           sync.Mutex
	claim        *repository.DialogueSummaryClaim
	messages     []*repository.DialogueMessage
	claimCh      chan bool
	claimForce   bool
	claimMinimum int64
	completed    int
	released     int
}

func (f *sessionSummaryRepositoryFake) ClaimDialogueSummary(_ context.Context, _ primitive.ObjectID, _ uint64, minPendingMessages int64, force bool, _ time.Duration) (*repository.DialogueSummaryClaim, error) {
	f.mu.Lock()
	f.claimForce = force
	f.claimMinimum = minPendingMessages
	claim := f.claim
	claimCh := f.claimCh
	f.mu.Unlock()
	if claimCh != nil {
		claimCh <- force
	}
	return claim, nil
}

func (f *sessionSummaryRepositoryFake) CompleteDialogueSummary(_ context.Context, _ repository.DialogueSummaryClaim) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completed++
	return nil
}

func (f *sessionSummaryRepositoryFake) ReleaseDialogueSummary(_ context.Context, _ repository.DialogueSummaryClaim) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released++
	return nil
}

func (f *sessionSummaryRepositoryFake) GetMessages(_ context.Context, _ primitive.ObjectID) ([]*repository.DialogueMessage, error) {
	return f.messages, nil
}

type sessionSummaryWriterFake struct {
	request rag.SessionSummaryRequest
	err     error
}

type blockingSessionSummaryWriter struct {
	started chan struct{}
	done    chan struct{}
}

func (w *blockingSessionSummaryWriter) SaveSessionSummary(ctx context.Context, _ rag.SessionSummaryRequest) error {
	close(w.started)
	<-ctx.Done()
	close(w.done)
	return ctx.Err()
}

func (f *sessionSummaryWriterFake) SaveSessionSummary(_ context.Context, request rag.SessionSummaryRequest) error {
	f.request = request
	return f.err
}

type sessionEndRepositoryFake struct {
	repository.AgentRepository
	mu        sync.Mutex
	dialogue  *repository.Dialogue
	messages  []*repository.DialogueMessage
	claim     repository.DialogueSummaryClaim
	claimed   bool
	completed bool
	force     bool
}

func (f *sessionEndRepositoryFake) GetDialogue(_ context.Context, _ primitive.ObjectID) (*repository.Dialogue, error) {
	return f.dialogue, nil
}

func (f *sessionEndRepositoryFake) GetMessages(_ context.Context, _ primitive.ObjectID) ([]*repository.DialogueMessage, error) {
	return f.messages, nil
}

func (f *sessionEndRepositoryFake) ClaimDialogueSummary(_ context.Context, _ primitive.ObjectID, _ uint64, _ int64, force bool, _ time.Duration) (*repository.DialogueSummaryClaim, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.force = force
	if f.claimed || f.completed {
		return nil, nil
	}
	f.claimed = true
	claim := f.claim
	return &claim, nil
}

func (f *sessionEndRepositoryFake) CompleteDialogueSummary(_ context.Context, _ repository.DialogueSummaryClaim) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claimed = false
	f.completed = true
	return nil
}

func (f *sessionEndRepositoryFake) ReleaseDialogueSummary(_ context.Context, _ repository.DialogueSummaryClaim) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claimed = false
	return nil
}

type countingSessionSummaryWriter struct {
	mu      sync.Mutex
	calls   int
	request rag.SessionSummaryRequest
}

func (w *countingSessionSummaryWriter) SaveSessionSummary(_ context.Context, request rag.SessionSummaryRequest) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	w.request = request
	return nil
}

func TestCrystallizeDialogueSummaryUsesClaimedMessageRange(t *testing.T) {
	dialogueID := primitive.NewObjectID()
	repo := &sessionSummaryRepositoryFake{
		claim: &repository.DialogueSummaryClaim{
			DialogueID: dialogueID, UserID: 42, LeaseToken: "lease", Version: 3,
			FromMessage: 2, ThroughMessage: 4,
		},
		messages: []*repository.DialogueMessage{
			{Role: repository.RoleUser, Content: "old user"},
			{Role: repository.RoleAssistant, Content: "old answer"},
			{Role: repository.RoleUser, Content: "new question"},
			{Role: repository.RoleAssistant, Content: "new answer"},
		},
	}
	writer := &sessionSummaryWriterFake{}
	service := &AgentService{
		repo: repo, summaryWriter: writer,
		summaryMinMessages: 8, summaryLeaseDuration: time.Minute,
	}

	if err := service.crystallizeDialogueSummary(context.Background(), 42, dialogueID, false); err != nil {
		t.Fatalf("crystallize dialogue summary: %v", err)
	}
	if repo.claimForce {
		t.Fatal("expected threshold claim, got force claim")
	}
	if repo.claimMinimum != 8 {
		t.Fatalf("expected min pending messages 8, got %d", repo.claimMinimum)
	}
	if repo.completed != 1 || repo.released != 0 {
		t.Fatalf("unexpected claim finalization: completed=%d released=%d", repo.completed, repo.released)
	}
	if writer.request.SourceDialogue != dialogueID.Hex() || writer.request.SummaryVersion != 3 {
		t.Fatalf("unexpected summary identity: %#v", writer.request)
	}
	if strings.Contains(writer.request.DialogueHistory, "old user") || !strings.Contains(writer.request.DialogueHistory, "USER: new question") {
		t.Fatalf("summary did not use claimed range: %q", writer.request.DialogueHistory)
	}
}

func TestCrystallizeDialogueSummaryReleasesClaimOnWriterFailure(t *testing.T) {
	dialogueID := primitive.NewObjectID()
	repo := &sessionSummaryRepositoryFake{
		claim: &repository.DialogueSummaryClaim{
			DialogueID: dialogueID, UserID: 7, LeaseToken: "lease", Version: 1,
			FromMessage: 0, ThroughMessage: 2,
		},
		messages: []*repository.DialogueMessage{
			{Role: repository.RoleUser, Content: "question"},
			{Role: repository.RoleAssistant, Content: "answer"},
		},
	}
	writer := &sessionSummaryWriterFake{err: errors.New("embedding unavailable")}
	service := &AgentService{repo: repo, summaryWriter: writer}

	err := service.crystallizeDialogueSummary(context.Background(), 7, dialogueID, true)
	if err == nil || !strings.Contains(err.Error(), "embedding unavailable") {
		t.Fatalf("expected writer error, got %v", err)
	}
	if repo.completed != 0 || repo.released != 1 {
		t.Fatalf("expected released claim, completed=%d released=%d", repo.completed, repo.released)
	}
	if !repo.claimForce {
		t.Fatal("expected force claim")
	}
}

func TestDialogueSummaryHistoryRejectsOutOfBoundsClaim(t *testing.T) {
	_, err := dialogueSummaryHistory([]*repository.DialogueMessage{{Content: "one"}}, 0, 2)
	if err == nil {
		t.Fatal("expected invalid claim range error")
	}
}

func TestScheduleSessionSummaryTriggersThresholdAndIdleClaims(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	repo := &sessionSummaryRepositoryFake{claimCh: make(chan bool, 2)}
	service := &AgentService{
		repo: repo, summaryWriter: &sessionSummaryWriterFake{},
		serviceCtx: ctx, cancelFunc: cancel,
		summaryMinMessages: 12, summaryIdleDelay: 20 * time.Millisecond,
		summaryLeaseDuration: time.Second,
		summaryTimers:        make(map[primitive.ObjectID]*time.Timer),
	}
	defer service.Close()

	service.scheduleSessionSummary(9, primitive.NewObjectID())
	seen := map[bool]bool{}
	deadline := time.After(time.Second)
	for len(seen) < 2 {
		select {
		case force := <-repo.claimCh:
			seen[force] = true
		case <-deadline:
			t.Fatalf("timed out waiting for threshold and idle claims: %#v", seen)
		}
	}
}

func TestCancelSessionSummaryTimerCancelsInFlightJob(t *testing.T) {
	dialogueID := primitive.NewObjectID()
	ctx, cancel := context.WithCancel(context.Background())
	repo := &sessionSummaryRepositoryFake{
		claim: &repository.DialogueSummaryClaim{
			DialogueID: dialogueID, UserID: 9, LeaseToken: "lease", Version: 1,
			FromMessage: 0, ThroughMessage: 2,
		},
		messages: []*repository.DialogueMessage{
			{Role: repository.RoleUser, Content: "question"},
			{Role: repository.RoleAssistant, Content: "answer"},
		},
	}
	writer := &blockingSessionSummaryWriter{started: make(chan struct{}), done: make(chan struct{})}
	service := &AgentService{
		repo: repo, summaryWriter: writer,
		serviceCtx: ctx, cancelFunc: cancel,
		summaryJobs:   make(map[primitive.ObjectID]map[string]context.CancelFunc),
		summaryTimers: make(map[primitive.ObjectID]*time.Timer),
	}
	defer service.Close()

	service.runSessionSummaryAsync(9, dialogueID, true)
	select {
	case <-writer.started:
	case <-time.After(time.Second):
		t.Fatal("summary writer did not start")
	}
	service.cancelSessionSummaryTimer(dialogueID)
	select {
	case <-writer.done:
	case <-time.After(time.Second):
		t.Fatal("in-flight summary writer was not cancelled")
	}
}

func TestEndDialogueSessionIsConcurrentAndIdempotent(t *testing.T) {
	dialogueID := primitive.NewObjectID()
	repo := &sessionEndRepositoryFake{
		dialogue: &repository.Dialogue{ID: dialogueID, UserID: 42},
		messages: []*repository.DialogueMessage{
			{Role: repository.RoleUser, Content: "remember this preference"},
			{Role: repository.RoleAssistant, Content: "preference acknowledged"},
		},
		claim: repository.DialogueSummaryClaim{
			DialogueID: dialogueID, UserID: 42, LeaseToken: "lease", Version: 1,
			FromMessage: 0, ThroughMessage: 2,
		},
	}
	writer := &countingSessionSummaryWriter{}
	service := &AgentService{
		repo: repo, summaryWriter: writer,
		summaryJobs:   make(map[primitive.ObjectID]map[string]context.CancelFunc),
		summaryTimers: make(map[primitive.ObjectID]*time.Timer),
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var group sync.WaitGroup
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			errs <- service.EndDialogueSession(context.Background(), 42, dialogueID.Hex())
		}()
	}
	close(start)
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("end dialogue session: %v", err)
		}
	}

	writer.mu.Lock()
	calls := writer.calls
	request := writer.request
	writer.mu.Unlock()
	if calls != 1 {
		t.Fatalf("expected one durable summary write, got %d", calls)
	}
	if request.SourceDialogue != dialogueID.Hex() || request.UserID != 42 {
		t.Fatalf("unexpected summary request: %#v", request)
	}
	repo.mu.Lock()
	force := repo.force
	completed := repo.completed
	repo.mu.Unlock()
	if !force || !completed {
		t.Fatalf("expected a completed force claim, force=%v completed=%v", force, completed)
	}
}
