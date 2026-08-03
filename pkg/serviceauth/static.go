package serviceauth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	IdentityMetadataKey = "x-internal-service-identity"
	TokenMetadataKey    = "x-internal-service-token"
	MinimumTokenBytes   = 32
)

var (
	ErrIdentityRequired         = errors.New("service identity is required")
	ErrInvalidIdentity          = errors.New("service identity contains unsupported characters")
	ErrTokenRequired            = errors.New("service authentication token is required")
	ErrTokenTooShort            = fmt.Errorf("service authentication token must contain at least %d bytes", MinimumTokenBytes)
	ErrProtectedMethodsRequired = errors.New("at least one protected gRPC method is required")
)

type Outcome string

const (
	OutcomeAllowed      Outcome = "allowed"
	OutcomeMissing      Outcome = "missing"
	OutcomeInvalid      Outcome = "invalid"
	OutcomeUnconfigured Outcome = "unconfigured"
)

type Decision struct {
	Method  string
	Outcome Outcome
}

type Observer func(Decision)

type Option func(*StaticCredential)

func WithObserver(observer Observer) Option {
	return func(credential *StaticCredential) {
		credential.observer = observer
	}
}

// StaticCredential authenticates one internal service identity on an explicit
// set of unary RPC methods. Transport security remains the deployment layer's
// responsibility; this credential must be used over a trusted network or mTLS.
type StaticCredential struct {
	identity         string
	token            string
	protectedMethods map[string]struct{}
	observer         Observer
}

func NewStaticCredential(identity, token string, protectedMethods []string, options ...Option) (*StaticCredential, error) {
	if identity == "" {
		return nil, ErrIdentityRequired
	}
	if strings.TrimSpace(identity) != identity || !validIdentity(identity) {
		return nil, ErrInvalidIdentity
	}
	if token == "" {
		return nil, ErrTokenRequired
	}
	if strings.TrimSpace(token) != token || len(token) < MinimumTokenBytes {
		return nil, ErrTokenTooShort
	}
	methods, err := buildProtectedMethods(protectedMethods)
	if err != nil {
		return nil, err
	}

	credential := &StaticCredential{
		identity:         identity,
		token:            token,
		protectedMethods: methods,
	}
	for _, option := range options {
		if option != nil {
			option(credential)
		}
	}
	return credential, nil
}

// NewFailClosedUnaryServerInterceptor keeps public methods available while
// making protected methods unavailable until a credential is configured.
func NewFailClosedUnaryServerInterceptor(protectedMethods []string, observer Observer) (grpc.UnaryServerInterceptor, error) {
	methods, err := buildProtectedMethods(protectedMethods)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if info == nil {
			return handler(ctx, req)
		}
		if _, protected := methods[info.FullMethod]; !protected {
			return handler(ctx, req)
		}
		if observer != nil {
			observer(Decision{Method: info.FullMethod, Outcome: OutcomeUnconfigured})
		}
		return nil, status.Error(codes.Unavailable, "internal service authentication is not configured")
	}, nil
}

// NewFailClosedUnaryClientInterceptor prevents a privileged call from leaving
// the process when its service credential is not configured.
func NewFailClosedUnaryClientInterceptor(protectedMethods []string) (grpc.UnaryClientInterceptor, error) {
	methods, err := buildProtectedMethods(protectedMethods)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, method string, req, reply any, connection *grpc.ClientConn, invoker grpc.UnaryInvoker, options ...grpc.CallOption) error {
		if _, protected := methods[method]; protected {
			return status.Error(codes.FailedPrecondition, "internal service credentials are not configured")
		}
		return invoker(ctx, method, req, reply, connection, options...)
	}, nil
}

func (c *StaticCredential) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if c == nil || info == nil || !c.protects(info.FullMethod) {
			return handler(ctx, req)
		}

		identities := metadata.ValueFromIncomingContext(ctx, IdentityMetadataKey)
		tokens := metadata.ValueFromIncomingContext(ctx, TokenMetadataKey)
		if len(identities) == 0 || len(tokens) == 0 {
			c.observe(info.FullMethod, OutcomeMissing)
			return nil, status.Error(codes.Unauthenticated, "internal service credentials are required")
		}
		if len(identities) != 1 || len(tokens) != 1 ||
			subtle.ConstantTimeCompare([]byte(identities[0]), []byte(c.identity)) != 1 ||
			subtle.ConstantTimeCompare([]byte(tokens[0]), []byte(c.token)) != 1 {
			c.observe(info.FullMethod, OutcomeInvalid)
			return nil, status.Error(codes.Unauthenticated, "internal service credentials are invalid")
		}

		c.observe(info.FullMethod, OutcomeAllowed)
		return handler(ctx, req)
	}
}

func (c *StaticCredential) UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, connection *grpc.ClientConn, invoker grpc.UnaryInvoker, options ...grpc.CallOption) error {
		if c == nil || !c.protects(method) {
			return invoker(ctx, method, req, reply, connection, options...)
		}

		outgoing, _ := metadata.FromOutgoingContext(ctx)
		outgoing = outgoing.Copy()
		outgoing.Set(IdentityMetadataKey, c.identity)
		outgoing.Set(TokenMetadataKey, c.token)
		return invoker(metadata.NewOutgoingContext(ctx, outgoing), method, req, reply, connection, options...)
	}
}

func (c *StaticCredential) protects(method string) bool {
	if c == nil {
		return false
	}
	_, ok := c.protectedMethods[method]
	return ok
}

func (c *StaticCredential) observe(method string, outcome Outcome) {
	if c != nil && c.observer != nil {
		c.observer(Decision{Method: method, Outcome: outcome})
	}
}

func buildProtectedMethods(protectedMethods []string) (map[string]struct{}, error) {
	if len(protectedMethods) == 0 {
		return nil, ErrProtectedMethodsRequired
	}
	methods := make(map[string]struct{}, len(protectedMethods))
	for _, method := range protectedMethods {
		if method == "" || strings.TrimSpace(method) != method || !strings.HasPrefix(method, "/") {
			return nil, fmt.Errorf("invalid protected gRPC method %q", method)
		}
		methods[method] = struct{}{}
	}
	return methods, nil
}

func validIdentity(value string) bool {
	if len(value) > 128 {
		return false
	}
	for index, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' {
			continue
		}
		if index > 0 && (character == '.' || character == '_' || character == '-') {
			continue
		}
		return false
	}
	return true
}
