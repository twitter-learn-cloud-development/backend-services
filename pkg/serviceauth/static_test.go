package serviceauth

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	testProtectedMethod = "/test.v1.InternalService/Protected"
	testPublicMethod    = "/test.v1.InternalService/Public"
	testIdentity        = "agent-service"
	testToken           = "0123456789abcdef0123456789abcdef"
)

func TestNewStaticCredentialRejectsUnsafeConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		identity string
		token    string
		methods  []string
		want     error
	}{
		{name: "missing identity", token: testToken, methods: []string{testProtectedMethod}, want: ErrIdentityRequired},
		{name: "invalid identity", identity: "agent service", token: testToken, methods: []string{testProtectedMethod}, want: ErrInvalidIdentity},
		{name: "missing token", identity: testIdentity, methods: []string{testProtectedMethod}, want: ErrTokenRequired},
		{name: "short token", identity: testIdentity, token: "short", methods: []string{testProtectedMethod}, want: ErrTokenTooShort},
		{name: "missing methods", identity: testIdentity, token: testToken, want: ErrProtectedMethodsRequired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewStaticCredential(test.identity, test.token, test.methods)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestUnaryServerInterceptorProtectsOnlyConfiguredMethods(t *testing.T) {
	var decisions []Decision
	credential, err := NewStaticCredential(testIdentity, testToken, []string{testProtectedMethod}, WithObserver(func(decision Decision) {
		decisions = append(decisions, decision)
	}))
	if err != nil {
		t.Fatalf("NewStaticCredential() error = %v", err)
	}
	interceptor := credential.UnaryServerInterceptor()
	handlerCalls := 0
	handler := func(context.Context, any) (any, error) {
		handlerCalls++
		return "ok", nil
	}

	response, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: testPublicMethod}, handler)
	if err != nil || response != "ok" || handlerCalls != 1 || len(decisions) != 0 {
		t.Fatalf("public call response=%v error=%v handler_calls=%d decisions=%v", response, err, handlerCalls, decisions)
	}

	tests := []struct {
		name    string
		ctx     context.Context
		want    codes.Code
		outcome Outcome
	}{
		{name: "missing", ctx: context.Background(), want: codes.Unauthenticated, outcome: OutcomeMissing},
		{name: "wrong identity", ctx: incomingCredentials("other-service", testToken), want: codes.Unauthenticated, outcome: OutcomeInvalid},
		{name: "wrong token", ctx: incomingCredentials(testIdentity, "fedcba9876543210fedcba9876543210"), want: codes.Unauthenticated, outcome: OutcomeInvalid},
		{name: "valid", ctx: incomingCredentials(testIdentity, testToken), want: codes.OK, outcome: OutcomeAllowed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			beforeCalls := handlerCalls
			beforeDecisions := len(decisions)
			_, callErr := interceptor(test.ctx, nil, &grpc.UnaryServerInfo{FullMethod: testProtectedMethod}, handler)
			if got := status.Code(callErr); got != test.want {
				t.Fatalf("status code = %s, want %s (error=%v)", got, test.want, callErr)
			}
			if len(decisions) != beforeDecisions+1 || decisions[len(decisions)-1] != (Decision{Method: testProtectedMethod, Outcome: test.outcome}) {
				t.Fatalf("decisions = %v", decisions)
			}
			wantCalls := beforeCalls
			if test.want == codes.OK {
				wantCalls++
			}
			if handlerCalls != wantCalls {
				t.Fatalf("handler calls = %d, want %d", handlerCalls, wantCalls)
			}
		})
	}
}

func TestUnaryServerInterceptorRejectsDuplicateCredentials(t *testing.T) {
	credential, err := NewStaticCredential(testIdentity, testToken, []string{testProtectedMethod})
	if err != nil {
		t.Fatalf("NewStaticCredential() error = %v", err)
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		IdentityMetadataKey, testIdentity,
		IdentityMetadataKey, testIdentity,
		TokenMetadataKey, testToken,
	))
	_, callErr := credential.UnaryServerInterceptor()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: testProtectedMethod}, func(context.Context, any) (any, error) {
		t.Fatal("handler called with duplicate credentials")
		return nil, nil
	})
	if got := status.Code(callErr); got != codes.Unauthenticated {
		t.Fatalf("status code = %s, want %s", got, codes.Unauthenticated)
	}
}

func TestUnaryClientInterceptorInjectsOnlyConfiguredMethods(t *testing.T) {
	credential, err := NewStaticCredential(testIdentity, testToken, []string{testProtectedMethod})
	if err != nil {
		t.Fatalf("NewStaticCredential() error = %v", err)
	}
	interceptor := credential.UnaryClientInterceptor()
	assertMetadata := func(wantCredentials bool) grpc.UnaryInvoker {
		return func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
			values, _ := metadata.FromOutgoingContext(ctx)
			if wantCredentials {
				if got := values.Get(IdentityMetadataKey); len(got) != 1 || got[0] != testIdentity {
					t.Fatalf("identity metadata = %v", got)
				}
				if got := values.Get(TokenMetadataKey); len(got) != 1 || got[0] != testToken {
					t.Fatalf("token metadata count/value mismatch")
				}
			} else if len(values.Get(IdentityMetadataKey)) != 0 || len(values.Get(TokenMetadataKey)) != 0 {
				t.Fatal("credentials injected into an unprotected method")
			}
			return nil
		}
	}

	if err := interceptor(context.Background(), testPublicMethod, nil, nil, nil, assertMetadata(false)); err != nil {
		t.Fatalf("public invocation error = %v", err)
	}
	existing := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(TokenMetadataKey, "stale", TokenMetadataKey, "duplicate"))
	if err := interceptor(existing, testProtectedMethod, nil, nil, nil, assertMetadata(true)); err != nil {
		t.Fatalf("protected invocation error = %v", err)
	}
}

func TestFailClosedInterceptorsDisableOnlyProtectedMethods(t *testing.T) {
	serverDecisions := make([]Decision, 0, 1)
	serverInterceptor, err := NewFailClosedUnaryServerInterceptor([]string{testProtectedMethod}, func(decision Decision) {
		serverDecisions = append(serverDecisions, decision)
	})
	if err != nil {
		t.Fatalf("NewFailClosedUnaryServerInterceptor() error = %v", err)
	}
	handlerCalls := 0
	handler := func(context.Context, any) (any, error) {
		handlerCalls++
		return nil, nil
	}
	if _, err := serverInterceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: testPublicMethod}, handler); err != nil {
		t.Fatalf("public server invocation error = %v", err)
	}
	if _, err := serverInterceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: testProtectedMethod}, handler); status.Code(err) != codes.Unavailable {
		t.Fatalf("protected server status = %s, want %s", status.Code(err), codes.Unavailable)
	}
	if handlerCalls != 1 || len(serverDecisions) != 1 || serverDecisions[0].Outcome != OutcomeUnconfigured {
		t.Fatalf("handler_calls=%d decisions=%v", handlerCalls, serverDecisions)
	}

	clientInterceptor, err := NewFailClosedUnaryClientInterceptor([]string{testProtectedMethod})
	if err != nil {
		t.Fatalf("NewFailClosedUnaryClientInterceptor() error = %v", err)
	}
	invokerCalls := 0
	invoker := func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
		invokerCalls++
		return nil
	}
	if err := clientInterceptor(context.Background(), testPublicMethod, nil, nil, nil, invoker); err != nil {
		t.Fatalf("public client invocation error = %v", err)
	}
	if err := clientInterceptor(context.Background(), testProtectedMethod, nil, nil, nil, invoker); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("protected client status = %s, want %s", status.Code(err), codes.FailedPrecondition)
	}
	if invokerCalls != 1 {
		t.Fatalf("invoker calls = %d, want 1", invokerCalls)
	}
}

func incomingCredentials(identity, token string) context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		IdentityMetadataKey, identity,
		TokenMetadataKey, token,
	))
}
