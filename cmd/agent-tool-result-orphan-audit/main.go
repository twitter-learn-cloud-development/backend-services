package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	mongoInfra "twitter-clone/internal/infrastructure/mongo"
	agentObjectStore "twitter-clone/internal/module/agent/objectstore"
	"twitter-clone/internal/module/agent/repository"
	agentService "twitter-clone/internal/module/agent/service"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "agent tool result orphan audit failed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cursor := flag.String("cursor", "", "opaque next_cursor from the previous report")
	limit := flag.Int("limit", agentService.DefaultToolResultOrphanAuditLimit, "maximum objects to inspect (1-1000)")
	grace := flag.Duration("grace-period", agentService.DefaultToolResultOrphanGracePeriod, "minimum object age before orphan classification")
	timeout := flag.Duration("timeout", 30*time.Second, "bounded Mongo and MinIO audit timeout")
	flag.Parse()

	if *timeout < time.Second || *timeout > 5*time.Minute {
		return errors.New("--timeout must be between 1s and 5m")
	}
	config := agentService.ToolResultOrphanAuditConfig{GracePeriod: *grace, Limit: *limit}
	if err := config.Validate(); err != nil {
		return err
	}
	secure, err := envBool("AGENT_TOOL_RESULT_MINIO_SECURE", false)
	if err != nil {
		return err
	}
	store, err := agentObjectStore.NewMinIOToolResultStore(agentObjectStore.MinIOToolResultConfig{
		Endpoint:  env("AGENT_TOOL_RESULT_MINIO_ENDPOINT", env("MINIO_ENDPOINT", "localhost:9000")),
		AccessKey: env("AGENT_TOOL_RESULT_MINIO_ACCESS_KEY", env("MINIO_ACCESS_KEY", "")),
		SecretKey: env("AGENT_TOOL_RESULT_MINIO_SECRET_KEY", env("MINIO_SECRET_KEY", "")),
		Bucket:    env("AGENT_TOOL_RESULT_MINIO_BUCKET", "agent-tool-results"),
		Secure:    secure,
	})
	if err != nil {
		return err
	}

	mongoInfra.InitMongoDB()
	defer mongoInfra.Close()
	repo := repository.NewMongoAgentRepository(mongoInfra.GetDB())
	auditor, err := agentService.NewToolResultOrphanAuditor(store, repo, config)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report, auditErr := auditor.Audit(ctx, time.Now().UTC(), strings.TrimSpace(*cursor))
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		return fmt.Errorf("encode orphan audit report: %w", err)
	}
	return auditErr
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envBool(name string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return parsed, nil
}
