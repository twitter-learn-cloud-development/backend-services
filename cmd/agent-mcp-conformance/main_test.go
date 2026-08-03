package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

const conformanceCommandToken = "0123456789abcdef0123456789abcdef"

func TestRunRejectsNonLoopbackAndShortToken(t *testing.T) {
	t.Setenv("TEST_MCP_CONFORMANCE_TOKEN", conformanceCommandToken)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(context.Background(), []string{
		"--listen", "0.0.0.0:9320", "--token-env", "TEST_MCP_CONFORMANCE_TOKEN",
	}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "loopback") {
		t.Fatalf("unexpected non-loopback result: code=%d stderr=%q", code, stderr.String())
	}

	t.Setenv("TEST_MCP_CONFORMANCE_TOKEN", "short")
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"--listen", "127.0.0.1:0", "--token-env", "TEST_MCP_CONFORMANCE_TOKEN",
	}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "at least 32") {
		t.Fatalf("unexpected short-token result: code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunStartsAndStopsWithContext(t *testing.T) {
	t.Setenv("TEST_MCP_CONFORMANCE_TOKEN", conformanceCommandToken)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(ctx, []string{
		"--listen", "127.0.0.1:0", "--token-env", "TEST_MCP_CONFORMANCE_TOKEN",
	}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "listening on") {
		t.Fatalf("unexpected server result: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
