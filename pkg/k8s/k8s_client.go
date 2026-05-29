package k8s

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
)

type K8sClient struct {
	dynamicClient dynamic.Interface
}

func NewK8sClient() (*K8sClient, error) {
	var config *rest.Config
	var err error

	// 1. 优先尝试 InCluster 认证 (部署在 K8s 内部时)
	config, err = rest.InClusterConfig()
	if err != nil {
		log.Println("ℹ️  Not running in-cluster, falling back to local kubeconfig...")
		// 2. 本地开发环境通过 kubeconfig 读取
		homeDir, exists := os.LookupEnv("USERPROFILE") // Windows home
		if !exists {
			homeDir = os.Getenv("HOME") // Linux/macOS
		}
		kubeconfigPath := filepath.Join(homeDir, ".kube", "config")
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load kubeconfig from %s: %w", kubeconfigPath, err)
		}
	}

	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	return &K8sClient{dynamicClient: client}, nil
}

// UpdateVirtualServiceTrafficWeight 动态重调虚拟服务灰度权重并防范冲突
func (k *K8sClient) UpdateVirtualServiceTrafficWeight(ctx context.Context, vsName string, v1Weight, v2Weight int) error {
	vsGVR := schema.GroupVersionResource{
		Group:    "networking.istio.io",
		Version:  "v1alpha3",
		Resource: "virtualservices",
	}

	// 🎯 核心防坑：RetryOnConflict 应对 etcd 并发修改的 OCC 乐观锁冲突 (409)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// 1. 获取最新的资源定义
		vs, err := k.dynamicClient.Resource(vsGVR).Namespace("default").Get(ctx, vsName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get VirtualService %s: %w", vsName, err)
		}

		// 2. 利用 unstructured 无类型定位修改权重
		spec, found, err := unstructured.NestedMap(vs.Object, "spec")
		if !found || err != nil {
			return fmt.Errorf("spec field not found in VirtualService %s", vsName)
		}

		httpRoutes, found, err := unstructured.NestedSlice(spec, "http")
		if !found || err != nil {
			return fmt.Errorf("spec.http routes not found in VirtualService %s", vsName)
		}

		modifiedAny := false
		for i, routeItem := range httpRoutes {
			routeMap, ok := routeItem.(map[string]interface{})
			if !ok {
				continue
			}

			destinations, found, err := unstructured.NestedSlice(routeMap, "route")
			if !found || err != nil {
				continue
			}

			// 如果该路由具有多个 destination（灰度分流），进行权重修改
			routeModified := false
			for j, destItem := range destinations {
				destMap, ok := destItem.(map[string]interface{})
				if !ok {
					continue
				}

				subset, found, _ := unstructured.NestedString(destMap, "destination", "subset")
				if !found {
					continue
				}

				if subset == "v1" {
					destMap["weight"] = int64(v1Weight)
					routeModified = true
				} else if subset == "v2" {
					destMap["weight"] = int64(v2Weight)
					routeModified = true
				}
				destinations[j] = destMap
			}

			if routeModified {
				routeMap["route"] = destinations
				httpRoutes[i] = routeMap
				modifiedAny = true
			}
		}

		if !modifiedAny {
			return fmt.Errorf("no split route found to modify in VirtualService %s", vsName)
		}

		if err := unstructured.SetNestedSlice(vs.Object, httpRoutes, "spec", "http"); err != nil {
			return fmt.Errorf("failed to set modified http routes: %w", err)
		}

		// 3. 执行更新 (如有版本冲突会自动重试)
		_, err = k.dynamicClient.Resource(vsGVR).Namespace("default").Update(ctx, vs, metav1.UpdateOptions{})
		if err != nil {
			return err
		}

		log.Printf("🛡️ [K8s Client] Successfully patched Istio VirtualService %s (v1: %d, v2: %d)", vsName, v1Weight, v2Weight)
		return nil
	})
}
