package trace

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
)

const httpInstrumentationName = "twitter-clone/http"

// InstrumentHTTPClient clones base and wraps only its transport. Redirect,
// cookie, timeout and connection policies remain owned by the caller.
func InstrumentHTTPClient(base *http.Client, spanName string, tracer oteltrace.Tracer) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	clone := *base
	clone.Transport = InstrumentHTTPTransport(base.Transport, spanName, tracer)
	return &clone
}

// InstrumentHTTPTransport injects the configured W3C trace context and emits
// a client span without recording URL paths, query strings, headers or bodies.
func InstrumentHTTPTransport(base http.RoundTripper, spanName string, tracer oteltrace.Tracer) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if spanName == "" {
		spanName = "http.client"
	}
	if tracer == nil {
		tracer = otel.Tracer(httpInstrumentationName)
	}
	return &tracingRoundTripper{base: base, spanName: spanName, tracer: tracer}
}

// HTTPServerMiddleware extracts trace context and creates a server span while
// preserving the original ResponseWriter capabilities required by SSE.
func HTTPServerMiddleware(next http.Handler, spanName string, tracer oteltrace.Tracer) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	if spanName == "" {
		spanName = "http.server"
	}
	if tracer == nil {
		tracer = otel.Tracer(httpInstrumentationName)
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		ctx := otel.GetTextMapPropagator().Extract(
			request.Context(),
			propagation.HeaderCarrier(request.Header),
		)
		ctx, span := tracer.Start(ctx, spanName, oteltrace.WithSpanKind(oteltrace.SpanKindServer))
		span.SetAttributes(
			attribute.String("http.request.method", request.Method),
			attribute.String("url.scheme", requestScheme(request)),
		)
		defer span.End()
		next.ServeHTTP(writer, request.WithContext(ctx))
	})
}

type tracingRoundTripper struct {
	base     http.RoundTripper
	spanName string
	tracer   oteltrace.Tracer
}

func (transport *tracingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil {
		return nil, errors.New("nil HTTP request")
	}
	ctx, span := transport.tracer.Start(
		request.Context(),
		transport.spanName,
		oteltrace.WithSpanKind(oteltrace.SpanKindClient),
	)
	span.SetAttributes(safeClientAttributes(request)...)

	tracedRequest := request.Clone(ctx)
	tracedRequest.Header = request.Header.Clone()
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(tracedRequest.Header))

	response, err := transport.base.RoundTrip(tracedRequest)
	if err != nil {
		finishHTTPSpan(span, err)
		return nil, err
	}
	span.SetAttributes(attribute.Int("http.response.status_code", response.StatusCode))
	if response.StatusCode >= http.StatusBadRequest {
		span.SetStatus(codes.Error, "HTTP "+strconv.Itoa(response.StatusCode))
	}
	if response.Body == nil {
		span.End()
		return response, nil
	}
	response.Body = &spanReadCloser{ReadCloser: response.Body, span: span}
	return response, nil
}

type spanReadCloser struct {
	io.ReadCloser
	span oteltrace.Span
	once sync.Once
}

func (body *spanReadCloser) Read(buffer []byte) (int, error) {
	count, err := body.ReadCloser.Read(buffer)
	if err != nil {
		if errors.Is(err, io.EOF) {
			body.finish(nil)
		} else {
			body.finish(err)
		}
	}
	return count, err
}

func (body *spanReadCloser) Close() error {
	err := body.ReadCloser.Close()
	body.finish(err)
	return err
}

func (body *spanReadCloser) finish(err error) {
	body.once.Do(func() {
		finishHTTPSpan(body.span, err)
	})
}

func finishHTTPSpan(span oteltrace.Span, err error) {
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			span.SetAttributes(attribute.Bool("request.canceled", true))
		case errors.Is(err, context.DeadlineExceeded):
			span.SetAttributes(attribute.Bool("request.deadline_exceeded", true))
		default:
			// Raw transport errors may contain a full URL. Keep the span useful
			// without copying those potentially sensitive values into telemetry.
			span.SetStatus(codes.Error, "HTTP transport error")
		}
	}
	span.End()
}

func safeClientAttributes(request *http.Request) []attribute.KeyValue {
	attributes := []attribute.KeyValue{
		attribute.String("http.request.method", request.Method),
		attribute.String("url.scheme", requestScheme(request)),
	}
	if request.URL == nil {
		return attributes
	}
	if host := request.URL.Hostname(); host != "" {
		attributes = append(attributes, attribute.String("server.address", host))
	}
	if port := request.URL.Port(); port != "" {
		if parsedPort, err := strconv.Atoi(port); err == nil {
			attributes = append(attributes, attribute.Int("server.port", parsedPort))
		}
	}
	return attributes
}

func requestScheme(request *http.Request) string {
	if request != nil && request.URL != nil && request.URL.Scheme != "" {
		return request.URL.Scheme
	}
	if request != nil && request.TLS != nil {
		return "https"
	}
	return "http"
}
