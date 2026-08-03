package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"twitter-clone/internal/module/agent/mcp/acceptance"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("agent-mcp-conformance", flag.ContinueOnError)
	flags.SetOutput(stderr)
	listenAddress := flags.String("listen", "127.0.0.1:9320", "loopback listen address")
	tokenEnv := flags.String("token-env", "AGENT_MCP_CONFORMANCE_TOKEN", "environment variable containing a 32+ byte bearer token")
	shutdownTimeout := flags.Duration("shutdown-timeout", 5*time.Second, "graceful shutdown timeout")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *shutdownTimeout <= 0 {
		return commandError(stderr, "--shutdown-timeout must be positive")
	}
	if err := validateLoopbackAddress(*listenAddress); err != nil {
		return commandError(stderr, "%v", err)
	}
	tokenEnvironment := strings.TrimSpace(*tokenEnv)
	if tokenEnvironment == "" {
		return commandError(stderr, "--token-env must not be empty")
	}
	handler, err := acceptance.NewConformanceHandler(os.Getenv(tokenEnvironment))
	if err != nil {
		return commandError(stderr, "configure conformance server authentication: %v", err)
	}
	listener, err := net.Listen("tcp", strings.TrimSpace(*listenAddress))
	if err != nil {
		return commandError(stderr, "listen: %v", err)
	}
	server := &http.Server{
		Handler: handler, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second,
	}
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- server.Serve(listener)
	}()
	fmt.Fprintf(stdout, "MCP conformance server listening on http://%s/mcp\n", listener.Addr().String())
	select {
	case err := <-serveErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return commandError(stderr, "serve: %v", err)
		}
		return 0
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), *shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return commandError(stderr, "shutdown: %v", err)
		}
		err := <-serveErrors
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return commandError(stderr, "serve during shutdown: %v", err)
		}
		return 0
	}
}

func validateLoopbackAddress(address string) error {
	host, _, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return fmt.Errorf("invalid conformance listen address: %w", err)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("MCP conformance server must bind to a loopback address, got %q", host)
	}
	return nil
}

func commandError(stderr io.Writer, format string, args ...any) int {
	fmt.Fprintf(stderr, "agent-mcp-conformance: "+format+"\n", args...)
	return 2
}
