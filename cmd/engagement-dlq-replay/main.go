package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
	"twitter-clone/internal/infrastructure/mq"
	"twitter-clone/internal/mq/consumer"
)

type replayRunner interface {
	Run(context.Context, consumer.EngagementReplayOptions) (consumer.TimelineEventReplayReport, error)
}
type replayFactory func(context.Context) (replayRunner, func(), error)

func main() {
	if err := run(os.Args[1:], os.Stdout, connect); err != nil {
		fmt.Fprintln(os.Stderr, "engagement replay failed:", err)
		os.Exit(1)
	}
}
func run(args []string, out io.Writer, factory replayFactory) error {
	flags := flag.NewFlagSet("engagement-dlq-replay", flag.ContinueOnError)
	flags.SetOutput(out)
	event := flags.String("event", "liked", "liked or comment")
	limit := flags.Int("limit", 20, "1-100 messages")
	max := flags.Int("max-replays", 1, "1-10 cumulative attempts")
	live := flags.Bool("allow-live", false, "connect to RabbitMQ for bounded inspection")
	execute := flags.Bool("execute", false, "confirm-publish to the dedicated ingress and acknowledge DLQ")
	operator := flags.String("operator", "", "required for execution; digest only in report")
	reason := flags.String("reason", "", "required for execution; digest only in report")
	timeout := flags.Duration("timeout", 30*time.Second, "1-60 seconds including setup")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if *timeout < time.Second || *timeout > time.Minute {
		return errors.New("timeout must be 1-60 seconds")
	}
	options := consumer.EngagementReplayOptions{Event: *event, Limit: *limit, MaxReplayCount: *max, Execute: *execute, Operator: *operator, Reason: *reason}
	if err := options.Validate(); err != nil {
		return err
	}
	if *execute && !*live {
		return errors.New("execute requires explicit allow-live")
	}
	if !*live {
		return json.NewEncoder(out).Encode(struct {
			Mode  string `json:"mode"`
			Event string `json:"event"`
			Limit int    `json:"limit"`
		}{"offline", options.Event, options.Limit})
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	runner, close, err := factory(ctx)
	if err != nil {
		return errors.New("replay broker initialization unavailable")
	}
	defer close()
	report, err := runner.Run(ctx, options)
	if encodeErr := json.NewEncoder(out).Encode(report); encodeErr != nil {
		return errors.New("replay report unavailable")
	}
	if err != nil {
		return errors.New("replay incomplete; inspect fixed report outcomes")
	}
	return nil
}
func connect(ctx context.Context) (replayRunner, func(), error) {
	broker, err := mq.NewRabbitMQWithContext(ctx, mq.DefaultRabbitMQConfig())
	if err != nil {
		return nil, nil, err
	}
	stop, err := broker.CloseOnCancellation(ctx)
	if err != nil {
		_ = broker.Close()
		return nil, nil, err
	}
	runner, err := consumer.NewEngagementDLQReplayer(broker)
	if err != nil {
		stop()
		_ = broker.Close()
		return nil, nil, err
	}
	return runner, func() { _ = broker.Close(); stop() }, nil
}
