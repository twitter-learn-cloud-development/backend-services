package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"twitter-clone/internal/module/agent/eval"
)

type agentTaskExecutorPreflighter interface {
	Preflight(context.Context) error
}

type agentTaskEvaluationRunOptions struct {
	Side             string
	Live             bool
	CheckpointRoot   string
	IntegrityKey     []byte
	IntegrityKeyID   string
	PreflightTimeout time.Duration
	Progress         bool
	PreflightDone    *bool
	Stderr           io.Writer
}

func runAgentTaskEvaluation(
	ctx context.Context,
	dataset []eval.AgentTaskCase,
	executor eval.AgentTaskExecutor,
	cfg eval.AgentTaskRunnerConfig,
	options agentTaskEvaluationRunOptions,
) (eval.AgentTaskReport, error) {
	if options.Stderr == nil {
		options.Stderr = io.Discard
	}
	var checkpoint *agentTaskCheckpointStore
	if strings.TrimSpace(options.CheckpointRoot) != "" {
		datasetHash, err := eval.HashAgentTaskDataset(dataset)
		if err != nil {
			return eval.AgentTaskReport{}, fmt.Errorf("hash checkpoint dataset: %w", err)
		}
		executionConfigHash := strings.ToLower(strings.TrimSpace(cfg.ExecutionConfigHash))
		if executionConfigHash == "" {
			executionConfigHash, err = eval.HashCanonicalJSON(cfg.Execution)
			if err != nil {
				return eval.AgentTaskReport{}, fmt.Errorf("hash checkpoint execution config: %w", err)
			}
		}
		checkpoint, err = openAgentTaskCheckpointStore(options.CheckpointRoot, agentTaskCheckpointIdentity{
			Side: options.Side, DatasetVersion: cfg.DatasetVersion, DatasetSHA256: datasetHash,
			ExecutionConfigHash: executionConfigHash, Environment: cfg.Environment, Seed: cfg.Seed,
			CaseTimeoutMS: cfg.CaseTimeout.Milliseconds(), Execution: cfg.Execution, TotalCases: len(dataset),
		}, options.IntegrityKey, options.IntegrityKeyID, time.Now().UTC())
		if err != nil {
			return eval.AgentTaskReport{}, fmt.Errorf("open %s checkpoint: %w", options.Side, err)
		}
		cfg.ResumeCases = checkpoint.ResumeCases()
		generatedAt := checkpoint.GeneratedAt()
		cfg.Now = func() time.Time { return generatedAt }
	}

	progressEnabled := options.Progress || checkpoint != nil
	if progressEnabled && len(cfg.ResumeCases) > 0 {
		fmt.Fprintf(options.Stderr, "agent-task-eval: %s resumed %d/%d signed checkpoint case(s)\n", options.Side, len(cfg.ResumeCases), len(dataset))
	}
	if err := eval.ValidateAgentTaskResumeCases(dataset, cfg.ResumeCases); err != nil {
		return eval.AgentTaskReport{}, fmt.Errorf("validate %s checkpoint evidence: %w", options.Side, err)
	}
	if options.Live && len(cfg.ResumeCases) < len(dataset) {
		alreadyDone := options.PreflightDone != nil && *options.PreflightDone
		if !alreadyDone {
			preflighter, ok := executor.(agentTaskExecutorPreflighter)
			if !ok {
				return eval.AgentTaskReport{}, fmt.Errorf("%s live executor does not implement model/tool preflight", options.Side)
			}
			preflightCtx, cancel := context.WithTimeout(ctx, options.PreflightTimeout)
			err := preflighter.Preflight(preflightCtx)
			cancel()
			if err != nil {
				return eval.AgentTaskReport{}, fmt.Errorf("%s model/tool preflight failed: %w", options.Side, err)
			}
			if options.PreflightDone != nil {
				*options.PreflightDone = true
			}
			if progressEnabled {
				fmt.Fprintf(options.Stderr, "agent-task-eval: %s model/tool preflight passed\n", options.Side)
			}
		}
	}

	cfg.AbortOnExecutorError = options.Live
	if checkpoint != nil || progressEnabled {
		cfg.ProgressObserver = func(progress eval.AgentTaskProgress) error {
			if checkpoint != nil {
				if err := checkpoint.Append(progress); err != nil {
					return err
				}
			}
			if progressEnabled {
				status := "failed"
				if progress.Evidence.Result.Passed {
					status = "passed"
				}
				fmt.Fprintf(
					options.Stderr,
					"agent-task-eval: %s case %d/%d id=%s status=%s duration_ms=%d\n",
					options.Side,
					progress.Completed,
					progress.Total,
					progress.Evidence.Result.CaseID,
					status,
					progress.Evidence.Result.DurationMS,
				)
			}
			return nil
		}
	}
	return eval.RunAgentTasks(ctx, dataset, executor, cfg)
}
