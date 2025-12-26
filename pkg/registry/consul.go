package registry

import (
	"fmt"
	"log"

	"github.com/hashicorp/consul/api"
)

// ConsulRegistry defines a registry that uses Consul
type ConsulRegistry struct {
	client *api.Client
}

// NewConsulRegistry creates a new Consul registry client
func NewConsulRegistry(addr string) (*ConsulRegistry, error) {
	config := api.DefaultConfig()
	config.Address = addr
	client, err := api.NewClient(config)
	if err != nil {
		return nil, err
	}
	return &ConsulRegistry{client: client}, nil
}

// RegisterService registers a service with Consul
func (r *ConsulRegistry) RegisterService(serviceName, serviceID, serviceHost string, servicePort int, tags []string) error {
	registration := &api.AgentServiceRegistration{
		ID:      serviceID,
		Name:    serviceName,
		Port:    servicePort,
		Address: serviceHost,
		Tags:    tags,
		Check: &api.AgentServiceCheck{
			// GRPC 检查 (需要服务实现 GRPC Health Checking Protocol)
			// 为了简单起见，这里先用 TCP 检查
			TCP:                            fmt.Sprintf("%s:%d", serviceHost, servicePort),
			Interval:                       "10s",
			Timeout:                        "5s",
			DeregisterCriticalServiceAfter: "30s",
		},
	}

	if err := r.client.Agent().ServiceRegister(registration); err != nil {
		return fmt.Errorf("failed to register service: %w", err)
	}

	log.Printf("✅ Service registered: %s (ID: %s) at %s:%d", serviceName, serviceID, serviceHost, servicePort)
	return nil
}

// DeregisterService deregisters a service from Consul
func (r *ConsulRegistry) DeregisterService(serviceID string) error {
	if err := r.client.Agent().ServiceDeregister(serviceID); err != nil {
		return fmt.Errorf("failed to deregister service: %w", err)
	}
	log.Printf("✅ Service deregistered: %s", serviceID)
	return nil
}

// DiscoverService discovers a service from Consul (Simple Round Robin or Random)
func (r *ConsulRegistry) DiscoverService(serviceName string) (string, error) {
	services, _, err := r.client.Health().Service(serviceName, "", true, nil)
	if err != nil {
		return "", fmt.Errorf("failed to discover service %s: %w", serviceName, err)
	}

	if len(services) == 0 {
		return "", fmt.Errorf("service %s not found", serviceName)
	}

	// 这里可以做简单的负载均衡，比如随机或者轮询
	// 为了演示，直接取第一个
	service := services[0]
	return fmt.Sprintf("%s:%d", service.Service.Address, service.Service.Port), nil
}
