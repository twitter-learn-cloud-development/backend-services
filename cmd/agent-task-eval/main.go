package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"twitter-clone/internal/module/agent/eval"
)

type agentTaskEvaluationOutput = eval.AgentTaskEvaluationOutput

const agentTaskEvaluationSchemaVersion = eval.AgentTaskEvaluationSchemaVersion

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("agent-task-eval", flag.ContinueOnError)
	flags.SetOutput(stderr)
	datasetPath := flags.String("dataset", "internal/module/agent/eval/testdata/agent_task_cases.json", "agent task dataset path")
	resultsPath := flags.String("results", "internal/module/agent/eval/testdata/agent_task_recorded_results.json", "candidate recorded execution results path")
	stableResultsPath := flags.String("stable-results", "", "optional stable recorded execution results path")
	stableReportPath := flags.String("stable-report", "", "optional previously signed evaluation report used as the stable baseline")
	runtimeConfigPath := flags.String("runtime-config", "", "fixed Provider/Model/Profile runtime evaluation config path")
	strategyRuntimeConfigPath := flags.String("strategy-runtime-config", "", "fixed Provider/Model/Profile-set config for a live single/multi strategy comparison")
	allowLive := flags.Bool("allow-live", false, "allow calls to the fixed runtime model provider")
	planLiveEvaluationPath := flags.String("plan-live-evaluation", "", "new offline live evaluation resource plan output path; does not read credentials or call a provider")
	createLiveAuthorizationPath := flags.String("create-live-authorization", "", "new signed live evaluation authorization output path; does not call a provider")
	liveAuthorizationPath := flags.String("live-authorization", "", "signed live evaluation authorization required with --allow-live")
	liveAuthorizationState := flags.String("live-authorization-state", "", "append-only file live authorization consumption ledger root")
	liveAuthorizationStateBackend := flags.String("live-authorization-state-backend", "file", "live authorization state backend: file or redis")
	liveAuthorizationRedisConfigPath := flags.String("live-authorization-redis-config", "", "strict Redis shared-ledger config used when the state backend is redis")
	initializeLiveAuthorizationState := flags.Bool("initialize-live-authorization-state", false, "initialize an already signed authorization in the Redis shared ledger without calling a model provider")
	inspectLiveAuthorizationState := flags.Bool("inspect-live-authorization-state", false, "inspect an initialized Redis live authorization ledger without calling a model provider")
	revokeLiveAuthorizationState := flags.Bool("revoke-live-authorization-state", false, "atomically revoke an initialized Redis live authorization ledger without calling a model provider")
	liveAuthorizationRevocationOperator := flags.String("live-authorization-revocation-operator", "", "portable pseudonymous operator identifier required for Redis ledger revocation")
	liveAuthorizationRevocationReason := flags.String("live-authorization-revocation-reason", "", "fixed Redis ledger revocation reason code")
	liveAuthorizationID := flags.String("live-authorization-id", "", "portable unique identifier used when creating a live authorization")
	liveAuthorizationTTL := flags.Duration("live-authorization-ttl", 0, "validity duration used when creating a live authorization")
	liveAuthorizationMaxRuns := flags.Int("live-authorization-max-runs", 0, "maximum CLI invocations permitted by a new live authorization")
	liveAuthorizationMaxProviderCalls := flags.Int("live-authorization-max-provider-calls", 0, "maximum model provider calls permitted by a new live authorization")
	liveAuthorizationMaxCapturedOutputs := flags.Int("live-authorization-max-captured-outputs", 0, "maximum sensitive outputs permitted by a new live authorization")
	liveAuthorizationMaxEstimatedCostMicros := flags.Int64("live-authorization-max-estimated-cost-micros", 0, "maximum pre-call estimated provider cost permitted by a new live authorization")
	liveAuthorizationKeyEnv := flags.String("live-authorization-key-env", "AGENT_TASK_EVAL_LIVE_AUTHORIZATION_KEY", "environment variable containing the live authorization HMAC key")
	liveAuthorizationKeyID := flags.String("live-authorization-key-id", envOr("AGENT_TASK_EVAL_LIVE_AUTHORIZATION_KEY_ID", ""), "non-secret live authorization HMAC key identifier")
	checkpointDir := flags.String("checkpoint-dir", "", "append-only signed per-case checkpoint directory for a live evaluation")
	progress := flags.Bool("progress", false, "write report-safe per-case progress to stderr")
	preflightTimeout := flags.Duration("preflight-timeout", 20*time.Second, "timeout for the live model chat/tool capability preflight")
	verifyReportPath := flags.String("verify-report", "", "verify an existing signed report and exit")
	archiveConfigPath := flags.String("archive-config", "", "MinIO Object Lock archive config path; archives the generated signed report")
	archiveReportPath := flags.String("archive-report", "", "archive an existing signed report instead of running evaluation")
	archiveContentSignoffPath := flags.String("archive-content-signoff", "", "content review signoff to verify and archive with --archive-report")
	archiveReceiptPath := flags.String("archive-receipt", "", "append-only local archive receipt output path")
	verifyArchiveReceiptPath := flags.String("verify-archive-receipt", "", "read and verify a versioned archived report using this receipt")
	requireArchivedContentSignoff := flags.Bool("require-archived-content-signoff", false, "require --verify-archive-receipt to contain approved external human content signoff")
	reviewBundlePath := flags.String("review-bundle", "", "new encrypted human-review bundle path for a live strategy comparison")
	captureFailedReviewBundle := flags.Bool("capture-failed-review-bundle", false, "write the encrypted review bundle even when automatic gates fail; diagnostic only and ineligible for signoff")
	openReviewBundlePath := flags.String("open-review-bundle", "", "decrypt and validate an existing human-review bundle")
	reviewReportPath := flags.String("review-report", "", "signed report bound to a review operation")
	reviewOutputPath := flags.String("review-output", "", "new plaintext review output path for --open-review-bundle")
	reviewDecisionTemplatePath := flags.String("review-decision-template", "", "new fail-closed external-human decision template written while opening a review bundle")
	reviewBundleInputPath := flags.String("review-bundle-input", "", "encrypted review bundle bound to a content review signoff")
	reviewDecisionPath := flags.String("review-decision", "", "versioned content review decision used to create a signoff")
	createReviewSignoffPath := flags.String("create-review-signoff", "", "new signed content review signoff output path")
	verifyReviewSignoffPath := flags.String("verify-review-signoff", "", "verify an existing content review signoff")
	allowReviewContent := flags.Bool("allow-review-content", false, "explicitly allow capture or decryption of evaluation inputs and model outputs")
	reviewKeyEnv := flags.String("review-key-env", "AGENT_TASK_EVAL_REVIEW_KEY", "environment variable containing a base64-encoded 32-byte review encryption key")
	reviewKeyID := flags.String("review-key-id", envOr("AGENT_TASK_EVAL_REVIEW_KEY_ID", ""), "non-secret review encryption key identifier")
	reviewSignoffKeyEnv := flags.String("review-signoff-key-env", "AGENT_TASK_EVAL_REVIEW_SIGNOFF_KEY", "environment variable containing the content review signoff HMAC key")
	reviewSignoffKeyID := flags.String("review-signoff-key-id", envOr("AGENT_TASK_EVAL_REVIEW_SIGNOFF_KEY_ID", ""), "non-secret content review signoff HMAC key identifier")
	integrityKeyEnv := flags.String("integrity-key-env", "AGENT_TASK_EVAL_INTEGRITY_KEY", "environment variable containing the report HMAC key")
	integrityKeyID := flags.String("integrity-key-id", envOr("AGENT_TASK_EVAL_INTEGRITY_KEY_ID", ""), "non-secret report HMAC key identifier")
	outPath := flags.String("out", "", "report output path; empty writes JSON to stdout")
	datasetVersion := flags.String("dataset-version", envOr("AGENT_TASK_EVAL_DATASET_VERSION", "agent-task-cases-v1"), "immutable dataset version")
	caseTimeout := flags.Duration("case-timeout", envDuration("AGENT_TASK_EVAL_CASE_TIMEOUT", 15*time.Second), "timeout per evaluation case")
	overallTimeout := flags.Duration("timeout", envDuration("AGENT_TASK_EVAL_TIMEOUT", 10*time.Minute), "overall evaluation timeout")
	enforceGate := flags.Bool("enforce-gate", false, "return a non-zero exit code unless stable/candidate quality gate passes")
	minCases := flags.Int("min-cases", 50, "minimum cases required in stable and candidate reports")
	minReadAccuracyBPS := flags.Int("min-read-tool-accuracy-bps", 9000, "minimum candidate read-tool selection accuracy in basis points")
	maxTaskRegressionBPS := flags.Int("max-task-regression-bps", 200, "maximum candidate task completion regression in basis points")
	maxToolRegressionBPS := flags.Int("max-tool-regression-bps", 200, "maximum candidate tool selection regression in basis points")
	maxSemanticRegressionBPS := flags.Int("max-semantic-regression-bps", 200, "maximum candidate semantic assertion regression in basis points")
	strategyGate := flags.Bool("strategy-gate", false, "evaluate the bounded single-agent versus multi-agent strategy gate")
	enforceStrategyGate := flags.Bool("enforce-strategy-gate", false, "return a non-zero exit code unless the strategy gate passes")
	strategyMinCases := flags.Int("strategy-min-cases", 20, "minimum comparable cases required by the strategy gate")
	strategyMinSemanticRateBPS := flags.Int("strategy-min-semantic-rate-bps", 9000, "minimum multi-agent semantic pass rate in basis points")
	strategyMinSemanticGainBPS := flags.Int("strategy-min-semantic-gain-bps", 500, "minimum multi-agent semantic gain over single-agent in basis points")
	strategyMaxTaskRegressionBPS := flags.Int("strategy-max-task-regression-bps", 0, "maximum multi-agent task completion regression in basis points")
	strategyMaxToolRegressionBPS := flags.Int("strategy-max-tool-regression-bps", 0, "maximum multi-agent tool selection regression in basis points")
	strategyMaxCostRatioBPS := flags.Int("strategy-max-average-cost-ratio-bps", 30000, "maximum multi-agent average cost ratio in basis points")
	strategyMaxP95RatioBPS := flags.Int("strategy-max-p95-ratio-bps", 35000, "maximum multi-agent P95 latency ratio in basis points")
	strategyMaxP95MS := flags.Int64("strategy-max-p95-ms", 60000, "maximum absolute multi-agent P95 latency in milliseconds")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *caseTimeout <= 0 || *overallTimeout <= 0 || *preflightTimeout <= 0 {
		return commandError(stderr, "--case-timeout, --timeout and --preflight-timeout must be positive")
	}
	runtimeConfigName := strings.TrimSpace(*runtimeConfigPath)
	strategyRuntimeConfigName := strings.TrimSpace(*strategyRuntimeConfigPath)
	planLiveEvaluationName := strings.TrimSpace(*planLiveEvaluationPath)
	createLiveAuthorizationName := strings.TrimSpace(*createLiveAuthorizationPath)
	liveAuthorizationName := strings.TrimSpace(*liveAuthorizationPath)
	liveAuthorizationStateRoot := strings.TrimSpace(*liveAuthorizationState)
	liveAuthorizationStateBackendName := strings.ToLower(strings.TrimSpace(*liveAuthorizationStateBackend))
	liveAuthorizationRedisConfigName := strings.TrimSpace(*liveAuthorizationRedisConfigPath)
	revocationOperator := strings.TrimSpace(*liveAuthorizationRevocationOperator)
	revocationReason := strings.ToLower(strings.TrimSpace(*liveAuthorizationRevocationReason))
	liveAuthorizationAdminModeCount := 0
	for _, enabled := range []bool{*initializeLiveAuthorizationState, *inspectLiveAuthorizationState, *revokeLiveAuthorizationState} {
		if enabled {
			liveAuthorizationAdminModeCount++
		}
	}
	if liveAuthorizationAdminModeCount > 1 {
		return commandError(stderr, "live authorization initialize, inspect and revoke modes are mutually exclusive")
	}
	if !*revokeLiveAuthorizationState && (revocationOperator != "" || revocationReason != "") {
		return commandError(stderr, "live authorization revocation metadata requires --revoke-live-authorization-state")
	}
	liveAuthorizationAdminMode := liveAuthorizationAdminModeCount == 1
	if liveAuthorizationStateBackendName != "file" && liveAuthorizationStateBackendName != "redis" {
		return commandError(stderr, "--live-authorization-state-backend must be file or redis")
	}
	archiveConfigName := strings.TrimSpace(*archiveConfigPath)
	archiveReportName := strings.TrimSpace(*archiveReportPath)
	archiveContentSignoffName := strings.TrimSpace(*archiveContentSignoffPath)
	archiveReceiptName := strings.TrimSpace(*archiveReceiptPath)
	verifyArchiveReceiptName := strings.TrimSpace(*verifyArchiveReceiptPath)
	reviewBundleName := strings.TrimSpace(*reviewBundlePath)
	if *captureFailedReviewBundle && reviewBundleName == "" {
		return commandError(stderr, "--capture-failed-review-bundle requires --review-bundle")
	}
	openReviewBundleName := strings.TrimSpace(*openReviewBundlePath)
	reviewReportName := strings.TrimSpace(*reviewReportPath)
	reviewOutputName := strings.TrimSpace(*reviewOutputPath)
	reviewDecisionTemplateName := strings.TrimSpace(*reviewDecisionTemplatePath)
	reviewBundleInputName := strings.TrimSpace(*reviewBundleInputPath)
	reviewDecisionName := strings.TrimSpace(*reviewDecisionPath)
	createReviewSignoffName := strings.TrimSpace(*createReviewSignoffPath)
	verifyReviewSignoffName := strings.TrimSpace(*verifyReviewSignoffPath)
	if planLiveEvaluationName != "" {
		if *allowLive || createLiveAuthorizationName != "" || liveAuthorizationName != "" || liveAuthorizationStateRoot != "" ||
			liveAuthorizationAdminMode || liveAuthorizationStateBackendName != "file" || liveAuthorizationRedisConfigName != "" ||
			strings.TrimSpace(*checkpointDir) != "" || strings.TrimSpace(*outPath) != "" || strings.TrimSpace(*verifyReportPath) != "" ||
			archiveConfigName != "" || archiveReportName != "" || verifyArchiveReceiptName != "" || reviewBundleName != "" ||
			openReviewBundleName != "" || createReviewSignoffName != "" || verifyReviewSignoffName != "" ||
			strings.TrimSpace(*stableResultsPath) != "" || strings.TrimSpace(*stableReportPath) != "" || *allowReviewContent ||
			*enforceGate || *strategyGate || *enforceStrategyGate || *liveAuthorizationMaxRuns != 0 ||
			*liveAuthorizationMaxProviderCalls != 0 || *liveAuthorizationMaxCapturedOutputs != 0 ||
			*liveAuthorizationMaxEstimatedCostMicros != 0 {
			return commandError(stderr, "--plan-live-evaluation cannot be combined with evaluation, authorization, review or archive operations")
		}
		if (runtimeConfigName == "") == (strategyRuntimeConfigName == "") {
			return commandError(stderr, "--plan-live-evaluation requires exactly one of --runtime-config or --strategy-runtime-config")
		}
		plan, planErr := runCreateAgentTaskLivePlan(createAgentTaskLivePlanCommand{
			OutputPath: planLiveEvaluationName, DatasetPath: *datasetPath, DatasetVersion: *datasetVersion,
			RuntimeConfigPath: runtimeConfigName, StrategyConfigPath: strategyRuntimeConfigName,
		})
		if planErr != nil {
			return commandError(stderr, "plan live evaluation: %v", planErr)
		}
		fmt.Fprintf(
			stdout,
			"Offline live evaluation plan created: %s (model=%s, calls=%d..%d, token_upper=%d, cost_upper_micros=%d)\n",
			planLiveEvaluationName, plan.Model, plan.Budget.ProviderCallsMinimum, plan.Budget.ProviderCallsUpperBound,
			plan.Budget.TokenBudgetUpperBound, plan.Budget.EstimatedCostUpperBoundMicros,
		)
		return 0
	}
	if createLiveAuthorizationName != "" {
		if *allowLive || liveAuthorizationName != "" || liveAuthorizationStateRoot != "" || liveAuthorizationAdminMode ||
			liveAuthorizationStateBackendName != "file" || liveAuthorizationRedisConfigName != "" || strings.TrimSpace(*checkpointDir) != "" ||
			strings.TrimSpace(*outPath) != "" || strings.TrimSpace(*verifyReportPath) != "" || archiveConfigName != "" ||
			archiveReportName != "" || verifyArchiveReceiptName != "" || reviewBundleName != "" || openReviewBundleName != "" ||
			createReviewSignoffName != "" || verifyReviewSignoffName != "" || strings.TrimSpace(*stableResultsPath) != "" ||
			strings.TrimSpace(*stableReportPath) != "" || *allowReviewContent {
			return commandError(stderr, "--create-live-authorization cannot be combined with evaluation, review or archive operations")
		}
		if (runtimeConfigName == "") == (strategyRuntimeConfigName == "") {
			return commandError(stderr, "--create-live-authorization requires exactly one of --runtime-config or --strategy-runtime-config")
		}
		authorizationKey, authorizationKeyErr := readAgentTaskLiveAuthorizationKey(*liveAuthorizationKeyEnv, *liveAuthorizationKeyID)
		if authorizationKeyErr != nil {
			return commandError(stderr, "%v", authorizationKeyErr)
		}
		authorization, createErr := runCreateAgentTaskLiveAuthorization(createAgentTaskLiveAuthorizationCommand{
			OutputPath: createLiveAuthorizationName, AuthorizationID: strings.TrimSpace(*liveAuthorizationID),
			TTL: *liveAuthorizationTTL, DatasetPath: *datasetPath, DatasetVersion: *datasetVersion,
			RuntimeConfigPath: runtimeConfigName, StrategyConfigPath: strategyRuntimeConfigName,
			Limits: agentTaskLiveAuthorizationLimits{
				MaxRuns: *liveAuthorizationMaxRuns, MaxProviderCalls: *liveAuthorizationMaxProviderCalls,
				MaxCapturedOutputs:     *liveAuthorizationMaxCapturedOutputs,
				MaxEstimatedCostMicros: *liveAuthorizationMaxEstimatedCostMicros,
			},
			Key: authorizationKey, KeyID: strings.TrimSpace(*liveAuthorizationKeyID), Now: time.Now().UTC(),
		})
		if createErr != nil {
			return commandError(stderr, "create live authorization: %v", createErr)
		}
		fmt.Fprintf(
			stdout,
			"Signed live authorization created: %s (authorization_id=%s, provider=%s, model=%s, expires_at=%s)\n",
			createLiveAuthorizationName, authorization.AuthorizationID, authorization.Provider, authorization.Model,
			authorization.ExpiresAt.Format(time.RFC3339),
		)
		return 0
	}
	if liveAuthorizationAdminMode {
		if liveAuthorizationStateBackendName != "redis" || liveAuthorizationName == "" || liveAuthorizationRedisConfigName == "" ||
			liveAuthorizationStateRoot != "" {
			return commandError(stderr, "live authorization Redis administration requires --live-authorization, Redis backend/config, and no file state root")
		}
		if *allowLive || planLiveEvaluationName != "" || createLiveAuthorizationName != "" || runtimeConfigName != "" ||
			strategyRuntimeConfigName != "" || strings.TrimSpace(*checkpointDir) != "" || strings.TrimSpace(*outPath) != "" ||
			strings.TrimSpace(*verifyReportPath) != "" || archiveConfigName != "" || archiveReportName != "" ||
			archiveContentSignoffName != "" || archiveReceiptName != "" || verifyArchiveReceiptName != "" ||
			reviewBundleName != "" || *captureFailedReviewBundle || openReviewBundleName != "" || reviewReportName != "" ||
			reviewOutputName != "" || reviewDecisionTemplateName != "" || reviewBundleInputName != "" || reviewDecisionName != "" ||
			createReviewSignoffName != "" || verifyReviewSignoffName != "" || strings.TrimSpace(*stableResultsPath) != "" ||
			strings.TrimSpace(*stableReportPath) != "" || strings.TrimSpace(*liveAuthorizationID) != "" || *liveAuthorizationTTL != 0 ||
			*liveAuthorizationMaxRuns != 0 || *liveAuthorizationMaxProviderCalls != 0 ||
			*liveAuthorizationMaxCapturedOutputs != 0 || *liveAuthorizationMaxEstimatedCostMicros != 0 ||
			*allowReviewContent || *enforceGate || *strategyGate || *enforceStrategyGate || *requireArchivedContentSignoff || *progress {
			return commandError(stderr, "live authorization Redis administration cannot be combined with evaluation, planning, review or archive operations")
		}
		if *revokeLiveAuthorizationState {
			if revocationOperator == "" || revocationReason == "" {
				return commandError(stderr, "--revoke-live-authorization-state requires --live-authorization-revocation-operator and --live-authorization-revocation-reason")
			}
			if err := validateAgentTaskLiveRedisRevocationMetadata(revocationOperator, revocationReason); err != nil {
				return commandError(stderr, "%v", err)
			}
		} else if revocationOperator != "" || revocationReason != "" {
			return commandError(stderr, "live authorization revocation metadata requires --revoke-live-authorization-state")
		}
		authorizationKey, authorizationKeyErr := readAgentTaskLiveAuthorizationKey(*liveAuthorizationKeyEnv, *liveAuthorizationKeyID)
		if authorizationKeyErr != nil {
			return commandError(stderr, "%v", authorizationKeyErr)
		}
		now := time.Now().UTC()
		keyID := strings.TrimSpace(*liveAuthorizationKeyID)
		if *initializeLiveAuthorizationState {
			created, evidence, initializeErr := initializeAgentTaskLiveRedisAuthorizationState(
				context.Background(), liveAuthorizationName, liveAuthorizationRedisConfigName,
				authorizationKey, keyID, now,
			)
			if initializeErr != nil {
				return commandError(stderr, "initialize live authorization state: %v", initializeErr)
			}
			status := "already_initialized"
			if created {
				status = "initialized"
			}
			fmt.Fprintf(
				stdout,
				"Redis live authorization state %s (authorization_id=%s, namespace_sha256=%s)\n",
				status, evidence.AuthorizationID, evidence.StateNamespaceSHA256,
			)
			return 0
		}
		operation := "inspect"
		changed := false
		var snapshot agentTaskLiveRedisStateSnapshot
		var adminErr error
		if *inspectLiveAuthorizationState {
			snapshot, adminErr = inspectAgentTaskLiveRedisAuthorizationState(
				context.Background(), liveAuthorizationName, liveAuthorizationRedisConfigName,
				authorizationKey, keyID, now,
			)
		} else {
			operation = "revoke"
			changed, snapshot, adminErr = revokeAgentTaskLiveRedisAuthorizationState(
				context.Background(), liveAuthorizationName, liveAuthorizationRedisConfigName,
				authorizationKey, keyID, revocationOperator, revocationReason, now,
			)
		}
		if adminErr != nil {
			return commandError(stderr, "%s live authorization state: %v", operation, adminErr)
		}
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(agentTaskLiveRedisAdminOutput{
			SchemaVersion: agentTaskLiveRedisAdminOutputSchemaVersion,
			Operation:     operation,
			Changed:       changed,
			State:         snapshot,
		}); err != nil {
			return commandError(stderr, "write live authorization Redis administration output: %v", err)
		}
		return 0
	}
	integrityKey, keyErr := readIntegrityKey(*integrityKeyEnv, *integrityKeyID)
	if keyErr != nil {
		return commandError(stderr, "%v", keyErr)
	}
	if strings.TrimSpace(*liveAuthorizationID) != "" || *liveAuthorizationTTL != 0 || *liveAuthorizationMaxRuns != 0 ||
		*liveAuthorizationMaxProviderCalls != 0 || *liveAuthorizationMaxCapturedOutputs != 0 || *liveAuthorizationMaxEstimatedCostMicros != 0 {
		return commandError(stderr, "live authorization limit flags require --create-live-authorization")
	}
	archiveModeCount := 0
	if archiveReportName != "" {
		archiveModeCount++
	}
	if verifyArchiveReceiptName != "" {
		archiveModeCount++
	}
	if archiveModeCount > 1 {
		return commandError(stderr, "--archive-report and --verify-archive-receipt are mutually exclusive")
	}
	qualifiedArchiveOperation := archiveContentSignoffName != ""
	if qualifiedArchiveOperation && archiveReportName == "" {
		return commandError(stderr, "--archive-content-signoff requires --archive-report")
	}
	if *requireArchivedContentSignoff && verifyArchiveReceiptName == "" {
		return commandError(stderr, "--require-archived-content-signoff requires --verify-archive-receipt")
	}
	signoffModeCount := 0
	if createReviewSignoffName != "" {
		signoffModeCount++
	}
	if verifyReviewSignoffName != "" {
		signoffModeCount++
	}
	if signoffModeCount > 1 {
		return commandError(stderr, "--create-review-signoff and --verify-review-signoff are mutually exclusive")
	}
	signoffOperation := signoffModeCount == 1
	if qualifiedArchiveOperation && signoffOperation {
		return commandError(stderr, "--archive-content-signoff cannot be combined with content review signoff creation or verification")
	}
	if (reviewBundleName != "" && openReviewBundleName != "") ||
		(signoffOperation && (reviewBundleName != "" || openReviewBundleName != "")) {
		return commandError(stderr, "review bundle creation, opening and signoff operations are mutually exclusive")
	}
	interactiveReviewOperation := reviewBundleName != "" || openReviewBundleName != "" || signoffOperation
	reviewOperation := interactiveReviewOperation || qualifiedArchiveOperation
	if reviewOperation && !*allowReviewContent {
		return commandError(stderr, "review operations require explicit --allow-review-content")
	}
	if !reviewOperation && (reviewReportName != "" || reviewOutputName != "" || reviewDecisionTemplateName != "" || reviewBundleInputName != "" || reviewDecisionName != "" || *allowReviewContent) {
		return commandError(stderr, "review input and consent flags require a review operation")
	}
	if openReviewBundleName == "" && !signoffOperation && (reviewReportName != "" || reviewOutputName != "" || reviewDecisionTemplateName != "") {
		return commandError(stderr, "--review-report and --review-output require a bundle open or signoff operation")
	}
	if reviewDecisionTemplateName != "" && openReviewBundleName == "" {
		return commandError(stderr, "--review-decision-template requires --open-review-bundle")
	}
	if !signoffOperation && !qualifiedArchiveOperation && (reviewBundleInputName != "" || reviewDecisionName != "") {
		return commandError(stderr, "--review-bundle-input and --review-decision require a content review signoff operation")
	}
	if qualifiedArchiveOperation && reviewBundleInputName == "" {
		return commandError(stderr, "--archive-content-signoff requires --review-bundle-input")
	}
	if qualifiedArchiveOperation && reviewDecisionName != "" {
		return commandError(stderr, "--review-decision is not valid while archiving an existing content signoff")
	}
	if openReviewBundleName != "" && (reviewReportName == "" || reviewOutputName == "") {
		return commandError(stderr, "--open-review-bundle requires --review-report and --review-output")
	}
	if signoffOperation && (reviewReportName == "" || reviewBundleInputName == "") {
		return commandError(stderr, "content review signoff operations require --review-report and --review-bundle-input")
	}
	if signoffOperation && reviewOutputName != "" {
		return commandError(stderr, "content review signoff operations do not write --review-output plaintext")
	}
	if createReviewSignoffName != "" && reviewDecisionName == "" {
		return commandError(stderr, "--create-review-signoff requires --review-decision")
	}
	if verifyReviewSignoffName != "" && reviewDecisionName != "" {
		return commandError(stderr, "--review-decision is only valid with --create-review-signoff")
	}
	if interactiveReviewOperation && (strings.TrimSpace(*verifyReportPath) != "" || archiveConfigName != "" || archiveModeCount > 0) {
		return commandError(stderr, "review operations cannot be combined with report verification or archive operations")
	}
	if signoffOperation && (strings.TrimSpace(*runtimeConfigPath) != "" || strings.TrimSpace(*strategyRuntimeConfigPath) != "" ||
		strings.TrimSpace(*checkpointDir) != "" || strings.TrimSpace(*outPath) != "" || *allowLive) {
		return commandError(stderr, "content review signoff operations cannot be combined with live evaluation flags")
	}
	var reviewKey []byte
	if reviewOperation {
		var reviewKeyErr error
		reviewKey, reviewKeyErr = readAgentTaskReviewKey(*reviewKeyEnv, *reviewKeyID)
		if reviewKeyErr != nil {
			return commandError(stderr, "%v", reviewKeyErr)
		}
	}
	var signoffKey []byte
	needSignoffKey := signoffOperation || qualifiedArchiveOperation || *requireArchivedContentSignoff
	if needSignoffKey {
		if len(integrityKey) == 0 {
			return commandError(stderr, "content review signoff verification requires a configured report integrity key")
		}
		var signoffKeyErr error
		signoffKey, signoffKeyErr = readAgentTaskContentReviewSignoffKey(*reviewSignoffKeyEnv, *reviewSignoffKeyID)
		if signoffKeyErr != nil {
			return commandError(stderr, "%v", signoffKeyErr)
		}
	}
	if signoffOperation {
		result, signoffErr := runAgentTaskContentReviewSignoffCommand(agentTaskContentReviewSignoffCommand{
			CreatePath:       createReviewSignoffName,
			VerifyPath:       verifyReviewSignoffName,
			DecisionPath:     reviewDecisionName,
			ReportPath:       reviewReportName,
			ReviewBundlePath: reviewBundleInputName,
			ReportKey:        integrityKey,
			ReportKeyID:      strings.TrimSpace(*integrityKeyID),
			ReviewKey:        reviewKey,
			ReviewKeyID:      strings.TrimSpace(*reviewKeyID),
			SignoffKey:       signoffKey,
			SignoffKeyID:     strings.TrimSpace(*reviewSignoffKeyID),
		})
		if signoffErr != nil {
			return commandError(stderr, "content review signoff: %v", signoffErr)
		}
		operation := "verified"
		path := verifyReviewSignoffName
		if result.Created {
			operation = "created"
			path = createReviewSignoffName
		}
		fmt.Fprintf(
			stdout,
			"Agent task content review signoff %s: %s (reviewer_kind=%s, candidate_verdict=%s, external_human_approved=%t)\n",
			operation,
			path,
			result.Signoff.Reviewer.Kind,
			result.Signoff.CandidateVerdict,
			eval.AgentTaskContentReviewHasApprovedExternalHumanSignoff(result.Signoff),
		)
		return 0
	}
	if openReviewBundleName != "" {
		if len(integrityKey) == 0 {
			return commandError(stderr, "--open-review-bundle requires a configured report integrity key")
		}
		same, pathErr := sameReviewPath(openReviewBundleName, reviewOutputName)
		if pathErr != nil {
			return commandError(stderr, "compare review paths: %v", pathErr)
		}
		if same {
			return commandError(stderr, "--review-output must differ from --open-review-bundle")
		}
		if err := ensureReviewPathAvailable(reviewOutputName, "plaintext review output"); err != nil {
			return commandError(stderr, "%v", err)
		}
		if reviewDecisionTemplateName != "" {
			for _, other := range []string{openReviewBundleName, reviewReportName, reviewOutputName} {
				same, pathErr := sameReviewPath(reviewDecisionTemplateName, other)
				if pathErr != nil {
					return commandError(stderr, "compare decision template paths: %v", pathErr)
				}
				if same {
					return commandError(stderr, "--review-decision-template must differ from review inputs and plaintext output")
				}
			}
			if err := ensureReviewPathAvailable(reviewDecisionTemplateName, "content review decision template"); err != nil {
				return commandError(stderr, "%v", err)
			}
		}
		verified, verifyErr := loadVerifiedEvaluationOutput(reviewReportName, integrityKey, strings.TrimSpace(*integrityKeyID))
		if verifyErr != nil {
			return commandError(stderr, "verify review report: %v", verifyErr)
		}
		payload, binding, openErr := loadAndOpenAgentTaskReviewBundleWithBinding(openReviewBundleName, reviewKey, strings.TrimSpace(*reviewKeyID), verified)
		if openErr != nil {
			return commandError(stderr, "open review bundle: %v", openErr)
		}
		if err := writeAgentTaskReviewPayload(reviewOutputName, payload); err != nil {
			return commandError(stderr, "%v", err)
		}
		if reviewDecisionTemplateName != "" {
			if err := writeAgentTaskContentReviewDecisionTemplate(reviewDecisionTemplateName, verified, binding); err != nil {
				return commandError(stderr, "write content review decision template: %v", err)
			}
		}
		fmt.Fprintf(stdout, "Agent task review bundle opened to sensitive local file %s (report_payload_sha256=%s, review_bundle_sha256=%s)\n", reviewOutputName, payload.ReportPayloadSHA256, binding.FileSHA256)
		if reviewDecisionTemplateName != "" {
			fmt.Fprintf(stdout, "Fail-closed external-human decision template written to %s; reviewer identity, time and every dimension require human completion\n", reviewDecisionTemplateName)
		}
		return 0
	}
	if strings.TrimSpace(*verifyReportPath) != "" && (archiveConfigName != "" || archiveModeCount > 0) {
		return commandError(stderr, "--verify-report cannot be combined with archive operations")
	}
	if strings.TrimSpace(*verifyReportPath) != "" {
		if len(integrityKey) == 0 {
			return commandError(stderr, "--verify-report requires a configured integrity key")
		}
		verified, verifyErr := loadVerifiedEvaluationOutput(*verifyReportPath, integrityKey, strings.TrimSpace(*integrityKeyID))
		if verifyErr != nil {
			return commandError(stderr, "verify report: %v", verifyErr)
		}
		fmt.Fprintf(stdout, "Agent task evaluation report verified: %s (key_id=%s)\n", *verifyReportPath, verified.Integrity.KeyID)
		return 0
	}
	if archiveModeCount > 0 {
		if archiveConfigName == "" {
			return commandError(stderr, "archive operations require --archive-config")
		}
		if len(integrityKey) == 0 {
			return commandError(stderr, "archive operations require a configured integrity key")
		}
		archiveConfig, loadErr := loadAgentTaskArchiveConfig(archiveConfigName)
		if loadErr != nil {
			return commandError(stderr, "load archive config: %v", loadErr)
		}
		reportArchive, configureErr := newAgentTaskReportArchive(archiveConfig)
		if configureErr != nil {
			return commandError(stderr, "configure report archive: %v", configureErr)
		}
		ctx, cancel := context.WithTimeout(context.Background(), *overallTimeout)
		defer cancel()
		if archiveReportName != "" {
			if archiveReceiptName == "" {
				return commandError(stderr, "--archive-report requires --archive-receipt")
			}
			if err := ensureArchiveReceiptPathAvailable(archiveReceiptName); err != nil {
				return commandError(stderr, "%v", err)
			}
			output, loadErr := loadVerifiedEvaluationOutput(archiveReportName, integrityKey, strings.TrimSpace(*integrityKeyID))
			if loadErr != nil {
				return commandError(stderr, "load report for archive: %v", loadErr)
			}
			if qualifiedArchiveOperation {
				result, signoffErr := runAgentTaskContentReviewSignoffCommand(agentTaskContentReviewSignoffCommand{
					VerifyPath:       archiveContentSignoffName,
					ReportPath:       archiveReportName,
					ReviewBundlePath: reviewBundleInputName,
					ReportKey:        integrityKey,
					ReportKeyID:      strings.TrimSpace(*integrityKeyID),
					ReviewKey:        reviewKey,
					ReviewKeyID:      strings.TrimSpace(*reviewKeyID),
					SignoffKey:       signoffKey,
					SignoffKeyID:     strings.TrimSpace(*reviewSignoffKeyID),
				})
				if signoffErr != nil {
					return commandError(stderr, "verify content review before archive: %v", signoffErr)
				}
				receipt, archiveErr := archiveContentQualifiedEvaluationOutput(
					ctx, reportArchive, output, result.Signoff,
					integrityKey, strings.TrimSpace(*integrityKeyID),
					signoffKey, strings.TrimSpace(*reviewSignoffKeyID),
					archiveConfig.RetentionDays, time.Now(),
				)
				if archiveErr != nil {
					return commandError(stderr, "archive content-qualified evidence: %v", archiveErr)
				}
				if err := writeAgentTaskArchiveReceipt(archiveReceiptName, receipt); err != nil {
					return commandError(stderr, "write receipt for %s/%s version %s: %v", receipt.Bucket, receipt.Key, receipt.VersionID, err)
				}
				fmt.Fprintf(stdout, "Content-qualified agent task evidence archived to %s/%s (version_id=%s, receipt=%s)\n", receipt.Bucket, receipt.Key, receipt.VersionID, archiveReceiptName)
				return 0
			}
			receipt, archiveErr := archiveEvaluationOutput(ctx, reportArchive, output, integrityKey, strings.TrimSpace(*integrityKeyID), archiveConfig.RetentionDays, time.Now())
			if archiveErr != nil {
				return commandError(stderr, "archive report: %v", archiveErr)
			}
			if err := writeAgentTaskArchiveReceipt(archiveReceiptName, receipt); err != nil {
				return commandError(stderr, "write receipt for %s/%s version %s: %v", receipt.Bucket, receipt.Key, receipt.VersionID, err)
			}
			fmt.Fprintf(stdout, "Agent task evaluation report archived to %s/%s (version_id=%s, receipt=%s)\n", receipt.Bucket, receipt.Key, receipt.VersionID, archiveReceiptName)
			return 0
		}
		receipt, loadErr := loadAgentTaskArchiveReceipt(verifyArchiveReceiptName)
		if loadErr != nil {
			return commandError(stderr, "load archive receipt: %v", loadErr)
		}
		if *requireArchivedContentSignoff {
			verified, verifyErr := verifyArchivedContentQualifiedEvaluationOutput(
				ctx, reportArchive, receipt,
				integrityKey, strings.TrimSpace(*integrityKeyID),
				signoffKey, strings.TrimSpace(*reviewSignoffKeyID),
			)
			if verifyErr != nil {
				return commandError(stderr, "verify archived content-qualified evidence: %v", verifyErr)
			}
			fmt.Fprintf(
				stdout,
				"Archived content-qualified agent task evidence verified: %s/%s (version_id=%s, report_key_id=%s, signoff_key_id=%s)\n",
				receipt.Bucket, receipt.Key, receipt.VersionID,
				verified.Report.Integrity.KeyID, verified.ContentReviewSignoff.Integrity.KeyID,
			)
			return 0
		}
		verified, verifyErr := verifyArchivedEvaluationOutput(ctx, reportArchive, receipt, integrityKey, strings.TrimSpace(*integrityKeyID))
		if verifyErr != nil {
			return commandError(stderr, "verify archived report: %v", verifyErr)
		}
		fmt.Fprintf(stdout, "Archived agent task report verified: %s/%s (version_id=%s, key_id=%s)\n", receipt.Bucket, receipt.Key, receipt.VersionID, verified.Integrity.KeyID)
		return 0
	}
	if strings.TrimSpace(*datasetVersion) == "" {
		return commandError(stderr, "--dataset-version must not be empty")
	}
	if strings.TrimSpace(*stableResultsPath) != "" && strings.TrimSpace(*stableReportPath) != "" {
		return commandError(stderr, "--stable-results and --stable-report are mutually exclusive")
	}
	checkpointRoot := strings.TrimSpace(*checkpointDir)
	liveStrategyComparison := strategyRuntimeConfigName != ""
	liveEvaluation := runtimeConfigName != "" || liveStrategyComparison
	strategyGateRequested := *strategyGate || *enforceStrategyGate || liveStrategyComparison
	if *enforceGate && strings.TrimSpace(*stableResultsPath) == "" && strings.TrimSpace(*stableReportPath) == "" {
		if !liveStrategyComparison {
			return commandError(stderr, "--enforce-gate requires --stable-results or --stable-report")
		}
	}
	if strategyGateRequested && strings.TrimSpace(*stableResultsPath) == "" && strings.TrimSpace(*stableReportPath) == "" {
		if !liveStrategyComparison {
			return commandError(stderr, "--strategy-gate/--enforce-strategy-gate requires --stable-results or --stable-report")
		}
	}
	if runtimeConfigName != "" && strategyRuntimeConfigName != "" {
		return commandError(stderr, "--runtime-config and --strategy-runtime-config are mutually exclusive")
	}
	if liveEvaluation && !*allowLive {
		return commandError(stderr, "live runtime config requires explicit --allow-live")
	}
	if *allowLive && runtimeConfigName == "" && strategyRuntimeConfigName == "" {
		return commandError(stderr, "--allow-live requires --runtime-config or --strategy-runtime-config")
	}
	if checkpointRoot != "" && runtimeConfigName == "" && strategyRuntimeConfigName == "" {
		return commandError(stderr, "--checkpoint-dir is only available for live runtime evaluation")
	}
	if reviewBundleName != "" {
		if !liveStrategyComparison {
			return commandError(stderr, "--review-bundle is only available for a live --strategy-runtime-config comparison")
		}
		if checkpointRoot != "" {
			return commandError(stderr, "--review-bundle cannot be combined with --checkpoint-dir because resumed output bodies are intentionally unavailable")
		}
		if !*enforceGate || !*enforceStrategyGate {
			return commandError(stderr, "--review-bundle requires --enforce-gate and --enforce-strategy-gate")
		}
		outName := strings.TrimSpace(*outPath)
		if outName == "" {
			return commandError(stderr, "--review-bundle requires a dedicated --out report path")
		}
		same, pathErr := sameReviewPath(reviewBundleName, outName)
		if pathErr != nil {
			return commandError(stderr, "compare report and review bundle paths: %v", pathErr)
		}
		if same {
			return commandError(stderr, "--review-bundle must differ from --out")
		}
		if err := ensureReviewPathAvailable(outName, "signed report output"); err != nil {
			return commandError(stderr, "%v", err)
		}
		if err := ensureReviewPathAvailable(reviewBundleName, "encrypted review bundle"); err != nil {
			return commandError(stderr, "%v", err)
		}
	}
	if liveEvaluation && liveAuthorizationStateBackendName == "file" &&
		(liveAuthorizationName == "" || liveAuthorizationStateRoot == "") {
		return commandError(stderr, "live runtime evaluation requires --live-authorization and --live-authorization-state")
	}
	if liveEvaluation && liveAuthorizationStateBackendName == "file" && liveAuthorizationRedisConfigName != "" {
		return commandError(stderr, "file live authorization state cannot use --live-authorization-redis-config")
	}
	if liveEvaluation && liveAuthorizationStateBackendName == "redis" &&
		(liveAuthorizationName == "" || liveAuthorizationRedisConfigName == "" || liveAuthorizationStateRoot != "") {
		return commandError(stderr, "Redis live runtime evaluation requires --live-authorization, --live-authorization-redis-config, and no file state root")
	}
	if !liveEvaluation && (liveAuthorizationName != "" || liveAuthorizationStateRoot != "" ||
		liveAuthorizationRedisConfigName != "" || liveAuthorizationStateBackendName != "file") {
		return commandError(stderr, "live authorization state flags require live runtime evaluation or Redis state initialization")
	}
	var liveAuthorizationKey []byte
	if liveEvaluation {
		var authorizationKeyErr error
		liveAuthorizationKey, authorizationKeyErr = readAgentTaskLiveAuthorizationKey(*liveAuthorizationKeyEnv, *liveAuthorizationKeyID)
		if authorizationKeyErr != nil {
			return commandError(stderr, "%v", authorizationKeyErr)
		}
		if sameAgentTaskLiveAuthorizationKey(liveAuthorizationKey, *liveAuthorizationKeyID, integrityKey, *integrityKeyID) {
			return commandError(stderr, "live authorization and report integrity keys must be independent")
		}
		if sameAgentTaskLiveAuthorizationKey(liveAuthorizationKey, *liveAuthorizationKeyID, reviewKey, *reviewKeyID) {
			return commandError(stderr, "live authorization and review encryption keys must be independent")
		}
	}
	if liveEvaluation && len(integrityKey) == 0 {
		return commandError(stderr, "live runtime evaluation requires a signed report integrity key")
	}
	openLiveAuthorization := func(
		binding agentTaskLiveAuthorizationBinding,
		capturedOutputs int,
	) (agentTaskLiveAuthorizationBudget, func(), error) {
		if liveAuthorizationStateBackendName == "redis" {
			ledger, client, err := openAndReserveAgentTaskLiveRedisAuthorization(
				context.Background(), liveAuthorizationName, liveAuthorizationRedisConfigName,
				liveAuthorizationKey, strings.TrimSpace(*liveAuthorizationKeyID), binding,
				capturedOutputs, time.Now().UTC(),
			)
			if err != nil {
				return nil, nil, err
			}
			return ledger, func() { _ = client.Close() }, nil
		}
		ledger, err := openAndReserveAgentTaskLiveAuthorization(
			liveAuthorizationName, liveAuthorizationStateRoot,
			liveAuthorizationKey, strings.TrimSpace(*liveAuthorizationKeyID), binding,
			capturedOutputs, time.Now().UTC(),
		)
		return ledger, func() {}, err
	}
	if liveStrategyComparison && (strings.TrimSpace(*stableResultsPath) != "" || strings.TrimSpace(*stableReportPath) != "") {
		return commandError(stderr, "--strategy-runtime-config generates its own single-agent stable report and cannot use external stable evidence")
	}
	if liveStrategyComparison && *caseTimeout < strategyRuntimeMinimumCaseTime {
		return commandError(stderr, "--strategy-runtime-config requires --case-timeout of at least %s", strategyRuntimeMinimumCaseTime)
	}
	if strings.TrimSpace(*stableReportPath) != "" && len(integrityKey) == 0 {
		return commandError(stderr, "--stable-report requires a configured integrity key")
	}
	var reportArchive eval.AgentTaskReportArchive
	var archiveConfig agentTaskArchiveConfig
	if archiveConfigName != "" {
		if archiveReceiptName == "" {
			return commandError(stderr, "--archive-config requires --archive-receipt")
		}
		if len(integrityKey) == 0 {
			return commandError(stderr, "report archive requires a configured integrity key")
		}
		if err := ensureArchiveReceiptPathAvailable(archiveReceiptName); err != nil {
			return commandError(stderr, "%v", err)
		}
		loadedArchiveConfig, loadErr := loadAgentTaskArchiveConfig(archiveConfigName)
		if loadErr != nil {
			return commandError(stderr, "load archive config: %v", loadErr)
		}
		configuredArchive, configureErr := newAgentTaskReportArchive(loadedArchiveConfig)
		if configureErr != nil {
			return commandError(stderr, "configure report archive: %v", configureErr)
		}
		preflightCtx, preflightCancel := context.WithTimeout(context.Background(), minDuration(*overallTimeout, 30*time.Second))
		preflightErr := configuredArchive.Ensure(preflightCtx)
		preflightCancel()
		if preflightErr != nil {
			return commandError(stderr, "preflight report archive: %v", preflightErr)
		}
		reportArchive = configuredArchive
		archiveConfig = loadedArchiveConfig
	} else if archiveReceiptName != "" {
		return commandError(stderr, "--archive-receipt requires --archive-config")
	}
	var signedStableReport *eval.AgentTaskReport
	if strings.TrimSpace(*stableReportPath) != "" {
		stableOutput, loadErr := loadVerifiedEvaluationOutput(*stableReportPath, integrityKey, strings.TrimSpace(*integrityKeyID))
		if loadErr != nil {
			return commandError(stderr, "load stable report: %v", loadErr)
		}
		stable := stableOutput.Candidate
		signedStableReport = &stable
	}

	dataset, err := loadAgentTaskDataset(*datasetPath)
	if err != nil {
		return commandError(stderr, "%v", err)
	}
	datasetHash := ""
	if liveEvaluation {
		datasetHash, err = eval.HashAgentTaskDataset(dataset)
		if err != nil {
			return commandError(stderr, "hash live evaluation dataset: %v", err)
		}
	}
	var candidateExecutor eval.AgentTaskExecutor
	var candidateDescriptor eval.AgentTaskExecutionDescriptor
	var candidateConfigHash string
	var generatedStableExecutor eval.AgentTaskExecutor
	var generatedStableDescriptor eval.AgentTaskExecutionDescriptor
	var generatedStableConfigHash string
	var liveAuthorizationEvidence *eval.AgentTaskLiveAuthorizationEvidence
	if liveStrategyComparison {
		strategyConfig, loadErr := loadStrategyRuntimeEvalConfig(strategyRuntimeConfigName)
		if loadErr != nil {
			return commandError(stderr, "load strategy runtime evaluation config: %v", loadErr)
		}
		configHash, hashErr := hashStrategyRuntimeEvalConfig(strategyConfig)
		if hashErr != nil {
			return commandError(stderr, "%v", hashErr)
		}
		executors, configureErr := newLiveRuntimeStrategyExecutors(strategyConfig)
		if configureErr != nil {
			return commandError(stderr, "configure live strategy runtime executors: %v", configureErr)
		}
		capturedOutputs := 0
		if reviewBundleName != "" {
			capturedOutputs = len(dataset) * 2
		}
		ledger, closeAuthorization, authorizationErr := openLiveAuthorization(
			agentTaskLiveAuthorizationBinding{
				Provider: strategyConfig.Provider, Model: strategyConfig.Model,
				DatasetVersion: *datasetVersion, DatasetSHA256: datasetHash,
				ExecutionConfigSHA256: configHash,
			},
			capturedOutputs,
		)
		if authorizationErr != nil {
			return commandError(stderr, "authorize live strategy evaluation: %v", authorizationErr)
		}
		defer closeAuthorization()
		authorizationEvidence := ledger.Evidence()
		liveAuthorizationEvidence = &authorizationEvidence
		authorizedClient, authorizationErr := newAuthorizedLiveModelClient(
			executors.multi.modelClient, ledger, executors.multi.costEstimator,
			strategyConfig.Model, strategyConfig.MaxOutputTokens, isLocalRuntimeEvalProvider(strategyConfig.Provider),
		)
		if authorizationErr != nil {
			return commandError(stderr, "configure authorized live strategy client: %v", authorizationErr)
		}
		executors.multi.modelClient = authorizedClient
		executors.single.modelClient = authorizedClient
		candidateExecutor = executors.multi
		candidateDescriptor = executors.multi.Descriptor()
		candidateConfigHash = configHash
		generatedStableExecutor = executors.single
		generatedStableDescriptor = executors.single.Descriptor()
		generatedStableConfigHash = configHash
	} else if runtimeConfigName != "" {
		runtimeConfig, loadErr := loadRuntimeEvalConfig(runtimeConfigName)
		if loadErr != nil {
			return commandError(stderr, "load runtime evaluation config: %v", loadErr)
		}
		configHash, hashErr := hashRuntimeEvalConfig(runtimeConfig)
		if hashErr != nil {
			return commandError(stderr, "%v", hashErr)
		}
		liveExecutor, _, configureErr := newLiveRuntimeAgentTaskExecutor(runtimeConfig)
		if configureErr != nil {
			return commandError(stderr, "configure live runtime executor: %v", configureErr)
		}
		ledger, closeAuthorization, authorizationErr := openLiveAuthorization(
			agentTaskLiveAuthorizationBinding{
				Provider: runtimeConfig.Provider, Model: runtimeConfig.Model,
				DatasetVersion: *datasetVersion, DatasetSHA256: datasetHash,
				ExecutionConfigSHA256: configHash,
			},
			0,
		)
		if authorizationErr != nil {
			return commandError(stderr, "authorize live runtime evaluation: %v", authorizationErr)
		}
		defer closeAuthorization()
		authorizationEvidence := ledger.Evidence()
		liveAuthorizationEvidence = &authorizationEvidence
		authorizedClient, authorizationErr := newAuthorizedLiveModelClient(
			liveExecutor.modelClient, ledger, liveExecutor.costEstimator,
			runtimeConfig.Model, runtimeConfig.MaxOutputTokens, isLocalRuntimeEvalProvider(runtimeConfig.Provider),
		)
		if authorizationErr != nil {
			return commandError(stderr, "configure authorized live runtime client: %v", authorizationErr)
		}
		liveExecutor.modelClient = authorizedClient
		candidateExecutor = liveExecutor
		candidateDescriptor = liveExecutor.Descriptor()
		candidateConfigHash = configHash
	} else {
		candidateSet, loadErr := loadRecordedResults(*resultsPath)
		if loadErr != nil {
			return commandError(stderr, "load candidate results: %v", loadErr)
		}
		recordedExecutor, configureErr := eval.NewRecordedAgentTaskExecutor(candidateSet)
		if configureErr != nil {
			return commandError(stderr, "configure candidate executor: %v", configureErr)
		}
		candidateExecutor = recordedExecutor
		candidateDescriptor = candidateSet.Descriptor()
		candidateConfigHash = candidateSet.ExecutionConfigHash
	}
	var reviewCollector *agentTaskReviewCollector
	if reviewBundleName != "" {
		reviewCollector = newAgentTaskReviewCollector()
		candidateExecutor = reviewCollector.Wrap("candidate", candidateExecutor)
		generatedStableExecutor = reviewCollector.Wrap("stable", generatedStableExecutor)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *overallTimeout)
	defer cancel()
	environment := envOr("APP_ENV", "local")
	preflightDone := false
	candidate, err := runAgentTaskEvaluation(ctx, dataset, candidateExecutor, eval.AgentTaskRunnerConfig{
		DatasetVersion:      *datasetVersion,
		ExecutionConfigHash: candidateConfigHash,
		Environment:         environment,
		Seed:                0,
		CaseTimeout:         *caseTimeout,
		Execution:           candidateDescriptor,
	}, agentTaskEvaluationRunOptions{
		Side: "candidate", Live: runtimeConfigName != "" || liveStrategyComparison,
		CheckpointRoot: checkpointRoot, IntegrityKey: integrityKey,
		IntegrityKeyID: strings.TrimSpace(*integrityKeyID), PreflightTimeout: *preflightTimeout,
		Progress: *progress, PreflightDone: &preflightDone, Stderr: stderr,
	})
	if err != nil {
		return commandError(stderr, "run candidate evaluation: %v", err)
	}
	output := agentTaskEvaluationOutput{
		SchemaVersion: agentTaskEvaluationSchemaVersion, Candidate: candidate,
		LiveAuthorization: liveAuthorizationEvidence,
	}
	var comparisonStable *eval.AgentTaskReport

	if generatedStableExecutor != nil {
		stable, runErr := runAgentTaskEvaluation(ctx, dataset, generatedStableExecutor, eval.AgentTaskRunnerConfig{
			DatasetVersion:      *datasetVersion,
			ExecutionConfigHash: generatedStableConfigHash,
			Environment:         environment,
			Seed:                0,
			CaseTimeout:         *caseTimeout,
			Execution:           generatedStableDescriptor,
		}, agentTaskEvaluationRunOptions{
			Side: "stable", Live: true, CheckpointRoot: checkpointRoot,
			IntegrityKey: integrityKey, IntegrityKeyID: strings.TrimSpace(*integrityKeyID),
			PreflightTimeout: *preflightTimeout, Progress: *progress,
			PreflightDone: &preflightDone, Stderr: stderr,
		})
		if runErr != nil {
			return commandError(stderr, "run generated single-agent stable evaluation: %v", runErr)
		}
		output.Stable = &stable
		comparisonStable = &stable
	} else if strings.TrimSpace(*stableResultsPath) != "" {
		stableSet, loadErr := loadRecordedResults(*stableResultsPath)
		if loadErr != nil {
			return commandError(stderr, "load stable results: %v", loadErr)
		}
		stableExecutor, executorErr := eval.NewRecordedAgentTaskExecutor(stableSet)
		if executorErr != nil {
			return commandError(stderr, "configure stable executor: %v", executorErr)
		}
		stable, runErr := runAgentTaskEvaluation(ctx, dataset, stableExecutor, eval.AgentTaskRunnerConfig{
			DatasetVersion:      *datasetVersion,
			ExecutionConfigHash: stableSet.ExecutionConfigHash,
			Environment:         environment,
			Seed:                0,
			CaseTimeout:         *caseTimeout,
			Execution:           stableSet.Descriptor(),
		}, agentTaskEvaluationRunOptions{
			Side: "stable", PreflightTimeout: *preflightTimeout,
			Progress: *progress, Stderr: stderr,
		})
		if runErr != nil {
			return commandError(stderr, "run stable evaluation: %v", runErr)
		}
		output.Stable = &stable
		comparisonStable = &stable
	} else if signedStableReport != nil {
		output.Stable = signedStableReport
		comparisonStable = signedStableReport
	}
	if comparisonStable != nil {
		qualityMinCases := *minCases
		if liveStrategyComparison {
			qualityMinCases = *strategyMinCases
		}
		gate, gateErr := eval.EvaluateAgentQualityGate(*comparisonStable, candidate, eval.AgentQualityGatePolicy{
			MinCases:                        qualityMinCases,
			MinReadToolSelectionAccuracyBPS: *minReadAccuracyBPS,
			MaxTaskCompletionRegressionBPS:  *maxTaskRegressionBPS,
			MaxToolSelectionRegressionBPS:   *maxToolRegressionBPS,
			MaxSemanticPassRegressionBPS:    *maxSemanticRegressionBPS,
		})
		if gateErr != nil {
			return commandError(stderr, "evaluate quality gate: %v", gateErr)
		}
		output.Gate = &gate
		if strategyGateRequested {
			decision, strategyErr := eval.EvaluateAgentStrategyGate(*comparisonStable, candidate, eval.AgentStrategyGatePolicy{
				MinCases:                        *strategyMinCases,
				MinCandidateSemanticPassRateBPS: *strategyMinSemanticRateBPS,
				MinSemanticGainBPS:              *strategyMinSemanticGainBPS,
				MaxTaskCompletionRegressionBPS:  *strategyMaxTaskRegressionBPS,
				MaxToolSelectionRegressionBPS:   *strategyMaxToolRegressionBPS,
				MaxAverageCostRatioBPS:          *strategyMaxCostRatioBPS,
				MaxP95LatencyRatioBPS:           *strategyMaxP95RatioBPS,
				MaxCandidateP95MS:               *strategyMaxP95MS,
			})
			if strategyErr != nil {
				return commandError(stderr, "evaluate strategy gate: %v", strategyErr)
			}
			output.StrategyGate = &decision
		}
	}
	if len(integrityKey) > 0 {
		if err := signEvaluationOutput(&output, integrityKey, strings.TrimSpace(*integrityKeyID), time.Now()); err != nil {
			return commandError(stderr, "sign report: %v", err)
		}
	}

	if err := writeAgentTaskEvaluationOutput(stdout, strings.TrimSpace(*outPath), output, reviewBundleName != ""); err != nil {
		return commandError(stderr, "%v", err)
	}
	automaticGatesPassed := output.Gate != nil && output.Gate.Status == eval.AgentQualityGatePassed &&
		output.StrategyGate != nil && output.StrategyGate.Status == eval.AgentQualityGatePassed
	if reviewCollector != nil && candidate.Metrics.Errors == 0 &&
		(automaticGatesPassed || *captureFailedReviewBundle) {
		if err := verifyEvaluationOutput(output, integrityKey, strings.TrimSpace(*integrityKeyID)); err != nil {
			return commandError(stderr, "verify report before review packaging: %v", err)
		}
		payload, buildErr := reviewCollector.Build(output, time.Now().UTC())
		if buildErr != nil {
			return commandError(stderr, "build review bundle: %v", buildErr)
		}
		bundle, encryptErr := encryptAgentTaskReviewPayload(payload, reviewKey, strings.TrimSpace(*reviewKeyID), nil)
		if encryptErr != nil {
			return commandError(stderr, "encrypt review bundle: %v", encryptErr)
		}
		if err := writeAgentTaskReviewBundle(reviewBundleName, bundle); err != nil {
			return commandError(stderr, "%v", err)
		}
		if automaticGatesPassed {
			fmt.Fprintf(stdout, "Encrypted agent task review bundle written to %s (report_payload_sha256=%s)\n", reviewBundleName, payload.ReportPayloadSHA256)
		} else {
			fmt.Fprintf(stdout, "Encrypted diagnostic agent task review bundle written to %s (report_payload_sha256=%s, eligible_for_signoff=false)\n", reviewBundleName, payload.ReportPayloadSHA256)
		}
	}
	if reportArchive != nil {
		archiveCtx, archiveCancel := context.WithTimeout(context.Background(), minDuration(*overallTimeout, time.Minute))
		receipt, archiveErr := archiveEvaluationOutput(archiveCtx, reportArchive, output, integrityKey, strings.TrimSpace(*integrityKeyID), archiveConfig.RetentionDays, time.Now())
		archiveCancel()
		if archiveErr != nil {
			return commandError(stderr, "archive generated report: %v", archiveErr)
		}
		if err := writeAgentTaskArchiveReceipt(archiveReceiptName, receipt); err != nil {
			return commandError(stderr, "write receipt for %s/%s version %s: %v", receipt.Bucket, receipt.Key, receipt.VersionID, err)
		}
		fmt.Fprintf(stdout, "Agent task evaluation report archived to %s/%s (version_id=%s, receipt=%s)\n", receipt.Bucket, receipt.Key, receipt.VersionID, archiveReceiptName)
	}
	if candidate.Metrics.Errors > 0 {
		fmt.Fprintf(stderr, "agent-task-eval: candidate report contains %d executor errors\n", candidate.Metrics.Errors)
		return 2
	}
	if *enforceGate && (output.Gate == nil || output.Gate.Status != eval.AgentQualityGatePassed) {
		fmt.Fprintln(stderr, "agent-task-eval: candidate quality gate did not pass")
		return 2
	}
	if *enforceStrategyGate && (output.StrategyGate == nil || output.StrategyGate.Status != eval.AgentQualityGatePassed) {
		fmt.Fprintln(stderr, "agent-task-eval: candidate strategy gate did not pass")
		return 2
	}
	return 0
}

func loadAgentTaskDataset(path string) ([]eval.AgentTaskCase, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open dataset: %w", err)
	}
	defer file.Close()
	dataset, err := eval.LoadAgentTaskDataset(file)
	if err != nil {
		return nil, fmt.Errorf("load dataset: %w", err)
	}
	return dataset, nil
}

func loadRecordedResults(path string) (eval.RecordedAgentTaskResultSet, error) {
	file, err := os.Open(path)
	if err != nil {
		return eval.RecordedAgentTaskResultSet{}, err
	}
	defer file.Close()
	return eval.LoadRecordedAgentTaskResults(file)
}

func writeAgentTaskEvaluationOutput(stdout io.Writer, outPath string, output agentTaskEvaluationOutput, exclusive bool) error {
	writer := stdout
	var file *os.File
	if outPath != "" {
		if dir := filepath.Dir(outPath); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("create report directory: %w", err)
			}
		}
		flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		if exclusive {
			flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
		}
		var err error
		file, err = os.OpenFile(outPath, flags, 0o600)
		if err != nil {
			return fmt.Errorf("create report: %w", err)
		}
		defer file.Close()
		writer = file
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	if outPath != "" {
		fmt.Fprintf(stdout, "Agent task evaluation report written to %s\n", outPath)
	}
	return nil
}

func commandError(stderr io.Writer, format string, args ...interface{}) int {
	fmt.Fprintf(stderr, "agent-task-eval: "+format+"\n", args...)
	return 2
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}
