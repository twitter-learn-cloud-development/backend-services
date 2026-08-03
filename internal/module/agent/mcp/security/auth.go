package security

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	protocol "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type principalContextKey struct{}

type principal struct{}

type Authenticator struct {
	tokenHash [sha256.Size]byte
}

func NewAuthenticator(token string) (*Authenticator, error) {
	token = strings.TrimSpace(token)
	if len(token) < 32 {
		return nil, errors.New("MCP authentication token must contain at least 32 characters")
	}
	return &Authenticator{tokenHash: sha256.Sum256([]byte(token))}, nil
}

func (a *Authenticator) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a == nil || !a.validAuthorization(r.Header.Get("Authorization")) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), principalContextKey{}, principal{})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ToolMiddleware is the second authentication boundary. HTTP authentication
// must have attached a principal before any concrete MCP handler can run.
func (a *Authenticator) ToolMiddleware() server.ToolHandlerMiddleware {
	return func(next server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, request protocol.CallToolRequest) (*protocol.CallToolResult, error) {
			if !Authenticated(ctx) {
				return protocol.NewToolResultError("unauthorized MCP tool call"), nil
			}
			return next(ctx, request)
		}
	}
}

func Authenticated(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	_, ok := ctx.Value(principalContextKey{}).(principal)
	return ok
}

func (a *Authenticator) validAuthorization(value string) bool {
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return false
	}
	presented := sha256.Sum256([]byte(parts[1]))
	return subtle.ConstantTimeCompare(a.tokenHash[:], presented[:]) == 1
}
