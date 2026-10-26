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
	DialogueID       primitive.ObjectID
	UserID           uint64
	LeaseToken       string
	Version          int
	FromMessage      int64
	ThroughMessage   int64
	ScheduleRevision int64
}

type DialogueSummarySchedule struct {
	DialogueID primitive.ObjectID
	UserID     uint64
}

// DialogueSummaryRepository is intentionally separate from AgentRepository.
// Services can feature-detect the durable summary capability while tests and
// alternative repositories keep the existing persistence contract.
type DialogueSummaryRepository interface {
	ClaimDialogueSummary(ctx context.Context, dialogueID primitive.ObjectID, userID uint64, minPendingMessages int64, force bool, leaseDuration time.Duration) (*DialogueSummaryClaim, error)
	CompleteDialogueSummary(ctx context.Context, claim DialogueSummaryClaim) error
	ReleaseDialogueSummary(ctx context.Context, claim DialogueSummaryClaim) error
}

// DialogueSummaryScheduleRepository provides durable discovery for idle
// summary work. Claim/lease remains the single execution ownership boundary.
type DialogueSummaryScheduleRepository interface {
	ScheduleDialogueSummary(ctx context.Context, dialogueID primitive.ObjectID, userID uint64, dueAt time.Time) error
	ListDueDialogueSummaries(ctx context.Context, dueBefore time.Time, limit int64) ([]DialogueSummarySchedule, error)
}

func (r *MongoAgentRepository) ScheduleDialogueSummary(
	ctx context.Context,
	dialogueID primitive.ObjectID,
	userID uint64,
	dueAt time.Time,
) error {
	result, err := r.dialogueColl.UpdateOne(ctx, bson.M{"_id": dialogueID, "user_id": userID}, bson.M{
		"$set": bson.M{"summary_due_at": dueAt},
		"$inc": bson.M{"summary_schedule_revision": int64(1)},
	})
	if err != nil {
		return fmt.Errorf("schedule dialogue summary failed: %w", err)
	}
	if result.MatchedCount == 0 {
		return fmt.Errorf("dialogue does not belong to user %d", userID)
	}
	return nil
}

func (r *MongoAgentRepository) ListDueDialogueSummaries(
	ctx context.Context,
	dueBefore time.Time,
	limit int64,
) ([]DialogueSummarySchedule, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	filter := bson.M{
		"summary_due_at": bson.M{"$lte": dueBefore},
		"$or": []bson.M{
			{"summary_status": bson.M{"$ne": summaryStatusRunning}},
			{"summary_lease_until": bson.M{"$lte": dueBefore}},
			{"summary_lease_until": bson.M{"$exists": false}},
		},
	}
	cursor, err := r.dialogueColl.Find(ctx, filter, options.Find().
		SetProjection(bson.M{"_id": 1, "user_id": 1}).
		SetSort(bson.D{{Key: "summary_due_at", Value: 1}, {Key: "_id", Value: 1}}).
		SetLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list due dialogue summaries failed: %w", err)
	}
	defer cursor.Close(ctx)
	var rows []struct {
		ID     primitive.ObjectID `bson:"_id"`
		UserID uint64             `bson:"user_id"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("decode due dialogue summaries failed: %w", err)
	}
	result := make([]DialogueSummarySchedule, 0, len(rows))
	for _, row := range rows {
		result = append(result, DialogueSummarySchedule{DialogueID: row.ID, UserID: row.UserID})
	}
	return result, nil
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
		if force {
			if _, clearErr := r.dialogueColl.UpdateOne(ctx, bson.M{
				"_id": dialogueID, "user_id": userID,
				"summary_schedule_revision": dialogue.SummaryScheduleRevision,
			}, bson.M{"$unset": bson.M{"summary_due_at": ""}}); clearErr != nil {
				return nil, fmt.Errorf("clear empty dialogue summary schedule failed: %w", clearErr)
			}
		}
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
		DialogueID:       dialogueID,
		UserID:           userID,
		LeaseToken:       leaseToken,
		Version:          dialogue.SummaryVersion + 1,
		FromMessage:      dialogue.SummarizedMessageCount,
		ThroughMessage:   messageCount,
		ScheduleRevision: dialogue.SummaryScheduleRevision,
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
	_, err = r.dialogueColl.UpdateOne(ctx, bson.M{
		"_id":                       claim.DialogueID,
		"user_id":                   claim.UserID,
		"summary_schedule_revision": claim.ScheduleRevision,
	}, bson.M{"$unset": bson.M{"summary_due_at": ""}})
	if err != nil {
		return fmt.Errorf("clear completed dialogue summary schedule failed: %w", err)
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
