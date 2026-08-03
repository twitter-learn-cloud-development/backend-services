package trace

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func TestInstrumentHTTPClientPropagatesWithoutSensitiveTelemetry(t *testing.T) {
	provider, recorder, tracer := newHTTPTestTracer(t)
	_ = provider

	var receivedTraceparent string
	server := httptest.NewServer(HTTPServerMiddleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		receivedTraceparent = request.Header.Get("traceparent")
		_, _ = io.ReadAll(request.Body)
		writer.WriteHeader(http.StatusAccepted)
		_, _ = writer.Write([]byte("ok"))
	}), "agent.mcp.http", tracer))
	defer server.Close()

	client := InstrumentHTTPClient(nil, "agent.provider.http", tracer)
	ctx, parent := tracer.Start(context.Background(), "agent.step")
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		server.URL+"/private/completions?api_key=query-secret",
		strings.NewReader("prompt-secret"),
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer header-secret")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	_, _ = io.ReadAll(response.Body)
	_ = response.Body.Close()
	parent.End()

	if receivedTraceparent == "" {
		t.Fatal("traceparent header was not propagated")
	}
	spans := recorder.Ended()
	clientSpan := findHTTPSpan(t, spans, "agent.provider.http")
	serverSpan := findHTTPSpan(t, spans, "agent.mcp.http")
	if clientSpan.Parent().SpanID() != parent.SpanContext().SpanID() {
		t.Fatalf("client parent span = %s, want %s", clientSpan.Parent().SpanID(), parent.SpanContext().SpanID())
	}
	if serverSpan.Parent().SpanID() != clientSpan.SpanContext().SpanID() {
		t.Fatalf("server parent span = %s, want %s", serverSpan.Parent().SpanID(), clientSpan.SpanContext().SpanID())
	}

	assertNoSensitiveHTTPSpanData(t, spans, []string{
		"private/completions", "query-secret", "prompt-secret", "header-secret", "Authorization",
	})
}

func TestInstrumentHTTPClientPreservesRedirectPolicy(t *testing.T) {
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer redirectTarget.Close()
	redirectSource := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, redirectTarget.URL, http.StatusFound)
	}))
	defer redirectSource.Close()

	errRedirectBlocked := errors.New("redirect blocked by policy")
	base := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return errRedirectBlocked
	}}
	client := InstrumentHTTPClient(base, "agent.provider.http", nil)
	response, err := client.Get(redirectSource.URL)
	if response != nil {
		_ = response.Body.Close()
	}
	if !errors.Is(err, errRedirectBlocked) {
		t.Fatalf("Get() error = %v, want redirect policy error", err)
	}
	if base.Transport != nil {
		t.Fatalf("base client was mutated: transport = %T", base.Transport)
	}
}

func TestInstrumentHTTPTransportPropagatesCancellation(t *testing.T) {
	started := make(chan struct{})
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		close(started)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	client := InstrumentHTTPClient(&http.Client{Transport: transport}, "agent.provider.http", nil)
	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, requestErr := client.Do(request)
		done <- requestErr
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("transport did not start")
	}
	cancel()
	select {
	case requestErr := <-done:
		if !errors.Is(requestErr, context.Canceled) {
			t.Fatalf("Do() error = %v, want context.Canceled", requestErr)
		}
	case <-time.After(time.Second):
		t.Fatal("transport did not observe cancellation")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func newHTTPTestTracer(t *testing.T) (*sdktrace.TracerProvider, *tracetest.SpanRecorder, oteltrace.Tracer) {
	t.Helper()
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})
	return provider, recorder, provider.Tracer("http-test")
}

func findHTTPSpan(t *testing.T, spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, span := range spans {
		if span.Name() == name {
			return span
		}
	}
	t.Fatalf("span %q not found in %d spans", name, len(spans))
	return nil
}

func assertNoSensitiveHTTPSpanData(t *testing.T, spans []sdktrace.ReadOnlySpan, secrets []string) {
	t.Helper()
	for _, span := range spans {
		values := []string{span.Name(), span.Status().Description}
		for _, item := range span.Attributes() {
			values = append(values, string(item.Key), item.Value.Emit())
		}
		for _, event := range span.Events() {
			values = append(values, event.Name)
			for _, item := range event.Attributes {
				values = append(values, string(item.Key), item.Value.Emit())
			}
		}
		telemetry := strings.Join(values, " ")
		for _, secret := range secrets {
			if strings.Contains(telemetry, secret) {
				t.Fatalf("span %q contains sensitive value %q: %s", span.Name(), secret, telemetry)
			}
		}
	}
}
