package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

const supportQueryActorContextKey = "support_query_actor_user_id"

var ErrServiceTokenReplay = errors.New("service token replayed")

type ServiceTokenReplayGuard interface {
	Consume(ctx context.Context, tokenID string, expiresAt time.Time) error
}

type serviceActorClaims struct {
	UserID string `json:"user_id"`
}

type supportQueryServiceClaims struct {
	Actor serviceActorClaims `json:"act"`
	jwt.RegisteredClaims
}

type SupportQueryAuth struct {
	secret     []byte
	issuer     string
	audience   string
	subject    string
	replay     ServiceTokenReplayGuard
	now        func() time.Time
	maxTokenTT time.Duration
}

func NewSupportQueryAuth(
	secret, issuer, audience, subject string,
	replay ServiceTokenReplayGuard,
	maxTokenTTL time.Duration,
) (*SupportQueryAuth, error) {
	if len([]byte(secret)) < 32 {
		return nil, errors.New("support query JWT secret must contain at least 32 bytes")
	}
	if strings.TrimSpace(issuer) == "" || strings.TrimSpace(audience) == "" || strings.TrimSpace(subject) == "" {
		return nil, errors.New("support query JWT issuer, audience, and subject are required")
	}
	if replay == nil {
		return nil, errors.New("support query JWT replay guard is required")
	}
	if maxTokenTTL <= 0 {
		return nil, errors.New("support query JWT max TTL must be positive")
	}
	return &SupportQueryAuth{
		secret:     []byte(secret),
		issuer:     issuer,
		audience:   audience,
		subject:    subject,
		replay:     replay,
		now:        time.Now,
		maxTokenTT: maxTokenTTL,
	}, nil
}

func (a *SupportQueryAuth) Authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenValue, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			writeSupportQueryAuthProblem(c, http.StatusUnauthorized, "invalid_service_identity", "Invalid service identity", "A valid service bearer token is required.")
			c.Abort()
			return
		}
		claims := &supportQueryServiceClaims{}
		token, err := jwt.ParseWithClaims(
			tokenValue,
			claims,
			func(token *jwt.Token) (any, error) {
				if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
					return nil, errors.New("unexpected service token signing method")
				}
				return a.secret, nil
			},
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
			jwt.WithIssuer(a.issuer),
			jwt.WithAudience(a.audience),
			jwt.WithExpirationRequired(),
			jwt.WithIssuedAt(),
			jwt.WithLeeway(2*time.Second),
		)
		if err != nil || token == nil || !token.Valid || claims.Subject != a.subject || claims.ID == "" || claims.ExpiresAt == nil || claims.IssuedAt == nil {
			writeSupportQueryAuthProblem(c, http.StatusUnauthorized, "invalid_service_identity", "Invalid service identity", "The service bearer token is invalid.")
			c.Abort()
			return
		}
		now := a.now()
		if claims.ExpiresAt.Time.Sub(claims.IssuedAt.Time) > a.maxTokenTT || claims.IssuedAt.Time.After(now.Add(2*time.Second)) {
			writeSupportQueryAuthProblem(c, http.StatusUnauthorized, "invalid_service_identity", "Invalid service identity", "The service bearer token lifetime is invalid.")
			c.Abort()
			return
		}
		actorUserID, err := parseActorUserID(claims.Actor.UserID)
		if err != nil {
			writeSupportQueryAuthProblem(c, http.StatusUnauthorized, "invalid_service_identity", "Invalid service identity", "The service bearer token actor is invalid.")
			c.Abort()
			return
		}
		if err := a.replay.Consume(c.Request.Context(), claims.ID, claims.ExpiresAt.Time); err != nil {
			if errors.Is(err, ErrServiceTokenReplay) {
				writeSupportQueryAuthProblem(c, http.StatusUnauthorized, "invalid_service_identity", "Invalid service identity", "The service bearer token has already been used.")
			} else {
				writeSupportQueryAuthProblem(c, http.StatusServiceUnavailable, "query_temporarily_unavailable", "Query temporarily unavailable", "Service identity verification is temporarily unavailable.")
			}
			c.Abort()
			return
		}
		c.Set(supportQueryActorContextKey, actorUserID)
		c.Next()
	}
}

func SupportQueryActorUserID(c *gin.Context) (uint64, bool) {
	value, exists := c.Get(supportQueryActorContextKey)
	actorUserID, ok := value.(uint64)
	return actorUserID, exists && ok && actorUserID > 0
}

func bearerToken(value string) (string, bool) {
	parts := strings.SplitN(value, " ", 2)
	returnValue := ""
	if len(parts) == 2 && parts[0] == "Bearer" {
		returnValue = strings.TrimSpace(parts[1])
	}
	return returnValue, returnValue != ""
}

func parseActorUserID(value string) (uint64, error) {
	actorUserID, err := strconv.ParseUint(value, 10, 64)
	if err != nil || actorUserID == 0 {
		return 0, errors.New("invalid actor user ID")
	}
	return actorUserID, nil
}

type RedisServiceTokenReplayGuard struct {
	client *redis.Client
	prefix string
}

func NewRedisServiceTokenReplayGuard(client *redis.Client) *RedisServiceTokenReplayGuard {
	return &RedisServiceTokenReplayGuard{client: client, prefix: "support-query:jwt:"}
}

func (g *RedisServiceTokenReplayGuard) Consume(ctx context.Context, tokenID string, expiresAt time.Time) error {
	if g == nil || g.client == nil {
		return errors.New("service token replay store is unavailable")
	}
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return ErrServiceTokenReplay
	}
	digest := sha256.Sum256([]byte(tokenID))
	created, err := g.client.SetNX(ctx, g.prefix+hex.EncodeToString(digest[:]), "1", ttl).Result()
	if err != nil {
		return err
	}
	if !created {
		return ErrServiceTokenReplay
	}
	return nil
}

type MemoryServiceTokenReplayGuard struct {
	mu      sync.Mutex
	records map[string]time.Time
	now     func() time.Time
}

func NewMemoryServiceTokenReplayGuard() *MemoryServiceTokenReplayGuard {
	return &MemoryServiceTokenReplayGuard{records: make(map[string]time.Time), now: time.Now}
}

func (g *MemoryServiceTokenReplayGuard) Consume(ctx context.Context, tokenID string, expiresAt time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := g.now()
	g.mu.Lock()
	defer g.mu.Unlock()
	for candidate, expiration := range g.records {
		if !expiration.After(now) {
			delete(g.records, candidate)
		}
	}
	if _, exists := g.records[tokenID]; exists {
		return ErrServiceTokenReplay
	}
	g.records[tokenID] = expiresAt
	return nil
}

func writeSupportQueryAuthProblem(c *gin.Context, status int, code, title, detail string) {
	traceID := ""
	if spanContext := trace.SpanContextFromContext(c.Request.Context()); spanContext.IsValid() {
		traceID = spanContext.TraceID().String()
	}
	body, _ := json.Marshal(gin.H{
		"type":       "https://errors.example.com/support/" + strings.ReplaceAll(code, "_", "-"),
		"title":      title,
		"status":     status,
		"code":       code,
		"detail":     detail,
		"request_id": uuid.NewString(),
		"trace_id":   traceID,
	})
	c.Data(status, "application/problem+json", body)
}
