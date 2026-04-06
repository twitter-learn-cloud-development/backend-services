package logger

import (
	"context"
	"log"
	"os"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Log *zap.Logger

// InitLogger 初始化日志
func InitLogger() {
	// 配置 Zap
	config := zap.NewProductionEncoderConfig()
	config.EncodeTime = zapcore.ISO8601TimeEncoder
	config.EncodeLevel = zapcore.CapitalLevelEncoder

	// 使用 JSON 格式 (便于 Loki 解析)
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(config),
		zapcore.Lock(os.Stdout),
		zapcore.InfoLevel,
	)

	// 开启 Caller 显示调用行号
	Log = zap.New(core, zap.AddCaller())
}

// Info 带有 Context 的 Info 日志 (自动提取 TraceID)
func Info(ctx context.Context, msg string, fields ...zap.Field) {
	if Log == nil {
		log.Printf("[INFO] %s\n", msg)
		return
	}
	traceID := trace.SpanFromContext(ctx).SpanContext().TraceID().String()
	if traceID != "" && traceID != "00000000000000000000000000000000" {
		fields = append(fields, zap.String("trace_id", traceID))
	}
	Log.Info(msg, fields...)
}

// Error 带有 Context 的 Error 日志
func Error(ctx context.Context, msg string, fields ...zap.Field) {
	if Log == nil {
		log.Printf("[ERROR] %s\n", msg)
		return
	}
	traceID := trace.SpanFromContext(ctx).SpanContext().TraceID().String()
	if traceID != "" && traceID != "00000000000000000000000000000000" {
		fields = append(fields, zap.String("trace_id", traceID))
	}
	Log.Error(msg, fields...)
}

// Warn 带有 Context 的 Warn 日志
func Warn(ctx context.Context, msg string, fields ...zap.Field) {
	traceID := trace.SpanFromContext(ctx).SpanContext().TraceID().String()
	if traceID != "" && traceID != "00000000000000000000000000000000" {
		fields = append(fields, zap.String("trace_id", traceID))
	}
	Log.Warn(msg, fields...)
}

// Fatal 带有 Context 的 Fatal 日志
func Fatal(ctx context.Context, msg string, fields ...zap.Field) {
	traceID := trace.SpanFromContext(ctx).SpanContext().TraceID().String()
	if traceID != "" && traceID != "00000000000000000000000000000000" {
		fields = append(fields, zap.String("trace_id", traceID))
	}
	Log.Fatal(msg, fields...)
}
