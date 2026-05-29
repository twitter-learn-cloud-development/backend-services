package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

const gatewayURL = "http://localhost:9638/api/v1"

type RegisterResponse struct {
	User struct {
		ID       uint64 `json:"id"`
		Username string `json:"username"`
	} `json:"user"`
}

type LoginResponse struct {
	Token string `json:"token"`
}

type TweetResponse struct {
	Tweet struct {
		ID uint64 `json:"id"`
	} `json:"tweet"`
}

func main() {
	log.Println("🌱 Starting Database Seeding via API...")

	// 1. Register Users
	tokenA := registerAndLogin("user_a", "user_a@example.com", "password123")
	tokenB := registerAndLogin("user_b", "user_b@example.com", "password123")

	// 2. User A posts Tweets
	log.Println("📝 User A is posting tweets...")
	for i := 1; i <= 5; i++ {
		content := fmt.Sprintf("Hello World! This is tweet #%d from User A 🚀", i)
		createTweet(tokenA, content)
		time.Sleep(100 * time.Millisecond)
	}

	// 3. User B posts Tweets
	log.Println("📝 User B is posting tweets...")
	createTweet(tokenB, "I am User B, nice to meet you!")
	time.Sleep(100 * time.Millisecond)

	// 4. Seeding Chinese Cloud-Native & Go tech tweets
	log.Println("📝 Seeding Chinese Cloud-Native & Go tech tweets...")
	techTweets := []string{
		"云原生微服务正在重塑现代软件架构。通过容器化、服务网格与CI/CD的深度融合，企业能够实现敏捷迭代、弹性扩缩与高可用部署。拥抱Kubernetes与分布式追踪，不仅是技术升级，更是业务连续性的核心保障。🚀☁️ #CloudNative #Microservices",
		"以前单体应用改个bug像拆炸弹💣，现在上云原生微服务，拆成小模块各自干活，出问题秒级定位，扩容就像点外卖一样方便🍕☁️！K8s+服务网格搞定一切，开发终于能早点下班喝咖啡了。你的团队上云了吗？#程序员日常 #微服务",
		"云原生进入深水区！微服务不再只是“拆模块”，而是与Serverless、AIops、eBPF深度绑定。网络更透明、启动更轻量、运维更智能。技术债不等人，架构升级趁现在。你的团队演进到哪一步了？👇💡 #云原生 #技术趋势 #DevOps",
		"Go语言（Golang）在云原生时代简直是天生赢家！K8s、Docker、Prometheus 全是用 Go 写的。高并发、轻量级、超快编译速度，后端开发的首选！最近在写 Go 的微服务，gRPC 体验极佳。💻🔥 #Golang #后端开发",
		"今天分享一个 Go 语言切片的避坑指南：切片扩容时，如果容量小于 256，会翻倍扩容；大于 256 后则是按 1.25 倍加固定常数扩容。另外，注意底层数组共享导致的内存泄露，记得使用 copy 复制！#Golang #Go语言 #避坑指南",
		"如何设计一个高可用的微服务架构？核心在于：服务拆分边界清晰、强隔离（数据库隔离）、引入服务发现（如 Consul）、熔断降级（Sentinel/Hystrix）、消息队列异步解耦（RabbitMQ）。任何一个单点故障都不应拖垮全局。🛠️💡 #微服务架构 #分布式系统",
		"微服务数据隔离非常关键，每个微服务都应该拥有独立的数据库，禁止跨服务直接联表查询！所有跨服务的数据获取都必须走 gRPC 或 REST API 接口。这样才能实现真正独立演化。💾🛡️ #数据库隔离 #微服务治理",
		"Kubernetes 声明式 API 的魅力在于它的“期望状态与实际状态”协调环。不管是 Pod 挂了，还是流量激增，K8s 都会自动帮我们治愈并扩容。结合 Istio Service Mesh，还可以轻松搞定蓝绿发布和金丝雀切流。🎡☁️ #Kubernetes #Istio",
		"分享一下 eBPF 技术在云原生观测性中的应用。以前分布式链路追踪（OTel）必须在代码里埋点，现在通过 eBPF 可以零侵入捕获网络数据包，秒级生成服务拓扑图，真是黑科技！🎯📡 #eBPF #可观测性 #AIOps",
		"对于高并发下的 Feed流系统，写扩散（Push）适合粉丝量少的主播，读扩散（Pull）适合大V。我们现在使用的是混合 Feed 流模式：大V发布时写缓存，粉丝拉取时并发拉取大V的推文列表并在内存进行 Merge Sort。这极大地减轻了 Redis 的写压力！🚀💾 #系统设计 #高并发 #Redis",
	}

	for idx, t := range techTweets {
		// 轮流使用 tokenA 和 tokenB 进行发布
		if idx%2 == 0 {
			createTweet(tokenA, t)
		} else {
			createTweet(tokenB, t)
		}
		time.Sleep(100 * time.Millisecond)
	}

	log.Println("🌱 Database Seeding via API Completed successfully!")
}

func registerAndLogin(username, email, password string) string {
	// Register
	regBody := map[string]string{
		"username": username,
		"email":    email,
		"password": password,
	}
	sendRequest("POST", "/auth/register", regBody, "")
	log.Printf("✅ Registered user: %s", username)

	// Login
	loginBody := map[string]string{
		"email":    email,
		"password": password,
	}
	resp := sendRequest("POST", "/auth/login", loginBody, "")

	var loginResp LoginResponse
	json.Unmarshal(resp, &loginResp)
	log.Printf("🔑 Logged in %s, Token len: %d", username, len(loginResp.Token))
	return loginResp.Token
}

func createTweet(token, content string) {
	body := map[string]interface{}{
		"content": content,
	}
	resp := sendRequest("POST", "/tweets", body, token)
	var tweetResp TweetResponse
	json.Unmarshal(resp, &tweetResp)
	log.Printf("   -> Created Tweet ID: %d", tweetResp.Tweet.ID)
}

func sendRequest(method, endpoint string, body interface{}, token string) []byte {
	jsonBody, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, gatewayURL+endpoint, bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("❌ Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBytes, _ := io.ReadAll(resp.Body)
		// Ignore "user already exists" for idempotency
		if method == "POST" && endpoint == "/auth/register" {
			return nil
		}
		log.Printf("⚠️ API Error (%d): %s", resp.StatusCode, string(respBytes))
	}

	respBytes, _ := io.ReadAll(resp.Body)
	return respBytes
}
