package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"twitter-clone/internal/gateway/middleware"
	"twitter-clone/internal/module/supportquery"
)

const maxSupportQueryBodyBytes = 64 * 1024

var supportQueryOperationPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)

type SupportQueryHandler struct {
	registry       *supportquery.Registry
	idempotency    supportquery.IdempotencyGuard
	timeout        time.Duration
	idempotencyTTL time.Duration
}

func NewSupportQueryHandler(
	registry *supportquery.Registry,
	idempotency supportquery.IdempotencyGuard,
	timeout time.Duration,
	idempotencyTTL time.Duration,
) (*SupportQueryHandler, error) {
	if registry == nil || idempotency == nil {
		return nil, errors.New("support query registry and idempotency guard are required")
	}
	if timeout <= 0 || idempotencyTTL <= 0 {
		return nil, errors.New("support query timeout and idempotency TTL must be positive")
	}
	return &SupportQueryHandler{
		registry:       registry,
		idempotency:    idempotency,
		timeout:        timeout,
		idempotencyTTL: idempotencyTTL,
	}, nil
}

func (h *SupportQueryHandler) Execute(c *gin.Context) {
	startedAt := time.Now()
	actorUserID, ok := middleware.SupportQueryActorUserID(c)
	if !ok {
		writeSupportQueryProblem(c, supportquery.NewError(
			http.StatusUnauthorized,
			"invalid_service_identity",
			"Invalid service identity",
			"The authenticated service actor is missing.",
		))
		return
	}

	request, queryErr := decodeSupportQueryRequest(c)
	if queryErr != nil {
		writeSupportQueryProblem(c, queryErr)
		return
	}
	fingerprint, err := supportQueryFingerprint(request)
	if err != nil {
		writeSupportQueryProblem(c, invalidSupportQueryRequest())
		return
	}
	if err := h.idempotency.Check(
		c.Request.Context(),
		actorUserID,
		request.RequestContext.ToolCallID,
		fingerprint,
		h.idempotencyTTL,
	); err != nil {
		if errors.Is(err, supportquery.ErrToolCallConflict) {
			writeSupportQueryProblem(c, supportquery.NewError(
				http.StatusConflict,
				supportquery.CodeToolCallConflict,
				"Tool call conflict",
				"The tool call ID was already used for a different request.",
			))
		} else {
			writeSupportQueryProblem(c, supportquery.NewError(
				http.StatusServiceUnavailable,
				supportquery.CodeQueryTemporarilyUnavailable,
				"Query temporarily unavailable",
				"Support query idempotency verification is unavailable.",
			))
		}
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.timeout)
	defer cancel()
	result, err := h.registry.Execute(ctx, actorUserID, request)
	if err != nil {
		queryError := queryErrorFrom(err)
		recordSupportQuerySpan(c, request.Operation, queryError.Code, startedAt)
		writeSupportQueryProblem(c, queryError)
		return
	}

	response := gin.H{
		"request_id": uuid.NewString(),
		"operation":  request.Operation,
		"status":     "success",
		"data":       result.Data,
		"evidence":   result.Evidence,
		"meta": gin.H{
			"duration_ms":    time.Since(startedAt).Milliseconds(),
			"data_freshness": result.DataFreshness,
		},
	}
	recordSupportQuerySpan(c, request.Operation, "success", startedAt)
	c.JSON(http.StatusOK, response)
}

func decodeSupportQueryRequest(c *gin.Context) (supportquery.Request, *supportquery.Error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSupportQueryBodyBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var request supportquery.Request
	if err := decoder.Decode(&request); err != nil {
		return supportquery.Request{}, invalidSupportQueryRequest()
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return supportquery.Request{}, invalidSupportQueryRequest()
	}
	context := request.RequestContext
	if !supportQueryOperationPattern.MatchString(request.Operation) ||
		len(request.Operation) > 128 ||
		len(context.CaseID) == 0 || len(context.CaseID) > 128 ||
		len(context.TaskID) == 0 || len(context.TaskID) > 128 ||
		len(context.ToolCallID) == 0 || len(context.ToolCallID) > 128 {
		return supportquery.Request{}, invalidSupportQueryRequest()
	}
	var arguments map[string]any
	if err := json.Unmarshal(request.Arguments, &arguments); err != nil || arguments == nil {
		return supportquery.Request{}, invalidSupportQueryRequest()
	}
	return request, nil
}

func invalidSupportQueryRequest() *supportquery.Error {
	return supportquery.NewError(
		http.StatusBadRequest,
		supportquery.CodeInvalidArguments,
		"Invalid arguments",
		"The support query request does not match the expected schema.",
	)
}

func supportQueryFingerprint(request supportquery.Request) (string, error) {
	var arguments map[string]any
	if err := json.Unmarshal(request.Arguments, &arguments); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(struct {
		Operation string                      `json:"operation"`
		Arguments map[string]any              `json:"arguments"`
		Context   supportquery.RequestContext `json:"request_context"`
	}{request.Operation, arguments, request.RequestContext})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

func queryErrorFrom(err error) *supportquery.Error {
	var queryError *supportquery.Error
	if errors.As(err, &queryError) {
		return queryError
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return supportquery.NewError(
			http.StatusGatewayTimeout,
			supportquery.CodeQueryTimeout,
			"Query timeout",
			"The support query timed out.",
		)
	}
	return supportquery.NewError(
		http.StatusBadGateway,
		supportquery.CodeUpstreamInvalidResponse,
		"Upstream invalid response",
		"The business service returned an invalid response.",
	)
}

func writeSupportQueryProblem(c *gin.Context, queryError *supportquery.Error) {
	traceID := ""
	if spanContext := trace.SpanContextFromContext(c.Request.Context()); spanContext.IsValid() {
		traceID = spanContext.TraceID().String()
	}
	body, _ := json.Marshal(gin.H{
		"type":       "https://errors.example.com/support/" + strings.ReplaceAll(queryError.Code, "_", "-"),
		"title":      queryError.Title,
		"status":     queryError.Status,
		"code":       queryError.Code,
		"detail":     queryError.Detail,
		"request_id": uuid.NewString(),
		"trace_id":   traceID,
	})
	c.Data(queryError.Status, "application/problem+json", body)
}

func recordSupportQuerySpan(c *gin.Context, operation, result string, startedAt time.Time) {
	span := trace.SpanFromContext(c.Request.Context())
	span.SetAttributes(
		attribute.String("support.operation", operation),
		attribute.String("support.result", result),
		attribute.Int64("support.duration_ms", time.Since(startedAt).Milliseconds()),
	)
	if result != "success" {
		span.SetStatus(codes.Error, result)
	}
}

type RedisSupportQueryIdempotencyGuard struct {
	client *redis.Client
	prefix string
}

func NewRedisSupportQueryIdempotencyGuard(client *redis.Client) *RedisSupportQueryIdempotencyGuard {
	return &RedisSupportQueryIdempotencyGuard{client: client, prefix: "support-query:call:"}
}

func (g *RedisSupportQueryIdempotencyGuard) Check(
	ctx context.Context,
	actorUserID uint64,
	toolCallID string,
	fingerprint string,
	ttl time.Duration,
) error {
	if g == nil || g.client == nil {
		return errors.New("support query idempotency store is unavailable")
	}
	keyDigest := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", actorUserID, toolCallID)))
	key := g.prefix + hex.EncodeToString(keyDigest[:])
	created, err := g.client.SetNX(ctx, key, fingerprint, ttl).Result()
	if err != nil {
		return err
	}
	if created {
		return nil
	}
	existing, err := g.client.Get(ctx, key).Result()
	if err != nil {
		return err
	}
	if existing != fingerprint {
		return supportquery.ErrToolCallConflict
	}
	return nil
}
