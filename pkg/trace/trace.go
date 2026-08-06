package trace

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

// InitTracer preserves the process-lifetime setup used by existing services.
// New composition roots should use InitTracerProvider and flush on shutdown.
func InitTracer(serviceName string, collectorHost string) {
	if _, err := InitTracerProvider(context.Background(), serviceName, collectorHost, 1); err != nil {
		log.Printf("failed to initialize tracer: %v", err)
	}
}

// InitTracerProvider configures the global OTLP gRPC provider and returns its
// shutdown function. sampleRatio is clamped to [0,1] and remains parent-based.
func InitTracerProvider(
	ctx context.Context,
	serviceName string,
	collectorHost string,
	sampleRatio float64,
) (func(context.Context) error, error) {
	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return nil, fmt.Errorf("merge trace resource: %w", err)
	}

	setupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	exporter, err := otlptracegrpc.New(
		setupCtx,
		otlptracegrpc.WithEndpoint(collectorHost),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("create OTLP gRPC exporter: %w", err)
	}
	if sampleRatio < 0 {
		sampleRatio = 0
	}
	if sampleRatio > 1 {
		sampleRatio = 1
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampleRatio))),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	log.Printf("tracer initialized for service %s (OTLP gRPC collector %s, sample ratio %.2f)", serviceName, collectorHost, sampleRatio)
	return provider.Shutdown, nil
}
