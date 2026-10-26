package main

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"twitter-clone/internal/module/agent/repository"
)

type deletionRequestClientFake struct {
	called string
}

func (f *deletionRequestClientFake) Request(_ context.Context, userID uint64, reason, actor string, now time.Time) (*repository.DataDeletionRequest, error) {
	f.called = actionRequest
	return &repository.DataDeletionRequest{ID: primitive.NewObjectID(), UserID: userID, ReasonCode: reason, ActorDigest: repository.DataDeletionActorDigest(actor), RequestedAt: now}, nil
}

func (f *deletionRequestClientFake) Get(_ context.Context, id primitive.ObjectID) (*repository.DataDeletionRequest, error) {
	f.called = actionGet
	return &repository.DataDeletionRequest{ID: id}, nil
}

func (f *deletionRequestClientFake) Preflight(_ context.Context, id primitive.ObjectID, _ time.Time) (*repository.DataDeletionRequest, error) {
	f.called = actionPreflight
	return &repository.DataDeletionRequest{ID: id, Status: repository.DataDeletionPreviewReady}, nil
}

func (f *deletionRequestClientFake) Cancel(_ context.Context, id primitive.ObjectID, _ time.Time) (*repository.DataDeletionRequest, error) {
	f.called = actionCancel
	return &repository.DataDeletionRequest{ID: id, Status: repository.DataDeletionCancelled}, nil
}

func TestValidateCommandOptionsKeepsMutationsExplicit(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	options := commandOptions{
		Action: actionRequest, UserID: 42, ReasonCode: "user_request", Actor: "privacy@example.com",
		Timeout: 30 * time.Second,
	}
	if _, err := validateCommandOptions(options, now); err != nil {
		t.Fatal(err)
	}
	report := planOnlyReport(options)
	if report.MutationApplied || report.ActorDigest == "" || report.Notice == "" {
		t.Fatalf("unexpected plan-only report: %+v", report)
	}
}

func TestValidateCommandOptionsRequiresValidRequestID(t *testing.T) {
	_, err := validateCommandOptions(commandOptions{Action: actionPreflight, RequestID: "bad", Timeout: 30 * time.Second}, time.Now())
	if err == nil {
		t.Fatal("expected invalid request id error")
	}
}

func TestExecuteCommandDispatchesPreflightWithoutPhysicalDeletion(t *testing.T) {
	client := &deletionRequestClientFake{}
	id := primitive.NewObjectID()
	report, err := executeCommand(context.Background(), client, commandOptions{Action: actionPreflight, Execute: true}, id, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if client.called != actionPreflight || !report.MutationApplied || report.Request.Status != repository.DataDeletionPreviewReady {
		t.Fatalf("unexpected preflight dispatch: client=%+v report=%+v", client, report)
	}
	if report.Request.ExecutionSupported {
		t.Fatal("preflight must not enable physical deletion")
	}
}
