package grpc

import (
	userv1 "twitter-clone/api/user/v1"

	"fmt"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func NewUserCLient() (userv1.UserServiceClient, error) {
	//user-service（通过 Consul 服务发现）
	consulAddrUserService := getEnv("CONSUL_HOST", "localhost") + ":" + getEnv("CONSUL_PORT", "8500")
	userTarget := fmt.Sprintf("consul://%s/user-service?healthy=true", consulAddrUserService)
	userConn, err := grpc.NewClient(userTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	)
	if err != nil {
		return nil, err
	}
	return userv1.NewUserServiceClient(userConn), nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
