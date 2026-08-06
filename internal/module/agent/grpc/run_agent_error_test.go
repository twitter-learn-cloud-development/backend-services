package grpc

import (
	"context"
	"testing"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"
	"twitter-clone/internal/module/agent/service"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRunAgentMapsPlannedCapabilityToFailedPrecondition(t *testing.T) {
	t.Parallel()

	catalog, err := service.NewAgentCapabilityCatalog(
		[]service.AgentCapabilityDefinition{{
			ID: "web.search", Version: "v1", Status: service.AgentCapabilityPlanned,
		}},
		nil,
	)
	if err != nil {
		t.Fatalf("NewAgentCapabilityCatalog() error = %v", err)
	}
	agentService := service.NewAgentService(
		"http://127.0.0.1:1/v1",
		"test",
		"default-model",
		"127.0.0.1:1",
		nil,
		nil,
		nil,
		service.WithAgentCapabilityCatalog(catalog),
	)
	defer agentService.Close()

	_, err = NewAgentServer(agentService).RunAgent(context.Background(), &aiAgentv1.RunAgentRequest{
		UserId: 42,
		MainContent: &aiAgentv1.MainContent{
			Content: "search the web",
		},
		PreferredCapabilityIds: []string{"web.search"},
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("RunAgent() code = %s, want %s; error = %v", status.Code(err), codes.FailedPrecondition, err)
	}
}
