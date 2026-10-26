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

	mongoInfra "twitter-clone/internal/infrastructure/mongo"
	"twitter-clone/internal/module/agent/repository"
)

type holdCommandReport struct {
	Action     string                         `json:"action"`
	Executed   bool                           `json:"executed"`
	UserID     uint64                         `json:"user_id,omitempty"`
	ReasonCode string                         `json:"reason_code,omitempty"`
	HoldUntil  *time.Time                     `json:"hold_until,omitempty"`
	Hold       *repository.DataRetentionHold  `json:"hold,omitempty"`
	Holds      []repository.DataRetentionHold `json:"holds,omitempty"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "agent retention hold failed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	action := flag.String("action", "list", "operation: apply, release, or list")
	userID := flag.Uint64("user-id", 0, "target user ID; required for apply and release")
	reasonCode := flag.String("reason-code", "", "bounded lowercase reason code; required for apply")
	operator := flag.String("operator", strings.TrimSpace(os.Getenv("RETENTION_HOLD_OPERATOR")), "operator identity; required for apply and release and stored only as SHA-256")
	expiresIn := flag.Duration("expires-in", 0, "optional hold duration; zero creates an indefinite hold")
	limit := flag.Int("limit", 100, "maximum active holds to list (1-1000)")
	execute := flag.Bool("execute", false, "apply the requested mutation; omitted mutations are preview-only")
	timeout := flag.Duration("timeout", 30*time.Second, "bounded Mongo operation timeout")
	flag.Parse()

	*action = strings.ToLower(strings.TrimSpace(*action))
	if *timeout < time.Second || *timeout > 5*time.Minute {
		return errors.New("--timeout must be between 1s and 5m")
	}
	if *action != "apply" && *action != "release" && *action != "list" {
		return errors.New("--action must be apply, release, or list")
	}
	if *action == "list" && (*limit <= 0 || *limit > 1000) {
		return errors.New("--limit must be between 1 and 1000")
	}
	if *action != "list" {
		if *userID == 0 || strings.TrimSpace(*operator) == "" {
			return errors.New("--user-id and --operator are required for hold mutations")
		}
		if *action == "apply" && strings.TrimSpace(*reasonCode) == "" {
			return errors.New("--reason-code is required for apply")
		}
		if *expiresIn < 0 {
			return errors.New("--expires-in must not be negative")
		}
	}

	now := time.Now().UTC()
	var holdUntil time.Time
	if *action == "apply" && *expiresIn > 0 {
		holdUntil = now.Add(*expiresIn)
	}
	if *action == "apply" {
		if err := repository.ValidateDataRetentionHoldInput(*userID, strings.TrimSpace(*reasonCode), strings.TrimSpace(*operator), now, holdUntil); err != nil {
			return err
		}
	}
	report := holdCommandReport{
		Action: *action, Executed: *execute, UserID: *userID,
		ReasonCode: strings.TrimSpace(*reasonCode),
	}
	if !holdUntil.IsZero() {
		report.HoldUntil = &holdUntil
	}
	if *action != "list" && !*execute {
		return json.NewEncoder(os.Stdout).Encode(report)
	}

	mongoInfra.InitMongoDB()
	defer mongoInfra.Close()
	repo := repository.NewMongoAgentRepository(mongoInfra.GetDB())
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	switch *action {
	case "apply":
		hold, err := repo.ApplyDataRetentionHold(ctx, *userID, *reasonCode, *operator, now, holdUntil)
		if err != nil {
			return err
		}
		report.Hold = hold
	case "release":
		if err := repo.ReleaseDataRetentionHold(ctx, *userID, *operator, now); err != nil {
			return err
		}
	case "list":
		holds, err := repo.ListActiveDataRetentionHolds(ctx, now, *limit)
		if err != nil {
			return err
		}
		report.Executed = true
		report.Holds = holds
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}
