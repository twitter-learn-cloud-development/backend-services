package service

import (
	"context"
	"strings"
	"time"
)

// AIOpsReport is the persistence boundary for generated RCA output. Storage
// adapters own retention and access control; AgentService never writes reports
// to developer-specific local paths.
type AIOpsReport struct {
	Report        string
	StructuredRCA string
	CreatedAt     time.Time
}

type AIOpsReportSink interface {
	AppendAIOpsReport(context.Context, AIOpsReport) error
}

func WithAIOpsReportSink(sink AIOpsReportSink) Option {
	return func(service *AgentService) {
		service.aiOpsReportSink = sink
	}
}

func normalizeAIOpsReport(report AIOpsReport) AIOpsReport {
	report.Report = strings.TrimSpace(report.Report)
	report.StructuredRCA = strings.TrimSpace(report.StructuredRCA)
	if report.StructuredRCA == "" {
		report.StructuredRCA = "{}"
	}
	if report.CreatedAt.IsZero() {
		report.CreatedAt = time.Now().UTC()
	}
	return report
}
