package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"twitter-clone/internal/module/agent/mcp/acceptance"
	"twitter-clone/internal/module/agent/mcp/remote"
	agentModel "twitter-clone/internal/module/agent/model"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("agent-mcp-acceptance", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "strict MCP acceptance config JSON path")
	outPath := flags.String("out", "", "report output path; empty writes JSON to stdout")
	allowLive := flags.Bool("allow-live", false, "explicitly permit network calls to the configured MCP endpoint")
	allowWrite := flags.Bool("allow-write", false, "explicitly permit the configured idempotency write probe")
	expectRotation := flags.Bool("expect-credential-rotation", false, "wait for a projected bearer-token file to rotate and re-probe")
	requireComplete := flags.Bool("require-complete", false, "fail if any configured acceptance step is skipped")
	requireSigned := flags.Bool("require-signed", false, "require an HMAC-signed report")
	timeout := flags.Duration("timeout", 3*time.Minute, "overall acceptance timeout")
	callTimeout := flags.Duration("call-timeout", 15*time.Second, "MCP HTTP call timeout")
	rotationTimeout := flags.Duration("rotation-timeout", 2*time.Minute, "maximum wait for projected credential rotation")
	rotationPoll := flags.Duration("rotation-poll", time.Second, "projected credential rotation poll interval")
	integrityKeyEnv := flags.String("integrity-key-env", "AGENT_MCP_ACCEPTANCE_INTEGRITY_KEY", "environment variable containing the report HMAC key")
	integrityKeyID := flags.String("integrity-key-id", envOr("AGENT_MCP_ACCEPTANCE_INTEGRITY_KEY_ID", ""), "non-secret report HMAC key identifier")
	verifyReportPath := flags.String("verify-report", "", "verify an existing signed report and exit")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *timeout <= 0 || *callTimeout <= 0 || *rotationTimeout <= 0 || *rotationPoll <= 0 {
		return commandError(stderr, "--timeout, --call-timeout, --rotation-timeout and --rotation-poll must be positive")
	}

	integrityKey, keyErr := readIntegrityKey(*integrityKeyEnv, *integrityKeyID)
	if keyErr != nil {
		return commandError(stderr, "%v", keyErr)
	}
	if reportPath := strings.TrimSpace(*verifyReportPath); reportPath != "" {
		if strings.TrimSpace(*configPath) != "" || *allowLive || *allowWrite || *expectRotation {
			return commandError(stderr, "--verify-report cannot be combined with an acceptance run")
		}
		if len(integrityKey) == 0 {
			return commandError(stderr, "--verify-report requires a configured integrity key")
		}
		report, err := loadReport(reportPath)
		if err != nil {
			return commandError(stderr, "load report: %v", err)
		}
		if err := acceptance.VerifyReport(report, integrityKey, strings.TrimSpace(*integrityKeyID)); err != nil {
			return commandError(stderr, "verify report: %v", err)
		}
		fmt.Fprintf(stdout, "MCP acceptance report verified: %s (key_id=%s)\n", reportPath, report.Integrity.KeyID)
		return 0
	}

	if !*allowLive {
		return commandError(stderr, "network acceptance requires explicit --allow-live")
	}
	if strings.TrimSpace(*configPath) == "" {
		return commandError(stderr, "--config is required")
	}
	if *requireSigned && len(integrityKey) == 0 {
		return commandError(stderr, "--require-signed requires a configured integrity key")
	}
	config, err := loadConfig(*configPath)
	if err != nil {
		return commandError(stderr, "%v", err)
	}
	if *allowWrite && config.IdempotencyProbe == nil {
		return commandError(stderr, "--allow-write requires idempotency_probe in the config")
	}
	credentialSource, err := acceptance.NewCredentialSource(config.Auth)
	if err != nil {
		return commandError(stderr, "configure credential source: %v", err)
	}
	if *expectRotation && !credentialSource.Rotatable() {
		return commandError(stderr, "--expect-credential-rotation requires auth.bearer_token_file")
	}

	endpointPolicy := agentModel.NewEndpointPolicy(config.AllowedHosts...)
	if err := endpointPolicy.Validate(config.Endpoint, "external-mcp"); err != nil {
		return commandError(stderr, "validate endpoint policy: %v", err)
	}
	discoverer := remote.NewSDKDiscoverer(
		endpointPolicy,
		*callTimeout,
		remote.WithClientPool(remote.ClientPoolConfig{
			Enabled: true, MaxSessions: 4, MaxSessionsPerConnection: 1,
			IdleTimeout: 30 * time.Second, AcquireTimeout: minDuration(*callTimeout, 3*time.Second),
		}),
	)
	defer discoverer.Close()
	runner, err := acceptance.NewRunner(discoverer, credentialSource)
	if err != nil {
		return commandError(stderr, "configure runner: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report, runErr := runner.Run(ctx, config, acceptance.RunOptions{
		Environment: envOr("APP_ENV", "local"), AllowWrite: *allowWrite,
		ExpectCredentialRotation: *expectRotation,
		RotationTimeout:          *rotationTimeout, RotationPollInterval: *rotationPoll,
	})
	if len(integrityKey) > 0 {
		if err := acceptance.SignReport(&report, integrityKey, strings.TrimSpace(*integrityKeyID), time.Now()); err != nil {
			return commandError(stderr, "sign report: %v", err)
		}
	}
	if err := writeReport(stdout, strings.TrimSpace(*outPath), report); err != nil {
		return commandError(stderr, "%v", err)
	}
	if runErr != nil || report.Status == acceptance.StatusFailed {
		fmt.Fprintln(stderr, "agent-mcp-acceptance: one or more acceptance checks failed; inspect the redacted report")
		return 1
	}
	if *requireComplete && report.Status != acceptance.StatusPassed {
		fmt.Fprintln(stderr, "agent-mcp-acceptance: acceptance report is partial")
		return 1
	}
	return 0
}

func loadConfig(path string) (acceptance.Config, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return acceptance.Config{}, fmt.Errorf("open config: %w", err)
	}
	defer file.Close()
	return acceptance.DecodeConfig(file)
}

func loadReport(path string) (acceptance.Report, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return acceptance.Report{}, err
	}
	defer file.Close()
	return acceptance.DecodeReport(file)
}

func writeReport(stdout io.Writer, path string, report acceptance.Report) error {
	payload, err := acceptance.MarshalReport(report)
	if err != nil {
		return err
	}
	if path == "" {
		_, err = stdout.Write(payload)
		return err
	}
	directory := filepath.Dir(path)
	if directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("create report directory: %w", err)
		}
	}
	temp, err := os.CreateTemp(directory, ".mcp-acceptance-*.tmp")
	if err != nil {
		return fmt.Errorf("create report temp file: %w", err)
	}
	tempName := temp.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temp.Close()
		}
		_ = os.Remove(tempName)
	}()
	if err := temp.Chmod(0o600); err != nil {
		return fmt.Errorf("secure report temp file: %w", err)
	}
	if _, err := temp.Write(payload); err != nil {
		return fmt.Errorf("write report temp file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync report temp file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close report temp file: %w", err)
	}
	closed = true
	if err := os.Link(tempName, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New("report output already exists")
		}
		return fmt.Errorf("commit report: %w", err)
	}
	fmt.Fprintf(stdout, "MCP acceptance report written to %s\n", path)
	return nil
}

func readIntegrityKey(environmentName, keyID string) ([]byte, error) {
	environmentName = strings.TrimSpace(environmentName)
	if environmentName == "" {
		return nil, errors.New("--integrity-key-env must not be empty")
	}
	value := os.Getenv(environmentName)
	if value == "" {
		return nil, nil
	}
	if len([]byte(value)) < 32 {
		return nil, fmt.Errorf("%s must contain at least 32 bytes", environmentName)
	}
	if strings.TrimSpace(keyID) == "" {
		return nil, errors.New("integrity key ID is required when an integrity key is configured")
	}
	return []byte(value), nil
}

func commandError(stderr io.Writer, format string, args ...any) int {
	fmt.Fprintf(stderr, "agent-mcp-acceptance: "+format+"\n", args...)
	return 2
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}
