package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	agentdeletion "twitter-clone/internal/module/agent/deletion"
)

const (
	compositeSubjectWriteFenceAdapterName    = "service.agent-tweet-subject-write-fence"
	compositeSubjectWriteFenceAdapterVersion = "v1"
)

// CompositeDataDeletionSubjectWriteFence turns the Agent Mongo and Tweet
// MySQL authorities into one worker capability. A partial failure is
// intentionally fail-safe: an already installed authority remains fenced and
// the same request can retry until both receipts are present.
type CompositeDataDeletionSubjectWriteFence struct {
	agent DataDeletionSubjectWriteFence
	tweet agentdeletion.TweetSubjectWriteFenceClient
	now   func() time.Time
}

func NewCompositeDataDeletionSubjectWriteFence(
	agent DataDeletionSubjectWriteFence,
	tweet agentdeletion.TweetSubjectWriteFenceClient,
) (*CompositeDataDeletionSubjectWriteFence, error) {
	if agent == nil || tweet == nil {
		return nil, errors.New("composite data deletion write fence dependencies are incomplete")
	}
	descriptor := agent.DeletionCapability()
	if descriptor.ID != agentdeletion.CapabilitySubjectWriteFence ||
		descriptor.Adapter == "" || descriptor.Version == "" {
		return nil, errors.New("agent data deletion write fence capability is invalid")
	}
	return &CompositeDataDeletionSubjectWriteFence{agent: agent, tweet: tweet, now: time.Now}, nil
}

func (f *CompositeDataDeletionSubjectWriteFence) DeletionCapability() agentdeletion.CapabilityDescriptor {
	return agentdeletion.CapabilityDescriptor{
		ID:      agentdeletion.CapabilitySubjectWriteFence,
		Adapter: compositeSubjectWriteFenceAdapterName,
		Version: compositeSubjectWriteFenceAdapterVersion,
	}
}

func (f *CompositeDataDeletionSubjectWriteFence) EnforceDataDeletionFence(
	ctx context.Context,
	request agentdeletion.WriteFenceRequest,
) (agentdeletion.WriteFenceReceipt, error) {
	if f == nil || f.agent == nil || f.tweet == nil || f.now == nil {
		return agentdeletion.WriteFenceReceipt{}, errors.New("composite data deletion write fence is unavailable")
	}
	startedAt := f.now().UTC()
	if err := agentdeletion.ValidateWriteFenceRequest(request, startedAt); err != nil {
		return agentdeletion.WriteFenceReceipt{}, err
	}
	ctx, cancel := context.WithDeadline(ctx, request.Deadline)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return agentdeletion.WriteFenceReceipt{}, err
	}

	agentReceipt, err := f.agent.EnforceDataDeletionFence(ctx, request)
	if err != nil {
		return agentdeletion.WriteFenceReceipt{}, fmt.Errorf("enforce agent subject write fence: %w", err)
	}
	if err := agentdeletion.VerifyWriteFenceReceipt(request, agentReceipt, f.now().UTC()); err != nil {
		return agentdeletion.WriteFenceReceipt{}, fmt.Errorf("verify agent subject write fence receipt: %w", err)
	}

	tweetEvidence, err := f.tweet.EnforceSubjectWriteFence(ctx, request)
	if err != nil {
		return agentdeletion.WriteFenceReceipt{}, fmt.Errorf("enforce tweet subject write fence: %w", err)
	}
	completedAt := f.now().UTC()
	if tweetEvidence.Authority != "tweet-service.mysql" {
		return agentdeletion.WriteFenceReceipt{}, errors.New("unexpected tweet write fence authority")
	}
	if err := ctx.Err(); err != nil {
		return agentdeletion.WriteFenceReceipt{}, err
	}
	if tweetEvidence.CompletedAt.After(completedAt) || agentReceipt.CompletedAt.After(completedAt) {
		return agentdeletion.WriteFenceReceipt{}, errors.New("subject write fence authority completed in the future")
	}
	agentAuthority := f.agent.DeletionCapability().Adapter
	return agentdeletion.NewCompositeWriteFenceReceipt(
		request,
		[]agentdeletion.WriteFenceAuthorityEvidence{
			{
				Authority: agentAuthority, ReceiptDigest: agentReceipt.ReceiptDigest,
				FenceRevision: agentReceipt.FenceRevision, CompletedAt: agentReceipt.CompletedAt,
			},
			tweetEvidence,
		},
		completedAt,
	)
}
