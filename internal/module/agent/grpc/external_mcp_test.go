package grpc

import (
	"testing"
	"time"

	externalmcp "twitter-clone/internal/module/agent/mcp/remote"
)

func TestExternalMCPConnectionToProtoNormalizesLegacyHealthFields(t *testing.T) {
	checkedAt := time.Unix(1_721_000_000, 0).UTC()
	connection := externalMCPConnectionToProto(&externalmcp.Connection{
		ID: "mcpconn_1", HealthStatus: externalmcp.HealthStatusDegraded,
		HealthErrorCode: "timeout", HealthFailureCount: 2, LastHealthCheckedAt: checkedAt,
		CredentialSource: externalmcp.CredentialSourceManaged, ManagedCredentialRef: "team.research",
		ManagedCredentialVersion: 7,
	})
	if connection.HealthStatus != externalmcp.HealthStatusDegraded ||
		connection.HealthErrorCode != "timeout" || connection.HealthFailureCount != 2 ||
		connection.LastHealthCheckedAt != checkedAt.Unix() {
		t.Fatalf("mapped connection = %+v", connection)
	}
	if connection.CredentialSource != externalmcp.CredentialSourceManaged ||
		connection.ManagedCredentialRef != "team.research" || connection.ManagedCredentialVersion != 7 {
		t.Fatalf("mapped managed credential metadata = %+v", connection)
	}
	legacy := externalMCPConnectionToProto(&externalmcp.Connection{ID: "mcpconn_legacy"})
	if legacy.HealthStatus != externalmcp.HealthStatusUnknown {
		t.Fatalf("legacy health status = %q", legacy.HealthStatus)
	}
}
