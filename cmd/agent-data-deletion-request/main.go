package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	mongoInfra "twitter-clone/internal/infrastructure/mongo"
	"twitter-clone/internal/module/agent/repository"
	agentservice "twitter-clone/internal/module/agent/service"
)

const (
	actionRequest   = "request"
	actionGet       = "get"
	actionPreflight = "preflight"
	actionCancel    = "cancel"
)

type commandOptions struct {
	Action     string
	RequestID  string
	UserID     uint64
	ReasonCode string
	Actor      string
	Execute    bool
	Timeout    time.Duration
}

type commandReport struct {
	Action          string                          `json:"action"`
	MutationApplied bool                            `json:"mutation_applied"`
	RequestID       string                          `json:"request_id,omitempty"`
	UserID          uint64                          `json:"user_id,omitempty"`
	ReasonCode      string                          `json:"reason_code,omitempty"`
	ActorDigest     string                          `json:"actor_digest,omitempty"`
	Request         *repository.DataDeletionRequest `json:"request,omitempty"`
	Notice          string                          `json:"notice"`
}

type deletionRequestClient interface {
	Request(context.Context, uint64, string, string, time.Time) (*repository.DataDeletionRequest, error)
	Get(context.Context, primitive.ObjectID) (*repository.DataDeletionRequest, error)
	Preflight(context.Context, primitive.ObjectID, time.Time) (*repository.DataDeletionRequest, error)
	Cancel(context.Context, primitive.ObjectID, time.Time) (*repository.DataDeletionRequest, error)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "agent data deletion request command failed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	action := flag.String("action", actionGet, "operation: request, get, preflight, or cancel")
	requestID := flag.String("request-id", "", "deletion request ObjectID; required for get, preflight, and cancel")
	userID := flag.Uint64("user-id", 0, "target user ID; required for request")
	reasonCode := flag.String("reason-code", "", "bounded lowercase reason code; required for request")
	actor := flag.String("actor", strings.TrimSpace(os.Getenv("AGENT_DATA_DELETION_OPERATOR")), "requesting actor identity; required for request and stored only as SHA-256")
	execute := flag.Bool("execute", false, "apply a request, preflight, or cancellation mutation; omitted mutations are plan-only")
	timeout := flag.Duration("timeout", 30*time.Second, "bounded Mongo operation timeout")
	flag.Parse()

	options := commandOptions{
		Action: strings.ToLower(strings.TrimSpace(*action)), RequestID: strings.TrimSpace(*requestID),
		UserID: *userID, ReasonCode: strings.TrimSpace(*reasonCode), Actor: strings.TrimSpace(*actor),
		Execute: *execute, Timeout: *timeout,
	}
	requestObjectID, err := validateCommandOptions(options, time.Now().UTC())
	if err != nil {
		return err
	}
	if isMutation(options.Action) && !options.Execute {
		return json.NewEncoder(os.Stdout).Encode(planOnlyReport(options))
	}

	mongoInfra.InitMongoDB()
	defer mongoInfra.Close()
	repo := repository.NewMongoAgentRepository(mongoInfra.GetDB())
	client, err := agentservice.NewDataDeletionRequestService(repo, agentservice.DefaultDataDeletionPreflightLease)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), options.Timeout)
	defer cancel()
	report, err := executeCommand(ctx, client, options, requestObjectID, time.Now().UTC())
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}

func validateCommandOptions(options commandOptions, now time.Time) (primitive.ObjectID, error) {
	if options.Timeout < time.Second || options.Timeout > 5*time.Minute {
		return primitive.NilObjectID, errors.New("--timeout must be between 1s and 5m")
	}
	switch options.Action {
	case actionRequest:
		if err := repository.ValidateDataDeletionRequestInput(options.UserID, options.ReasonCode, options.Actor, now); err != nil {
			return primitive.NilObjectID, err
		}
		return primitive.NilObjectID, nil
	case actionGet, actionPreflight, actionCancel:
		requestID, err := primitive.ObjectIDFromHex(options.RequestID)
		if err != nil {
			return primitive.NilObjectID, errors.New("--request-id must be a valid Mongo ObjectID")
		}
		return requestID, nil
	default:
		return primitive.NilObjectID, errors.New("--action must be request, get, preflight, or cancel")
	}
}

func isMutation(action string) bool {
	return action == actionRequest || action == actionPreflight || action == actionCancel
}

func planOnlyReport(options commandOptions) commandReport {
	report := commandReport{
		Action: options.Action, MutationApplied: false, RequestID: options.RequestID,
		UserID: options.UserID, ReasonCode: options.ReasonCode,
		Notice: "plan only; pass --execute to mutate deletion-request control state; physical data deletion is not implemented",
	}
	if options.Actor != "" {
		report.ActorDigest = repository.DataDeletionActorDigest(options.Actor)
	}
	return report
}

func executeCommand(
	ctx context.Context,
	client deletionRequestClient,
	options commandOptions,
	requestID primitive.ObjectID,
	now time.Time,
) (commandReport, error) {
	if client == nil {
		return commandReport{}, errors.New("data deletion request client is unavailable")
	}
	var (
		request *repository.DataDeletionRequest
		err     error
	)
	switch options.Action {
	case actionRequest:
		request, err = client.Request(ctx, options.UserID, options.ReasonCode, options.Actor, now)
	case actionGet:
		request, err = client.Get(ctx, requestID)
	case actionPreflight:
		request, err = client.Preflight(ctx, requestID, now)
	case actionCancel:
		request, err = client.Cancel(ctx, requestID, now)
	default:
		err = errors.New("unsupported data deletion request action")
	}
	if err != nil {
		return commandReport{}, err
	}
	return commandReport{
		Action: options.Action, MutationApplied: isMutation(options.Action), Request: request,
		Notice: "control-plane request state only; physical data deletion is not implemented",
	}, nil
}
