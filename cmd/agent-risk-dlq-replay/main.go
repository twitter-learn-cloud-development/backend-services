package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	infrastructureMQ "twitter-clone/internal/infrastructure/mq"
	agentService "twitter-clone/internal/module/agent/service"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "agent risk dlq replay failed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	limit := flag.Int("limit", 20, "maximum messages to inspect or replay (1-100)")
	execute := flag.Bool("execute", false, "confirm-publish eligible messages to the Agent risk ingress and acknowledge them from the DLQ")
	maxReplays := flag.Int("max-replays", 1, "maximum replay attempts allowed per message (1-10)")
	operator := flag.String("operator", strings.TrimSpace(os.Getenv("DLQ_REPLAY_OPERATOR")), "operator identity; required with --execute and emitted only as SHA-256")
	reason := flag.String("reason", "", "bounded change reason; required with --execute and emitted only as SHA-256")
	timeout := flag.Duration("timeout", 30*time.Second, "bounded DLQ inspection and replay processing timeout")
	flag.Parse()

	if *timeout < time.Second || *timeout > 5*time.Minute {
		return fmt.Errorf("--timeout must be between 1s and 5m")
	}
	options := agentService.RiskControlReplayOptions{
		Limit:          *limit,
		Execute:        *execute,
		MaxReplayCount: *maxReplays,
		Operator:       *operator,
		Reason:         *reason,
	}
	if err := options.Validate(); err != nil {
		return fmt.Errorf("invalid options: %w", err)
	}

	broker, err := infrastructureMQ.NewRabbitMQ(infrastructureMQ.DefaultRabbitMQConfig())
	if err != nil {
		return fmt.Errorf("connect rabbitmq: %w", err)
	}
	defer broker.Close()

	replayer, err := agentService.NewRiskControlDLQReplayer(broker)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report, runErr := replayer.Run(ctx, options)
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		return fmt.Errorf("encode replay report: %w", err)
	}
	return runErr
}
