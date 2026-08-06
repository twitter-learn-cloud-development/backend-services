package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tweetv1 "twitter-clone/api/tweet/v1"
	"twitter-clone/internal/module/agent/attribution"
	agentObservability "twitter-clone/internal/module/agent/observability"
	"twitter-clone/internal/module/agent/profile"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"google.golang.org/grpc"
)

var (
	ErrConfirmedDraftPublisherUnavailable = errors.New("confirmed draft publisher is unavailable")
	ErrInvalidDraftSourceRun              = errors.New("invalid confirmed draft source run")
)

type ConfirmedDraftPublisher interface {
	PublishConfirmedDraft(ctx context.Context, userID uint64, content, idempotencyKey string) (uint64, error)
}

type ProductOutcomeRecorder interface {
	RecordProductOutcome(ctx context.Context, run agentObservability.RunRecord, signal string, positive bool) error
}

type ConfirmedDraftResult struct {
	TweetID uint64
}

type tweetServiceConfirmedDraftPublisher struct {
	client tweetv1.TweetServiceClient
}

func NewTweetServiceConfirmedDraftPublisher(client tweetv1.TweetServiceClient) ConfirmedDraftPublisher {
	return &tweetServiceConfirmedDraftPublisher{client: client}
}

func (p *tweetServiceConfirmedDraftPublisher) PublishConfirmedDraft(
	ctx context.Context,
	userID uint64,
	content, idempotencyKey string,
) (uint64, error) {
	if p == nil || p.client == nil {
		return 0, ErrConfirmedDraftPublisherUnavailable
	}
	response, err := p.client.CreateTweet(ctx, &tweetv1.CreateTweetRequest{
		UserId: userID, Content: content, IdempotencyKey: idempotencyKey,
	}, grpc.WaitForReady(false))
	if err != nil {
		return 0, fmt.Errorf("publish confirmed draft through TweetService: %w", err)
	}
	if response == nil || response.Tweet == nil || response.Tweet.Id == 0 {
		return 0, errors.New("TweetService returned an empty confirmed draft result")
	}
	return response.Tweet.Id, nil
}

func (s *AgentService) ConfirmPublishTwitter(
	ctx context.Context,
	userID uint64,
	sourceRunID, content string,
) (*ConfirmedDraftResult, error) {
	if s == nil || s.confirmedDraftPublisher == nil {
		return nil, ErrConfirmedDraftPublisherUnavailable
	}
	content = strings.TrimSpace(content)
	sourceRunID = strings.TrimSpace(sourceRunID)
	if userID == 0 || content == "" {
		return nil, errors.New("user and confirmed draft content are required")
	}

	var sourceRun *agentObservability.RunRecord
	var authoritativeRun *repository.AgentExecutionRun
	if sourceRunID != "" {
		if len(sourceRunID) > 128 || s.traceReader == nil {
			return nil, ErrInvalidDraftSourceRun
		}
		bundle, err := s.traceReader.GetTraceBundle(ctx, userID, sourceRunID)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidDraftSourceRun, err)
		}
		if bundle == nil || bundle.Run == nil || bundle.Run.UserID != userID ||
			bundle.Run.RunID != sourceRunID || bundle.Run.Mode != string(agentRuntime.ModeAssist) ||
			bundle.Run.Status != string(agentRuntime.RunStatusCompleted) {
			return nil, ErrInvalidDraftSourceRun
		}
		runCopy := *bundle.Run
		sourceRun = &runCopy
		if s.agentExecutionRunStore != nil {
			persistedRun, runErr := s.agentExecutionRunStore.GetAgentExecutionRun(ctx, sourceRunID, userID)
			switch {
			case runErr == nil:
				if persistedRun.Status != repository.AgentExecutionRunCompleted || !persistedRun.PublishableDraft {
					return nil, ErrInvalidDraftSourceRun
				}
				authoritativeRun = persistedRun
			case errors.Is(runErr, repository.ErrAgentExecutionRunNotFound):
				// Legacy Assist traces predate authoritative Run persistence and
				// retain their compatibility publish path without product metrics.
			default:
				return nil, fmt.Errorf("validate confirmed draft source run: %w", runErr)
			}
		}
	}

	idempotencyKey := confirmedDraftIdempotencyKey(sourceRunID, content)
	tweetID, err := s.confirmedDraftPublisher.PublishConfirmedDraft(ctx, userID, content, idempotencyKey)
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "confirmed Agent draft published",
		"user_id", userID,
		"source_run_id", sourceRunID,
		"tweet_id", tweetID,
	)
	if authoritativeRun != nil {
		if adoptionStore, ok := s.agentExecutionRunStore.(repository.AgentDraftAdoptionStore); ok {
			persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			updatedRun, recorded, adoptionErr := adoptionStore.MarkAgentDraftPublished(
				persistCtx, authoritativeRun.ID, userID, tweetID, time.Now(),
			)
			cancel()
			if adoptionErr != nil {
				slog.WarnContext(ctx, "mark confirmed Agent draft adoption failed", "error", adoptionErr)
			} else {
				s.recordDraftReadyProductEvent(ctx, updatedRun)
				s.recordDraftPublishedProductEvent(ctx, updatedRun, updatedRun.PublishedTweetID)
				if recorded {
					if observer, supported := s.unifiedAgentProductObserver.(UnifiedAgentDraftProductObserver); supported {
						observation := UnifiedAgentDraftPublishedObservation{
							ExecutionProfile: updatedRun.ExecutionProfile,
						}
						if updatedRun.ExecutionStrategyPlan != nil {
							observation.Strategy = updatedRun.ExecutionStrategyPlan.SelectedStrategy
						}
						observer.ObserveDraftPublished(observation)
					}
				}
			}
		}
	}
	if sourceRun != nil && s.contentAttributionStore != nil {
		window := s.contentAttributionWindow
		if window <= 0 {
			window = DefaultContentAttributionWindow
		}
		publishedAt := time.Now()
		persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		attributionErr := s.contentAttributionStore.SavePublishedContent(persistCtx, &attribution.PublishedContent{
			TweetID: tweetID, AuthorUserID: userID, SourceRunID: sourceRun.RunID,
			PublishedAt: publishedAt, ExpiresAt: publishedAt.Add(window), UpdatedAt: publishedAt,
		})
		cancel()
		if attributionErr != nil {
			slog.WarnContext(ctx, "save confirmed draft content attribution failed",
				"tweet_id", tweetID, "run_id", sourceRun.RunID, "error", attributionErr,
			)
		}
	}

	if sourceRun != nil && s.productOutcomeRecorder != nil {
		persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		outcomeErr := s.productOutcomeRecorder.RecordProductOutcome(
			persistCtx, *sourceRun, profile.ExperimentOutcomeSignalDraftPublished, true,
		)
		cancel()
		if outcomeErr != nil {
			slog.WarnContext(ctx, "record confirmed draft Profile outcome failed",
				"run_id", sourceRun.RunID,
				"error", outcomeErr,
			)
		}
	}
	return &ConfirmedDraftResult{TweetID: tweetID}, nil
}

func confirmedDraftIdempotencyKey(sourceRunID, content string) string {
	runID := strings.TrimSpace(sourceRunID)
	if runID == "" {
		runID = primitive.NewObjectID().Hex()
	}
	digest := sha256.Sum256([]byte(content))
	return "agent-confirm:" + runID + ":" + hex.EncodeToString(digest[:16])
}
