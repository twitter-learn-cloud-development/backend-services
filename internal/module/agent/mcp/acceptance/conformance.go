package acceptance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"twitter-clone/internal/module/agent/mcp/remote"
	mcpSecurity "twitter-clone/internal/module/agent/mcp/security"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	ConformanceReadTool        = "read_echo"
	ConformanceWriteTool       = "idempotent_write"
	ConformanceWriteStatusTool = "write_status"
	ConformanceFaultTool       = "fault_probe"
	ConformanceKeyArgument     = "idempotency_key"
)

type conformanceWriteRecord struct {
	Value        string
	Receipt      string
	EffectCount  int64
	AttemptCount int64
}

type conformanceState struct {
	mu      sync.Mutex
	records map[string]*conformanceWriteRecord
}

func NewConformanceHandler(bearerToken string) (http.Handler, error) {
	authenticator, err := mcpSecurity.NewAuthenticator(strings.TrimSpace(bearerToken))
	if err != nil {
		return nil, err
	}
	state := &conformanceState{records: make(map[string]*conformanceWriteRecord)}
	mcpServer := server.NewMCPServer(
		"twitter-clone-mcp-conformance",
		"1.0.0",
		server.WithToolCapabilities(true),
		server.WithToolHandlerMiddleware(authenticator.ToolMiddleware()),
	)

	mcpServer.AddTool(mcp.NewTool(
		ConformanceReadTool,
		mcp.WithDescription("Returns deterministic structured read-only evidence."),
		mcp.WithString("query", mcp.Required()),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		query := strings.TrimSpace(request.GetString("query", ""))
		if query == "" {
			return mcp.NewToolResultError("query is required"), nil
		}
		return mcp.NewToolResultStructured(map[string]any{
			"echo": query,
			"kind": "read_only",
		}, "read probe completed"), nil
	})

	writeTool := mcp.NewTool(
		ConformanceWriteTool,
		mcp.WithDescription("Creates one in-memory effect per stable idempotency key."),
		mcp.WithString("value", mcp.Required()),
		mcp.WithString(ConformanceKeyArgument, mcp.Required()),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
	)
	writeTool.Meta = mcp.NewMetaFromMap(map[string]any{
		remote.IdempotencyKeyArgumentMetaField: ConformanceKeyArgument,
	})
	mcpServer.AddTool(writeTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		value := strings.TrimSpace(request.GetString("value", ""))
		key := strings.TrimSpace(request.GetString(ConformanceKeyArgument, ""))
		if value == "" || key == "" {
			return mcp.NewToolResultError("value and idempotency_key are required"), nil
		}
		record, conflict := state.applyWrite(key, value)
		if conflict {
			return mcp.NewToolResultError("idempotency key was reused with different input"), nil
		}
		return mcp.NewToolResultStructured(map[string]any{
			"receipt":      record.Receipt,
			"effect_count": record.EffectCount,
		}, "idempotent write completed"), nil
	})

	mcpServer.AddTool(mcp.NewTool(
		ConformanceWriteStatusTool,
		mcp.WithDescription("Returns observable state for one idempotency key."),
		mcp.WithString(ConformanceKeyArgument, mcp.Required()),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		key := strings.TrimSpace(request.GetString(ConformanceKeyArgument, ""))
		if key == "" {
			return mcp.NewToolResultError("idempotency_key is required"), nil
		}
		record, exists := state.readWrite(key)
		if !exists {
			return mcp.NewToolResultError("idempotency key was not observed"), nil
		}
		return mcp.NewToolResultStructured(map[string]any{
			"receipt":       record.Receipt,
			"effect_count":  record.EffectCount,
			"attempt_count": record.AttemptCount,
		}, "write status returned"), nil
	})

	mcpServer.AddTool(mcp.NewTool(
		ConformanceFaultTool,
		mcp.WithDescription("Produces bounded delay or an explicit MCP tool error for fault drills."),
		mcp.WithInteger("delay_ms"),
		mcp.WithBoolean("return_error"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		delayMillis := request.GetInt("delay_ms", 0)
		if delayMillis < 0 || delayMillis > 10_000 {
			return mcp.NewToolResultError("delay_ms must be between 0 and 10000"), nil
		}
		if delayMillis > 0 {
			timer := time.NewTimer(time.Duration(delayMillis) * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		if request.GetBool("return_error", false) {
			return mcp.NewToolResultError("requested conformance fault"), nil
		}
		return mcp.NewToolResultStructured(map[string]any{
			"delayed_ms": delayMillis,
		}, "fault probe completed"), nil
	})

	streamable := server.NewStreamableHTTPServer(mcpServer, server.WithStateLess(true))
	return authenticator.HTTPMiddleware(streamable), nil
}

func (state *conformanceState) applyWrite(key, value string) (conformanceWriteRecord, bool) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if existing := state.records[key]; existing != nil {
		existing.AttemptCount++
		return *existing, existing.Value != value
	}
	digest := sha256.Sum256([]byte("mcp-conformance-receipt:v1\x00" + key + "\x00" + value))
	record := &conformanceWriteRecord{
		Value: value, Receipt: "receipt_" + hex.EncodeToString(digest[:16]),
		EffectCount: 1, AttemptCount: 1,
	}
	state.records[key] = record
	return *record, false
}

func (state *conformanceState) readWrite(key string) (conformanceWriteRecord, bool) {
	state.mu.Lock()
	defer state.mu.Unlock()
	record := state.records[key]
	if record == nil {
		return conformanceWriteRecord{}, false
	}
	return *record, true
}
