package trace

import (
	"log"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

// InitTracer 初始化全局 Tracer Provider
func InitTracer(serviceName string, collectorHost string) {
	// 1. 设置资源信息 (Resource)
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		log.Printf("⚠️ Failed to merge resource: %v", err)
	}

	// 2. 配置 Exporter (将数据发送给 Jaeger Collector - HTTP)
	// 使用 HTTP Collector Endpoint 替代 UDP Agent，适用于无 Sidecar 部署
	exporter, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(collectorHost)))
	if err != nil {
		log.Printf("⚠️ Failed to create jaeger exporter: %v", err)
		return
	}

	// 3. 创建 Provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter), // 异步批量上报
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()), // 采样策略：始终采样 (便于调试，生产环境请调整)
	)

	// 4. 注册为全局 Tracer
	otel.SetTracerProvider(tp)

	// 5. 设置传播器 (Context Propagators)
	// 确保 Trace ID 能在 HTTP/gRPC Header 中传递
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	log.Printf("✅ Tracer initialized for service: %s (Agent: %s)", serviceName, collectorHost)
}
