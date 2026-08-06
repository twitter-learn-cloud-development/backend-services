package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"twitter-clone/internal/module/agent/repository"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"
)

const DefaultToolResultInlineMaxBytes = 64 << 10

var (
	ErrToolResultObjectStoreUnavailable = errors.New("tool result object store is unavailable")
	ErrToolResultCorrupt                = errors.New("tool result object is corrupt")
)

type ToolResultObjectStore interface {
	Put(ctx context.Context, request workflowTool.ResultArchiveRequest) (*workflowTool.ResultReference, error)
	Get(ctx context.Context, reference workflowTool.ResultReference, maxBytes int) ([]byte, error)
	Delete(ctx context.Context, reference workflowTool.ResultReference) error
}

type PersistentToolResultStoreOption func(*PersistentToolResultStore)

func WithToolResultObjectStore(store ToolResultObjectStore) PersistentToolResultStoreOption {
	return func(resultStore *PersistentToolResultStore) {
		resultStore.objects = store
	}
}

func WithToolResultLimits(inlineMaxBytes, maxBytes int) PersistentToolResultStoreOption {
	return func(resultStore *PersistentToolResultStore) {
		if inlineMaxBytes > 0 {
			resultStore.inlineMaxBytes = inlineMaxBytes
		}
		if maxBytes > 0 {
			resultStore.maxBytes = maxBytes
		}
	}
}

type PersistentToolResultStore struct {
	repo           repository.ToolExecutionRepository
	objects        ToolResultObjectStore
	inlineMaxBytes int
	maxBytes       int
}

func NewPersistentToolResultStore(repo repository.ToolExecutionRepository, options ...PersistentToolResultStoreOption) *PersistentToolResultStore {
	store := &PersistentToolResultStore{
		repo: repo, inlineMaxBytes: DefaultToolResultInlineMaxBytes,
		maxBytes: workflowTool.DefaultMaxResultBytes,
	}
	for _, option := range options {
		if option != nil {
			option(store)
		}
	}
	if store.inlineMaxBytes > store.maxBytes {
		store.inlineMaxBytes = store.maxBytes
	}
	return store
}

func (s *PersistentToolResultStore) Claim(ctx context.Context, check workflowTool.IdempotencyCheck) (workflowTool.IdempotencyClaim, error) {
	if s == nil || s.repo == nil {
		return workflowTool.IdempotencyClaim{}, errors.New("tool result store is unavailable")
	}
	attemptID := workflowTool.NewAttemptID()
	record, acquired, err := s.repo.ClaimToolExecution(ctx, &repository.ToolExecutionRecord{
		UserID: check.UserID, ToolName: check.ToolName, IdempotencyKey: check.IdempotencyKey,
		InputDigest: check.InputDigest, AttemptID: attemptID, LeaseUntil: check.LeaseUntil,
	})
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrToolExecutionConflict):
			return workflowTool.IdempotencyClaim{}, workflowTool.ErrIdempotencyConflict
		case errors.Is(err, repository.ErrToolExecutionInProgress):
			return workflowTool.IdempotencyClaim{}, workflowTool.ErrAlreadyExecuting
		default:
			return workflowTool.IdempotencyClaim{}, err
		}
	}
	claim := workflowTool.IdempotencyClaim{
		ExecutionID: record.ID.Hex(), AttemptID: attemptID, UserID: check.UserID,
		Replayed: !acquired, Outputs: record.Output,
	}
	if acquired || record.OutputReference == nil {
		return claim, nil
	}
	if s.objects == nil {
		return workflowTool.IdempotencyClaim{}, ErrToolResultObjectStoreUnavailable
	}
	reference := toolResultReferenceFromRepository(record.OutputReference)
	payload, err := s.objects.Get(ctx, reference, s.maxBytes)
	if err != nil {
		return workflowTool.IdempotencyClaim{}, fmt.Errorf("load archived tool result: %w", err)
	}
	if err := validateArchivedToolResult(payload, reference); err != nil {
		return workflowTool.IdempotencyClaim{}, err
	}
	var outputs map[string]interface{}
	if err := json.Unmarshal(payload, &outputs); err != nil {
		return workflowTool.IdempotencyClaim{}, fmt.Errorf("%w: decode JSON: %v", ErrToolResultCorrupt, err)
	}
	claim.Outputs = outputs
	claim.OutputReference = &reference
	return claim, nil
}

func (s *PersistentToolResultStore) Archive(ctx context.Context, request workflowTool.ResultArchiveRequest) (*workflowTool.ResultReference, error) {
	if s == nil {
		return nil, errors.New("tool result store is unavailable")
	}
	if request.Snapshot.Length <= s.inlineMaxBytes {
		return nil, nil
	}
	if request.Snapshot.Length > s.maxBytes {
		return nil, fmt.Errorf("%w: got %d bytes, limit %d", workflowTool.ErrResultTooLarge, request.Snapshot.Length, s.maxBytes)
	}
	if s.objects == nil {
		return nil, ErrToolResultObjectStoreUnavailable
	}
	return s.objects.Put(ctx, request)
}

func (s *PersistentToolResultStore) Discard(ctx context.Context, reference workflowTool.ResultReference) error {
	if s == nil || s.objects == nil {
		return nil
	}
	return s.objects.Delete(ctx, reference)
}

func (s *PersistentToolResultStore) Complete(ctx context.Context, claim workflowTool.IdempotencyClaim, result workflowTool.ResultSnapshot) error {
	if s == nil || s.repo == nil {
		return errors.New("tool result store is unavailable")
	}
	id, err := primitive.ObjectIDFromHex(claim.ExecutionID)
	if err != nil {
		return fmt.Errorf("invalid tool execution id: %w", err)
	}
	if result.Length > s.inlineMaxBytes && result.Reference == nil {
		return ErrToolResultObjectStoreUnavailable
	}
	persisted := repository.ToolExecutionResult{Digest: result.Digest, Length: result.Length}
	if result.Reference == nil {
		persisted.Output = result.Outputs
	} else {
		persisted.OutputReference = toolResultReferenceToRepository(result.Reference)
	}
	return s.repo.CompleteToolExecution(ctx, id, claim.AttemptID, persisted)
}

func (s *PersistentToolResultStore) Fail(ctx context.Context, claim workflowTool.IdempotencyClaim, cause error) error {
	if s == nil || s.repo == nil {
		return errors.New("tool result store is unavailable")
	}
	id, err := primitive.ObjectIDFromHex(claim.ExecutionID)
	if err != nil {
		return fmt.Errorf("invalid tool execution id: %w", err)
	}
	message := "tool execution failed"
	if cause != nil {
		message = cause.Error()
	}
	return s.repo.FailToolExecution(ctx, id, claim.AttemptID, message)
}

func validateArchivedToolResult(payload []byte, reference workflowTool.ResultReference) error {
	if len(payload) != reference.Length {
		return fmt.Errorf("%w: expected %d bytes, got %d", ErrToolResultCorrupt, reference.Length, len(payload))
	}
	sum := sha256.Sum256(payload)
	if hex.EncodeToString(sum[:]) != reference.Digest {
		return fmt.Errorf("%w: digest mismatch", ErrToolResultCorrupt)
	}
	return nil
}

func toolResultReferenceToRepository(reference *workflowTool.ResultReference) *repository.ToolResultReference {
	if reference == nil {
		return nil
	}
	return &repository.ToolResultReference{
		Storage: reference.Storage, Bucket: reference.Bucket, Key: reference.Key,
		Digest: reference.Digest, Length: reference.Length, ContentType: reference.ContentType,
	}
}

func toolResultReferenceFromRepository(reference *repository.ToolResultReference) workflowTool.ResultReference {
	if reference == nil {
		return workflowTool.ResultReference{}
	}
	return workflowTool.ResultReference{
		Storage: reference.Storage, Bucket: reference.Bucket, Key: reference.Key,
		Digest: reference.Digest, Length: reference.Length, ContentType: reference.ContentType,
	}
}
