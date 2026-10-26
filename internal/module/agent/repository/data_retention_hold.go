package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	ErrDataRetentionSubjectBusy = errors.New("data retention subject is busy")
	ErrDataRetentionHoldMissing = errors.New("data retention hold does not exist")
)

type DataRetentionHold struct {
	UserID         uint64    `bson:"user_id" json:"user_id"`
	ReasonCode     string    `bson:"reason_code" json:"reason_code"`
	OperatorDigest string    `bson:"operator_digest" json:"operator_digest"`
	HeldAt         time.Time `bson:"held_at" json:"held_at"`
	HoldUntil      time.Time `bson:"hold_until,omitempty" json:"hold_until,omitempty"`
	Revision       int64     `bson:"revision" json:"revision"`
}

type DataRetentionHoldRepository interface {
	ApplyDataRetentionHold(context.Context, uint64, string, string, time.Time, time.Time) (*DataRetentionHold, error)
	ReleaseDataRetentionHold(context.Context, uint64, string, time.Time) error
	ListActiveDataRetentionHolds(context.Context, time.Time, int) ([]DataRetentionHold, error)
}

type dataRetentionSubjectDocument struct {
	ID             string                                 `bson:"_id"`
	UserID         uint64                                 `bson:"user_id"`
	Held           bool                                   `bson:"held"`
	ReasonCode     string                                 `bson:"reason_code"`
	OperatorDigest string                                 `bson:"operator_digest"`
	HeldAt         time.Time                              `bson:"held_at"`
	HoldUntil      time.Time                              `bson:"hold_until"`
	Revision       int64                                  `bson:"revision"`
	LeaseToken     string                                 `bson:"lease_token"`
	LeaseUntil     time.Time                              `bson:"lease_until"`
	WriteFence     *dataDeletionSubjectWriteFenceDocument `bson:"write_fence,omitempty"`
	ActiveWrites   int64                                  `bson:"active_writes,omitempty"`
}

func (r *MongoAgentRepository) AcquireDataRetentionSubjectLease(
	ctx context.Context,
	userID uint64,
	token string,
	now time.Time,
	leaseUntil time.Time,
) (DataRetentionSubjectLeaseState, error) {
	if r == nil || r.retentionSubjectColl == nil {
		return "", errors.New("data retention subject repository is unavailable")
	}
	if userID == 0 || token == "" || now.IsZero() || !leaseUntil.After(now) {
		return "", errors.New("data retention subject lease is invalid")
	}
	filter := bson.M{
		"_id": dataRetentionSubjectID(userID),
		"$and": bson.A{
			bson.M{"$or": bson.A{
				bson.M{"held": bson.M{"$ne": true}},
				bson.M{"hold_until": bson.M{"$lte": now}},
			}},
			bson.M{"$or": bson.A{
				bson.M{"lease_token": token},
				bson.M{"lease_until": bson.M{"$lte": now}},
				bson.M{"lease_until": bson.M{"$exists": false}},
			}},
		},
	}
	update := bson.M{
		"$set": bson.M{
			"user_id": userID, "held": false, "lease_token": token,
			"lease_until": leaseUntil, "updated_at": now,
		},
		"$setOnInsert": bson.M{"created_at": now, "revision": int64(0)},
	}
	result := r.retentionSubjectColl.FindOneAndUpdate(
		ctx, filter, update,
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	)
	if err := result.Err(); err == nil {
		return DataRetentionSubjectLeaseAcquired, nil
	} else if !errors.Is(err, mongo.ErrNoDocuments) && !mongo.IsDuplicateKeyError(err) {
		return "", fmt.Errorf("acquire data retention subject lease: %w", err)
	}

	var subject dataRetentionSubjectDocument
	if err := r.retentionSubjectColl.FindOne(ctx, bson.M{"_id": dataRetentionSubjectID(userID)}).Decode(&subject); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return DataRetentionSubjectBusy, nil
		}
		return "", fmt.Errorf("inspect data retention subject lease: %w", err)
	}
	if subject.holdActive(now) {
		return DataRetentionSubjectHeld, nil
	}
	return DataRetentionSubjectBusy, nil
}

func (r *MongoAgentRepository) ReleaseDataRetentionSubjectLease(ctx context.Context, userID uint64, token string) error {
	if r == nil || r.retentionSubjectColl == nil {
		return errors.New("data retention subject repository is unavailable")
	}
	if userID == 0 || token == "" {
		return errors.New("data retention subject lease identity is invalid")
	}
	_, err := r.retentionSubjectColl.UpdateOne(ctx, bson.M{
		"_id": dataRetentionSubjectID(userID), "lease_token": token,
	}, bson.M{"$unset": bson.M{"lease_token": "", "lease_until": ""}})
	if err != nil {
		return fmt.Errorf("release data retention subject lease: %w", err)
	}
	return nil
}

func (r *MongoAgentRepository) ApplyDataRetentionHold(
	ctx context.Context,
	userID uint64,
	reasonCode string,
	operator string,
	now time.Time,
	holdUntil time.Time,
) (*DataRetentionHold, error) {
	reasonCode = strings.TrimSpace(reasonCode)
	operator = strings.TrimSpace(operator)
	if err := ValidateDataRetentionHoldInput(userID, reasonCode, operator, now, holdUntil); err != nil {
		return nil, err
	}
	operatorDigest := dataRetentionOperatorDigest(operator)
	filter := bson.M{
		"_id": dataRetentionSubjectID(userID),
		"$or": bson.A{
			bson.M{"lease_until": bson.M{"$lte": now}},
			bson.M{"lease_until": bson.M{"$exists": false}},
		},
	}
	set := bson.M{
		"user_id": userID, "held": true, "reason_code": reasonCode,
		"operator_digest": operatorDigest, "held_at": now, "updated_at": now,
	}
	update := bson.M{
		"$set":         set,
		"$setOnInsert": bson.M{"created_at": now},
		"$inc":         bson.M{"revision": 1},
		"$unset":       bson.M{"lease_token": "", "lease_until": ""},
	}
	if holdUntil.IsZero() {
		update["$unset"].(bson.M)["hold_until"] = ""
	} else {
		set["hold_until"] = holdUntil
	}
	var subject dataRetentionSubjectDocument
	err := r.retentionSubjectColl.FindOneAndUpdate(
		ctx, filter, update,
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&subject)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) || mongo.IsDuplicateKeyError(err) {
			return nil, ErrDataRetentionSubjectBusy
		}
		return nil, fmt.Errorf("apply data retention hold: %w", err)
	}
	return subject.hold(), nil
}

func (r *MongoAgentRepository) ReleaseDataRetentionHold(
	ctx context.Context,
	userID uint64,
	operator string,
	now time.Time,
) error {
	operator = strings.TrimSpace(operator)
	if userID == 0 || operator == "" || len(operator) > 128 || now.IsZero() {
		return errors.New("data retention hold release is invalid")
	}
	result, err := r.retentionSubjectColl.UpdateOne(ctx, bson.M{
		"_id": dataRetentionSubjectID(userID), "held": true,
	}, bson.M{
		"$set": bson.M{
			"held": false, "released_at": now, "updated_at": now,
			"released_operator_digest": dataRetentionOperatorDigest(operator),
		},
		"$unset": bson.M{"hold_until": ""},
		"$inc":   bson.M{"revision": 1},
	})
	if err != nil {
		return fmt.Errorf("release data retention hold: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrDataRetentionHoldMissing
	}
	return nil
}

func (r *MongoAgentRepository) ListActiveDataRetentionHolds(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]DataRetentionHold, error) {
	if now.IsZero() || limit <= 0 || limit > 1000 {
		return nil, errors.New("data retention hold list parameters are invalid")
	}
	cursor, err := r.retentionSubjectColl.Find(ctx, bson.M{
		"held": true,
		"$or": bson.A{
			bson.M{"hold_until": bson.M{"$exists": false}},
			bson.M{"hold_until": bson.M{"$gt": now}},
		},
	}, options.Find().SetSort(bson.D{{Key: "held_at", Value: 1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("list active data retention holds: %w", err)
	}
	defer cursor.Close(ctx)
	holds := make([]DataRetentionHold, 0)
	for cursor.Next(ctx) {
		var subject dataRetentionSubjectDocument
		if err := cursor.Decode(&subject); err != nil {
			return nil, fmt.Errorf("decode active data retention hold: %w", err)
		}
		holds = append(holds, *subject.hold())
	}
	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("list active data retention holds: %w", err)
	}
	return holds, nil
}

func ValidateDataRetentionHoldInput(userID uint64, reasonCode, operator string, now, holdUntil time.Time) error {
	if userID == 0 || now.IsZero() {
		return errors.New("data retention hold subject or time is invalid")
	}
	if reasonCode == "" || len(reasonCode) > 64 {
		return errors.New("data retention hold reason code must contain 1 to 64 characters")
	}
	for _, value := range reasonCode {
		if !unicode.IsLower(value) && !unicode.IsDigit(value) && value != '_' && value != '-' && value != '.' {
			return errors.New("data retention hold reason code contains unsupported characters")
		}
	}
	if operator == "" || len(operator) > 128 {
		return errors.New("data retention hold operator must contain 1 to 128 characters")
	}
	if !holdUntil.IsZero() && !holdUntil.After(now) {
		return errors.New("data retention hold expiry must be in the future")
	}
	return nil
}

func dataRetentionSubjectID(userID uint64) string {
	return fmt.Sprintf("user:%d", userID)
}

func dataRetentionOperatorDigest(operator string) string {
	digest := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(operator))))
	return hex.EncodeToString(digest[:])
}

func (d dataRetentionSubjectDocument) holdActive(now time.Time) bool {
	return d.Held && (d.HoldUntil.IsZero() || d.HoldUntil.After(now))
}

func (d dataRetentionSubjectDocument) hold() *DataRetentionHold {
	return &DataRetentionHold{
		UserID: d.UserID, ReasonCode: d.ReasonCode, OperatorDigest: d.OperatorDigest,
		HeldAt: d.HeldAt, HoldUntil: d.HoldUntil, Revision: d.Revision,
	}
}
