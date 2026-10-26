package main

import (
	"context"
	"testing"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"

	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func TestAgentGRPCHealthLifecycle(t *testing.T) {
	server := grpc.NewServer()
	healthServer := newAgentGRPCHealthServer(server)

	if _, exists := server.GetServiceInfo()["grpc.health.v1.Health"]; !exists {
		t.Fatal("gRPC health service was not registered")
	}
	for _, service := range []string{"", aiAgentv1.AiAgentService_ServiceDesc.ServiceName} {
		response, err := healthServer.Check(context.Background(), &healthpb.HealthCheckRequest{Service: service})
		if err != nil {
			t.Fatalf("Check(%q) returned error: %v", service, err)
		}
		if response.GetStatus() != healthpb.HealthCheckResponse_SERVING {
			t.Fatalf("Check(%q) status = %s, want SERVING", service, response.GetStatus())
		}
	}

	markAgentGRPCNotServing(healthServer)
	for _, service := range []string{"", aiAgentv1.AiAgentService_ServiceDesc.ServiceName} {
		response, err := healthServer.Check(context.Background(), &healthpb.HealthCheckRequest{Service: service})
		if err != nil {
			t.Fatalf("Check(%q) after shutdown returned error: %v", service, err)
		}
		if response.GetStatus() != healthpb.HealthCheckResponse_NOT_SERVING {
			t.Fatalf("Check(%q) after shutdown status = %s, want NOT_SERVING", service, response.GetStatus())
		}
	}
}
