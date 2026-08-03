package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const summaryStatusRunning = "running"

var ErrSummaryLeaseLost = errors.New("dialogue summary lease lost")

// DialogueSummaryClaim is an immutable snapshot of the message range owned by
// one crystallization attempt. The lease token prevents stale workers from
// advancing the durable cursor after a retry has taken over.
type DialogueSummaryClaim struct {
	DialogueID     primitive.ObjectID
	UserID         uint64
	LeaseToken     string
	Version        int
	FromMessage    int64
	ThroughMessage int64
}

// DialogueSummaryRepository is intentionally separate from AgentRepository.
// Services can feature-detect the durable summary capability while tests and
// alternative repositories keep the existing persistence contract.
type DialogueSummaryRepository interface {
	ClaimDialogueSummary(ctx context.Context, dialogueID primitive.ObjectID, userID uint64, minPendingMessages int64, force bool, leaseDuration time.Duration) (*DialogueSummaryClaim, error)
	CompleteDialogueSummary(ctx context.Context, claim DialogueSummaryClaim) error
	ReleaseDialogueSummary(ctx context.Context, claim DialogueSummaryClaim) error
}

func (r *MongoAgentRepository) ClaimDialogueSummary(
	ctx context.Context,
	dialogueID primitive.ObjectID,
	userID uint64,
	minPendingMessages int64,
	force bool,
	leaseDuration time.Duration,
) (*DialogueSummaryClaim, error) {
	dialogue, err := r.GetDialogue(ctx, dialogueID)
	if err != nil {
		return nil, err
	}
	if dialogue.UserID != userID {
		return nil, fmt.Errorf("dialogue does not belong to user %d", userID)
	}

	messageCount, err := r.messageColl.CountDocuments(ctx, bson.M{"dialogue_id": dialogueID})
	if err != nil {
		return nil, fmt.Errorf("count dialogue messages for summary failed: %w", err)
	}
	pending := messageCount - dialogue.SummarizedMessageCount
	if pending <= 0 || (force && pending < 2) {
		return nil, nil
	}
	if minPendingMessages <= 0 {
		minPendingMessages = 2
	}
	if !force && pending < minPendingMessages {
		return nil, nil
	}
	if leaseDuration <= 0 {
		leaseDuration = 45 * time.Second
	}

	now := time.Now()
	leaseToken := primitive.NewObjectID().Hex()
	cursorFilter := bson.M{"summarized_message_count": dialogue.SummarizedMessageCount}
	if dialogue.SummarizedMessageCount == 0 {
		cursorFilter = bson.M{"$or": []bson.M{
			{"summarized_message_count": int64(0)},
			{"summarized_message_count": bson.M{"$exists": false}},
		}}
	}
	leaseFilter := bson.M{"$or": []bson.M{
		{"summary_status": bson.M{"$ne": summaryStatusRunning}},
		{"summary_lease_until": bson.M{"$lte": now}},
		{"summary_lease_until": bson.M{"$exists": false}},
	}}
	filter := bson.M{"$and": []bson.M{
		{"_id": dialogueID, "user_id": userID},
		cursorFilter,
		leaseFilter,
	}}
	update := bson.M{"$set": bson.M{
		"summary_status":      summaryStatusRunning,
		"summary_lease_token": leaseToken,
		"summary_lease_until": now.Add(leaseDuration),
	}}

	var claimed Dialogue
	err = r.dialogueColl.FindOneAndUpdate(
		ctx,
		filter,
		update,
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&claimed)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim dialogue summary failed: %w", err)
	}

	return &DialogueSummaryClaim{
		DialogueID:     dialogueID,
		UserID:         userID,
		LeaseToken:     leaseToken,
		Version:        dialogue.SummaryVersion + 1,
		FromMessage:    dialogue.SummarizedMessageCount,
		ThroughMessage: messageCount,
	}, nil
}

func (r *MongoAgentRepository) CompleteDialogueSummary(ctx context.Context, claim DialogueSummaryClaim) error {
	result, err := r.dialogueColl.UpdateOne(ctx, bson.M{
		"_id":                 claim.DialogueID,
		"user_id":             claim.UserID,
		"summary_lease_token": claim.LeaseToken,
	}, bson.M{
		"$set": bson.M{
			"summary_version":          claim.Version,
			"summarized_message_count": claim.ThroughMessage,
			"summary_status":           "idle",
			"summary_updated_at":       time.Now(),
		},
		"$unset": bson.M{
			"summary_lease_token": "",
			"summary_lease_until": "",
		},
	})
	if err != nil {
		return fmt.Errorf("complete dialogue summary failed: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrSummaryLeaseLost
	}
	return nil
}

func (r *MongoAgentRepository) ReleaseDialogueSummary(ctx context.Context, claim DialogueSummaryClaim) error {
	_, err := r.dialogueColl.UpdateOne(ctx, bson.M{
		"_id":                 claim.DialogueID,
		"user_id":             claim.UserID,
		"summary_lease_token": claim.LeaseToken,
	}, bson.M{
		"$set": bson.M{"summary_status": "idle"},
		"$unset": bson.M{
			"summary_lease_token": "",
			"summary_lease_until": "",
		},
	})
	if err != nil {
		return fmt.Errorf("release dialogue summary failed: %w", err)
	}
	return nil
}
