package service

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"

	"twitter-clone/internal/module/agent/repository"
	"twitter-clone/internal/module/agent/workflow/rag"
	"twitter-clone/pkg/logger"
)

const (
	defaultSessionSummaryMinMessages   int64 = 12
	defaultSessionSummaryIdleDelay           = 2 * time.Minute
	defaultSessionSummaryLeaseDuration       = 45 * time.Second
	defaultSessionSummaryTimeout             = 30 * time.Second
)

type SessionSummaryWriter interface {
	SaveSessionSummary(ctx context.Context, request rag.SessionSummaryRequest) error
}

func (s *AgentService) scheduleSessionSummary(userID uint64, dialogueID primitive.ObjectID) {
	if !s.sessionSummaryReady() {
		return
	}

	s.summaryMu.Lock()
	if s.summaryTimers == nil {
		s.summaryTimers = make(map[primitive.ObjectID]*time.Timer)
	}
	if existing := s.summaryTimers[dialogueID]; existing != nil {
		existing.Stop()
	}
	idleDelay := s.summaryIdleDelay
	if idleDelay <= 0 {
		idleDelay = defaultSessionSummaryIdleDelay
	}
	var timer *time.Timer
	timer = time.AfterFunc(idleDelay, func() {
		owned := false
		s.summaryMu.Lock()
		if s.summaryTimers[dialogueID] == timer {
			delete(s.summaryTimers, dialogueID)
			owned = true
		}
		s.summaryMu.Unlock()
		if owned {
			s.runSessionSummaryAsync(userID, dialogueID, true)
		}
	})
	s.summaryTimers[dialogueID] = timer
	s.summaryMu.Unlock()

	// Threshold crystallization keeps long-running sessions bounded even when
	// they never become idle. The repository lease makes concurrent attempts
	// for the same dialogue a no-op.
	s.runSessionSummaryAsync(userID, dialogueID, false)
}

func (s *AgentService) sessionSummaryReady() bool {
	if s == nil || s.summaryWriter == nil || s.repo == nil {
		return false
	}
	_, ok := s.repo.(repository.DialogueSummaryRepository)
	return ok
}

func (s *AgentService) runSessionSummaryAsync(userID uint64, dialogueID primitive.ObjectID, force bool) {
	baseCtx := s.serviceCtx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	select {
	case <-baseCtx.Done():
		return
	default:
	}
	jobID := primitive.NewObjectID().Hex()
	jobCtx, cancelJob := context.WithCancel(baseCtx)
	s.summaryMu.Lock()
	if s.summaryJobs == nil {
		s.summaryJobs = make(map[primitive.ObjectID]map[string]context.CancelFunc)
	}
	if s.summaryJobs[dialogueID] == nil {
		s.summaryJobs[dialogueID] = make(map[string]context.CancelFunc)
	}
	s.summaryJobs[dialogueID][jobID] = cancelJob
	s.summaryMu.Unlock()

	go func() {
		defer cancelJob()
		defer s.unregisterSessionSummaryJob(dialogueID, jobID)
		ctx, cancel := context.WithTimeout(jobCtx, defaultSessionSummaryTimeout)
		defer cancel()
		if err := s.crystallizeDialogueSummary(ctx, userID, dialogueID, force); err != nil && !errors.Is(err, context.Canceled) {
			logger.Warn(ctx, "crystallize session summary failed",
				zap.String("dialogue_id", dialogueID.Hex()),
				zap.Bool("force", force),
				zap.Error(err),
			)
		}
	}()
}

func (s *AgentService) crystallizeDialogueSummary(ctx context.Context, userID uint64, dialogueID primitive.ObjectID, force bool) error {
	summaryRepo, ok := s.repo.(repository.DialogueSummaryRepository)
	if !ok || s.summaryWriter == nil {
		return nil
	}

	minMessages := s.summaryMinMessages
	if minMessages <= 0 {
		minMessages = defaultSessionSummaryMinMessages
	}
	leaseDuration := s.summaryLeaseDuration
	if leaseDuration <= 0 {
		leaseDuration = defaultSessionSummaryLeaseDuration
	}
	claim, err := summaryRepo.ClaimDialogueSummary(ctx, dialogueID, userID, minMessages, force, leaseDuration)
	if err != nil || claim == nil {
		return err
	}
	release := true
	defer func() {
		if release {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = summaryRepo.ReleaseDialogueSummary(releaseCtx, *claim)
		}
	}()

	messages, err := s.repo.GetMessages(ctx, dialogueID)
	if err != nil {
		return fmt.Errorf("load dialogue messages for summary failed: %w", err)
	}
	history, err := dialogueSummaryHistory(messages, claim.FromMessage, claim.ThroughMessage)
	if err != nil {
		return err
	}
	if history == "" {
		return nil
	}

	if err := s.summaryWriter.SaveSessionSummary(ctx, rag.SessionSummaryRequest{
		UserID:          userID,
		PointID:         dialogueMemoryPointID(dialogueID, int64(claim.Version)),
		SourceDialogue:  dialogueID.Hex(),
		SummaryVersion:  claim.Version,
		DialogueHistory: history,
	}); err != nil {
		return err
	}
	if err := summaryRepo.CompleteDialogueSummary(ctx, *claim); err != nil {
		return err
	}
	release = false
	return nil
}

func dialogueSummaryHistory(messages []*repository.DialogueMessage, fromMessage int64, throughMessage int64) (string, error) {
	if fromMessage < 0 || throughMessage < fromMessage || throughMessage > int64(len(messages)) {
		return "", fmt.Errorf("invalid summary message range [%d,%d) for %d messages", fromMessage, throughMessage, len(messages))
	}
	var builder strings.Builder
	for _, message := range messages[fromMessage:throughMessage] {
		if message == nil || strings.TrimSpace(message.Content) == "" {
			continue
		}
		role := strings.ToUpper(string(message.Role))
		if role == "" {
			role = "UNKNOWN"
		}
		builder.WriteString(role)
		builder.WriteString(": ")
		builder.WriteString(strings.TrimSpace(message.Content))
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String()), nil
}

func dialogueMemoryPointID(dialogueID primitive.ObjectID, version int64) uint64 {
	idBytes := dialogueID[:]
	base := binary.BigEndian.Uint64(idBytes[4:])
	return base ^ uint64(version)
}

func (s *AgentService) cancelSessionSummaryTimer(dialogueID primitive.ObjectID) {
	if s == nil {
		return
	}
	s.summaryMu.Lock()
	if timer := s.summaryTimers[dialogueID]; timer != nil {
		timer.Stop()
		delete(s.summaryTimers, dialogueID)
	}
	for _, cancel := range s.summaryJobs[dialogueID] {
		cancel()
	}
	s.summaryMu.Unlock()
}

// waitForSessionSummaryJobs makes an explicit session end a lifecycle barrier:
// all cancelled background attempts must unregister before the synchronous
// force claim runs. This prevents a still-held lease from making End look
// successful while no summary was actually finalized.
func (s *AgentService) waitForSessionSummaryJobs(ctx context.Context, dialogueID primitive.ObjectID) error {
	if ctx == nil {
		return errors.New("session end context is nil")
	}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		s.summaryMu.Lock()
		pending := len(s.summaryJobs[dialogueID])
		s.summaryMu.Unlock()
		if pending == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *AgentService) stopSessionSummaryTimers() {
	if s == nil {
		return
	}
	s.summaryMu.Lock()
	for dialogueID, timer := range s.summaryTimers {
		if timer != nil {
			timer.Stop()
		}
		delete(s.summaryTimers, dialogueID)
	}
	for dialogueID, jobs := range s.summaryJobs {
		for _, cancel := range jobs {
			cancel()
		}
		delete(s.summaryJobs, dialogueID)
	}
	s.summaryMu.Unlock()
}

func (s *AgentService) unregisterSessionSummaryJob(dialogueID primitive.ObjectID, jobID string) {
	s.summaryMu.Lock()
	jobs := s.summaryJobs[dialogueID]
	delete(jobs, jobID)
	if len(jobs) == 0 {
		delete(s.summaryJobs, dialogueID)
	}
	s.summaryMu.Unlock()
}
