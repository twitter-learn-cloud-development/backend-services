package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/joho/godotenv"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"
	tweetv1 "twitter-clone/api/tweet/v1"
	userv1 "twitter-clone/api/user/v1"
	"twitter-clone/internal/infrastructure/cache"
	mongoInfra "twitter-clone/internal/infrastructure/mongo"
	"twitter-clone/internal/infrastructure/mq"
	"twitter-clone/internal/infrastructure/persistence"
	agentConsumer "twitter-clone/internal/module/agent/consumer"
	agentCredential "twitter-clone/internal/module/agent/credential"
	agentGrpc "twitter-clone/internal/module/agent/grpc"
	agentMarketplace "twitter-clone/internal/module/agent/marketplace"
	agentMcp "twitter-clone/internal/module/agent/mcp"
	externalmcp "twitter-clone/internal/module/agent/mcp/remote"
	agentModel "twitter-clone/internal/module/agent/model"
	agentObjectStore "twitter-clone/internal/module/agent/objectstore"
	agentObservability "twitter-clone/internal/module/agent/observability"
	agentProfile "twitter-clone/internal/module/agent/profile"
	agentProject "twitter-clone/internal/module/agent/project"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	agentService "twitter-clone/internal/module/agent/service"
	agentStartup "twitter-clone/internal/module/agent/startup"
	agentStrategy "twitter-clone/internal/module/agent/strategy"
	agentWebSearch "twitter-clone/internal/module/agent/websearch"
	"twitter-clone/internal/module/agent/workflow/rag"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"
	"twitter-clone/pkg/ai"
	"twitter-clone/pkg/es"
	"twitter-clone/pkg/logger"
	"twitter-clone/pkg/metric"
	"twitter-clone/pkg/profiler"
	"twitter-clone/pkg/qdrant"
	"twitter-clone/pkg/registry"
	"twitter-clone/pkg/serviceauth"
	appTrace "twitter-clone/pkg/trace"

	_ "github.com/mbobakov/grpc-consul-resolver"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	// 启动 Profiler 持续性能监控
	profiler.Init("agent-service")

	log.Println("========================================")
	log.Println("🤖 Agent Service (gRPC + MCP + MongoDB)")
	log.Println("========================================")

	// 0. 初始化 Logger
	logger.InitLogger()
	defer logger.Log.Sync()

	// 加载 .env
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  No .env file found, using environment config")
	}
	processPlan, err := agentStartup.Parse(
		getEnv(agentStartup.ProcessRoleEnv, string(agentStartup.ProcessRoleAll)),
		getEnv(agentStartup.TrendingReporterOwnerEnv, string(agentStartup.TrendingReporterOwnerTemporal)),
	)
	if err != nil {
		log.Fatalf("invalid Agent Service startup plan: %v", err)
	}
	log.Printf(
		"Agent Service startup plan: role=%s api=%t worker=%t trending_reporter_owner=%s",
		processPlan.Role(),
		processPlan.StartsAPI(),
		processPlan.StartsWorkers(),
		processPlan.TrendingReporterOwner(),
	)

	// 1. 初始化 MongoDB
	traceShutdown, traceErr := appTrace.InitTracerProvider(
		context.Background(),
		"agent-service",
		getEnv("JAEGER_COLLECTOR_ENDPOINT", "localhost:4317"),
		getEnvFloat64("AGENT_TRACE_SAMPLE_RATIO", 1),
	)
	if traceErr != nil {
		log.Printf("failed to initialize Agent OTel provider: %v", traceErr)
	} else {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := traceShutdown(shutdownCtx); err != nil {
				log.Printf("failed to flush Agent traces: %v", err)
			}
		}()
	}

	mongoInfra.InitMongoDB()
	defer mongoInfra.Close()
	log.Println("✅ MongoDB connected")

	// 创建 AgentRepository 并初始化索引
	repo := repository.NewMongoAgentRepository(mongoInfra.GetDB())
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		log.Printf("⚠️ Failed to ensure MongoDB indexes: %v", err)
	} else {
		log.Println("✅ MongoDB indexes ensured")
	}
	traceRepo := repository.NewMongoExecutionTraceRepository(mongoInfra.GetDB())
	if err := traceRepo.EnsureIndexes(context.Background()); err != nil {
		log.Printf("⚠️ Failed to ensure Agent trace indexes: %v", err)
	} else {
		log.Println("✅ Agent trace indexes ensured")
	}
	recoverableAgentRuns := getEnvBool("AGENT_RECOVERABLE_RUNS_ENABLED", false)
	agentTaskTemplatesEnabled := getEnvBool("AGENT_TASK_TEMPLATES_ENABLED", false)
	if agentTaskTemplatesEnabled && !recoverableAgentRuns {
		log.Fatal("AGENT_TASK_TEMPLATES_ENABLED requires AGENT_RECOVERABLE_RUNS_ENABLED")
	}
	externalMCPEnabled := getEnvBool("AGENT_EXTERNAL_MCP_ENABLED", false)
	workflowAsToolEnabled := getEnvBool("AGENT_WORKFLOW_AS_TOOL_ENABLED", false)
	skillCatalogEnabled := getEnvBool("AGENT_SKILL_CATALOG_ENABLED", false)
	extensionCatalogEnabled := getEnvBool("AGENT_EXTENSION_CATALOG_ENABLED", false)
	extensionMarketplaceEnabled := getEnvBool("AGENT_EXTENSION_MARKETPLACE_ENABLED", false)
	extensionMarketplaceAdminEnabled := getEnvBool(agentMarketplace.AdministrationEnabledEnv, false)
	extensionMarketplaceAdminToken := strings.TrimSpace(getEnv(agentMarketplace.AdministrationTokenEnv, ""))
	extensionMarketplaceAdministratorIDs, marketplaceAdminErr := parseProfileUserIDs(
		getEnv(agentMarketplace.AdministratorUserIDsEnv, ""), "extension marketplace administrator",
	)
	if marketplaceAdminErr != nil {
		log.Fatalf("invalid %s: %v", agentMarketplace.AdministratorUserIDsEnv, marketplaceAdminErr)
	}
	if extensionMarketplaceAdminEnabled {
		if len(extensionMarketplaceAdminToken) < 32 {
			log.Fatalf("%s must contain at least 32 characters when administration is enabled", agentMarketplace.AdministrationTokenEnv)
		}
		if len(extensionMarketplaceAdministratorIDs) == 0 {
			log.Fatalf("%s requires at least one %s", agentMarketplace.AdministrationEnabledEnv, agentMarketplace.AdministratorUserIDsEnv)
		}
	} else if extensionMarketplaceAdminToken != "" || len(extensionMarketplaceAdministratorIDs) > 0 {
		log.Fatalf("%s and %s require %s=true", agentMarketplace.AdministrationTokenEnv, agentMarketplace.AdministratorUserIDsEnv, agentMarketplace.AdministrationEnabledEnv)
	}
	if skillCatalogEnabled && !workflowAsToolEnabled {
		log.Fatal("AGENT_SKILL_CATALOG_ENABLED requires AGENT_WORKFLOW_AS_TOOL_ENABLED")
	}
	workflowToolPublicationRepo := repository.NewMongoWorkflowToolPublicationRepository(mongoInfra.GetDB())
	if err := workflowToolPublicationRepo.EnsureIndexes(context.Background()); err != nil {
		if workflowAsToolEnabled {
			log.Fatalf("failed to ensure workflow tool publication indexes: %v", err)
		}
		log.Printf("workflow tool publication indexes unavailable while feature is disabled: %v", err)
	}
	extensionMarketplaceRepo := repository.NewMongoExtensionMarketplaceRepository(mongoInfra.GetDB())
	if extensionMarketplaceEnabled || extensionMarketplaceAdminEnabled {
		if err := extensionMarketplaceRepo.EnsureIndexes(context.Background()); err != nil {
			log.Fatalf("failed to ensure extension marketplace indexes: %v", err)
		}
	}
	var extensionMarketplaceManager *agentService.ExtensionMarketplaceManager
	if extensionMarketplaceAdminEnabled {
		extensionMarketplaceManager = agentService.NewExtensionMarketplaceManager(
			extensionMarketplaceRepo, true, extensionMarketplaceAdministratorIDs,
		)
	}
	externalMCPProjectScopeEnabled := getEnvBool("AGENT_EXTERNAL_MCP_PROJECT_SCOPE_ENABLED", false)
	externalMCPManagedCredentialsEnabled := getEnvBool("AGENT_EXTERNAL_MCP_MANAGED_CREDENTIALS_ENABLED", false)
	if externalMCPProjectScopeEnabled && !externalMCPEnabled {
		log.Fatal("AGENT_EXTERNAL_MCP_PROJECT_SCOPE_ENABLED requires AGENT_EXTERNAL_MCP_ENABLED")
	}
	if externalMCPManagedCredentialsEnabled && (!externalMCPEnabled || !externalMCPProjectScopeEnabled) {
		log.Fatal("AGENT_EXTERNAL_MCP_MANAGED_CREDENTIALS_ENABLED requires external MCP and project scope")
	}
	externalMCPEndpointPolicy := agentModel.NewEndpointPolicy(
		strings.Split(getEnv("AGENT_EXTERNAL_MCP_ALLOWED_HOSTS", ""), ",")...,
	)
	var externalMCPManagedCredentials externalmcp.ManagedCredentialResolver
	if externalMCPManagedCredentialsEnabled {
		resolver, resolverErr := externalmcp.NewFileManagedCredentialResolver(
			getEnv("AGENT_EXTERNAL_MCP_MANAGED_CREDENTIALS_JSON", ""),
			getEnv("AGENT_EXTERNAL_MCP_MANAGED_CREDENTIAL_SECRET_DIR", "/var/run/secrets/agent-mcp-managed"),
			externalMCPEndpointPolicy,
		)
		if resolverErr != nil {
			log.Fatalf("failed to configure managed external MCP credentials: %v", resolverErr)
		}
		externalMCPManagedCredentials = resolver
	}
	unifiedApprovalRecovery := getEnvBool("AGENT_UNIFIED_APPROVAL_RECOVERY_ENABLED", false)
	if unifiedApprovalRecovery && (!recoverableAgentRuns || !externalMCPEnabled) {
		log.Fatal("AGENT_UNIFIED_APPROVAL_RECOVERY_ENABLED requires AGENT_RECOVERABLE_RUNS_ENABLED and AGENT_EXTERNAL_MCP_ENABLED")
	}
	multiAgentPlannerEnabled := getEnvBool("AGENT_MULTI_AGENT_PLANNER_ENABLED", false)
	multiAgentExecutionEnabled := getEnvBool("AGENT_MULTI_AGENT_EXECUTION_ENABLED", false)
	if multiAgentExecutionEnabled && (!multiAgentPlannerEnabled || !recoverableAgentRuns) {
		log.Fatal("AGENT_MULTI_AGENT_EXECUTION_ENABLED requires AGENT_MULTI_AGENT_PLANNER_ENABLED and AGENT_RECOVERABLE_RUNS_ENABLED")
	}
	executionStrategyPlanner, err := agentService.NewBuiltInAgentExecutionStrategyPlanner(agentStrategy.Policy{
		Enabled:                multiAgentPlannerEnabled,
		ExecutorAvailable:      multiAgentExecutionEnabled,
		MinimumComplexityScore: getEnvPositiveInt("AGENT_MULTI_AGENT_MIN_COMPLEXITY_SCORE", 6),
		MaxRoles:               getEnvPositiveInt("AGENT_MULTI_AGENT_MAX_ROLES", 3),
		MaxParallelRoles:       getEnvPositiveInt("AGENT_MULTI_AGENT_MAX_PARALLEL_ROLES", 1),
		MaxEstimatedLatency:    getEnvDuration("AGENT_MULTI_AGENT_MAX_ESTIMATED_LATENCY", 50*time.Second),
	})
	if err != nil {
		log.Fatalf("failed to configure Agent execution strategy planner: %v", err)
	}
	if multiAgentExecutionEnabled {
		log.Println("Agent bounded Multi-Agent aggregate execution is enabled for admitted research-draft templates")
	} else if multiAgentPlannerEnabled {
		log.Println("Agent Multi-Agent Planner shadow admission is enabled; execution remains single-agent")
	}
	var agentCheckpointCipher agentCredential.SecretCipher
	if recoverableAgentRuns {
		checkpointCipher, cipherErr := agentCredential.NewRunCheckpointCipherFromEnv()
		if cipherErr != nil {
			log.Fatalf("recoverable Agent runs require an independent checkpoint encryption key: %v", cipherErr)
		}
		agentCheckpointCipher = checkpointCipher
	}
	agentTaskTemplateRepo := repository.NewMongoAgentTaskTemplateRepository(mongoInfra.GetDB())
	if err := agentTaskTemplateRepo.EnsureIndexes(context.Background()); err != nil {
		if agentTaskTemplatesEnabled {
			log.Fatalf("failed to ensure Agent task template indexes: %v", err)
		}
		log.Printf("Agent task template indexes unavailable while feature is disabled: %v", err)
	}
	agentExecutionRunRepo := repository.NewMongoAgentExecutionRunRepository(mongoInfra.GetDB())
	if err := agentExecutionRunRepo.EnsureIndexes(context.Background()); err != nil {
		if recoverableAgentRuns {
			log.Fatalf("failed to ensure Agent execution run indexes: %v", err)
		}
		log.Printf("⚠️ Failed to ensure Agent execution run indexes: %v", err)
	} else {
		log.Println("✅ Agent execution run indexes ensured")
	}
	agentProductEventRepo := repository.NewMongoProductEventRepository(mongoInfra.GetDB())
	if err := agentProductEventRepo.EnsureIndexes(context.Background()); err != nil {
		if recoverableAgentRuns || externalMCPEnabled {
			log.Fatalf("failed to ensure Agent product event indexes: %v", err)
		}
		log.Printf("Agent product event indexes unavailable while dependent features are disabled: %v", err)
	}

	// 2. 初始化 ES 客户端 (高可用降级：允许 ES 离线启动)
	var esClient *es.Client
	if err := es.Init(); err != nil {
		log.Printf("⚠️ Warning: Failed to init elasticsearch: %v. Search features might be limited.", err)
	} else {
		log.Println("✅ Elasticsearch connected")
		esClient = es.GetClient()
	}
	_ = esClient

	// 2.1 Qdrant 向量库初始化
	var qdrantClient *qdrant.Client
	qdrantURL := getEnv("QDRANT_URL", "http://localhost:6333")
	qdrantClient = qdrant.NewClient(qdrantURL)
	log.Println("✅ Qdrant client initialized")
	// 预建 collection (1024 维 cosine 相似度)
	if err := qdrantClient.CreateCollection(context.Background(), "tweets", 1024); err != nil {
		log.Printf("⚠️ Failed to create qdrant collection: %v. Search features might be limited.", err)
	}

	// 3. 初始化 AI Embedding 客户端
	aiClient := ai.NewClient(getEnv("LM_STUDIO_API_URL", "http://localhost:1234/v1"))
	chatModelLocal := getEnv("LM_STUDIO_MODEL_CHAT", "qwen2.5-3b-instruct")
	chatModelPremium := getEnv("DASHSCOPE_MODEL_CHAT", getEnv("PREMIUM_AI_MODEL_CHAT", "qwen-plus"))
	log.Println("✅ AI Embedding client initialized")

	// 3.1 初始化 Reranker
	rerankerType := getEnv("RERANKER_TYPE", "local")
	rerankerApiKey := getEnv("RERANKER_API_KEY", "")
	if rerankerApiKey == "" && strings.ToLower(rerankerType) == "dashscope" {
		rerankerApiKey = getEnv("DASHSCOPE_API_KEY", "")
	}
	reranker := ai.NewReranker(
		rerankerType,
		rerankerApiKey,
		getEnv("RERANKER_API_URL", ""),
		getEnv("RERANKER_MODEL", ""),
	)
	log.Printf("✅ Reranker client initialized (Type: %s)", rerankerType)

	// 4. 连接 tweet-service（通过 Consul 服务发现）
	consulAddrTweetService := getEnv("CONSUL_HOST", "localhost") + ":" + getEnv("CONSUL_PORT", "8500")
	tweetTarget := fmt.Sprintf("consul://%s/tweet-service?healthy=true", consulAddrTweetService)
	tweetRiskControlMethods := []string{
		tweetv1.TweetService_GetAuthorPostingStats_FullMethodName,
		tweetv1.TweetService_ApplyTweetModeration_FullMethodName,
	}
	internalServiceIdentity := getEnv("TWEET_INTERNAL_SERVICE_IDENTITY", "agent-service")
	internalServiceToken := getEnv("TWEET_INTERNAL_SERVICE_TOKEN", "")
	var tweetRiskControlAuthInterceptor grpc.UnaryClientInterceptor
	if strings.TrimSpace(internalServiceToken) == "" {
		guard, guardErr := serviceauth.NewFailClosedUnaryClientInterceptor(tweetRiskControlMethods)
		if guardErr != nil {
			log.Fatalf("configure Tweet risk-control client guard: %v", guardErr)
		}
		tweetRiskControlAuthInterceptor = guard
		log.Println("Tweet risk-control client is unavailable until TWEET_INTERNAL_SERVICE_TOKEN is configured")
	} else {
		credential, credentialErr := serviceauth.NewStaticCredential(
			internalServiceIdentity,
			internalServiceToken,
			tweetRiskControlMethods,
		)
		if credentialErr != nil {
			log.Fatalf("configure Tweet risk-control client authentication: %v", credentialErr)
		}
		tweetRiskControlAuthInterceptor = credential.UnaryClientInterceptor()
		log.Printf("Tweet risk-control client authentication enabled for identity %q", internalServiceIdentity)
	}
	tweetConn, err := grpc.NewClient(tweetTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithChainUnaryInterceptor(tweetRiskControlAuthInterceptor),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	)
	if err != nil {
		log.Fatalf("❌ Failed to connect tweet-service: %v", err)
	}
	defer tweetConn.Close()
	tweetClient := tweetv1.NewTweetServiceClient(tweetConn)
	log.Println("✅ Tweet Service client connected")

	// 4.1 连接 user-service
	userTarget := fmt.Sprintf("consul://%s/user-service?healthy=true", consulAddrTweetService)
	userConn, err := grpc.NewClient(userTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	)
	if err != nil {
		log.Fatalf("❌ Failed to connect user-service: %v", err)
	}
	defer userConn.Close()
	userClient := userv1.NewUserServiceClient(userConn)
	log.Println("✅ User Service client connected")
	agentProjectManager := agentProject.NewManager(
		repo,
		agentProject.NewGRPCUserDirectory(userClient),
		agentProject.WithEnabled(externalMCPProjectScopeEnabled),
	)

	// 🆕 初始化 MySQL/Redis/RabbitMQ (用于影子风控与舆情播报双轨并行)
	dbConfig := persistence.DefaultDBConfig()
	db, err := persistence.NewDB(dbConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect database: %v", err)
	}
	log.Println("✅ Database connected for Agent Service bypass")

	redisConfig := cache.DefaultRedisConfig()
	redisClient, err := cache.NewRedis(redisConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect redis: %v", err)
	}
	log.Println("✅ Redis connected for Agent Service bypass")

	webAccess, err := configureWebAccess(redisClient)
	if err != nil {
		log.Fatalf("failed to configure public web access: %v", err)
	}
	if webAccess.search != nil {
		log.Printf("governed web access enabled (provider=%s, page_read=%t)", webAccess.search.Name(), webAccess.page != nil)
	} else {
		log.Println("public web access disabled")
	}

	// 5. 启动 MCP Server（后台 goroutine）
	mcpAddr := getEnv("MCP_SERVER_ADDR", "127.0.0.1:9200")
	mcpAuthToken, err := resolveMCPAuthToken()
	if err != nil {
		log.Fatalf("failed to configure MCP authentication: %v", err)
	}
	embeddingModel := getEnv("LM_STUDIO_MODEL_EMBEDDING", "text-embedding-bge-m3")
	mcpServer := agentMcp.NewMCPServer(
		esClient, qdrantClient, aiClient, reranker, tweetClient, userClient, embeddingModel,
		agentMcp.WithAuthToken(mcpAuthToken),
		agentMcp.WithWebSearchProvider(webAccess.search),
		agentMcp.WithPageReader(webAccess.page),
	)
	// 6. 初始化 AgentService（注入 Repository, aiClient 和 redisClient）
	runtimeRollout, rolloutErr := agentRuntime.ParseRollout(getEnv(agentRuntime.RuntimeV2ModesEnv, ""))
	if rolloutErr != nil {
		log.Printf("⚠️ Invalid %s, falling back to legacy runtime: %v", agentRuntime.RuntimeV2ModesEnv, rolloutErr)
		runtimeRollout = agentRuntime.Rollout{}
	}
	profileReleases, profileReleaseErr := agentProfile.ParseReleases(getEnv(agentProfile.ReleasesEnv, ""))
	if profileReleaseErr != nil {
		log.Fatalf("invalid %s: %v", agentProfile.ReleasesEnv, profileReleaseErr)
	}
	initialProfileCatalog, profileReleaseErr := agentService.NewBuiltInProfileCatalog(nil, nil, profileReleases)
	if profileReleaseErr != nil {
		log.Fatalf("invalid Agent Profile release snapshot: %v", profileReleaseErr)
	}
	profileResolver, profileReleaseErr := agentProfile.NewAtomicResolver(initialProfileCatalog)
	if profileReleaseErr != nil {
		log.Fatalf("initialize Agent Profile resolver: %v", profileReleaseErr)
	}
	profileStoreEnabled := getEnvBool(agentProfile.StoreEnabledEnv, true)
	profileQualityEvidenceRequired := getEnvBool(agentProfile.QualityEvidenceRequiredEnv, false)
	profileContentSignoffRequired := getEnvBool(agentProfile.QualityEvidenceContentSignoffRequiredEnv, false)
	if profileQualityEvidenceRequired && !profileStoreEnabled {
		log.Fatalf("%s=true requires %s=true", agentProfile.QualityEvidenceRequiredEnv, agentProfile.StoreEnabledEnv)
	}
	if profileContentSignoffRequired && !profileQualityEvidenceRequired {
		log.Fatalf("%s=true requires %s=true", agentProfile.QualityEvidenceContentSignoffRequiredEnv, agentProfile.QualityEvidenceRequiredEnv)
	}
	var profileManager *agentService.ProfileCatalogManager
	var profileRepo *repository.MongoProfileRepository
	var profileChangeBus *repository.RedisProfileCatalogChangeBus
	if profileStoreEnabled {
		profileStartupCtx, profileStartupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		profileRepo = repository.NewMongoProfileRepository(mongoInfra.GetDB())
		if err := profileRepo.EnsureIndexes(profileStartupCtx); err != nil {
			profileStartupCancel()
			log.Fatalf("failed to ensure Agent Profile indexes: %v", err)
		}
		profileChangeBus, err = repository.NewRedisProfileCatalogChangeBus(
			redisClient,
			getEnv(agentProfile.ChangeChannelEnv, agentProfile.DefaultChangeChannel),
		)
		if err != nil {
			profileStartupCancel()
			log.Fatalf("initialize Agent Profile change bus: %v", err)
		}
		profileManagerOptions := []agentService.ProfileCatalogManagerOption{
			agentService.WithProfileCatalogChangePublisher(profileChangeBus),
		}
		if profileQualityEvidenceRequired {
			archive, archiveErr := agentObjectStore.NewMinIOAgentTaskReportArchive(agentObjectStore.MinIOAgentTaskReportConfig{
				Endpoint:  getEnv(agentProfile.QualityEvidenceArchiveEndpointEnv, getEnv("MINIO_ENDPOINT", "localhost:9000")),
				AccessKey: getEnv(agentProfile.QualityEvidenceArchiveAccessKeyEnv, getEnv("MINIO_ACCESS_KEY", getEnv("MINIO_USER", ""))),
				SecretKey: getEnv(agentProfile.QualityEvidenceArchiveSecretKeyEnv, getEnv("MINIO_SECRET_KEY", getEnv("MINIO_PASSWORD", ""))),
				Bucket:    getEnv(agentProfile.QualityEvidenceArchiveBucketEnv, "agent-task-eval-reports"),
				Secure:    getEnvBool(agentProfile.QualityEvidenceArchiveSecureEnv, false),
			})
			if archiveErr != nil {
				profileStartupCancel()
				log.Fatalf("initialize Agent Profile quality evidence archive: %v", archiveErr)
			}
			if archiveErr = archive.Ensure(profileStartupCtx); archiveErr != nil {
				profileStartupCancel()
				log.Fatalf("verify Agent Profile quality evidence archive: %v", archiveErr)
			}
			qualityEvidenceOptions := make([]agentObjectStore.AgentTaskQualityEvidenceVerifierOption, 0, 1)
			if profileContentSignoffRequired {
				qualityEvidenceOptions = append(qualityEvidenceOptions, agentObjectStore.WithRequiredExternalHumanContentReview(
					[]byte(getEnv(agentProfile.QualityEvidenceContentSignoffKeyEnv, "")),
					getEnv(agentProfile.QualityEvidenceContentSignoffKeyIDEnv, ""),
				))
			}
			verifier, verifierErr := agentObjectStore.NewAgentTaskQualityEvidenceVerifier(
				archive,
				[]byte(getEnv(agentProfile.QualityEvidenceIntegrityKeyEnv, "")),
				getEnv(agentProfile.QualityEvidenceIntegrityKeyIDEnv, ""),
				qualityEvidenceOptions...,
			)
			if verifierErr != nil {
				profileStartupCancel()
				log.Fatalf("initialize Agent Profile quality evidence verifier: %v", verifierErr)
			}
			profileManagerOptions = append(profileManagerOptions, agentService.WithProfileQualityEvidenceVerifier(verifier, true))
		}
		profileManager, err = agentService.NewProfileCatalogManager(
			profileRepo,
			profileResolver,
			profileReleases,
			profileManagerOptions...,
		)
		if err != nil {
			profileStartupCancel()
			log.Fatalf("initialize Agent Profile catalog manager: %v", err)
		}
		if err := profileManager.Reload(profileStartupCtx); err != nil {
			profileStartupCancel()
			log.Fatalf("load persisted Agent Profile catalog: %v", err)
		}
		profileStartupCancel()
	}
	profileAdminToken := strings.TrimSpace(getEnv(agentProfile.AdminTokenEnv, ""))
	profileDirectPublishEnabled := getEnvBool(agentProfile.DirectPublishEnabledEnv, false)
	if profileAdminToken != "" && len(profileAdminToken) < 32 {
		log.Fatalf("%s must contain at least 32 characters", agentProfile.AdminTokenEnv)
	}
	if profileDirectPublishEnabled && profileAdminToken == "" {
		log.Fatalf("%s=true requires %s", agentProfile.DirectPublishEnabledEnv, agentProfile.AdminTokenEnv)
	}
	if profileQualityEvidenceRequired && profileAdminToken == "" {
		log.Fatalf("%s=true requires %s", agentProfile.QualityEvidenceRequiredEnv, agentProfile.AdminTokenEnv)
	}
	if profileAdminToken != "" && profileManager == nil {
		log.Fatalf("%s requires %s=true", agentProfile.AdminTokenEnv, agentProfile.StoreEnabledEnv)
	}
	var profileAccessManager *agentService.ProfileAccessManager
	var profileAdministratorIDs []uint64
	if profileAdminToken != "" {
		viewerIDs, parseErr := parseProfileUserIDs(getEnv(agentProfile.ViewerUserIDsEnv, ""), "viewer")
		if parseErr != nil {
			log.Fatalf("invalid %s: %v", agentProfile.ViewerUserIDsEnv, parseErr)
		}
		editorIDs, parseErr := parseProfileUserIDs(getEnv(agentProfile.EditorUserIDsEnv, ""), "editor")
		if parseErr != nil {
			log.Fatalf("invalid %s: %v", agentProfile.EditorUserIDsEnv, parseErr)
		}
		approverIDs, parseErr := parseProfileUserIDs(getEnv(agentProfile.ApproverUserIDsEnv, ""), "approver")
		if parseErr != nil {
			log.Fatalf("invalid %s: %v", agentProfile.ApproverUserIDsEnv, parseErr)
		}
		profileAdministratorIDs, parseErr = parseProfileUserIDs(getEnv(agentProfile.AdminUserIDsEnv, ""), "administrator")
		if parseErr != nil {
			log.Fatalf("invalid %s: %v", agentProfile.AdminUserIDsEnv, parseErr)
		}
		dynamicRBACEnabled := getEnvBool(agentProfile.DynamicRBACEnabledEnv, true)
		if (dynamicRBACEnabled || profileDirectPublishEnabled) && len(profileAdministratorIDs) == 0 {
			log.Fatalf("dynamic RBAC or direct publishing requires at least one %s", agentProfile.AdminUserIDsEnv)
		}
		profileAccessManager = agentService.NewProfileAccessManager(
			profileRepo,
			agentService.ProfileStaticRoleAssignments{
				ViewerUserIDs: viewerIDs, EditorUserIDs: editorIDs,
				ApproverUserIDs: approverIDs, AdminUserIDs: profileAdministratorIDs,
			},
			dynamicRBACEnabled,
		)
	}
	profileExperimentsEnabled := getEnvBool(agentProfile.ExperimentsEnabledEnv, false)
	var profileExperimentManager *agentService.ProfileExperimentManager
	var profileExperimentRecorder agentObservability.Recorder
	var profileExperimentAsyncRecorder *agentService.AsyncProfileExperimentRunRecorder
	var profileProductOutcomeRecorder agentService.ProductOutcomeRecorder
	var profileContentAttributionRepo *repository.MongoContentAttributionRepository
	var profileContentEngagementProcessor *agentService.ContentEngagementProcessor
	profileContentAttributionWindow := getEnvDuration(
		agentProfile.ContentAttributionWindowEnv,
		agentService.DefaultContentAttributionWindow,
	)
	if profileContentAttributionWindow <= 0 {
		profileContentAttributionWindow = agentService.DefaultContentAttributionWindow
	}
	if profileExperimentsEnabled {
		if profileManager == nil || profileRepo == nil || profileAdminToken == "" || len(profileAdministratorIDs) == 0 {
			log.Fatalf("%s=true requires the Profile store, administration token and at least one administrator", agentProfile.ExperimentsEnabledEnv)
		}
		profileExperimentMetrics, metricsErr := agentService.NewPrometheusProfileExperimentObserver(prometheus.DefaultRegisterer)
		if metricsErr != nil {
			log.Fatalf("initialize Agent Profile experiment metrics: %v", metricsErr)
		}
		profileExperimentManager, err = agentService.NewProfileExperimentManager(
			profileRepo, profileRepo, profileManager,
			agentService.WithProfileExperimentObserver(profileExperimentMetrics),
		)
		if err != nil {
			log.Fatalf("initialize Agent Profile experiment manager: %v", err)
		}
		profileProductOutcomeRecorder, err = agentService.NewProfileExperimentProductOutcomeRecorder(
			profileExperimentManager, profileAdministratorIDs[0],
		)
		if err != nil {
			log.Fatalf("initialize Agent Profile product outcome recorder: %v", err)
		}
		profileContentAttributionRepo = repository.NewMongoContentAttributionRepository(mongoInfra.GetDB())
		if err := profileContentAttributionRepo.EnsureIndexes(context.Background()); err != nil {
			log.Fatalf("initialize Agent Profile content attribution indexes: %v", err)
		}
		profileContentEngagementProcessor, err = agentService.NewContentEngagementProcessor(
			profileContentAttributionRepo, traceRepo, profileProductOutcomeRecorder,
		)
		if err != nil {
			log.Fatalf("initialize Agent Profile content engagement processor: %v", err)
		}
		profileExperimentAsyncRecorder, err = agentService.NewAsyncProfileExperimentRunRecorder(
			agentService.NewProfileExperimentRunRecorder(profileRepo, profileExperimentMetrics),
			getEnvPositiveInt("AGENT_PROFILE_EXPERIMENT_OBSERVATION_QUEUE_SIZE", 1024),
		)
		if err != nil {
			log.Fatalf("initialize Agent Profile experiment observation queue: %v", err)
		}
		profileExperimentRecorder = profileExperimentAsyncRecorder
	}
	workflowRegistry := workflowTool.NewRegistry()
	approvalGate := agentService.NewPersistentApprovalGate(repo, getEnvDuration("AGENT_TOOL_APPROVAL_TTL", 15*time.Minute))
	toolResultInlineMaxBytes := getEnvPositiveInt("AGENT_TOOL_RESULT_INLINE_MAX_BYTES", agentService.DefaultToolResultInlineMaxBytes)
	toolResultMaxBytes := getEnvPositiveInt("AGENT_TOOL_RESULT_MAX_BYTES", workflowTool.DefaultMaxResultBytes)
	if toolResultInlineMaxBytes > toolResultMaxBytes {
		log.Fatalf("AGENT_TOOL_RESULT_INLINE_MAX_BYTES must not exceed AGENT_TOOL_RESULT_MAX_BYTES")
	}
	toolResultStoreOptions := []agentService.PersistentToolResultStoreOption{
		agentService.WithToolResultLimits(toolResultInlineMaxBytes, toolResultMaxBytes),
	}
	if getEnvBool("AGENT_TOOL_RESULT_OBJECT_STORE_ENABLED", false) {
		objectStore, objectStoreErr := agentObjectStore.NewMinIOToolResultStore(agentObjectStore.MinIOToolResultConfig{
			Endpoint:  getEnv("AGENT_TOOL_RESULT_MINIO_ENDPOINT", getEnv("MINIO_ENDPOINT", "localhost:9000")),
			AccessKey: getEnv("AGENT_TOOL_RESULT_MINIO_ACCESS_KEY", getEnv("MINIO_ACCESS_KEY", "")),
			SecretKey: getEnv("AGENT_TOOL_RESULT_MINIO_SECRET_KEY", getEnv("MINIO_SECRET_KEY", "")),
			Bucket:    getEnv("AGENT_TOOL_RESULT_MINIO_BUCKET", "agent-tool-results"),
			Secure:    getEnvBool("AGENT_TOOL_RESULT_MINIO_SECURE", false),
		})
		if objectStoreErr != nil {
			log.Fatalf("failed to configure private tool result object store: %v", objectStoreErr)
		}
		bucketCtx, bucketCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if objectStoreErr = objectStore.EnsureBucket(bucketCtx); objectStoreErr != nil {
			bucketCancel()
			log.Fatalf("failed to initialize private tool result bucket: %v", objectStoreErr)
		}
		bucketCancel()
		toolResultStoreOptions = append(toolResultStoreOptions, agentService.WithToolResultObjectStore(objectStore))
		log.Printf("private tool result object store enabled: bucket=%s inline_max=%d max=%d", getEnv("AGENT_TOOL_RESULT_MINIO_BUCKET", "agent-tool-results"), toolResultInlineMaxBytes, toolResultMaxBytes)
	}
	toolResultStore := agentService.NewPersistentToolResultStore(repo, toolResultStoreOptions...)
	toolMetrics, err := workflowTool.NewPrometheusMetrics(prometheus.DefaultRegisterer)
	if err != nil {
		log.Fatalf("failed to register agent tool metrics: %v", err)
	}
	externalMCPMetrics, err := externalmcp.NewPrometheusMetrics(prometheus.DefaultRegisterer)
	if err != nil {
		log.Fatalf("failed to register external MCP metrics: %v", err)
	}
	executionMetrics, err := agentObservability.NewPrometheusRecorder(prometheus.DefaultRegisterer)
	if err != nil {
		log.Fatalf("failed to register agent execution metrics: %v", err)
	}
	unifiedProductMetrics, err := agentService.NewPrometheusUnifiedAgentProductObserver(prometheus.DefaultRegisterer)
	if err != nil {
		log.Fatalf("failed to register Unified Agent product metrics: %v", err)
	}
	traceContentSampler, err := agentObservability.NewSafeContentSampler(agentObservability.ContentSamplingConfig{
		Enabled:  getEnvBool("AGENT_PROMPT_SAMPLING_ENABLED", false),
		Ratio:    getEnvFloat64("AGENT_PROMPT_SAMPLE_RATIO", 0.01),
		MaxBytes: getEnvPositiveInt("AGENT_PROMPT_SAMPLE_MAX_BYTES", 512),
	})
	if err != nil {
		log.Fatalf("failed to configure Agent Prompt sampling: %v", err)
	}
	executionEventStore := repository.NewRedisExecutionEventStore(redisClient, repository.ExecutionEventStreamConfig{
		MaxLength: int64(getEnvPositiveInt("AGENT_WORKFLOW_EVENT_STREAM_MAX_LENGTH", 2000)),
		TTL:       getEnvDuration("AGENT_WORKFLOW_EVENT_STREAM_TTL", 24*time.Hour),
	})
	traceRecorder := agentObservability.NewFanoutRecorder(
		traceRepo,
		executionEventStore,
		executionMetrics,
		agentObservability.NewOTelRecorder(nil),
		profileExperimentRecorder,
	)
	metric.StartMetricsServer(getEnvPositiveInt("AGENT_METRICS_PORT", 9191))
	toolBreaker := workflowTool.NewToolCircuitBreaker(workflowTool.CircuitBreakerConfig{
		FailureThreshold: getEnvPositiveInt("AGENT_TOOL_BREAKER_FAILURE_THRESHOLD", 5),
		OpenTimeout:      getEnvDuration("AGENT_TOOL_BREAKER_OPEN_TIMEOUT", 30*time.Second),
		Observer:         toolMetrics,
	})
	workflowExecutor := workflowTool.NewExecutor(
		workflowRegistry,
		workflowTool.WithApprovalGate(approvalGate),
		workflowTool.WithAuditSink(agentService.NewToolTraceAuditSink(traceRecorder, workflowTool.SlogAuditSink{})),
		workflowTool.WithIdempotencyStore(toolResultStore),
		workflowTool.WithResultArchiver(toolResultStore),
		workflowTool.WithResultPolicy(workflowTool.ResultPolicy{MaxBytes: toolResultMaxBytes}),
		workflowTool.WithMetricsSink(toolMetrics),
		workflowTool.WithCircuitBreaker(toolBreaker),
	)
	capabilityCatalogOptions := make([]agentService.BuiltInAgentCapabilityCatalogOption, 0, 3)
	if webAccess.search != nil {
		capabilityCatalogOptions = append(
			capabilityCatalogOptions,
			agentService.WithAvailableWebSearchCapability(),
		)
	}
	if externalMCPEnabled {
		capabilityCatalogOptions = append(
			capabilityCatalogOptions,
			agentService.WithAvailableExternalMCPCapability(),
		)
	}
	if workflowAsToolEnabled {
		capabilityCatalogOptions = append(
			capabilityCatalogOptions,
			agentService.WithAvailableWorkflowCapability(),
		)
	}
	if skillCatalogEnabled {
		capabilityCatalogOptions = append(
			capabilityCatalogOptions,
			agentService.WithAvailableSkillCapability(),
		)
	}
	capabilityCatalog, err := agentService.NewBuiltInAgentCapabilityCatalog(capabilityCatalogOptions...)
	if err != nil {
		log.Fatalf("failed to configure Agent capability catalog: %v", err)
	}
	svc := agentService.NewAgentService(
		getEnv("DASHSCOPE_API_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
		getEnv("DASHSCOPE_API_KEY", ""),
		chatModelPremium,
		mcpAddr,
		repo,
		aiClient,
		redisClient,
		agentService.WithRuntimeRollout(runtimeRollout),
		agentService.WithProfileResolver(profileResolver),
		agentService.WithExecutionTraceStore(traceRecorder, traceRepo),
		agentService.WithTraceContentSampler(traceContentSampler),
		agentService.WithExecutionEventStore(executionEventStore),
		agentService.WithWorkflowToolExecutor(workflowExecutor),
		agentService.WithWorkflowToolPublications(
			workflowToolPublicationRepo,
			workflowAsToolEnabled,
			getEnvPositiveInt("AGENT_WORKFLOW_TOOL_CATALOG_LIMIT", 20),
			getEnvDuration("AGENT_WORKFLOW_TOOL_TIMEOUT", 60*time.Second),
		),
		agentService.WithWorkflowSkillCatalog(
			skillCatalogEnabled,
			getEnvPositiveInt("AGENT_SKILL_CATALOG_LIMIT", 20),
		),
		agentService.WithAgentExtensionCatalog(
			extensionCatalogEnabled,
			getEnvPositiveInt("AGENT_EXTENSION_CATALOG_LIMIT", 20),
		),
		agentService.WithAgentExtensionMarketplace(
			extensionMarketplaceRepo,
			extensionMarketplaceEnabled,
			getEnvPositiveInt("AGENT_EXTENSION_MARKETPLACE_LIMIT", 20),
		),
		agentService.WithConfirmedDraftPublisher(agentService.NewTweetServiceConfirmedDraftPublisher(tweetClient)),
		agentService.WithProductOutcomeRecorder(profileProductOutcomeRecorder),
		agentService.WithContentAttribution(profileContentAttributionRepo, profileContentAttributionWindow),
		agentService.WithMCPAuthToken(mcpAuthToken),
		agentService.WithAgentCapabilityCatalog(capabilityCatalog),
		agentService.WithAgentExecutionStrategyPlanner(executionStrategyPlanner),
		agentService.WithMultiAgentExecution(multiAgentExecutionEnabled),
		agentService.WithAgentExecutionRunStore(agentExecutionRunRepo),
		agentService.WithAgentRunAccountingStore(repo),
		agentService.WithRecoverableAgentRuns(recoverableAgentRuns),
		agentService.WithUnifiedAgentProductObserver(unifiedProductMetrics),
		agentService.WithAgentProductEvents(agentProductEventRepo, externalMCPMetrics),
		agentService.WithAgentTaskTemplates(
			agentTaskTemplateRepo,
			agentTaskTemplatesEnabled,
			getEnvPositiveInt("AGENT_TASK_TEMPLATE_LIST_LIMIT", 20),
		),
		agentService.WithUnifiedAgentApprovalRecovery(unifiedApprovalRecovery),
		agentService.WithAgentRunRecovery(
			agentCheckpointCipher,
			getEnvPositiveInt("AGENT_RUN_CHECKPOINT_MAX_BYTES", agentService.DefaultAgentRunCheckpointMaxBytes),
			getEnvDuration("AGENT_RUN_RESUME_LEASE_DURATION", agentService.DefaultAgentRunResumeLeaseDuration),
		),
		agentService.WithWebSearchProviderFactory(webAccess.factory),
		agentService.WithExternalMCPEnabled(externalMCPEnabled),
		agentService.WithExternalMCPProjectScope(externalMCPProjectScopeEnabled),
		agentService.WithExternalMCPManagedCredentials(
			externalMCPManagedCredentialsEnabled,
			externalMCPManagedCredentials,
		),
		agentService.WithExternalMCPEndpointPolicy(externalMCPEndpointPolicy),
		agentService.WithAgentProjectManager(agentProjectManager),
		agentService.WithExternalMCPClientPool(externalmcp.ClientPoolConfig{
			Enabled:                  getEnvBool("AGENT_EXTERNAL_MCP_POOL_ENABLED", true),
			MaxSessions:              getEnvPositiveInt("AGENT_EXTERNAL_MCP_POOL_MAX_SESSIONS", 64),
			MaxSessionsPerConnection: getEnvPositiveInt("AGENT_EXTERNAL_MCP_POOL_MAX_SESSIONS_PER_CONNECTION", 2),
			IdleTimeout:              getEnvDuration("AGENT_EXTERNAL_MCP_POOL_IDLE_TIMEOUT", 5*time.Minute),
			AcquireTimeout:           getEnvDuration("AGENT_EXTERNAL_MCP_POOL_ACQUIRE_TIMEOUT", 2*time.Second),
		}, externalMCPMetrics),
		agentService.WithExternalMCPHealthChecks(externalmcp.HealthCheckConfig{
			Enabled:             getEnvBool("AGENT_EXTERNAL_MCP_HEALTH_CHECK_ENABLED", false),
			PollInterval:        getEnvDuration("AGENT_EXTERNAL_MCP_HEALTH_POLL_INTERVAL", 15*time.Second),
			HealthyInterval:     getEnvDuration("AGENT_EXTERNAL_MCP_HEALTH_INTERVAL", 2*time.Minute),
			Timeout:             getEnvDuration("AGENT_EXTERNAL_MCP_HEALTH_TIMEOUT", 5*time.Second),
			LeaseDuration:       getEnvDuration("AGENT_EXTERNAL_MCP_HEALTH_LEASE_DURATION", 15*time.Second),
			FailureBackoffMin:   getEnvDuration("AGENT_EXTERNAL_MCP_HEALTH_FAILURE_BACKOFF_MIN", 30*time.Second),
			FailureBackoffMax:   getEnvDuration("AGENT_EXTERNAL_MCP_HEALTH_FAILURE_BACKOFF_MAX", 15*time.Minute),
			FailureThreshold:    int64(getEnvPositiveInt("AGENT_EXTERNAL_MCP_HEALTH_FAILURE_THRESHOLD", 3)),
			BatchSize:           getEnvPositiveInt("AGENT_EXTERNAL_MCP_HEALTH_BATCH_SIZE", 20),
			MaxConcurrentChecks: getEnvPositiveInt("AGENT_EXTERNAL_MCP_HEALTH_MAX_CONCURRENCY", 4),
		}, externalMCPMetrics),
	)
	if webAccess.providerResolver != nil {
		webAccess.providerResolver.Set(svc)
	}
	if processPlan.StartsAPI() {
		go func() {
			log.Printf("🔧 MCP Server starting on %s", mcpAddr)
			if err := mcpServer.Start(mcpAddr); err != nil {
				log.Fatalf("❌ MCP Server failed: %v", err)
			}
		}()
		log.Println("✅ MCP Server started")
	} else {
		log.Println("Agent MCP Server disabled for worker-only process role")
	}
	log.Printf("✅ Agent Runtime rollout loaded: v2_modes=%s", runtimeRollout.String())
	log.Printf("✅ Recoverable Agent run lifecycle enabled=%t checkpoint_max_bytes=%d resume_lease=%s",
		recoverableAgentRuns,
		getEnvPositiveInt("AGENT_RUN_CHECKPOINT_MAX_BYTES", agentService.DefaultAgentRunCheckpointMaxBytes),
		getEnvDuration("AGENT_RUN_RESUME_LEASE_DURATION", agentService.DefaultAgentRunResumeLeaseDuration),
	)
	log.Printf("Unified Agent approval recovery enabled=%t", unifiedApprovalRecovery)
	log.Printf("✅ Agent Profile catalog loaded: persisted=%t configured_overrides=%d", profileStoreEnabled, len(profileReleases))
	memoryManager := rag.NewMemoryManager(
		db,
		esClient,
		qdrantClient,
		aiClient,
		chatModelLocal,
		embeddingModel,
		rag.WithEpisodicMemoryConfig(rag.EpisodicMemoryConfig{
			CollectionName:    getEnv("AGENT_EPISODIC_COLLECTION", rag.DefaultEpisodicCollectionName),
			EmbeddingVersion:  getEnv("AGENT_EPISODIC_EMBEDDING_VERSION", rag.DefaultEpisodicEmbeddingVersion),
			LegacyReadEnabled: getEnvBool("AGENT_EPISODIC_LEGACY_READ_ENABLED", true),
		}),
		rag.WithScoringConfig(rag.ScoringConfig{
			SimilarityThreshold: getEnvFloat64("AGENT_RAG_SIMILARITY_THRESHOLD", 0.65),
			SimilarityWeight:    getEnvFloat64("AGENT_RAG_SIMILARITY_WEIGHT", 0.60),
			TimeDecayWeight:     getEnvFloat64("AGENT_RAG_TIME_DECAY_WEIGHT", 0.25),
			FrequencyWeight:     getEnvFloat64("AGENT_RAG_FREQUENCY_WEIGHT", 0.15),
			TimeDecayLambda:     getEnvFloat64("AGENT_RAG_TIME_DECAY_LAMBDA", 0.00001),
			KeywordBonus:        getEnvFloat64("AGENT_RAG_KEYWORD_BONUS", 0.15),
		}),
	)
	cascadeRouter := rag.NewCascadeRouter(aiClient, chatModelLocal)
	svc.SetCognitiveEngine(memoryManager, cascadeRouter, embeddingModel)
	if processPlan.StartsAPI() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := cascadeRouter.InitSemanticAnchors(ctx, embeddingModel); err != nil {
				log.Printf("Cognitive router semantic anchors unavailable: %v", err)
			}
		}()
	}
	mustRegisterWorkflowTool(workflowRegistry, workflowTool.NewLLMChatToolWithOptions(
		aiClient,
		chatModelPremium,
		workflowTool.WithLLMConfiguredEndpoint(
			getEnv("DASHSCOPE_API_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
			getEnv("DASHSCOPE_API_KEY", ""),
		),
		workflowTool.WithLLMProviderConfigResolver(svc),
		workflowTool.WithLLMCostEstimator(svc.RuntimeCostEstimator()),
		workflowTool.WithLLMTraceRecorder(traceRecorder),
		workflowTool.WithLLMContentSampler(traceContentSampler),
	))
	mustRegisterWorkflowTool(workflowRegistry, workflowTool.NewWebSearchTool(webAccess.search))
	mustRegisterWorkflowTool(workflowRegistry, workflowTool.NewPageReadTool(webAccess.page))
	mustRegisterWorkflowTool(workflowRegistry, workflowTool.NewPublishTweetTool(tweetClient))
	registerWorkflowMCPTools(workflowRegistry, svc)
	registerWorkflowStrategyTools(workflowRegistry, svc)
	log.Println("✅ Agent Service initialized (with MongoDB persistence)")

	backgroundCtx, backgroundCancel := context.WithCancel(context.Background())
	if profileExperimentAsyncRecorder != nil && processPlan.StartsAPI() {
		go profileExperimentAsyncRecorder.Run(backgroundCtx)
	}
	if profileManager != nil && profileChangeBus != nil {
		profileSynchronizer, syncErr := agentService.NewProfileCatalogSynchronizer(
			profileManager,
			profileChangeBus,
			getEnvDuration(agentProfile.SyncIntervalEnv, agentService.DefaultProfileCatalogSyncInterval),
		)
		if syncErr != nil {
			log.Fatalf("initialize Agent Profile synchronizer: %v", syncErr)
		}
		go profileSynchronizer.Run(backgroundCtx)
	}
	var mqClient *mq.RabbitMQ
	var riskMQClient *mq.RabbitMQ
	var temporalTaskWorker worker.Worker
	if processPlan.StartsWorkers() {
		mqConfig := mq.DefaultRabbitMQConfig()
		mqClient, err = mq.NewRabbitMQ(mqConfig)
		if err != nil {
			log.Fatalf("❌ Failed to connect rabbitmq: %v", err)
		}
		log.Println("✅ RabbitMQ connected for Agent worker role")
		riskMQClient, err = mq.NewRabbitMQ(mqConfig)
		if err != nil {
			log.Fatalf("connect dedicated Agent risk-control RabbitMQ channel: %v", err)
		}
		if err := agentService.DeclareRiskControlTopology(riskMQClient); err != nil {
			log.Fatalf("initialize Agent risk-control queue topology: %v", err)
		}
		log.Println("Agent risk-control queue topology ready on a dedicated RabbitMQ channel")

		if profileContentEngagementProcessor != nil {
			contentEngagementMetrics, metricsErr := agentConsumer.NewPrometheusContentEngagementObserver(prometheus.DefaultRegisterer)
			if metricsErr != nil {
				log.Fatalf("initialize Agent Profile content engagement metrics: %v", metricsErr)
			}
			contentEngagementConsumer, consumerErr := agentConsumer.NewContentEngagementConsumer(
				mqClient, profileContentEngagementProcessor, contentEngagementMetrics,
			)
			if consumerErr != nil {
				log.Fatalf("initialize Agent Profile content engagement consumer: %v", consumerErr)
			}
			go func() {
				if err := contentEngagementConsumer.Run(backgroundCtx); err != nil && backgroundCtx.Err() == nil {
					log.Printf("Agent Profile content engagement consumer stopped: %v", err)
				}
			}()
			log.Printf("Agent Profile content engagement attribution enabled (window=%s)", profileContentAttributionWindow)
		}
		if profileExperimentManager != nil {
			profileExperimentReconciler, reconcileErr := agentService.NewProfileExperimentReconciler(
				profileExperimentManager,
				profileAdministratorIDs[0],
				getEnvDuration(agentProfile.ExperimentIntervalEnv, agentService.DefaultProfileExperimentReconcileInterval),
			)
			if reconcileErr != nil {
				log.Fatalf("initialize Agent Profile experiment reconciler: %v", reconcileErr)
			}
			go profileExperimentReconciler.Run(backgroundCtx)
		}
		go agentService.NewToolGovernanceReconciler(
			repo,
			getEnvDuration("AGENT_TOOL_RECONCILE_INTERVAL", 30*time.Second),
			toolMetrics,
		).Run(backgroundCtx)
		go agentService.NewWorkflowCompensationReconciler(
			svc,
			repo,
			getEnvDuration("AGENT_WORKFLOW_COMPENSATION_RECONCILE_INTERVAL", 30*time.Second),
			getEnvPositiveInt("AGENT_WORKFLOW_COMPENSATION_RECONCILE_BATCH_SIZE", 50),
			toolMetrics,
		).Run(backgroundCtx)

		// Temporal remains the sole production scheduler for risk control and
		// trending reports. If it is unavailable, reporting stays disabled.
		temporalHost := getEnv("TEMPORAL_HOST", "localhost:7233")
		temporalClient, temporalErr := client.Dial(client.Options{HostPort: temporalHost})
		if temporalErr != nil {
			log.Printf(
				"Agent background component disabled: component=temporal host=%s reason=connection_failed error=%v",
				temporalHost,
				temporalErr,
			)
			if processPlan.TrendingReporterOwner() == agentStartup.TrendingReporterOwnerTemporal {
				log.Println("Agent background component disabled: component=trending_reporter owner=temporal reason=temporal_unavailable")
			}
		} else {
			log.Printf("✅ Connected to Temporal Server at %s", temporalHost)
			defer temporalClient.Close()

			chatModelCheap := getEnv("LM_STUDIO_MODEL_CHAT", "qwen2.5-3b-instruct")
			chatModelPremium := getEnv("PREMIUM_AI_MODEL_CHAT", "qwen-max")
			botUserIDStr := getEnv("TRENDING_BOT_USER_ID", "100")
			botUserID, _ := strconv.ParseUint(botUserIDStr, 10, 64)
			if botUserID == 0 {
				botUserID = 100
			}
			embeddingModel = getEnv("LM_STUDIO_MODEL_EMBEDDING", "text-embedding-bge-m3")
			activities := agentService.NewAgentActivities(
				redisClient,
				esClient,
				qdrantClient,
				aiClient,
				tweetClient,
				embeddingModel,
				chatModelCheap,
				chatModelPremium,
				botUserID,
			)

			temporalTaskWorker = worker.New(temporalClient, "AGENT_TASK_QUEUE", worker.Options{})
			temporalTaskWorker.RegisterWorkflow(agentService.TweetRiskControlWorkflow)
			activeReporterOwner := processPlan.ActiveTrendingReporterOwner(true)
			if activeReporterOwner == agentStartup.TrendingReporterOwnerTemporal {
				temporalTaskWorker.RegisterWorkflow(agentService.TrendingReporterWorkflow)
			}
			temporalTaskWorker.RegisterActivity(activities)
			if err := temporalTaskWorker.Start(); err != nil {
				log.Fatalf("❌ Temporal Worker failed to start: %v", err)
			}
			log.Println("👷 Temporal Worker started")

			riskControlMetrics, metricsErr := agentService.NewPrometheusRiskControlObserver(prometheus.DefaultRegisterer)
			if metricsErr != nil {
				log.Fatalf("initialize Agent risk-control metrics: %v", metricsErr)
			}
			riskControl, riskErr := agentService.NewRiskControl(riskMQClient, temporalClient, riskControlMetrics)
			if riskErr != nil {
				log.Fatalf("initialize Agent risk-control consumer: %v", riskErr)
			}
			go func() {
				if runErr := riskControl.Run(backgroundCtx); runErr != nil && backgroundCtx.Err() == nil {
					log.Printf("Agent risk-control consumer stopped: %v", runErr)
				}
			}()

			if activeReporterOwner == agentStartup.TrendingReporterOwnerTemporal {
				reporterOptions := client.StartWorkflowOptions{
					ID:        "TrendingReporter-Sentinel",
					TaskQueue: "AGENT_TASK_QUEUE",
				}
				_, reporterErr := temporalClient.ExecuteWorkflow(
					backgroundCtx,
					reporterOptions,
					agentService.TrendingReporterWorkflow,
					getEnvDuration("AGENT_TRENDING_REPORTER_INTERVAL", time.Minute),
				)
				if reporterErr != nil {
					if !temporal.IsWorkflowExecutionAlreadyStartedError(reporterErr) {
						log.Printf("Failed to start Temporal TrendingReporter workflow: %v", reporterErr)
					} else {
						log.Println("Agent background component active: component=trending_reporter owner=temporal state=already_running")
					}
				} else {
					log.Println("Agent background component active: component=trending_reporter owner=temporal state=started")
				}
			} else {
				log.Println("Agent background component disabled: component=trending_reporter owner=disabled reason=configured")
			}
		}
	} else {
		log.Println("Agent background workers disabled for api-only process role")
	}

	var grpcServer *grpc.Server
	if processPlan.StartsAPI() {
		// API processes alone register the discoverable gRPC endpoint.
		consulAddr := getEnv("CONSUL_HOST", "localhost") + ":" + getEnv("CONSUL_PORT", "8500")
		svcRegistry, registryErr := registry.NewConsulRegistry(consulAddr)
		if registryErr != nil {
			log.Printf("⚠️ Failed to connect consul: %v", registryErr)
		} else {
			serviceName := getEnv("SERVICE_NAME", "agent-service")
			serviceAddr := getLocalIP()
			if serviceAddr == "" {
				serviceAddr = getEnv("SERVICE_ADDR", "localhost")
			}
			servicePortStr := getEnv("SERVICE_PORT", "9100")
			servicePort, _ := strconv.Atoi(servicePortStr)
			hostname, _ := os.Hostname()
			serviceID := fmt.Sprintf("%s-%s-%s", serviceName, hostname, servicePortStr)

			if registerErr := svcRegistry.RegisterService(serviceName, serviceID, serviceAddr, servicePort, []string{"agent", "grpc"}); registerErr != nil {
				log.Printf("❌ Failed to register service: %v", registerErr)
			} else {
				defer svcRegistry.DeregisterService(serviceID)
			}
		}

		grpcServer = grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler()))
		agentServerOptions := []agentGrpc.AgentServerOption{}
		if profileAdminToken != "" {
			agentServerOptions = append(agentServerOptions, agentGrpc.WithProfileAdministration(profileManager, profileAdminToken))
			agentServerOptions = append(agentServerOptions, agentGrpc.WithProfileAccessManager(profileAccessManager))
			agentServerOptions = append(agentServerOptions, agentGrpc.WithProfileDirectPublish(profileDirectPublishEnabled))
			if profileExperimentManager != nil {
				agentServerOptions = append(agentServerOptions, agentGrpc.WithProfileExperimentManager(profileExperimentManager))
			}
		}
		if extensionMarketplaceAdminEnabled {
			agentServerOptions = append(agentServerOptions, agentGrpc.WithExtensionMarketplaceAdministration(
				extensionMarketplaceManager, extensionMarketplaceAdminToken,
			))
		}
		aiAgentv1.RegisterAiAgentServiceServer(grpcServer, agentGrpc.NewAgentServer(svc, agentServerOptions...))
		reflection.Register(grpcServer)

		grpcPort := getEnv("SERVICE_PORT", "9100")
		lis, listenErr := net.Listen("tcp", ":"+grpcPort)
		if listenErr != nil {
			log.Fatalf("❌ Failed to listen: %v", listenErr)
		}

		log.Println("========================================")
		log.Printf("🚀 Agent Service listening on :%s", grpcPort)
		log.Println("📡 gRPC endpoints:")
		log.Println("   - CallApiOfAi          (模式一：直接对话)")
		log.Println("   - ConsultContent        (模式二：RAG 搜索)")
		log.Println("   - AssistPublishTwitter  (模式三：AI 写推文)")
		log.Println("   - ConfirmPublishTwitter (模式三：确认发布)")
		log.Println("   - MultiAgentPublishTwitter (模式四：多Agent协作)")
		log.Println("   - GetRepositoryDialogue (对话历史列表)")
		log.Println("   - GetDialogueDetail     (对话消息详情)")
		log.Println("📦 MongoDB: dialogue persistence enabled")
		log.Println("========================================")

		go func() {
			if serveErr := grpcServer.Serve(lis); serveErr != nil {
				log.Fatalf("❌ Failed to serve: %v", serveErr)
			}
		}()
	} else {
		log.Println("Agent gRPC endpoint and Consul registration disabled for worker-only process role")
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("🛑 Shutting down Agent Service...")
	backgroundCancel()
	if temporalTaskWorker != nil {
		temporalTaskWorker.Stop()
	}
	if processPlan.StartsAPI() {
		mcpShutdownCtx, mcpShutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := mcpServer.Shutdown(mcpShutdownCtx); err != nil {
			log.Printf("failed to stop MCP server: %v", err)
		}
		mcpShutdownCancel()
		grpcServer.GracefulStop()
	}
	if svc != nil {
		svc.Close() // 🆕 关闭 AgentService 关联的 MCP 长连接及生命周期 Context
	}
	if mqClient != nil {
		mqClient.Close()
	}
	if riskMQClient != nil {
		riskMQClient.Close()
	}
	log.Println("✅ Server exited")
}

func registerWorkflowMCPTools(registry *workflowTool.ToolRegistry, svc *agentService.AgentService) {
	definitions := []struct {
		name        string
		mcpName     string
		description string
		schema      string
	}{
		{"SemanticTweetSearch", "search_tweets_by_semantic", "通过 MCP 对平台推文进行语义检索。", `{"type":"object","properties":{"query":{"type":"string"},"size":{"type":"number"}},"required":["query"]}`},
		{"HybridTweetSearch", "hybrid_search_tweets", "通过 MCP 执行 BM25 与向量混合推文检索。", `{"type":"object","properties":{"query":{"type":"string"},"size":{"type":"number"}},"required":["query"]}`},
		{"GetUserTweets", "get_user_tweets", "通过 MCP 获取指定用户的历史推文。", `{"type":"object","properties":{"user_id":{"type":"string"},"limit":{"type":"number"}},"required":["user_id"]}`},
		{"GetTweetsByIDs", "get_tweets_by_ids", "通过 MCP 按 ID 获取推文内容。", `{"type":"object","properties":{"tweet_ids":{"type":"string"}},"required":["tweet_ids"]}`},
		{"SearchUsers", "search_users", "通过 MCP 搜索平台用户。", `{"type":"object","properties":{"keyword":{"type":"string"},"limit":{"type":"number"}},"required":["keyword"]}`},
	}

	for _, definition := range definitions {
		definition := definition
		mustRegisterWorkflowTool(registry, workflowTool.NewDelegatedTool(
			definition.name,
			definition.description,
			definition.schema,
			func(ctx context.Context, inputs map[string]interface{}) (map[string]interface{}, error) {
				return svc.ExecuteWorkflowMCPTool(ctx, definition.mcpName, inputs)
			},
		))
	}
}

func registerWorkflowStrategyTools(registry *workflowTool.ToolRegistry, svc *agentService.AgentService) {
	schema := `{"type":"object","properties":{"objective":{"type":"string"},"plan":{"type":"string"},"system_prompt":{"type":"string"},"allowed_tools":{"type":"string"},"max_iterations":{"type":"number"},"model":{"type":"string"},"max_tokens":{"type":"number"}},"required":["objective"]}`
	for _, strategy := range []string{"ReActAgent", "PlanExecutor"} {
		strategy := strategy
		mustRegisterWorkflowTool(registry, workflowTool.NewDelegatedTool(
			strategy,
			"执行受限、可审计的智能体策略，只允许调用只读 MCP 工具。",
			schema,
			func(ctx context.Context, inputs map[string]interface{}) (map[string]interface{}, error) {
				return svc.ExecuteWorkflowStrategy(ctx, strategy, inputs)
			},
		))
	}
}

func mustRegisterWorkflowTool(registry *workflowTool.ToolRegistry, tool workflowTool.AgentTool) {
	if err := registry.Register(tool); err != nil {
		log.Fatalf("failed to register workflow tool %s: %v", tool.Spec().Name, err)
	}
}

func configureWebSearchProviderFactory() (*agentWebSearch.BraveProviderFactory, error) {
	if !getEnvBool("AGENT_WEB_SEARCH_ENABLED", false) {
		return nil, nil
	}
	provider := strings.ToLower(strings.TrimSpace(getEnv(
		"AGENT_WEB_SEARCH_PROVIDER",
		agentWebSearch.BraveProviderName,
	)))
	switch provider {
	case agentWebSearch.BraveProviderName:
		policy := agentModel.NewEndpointPolicy(
			strings.Split(getEnv("AGENT_WEB_SEARCH_ALLOWED_HOSTS", ""), ",")...,
		)
		return agentWebSearch.NewBraveProviderFactory(agentWebSearch.BraveProviderFactoryConfig{
			Timeout:          getEnvDuration("AGENT_WEB_SEARCH_TIMEOUT", agentWebSearch.DefaultSearchTimeout),
			MaxResults:       getEnvPositiveInt("AGENT_WEB_SEARCH_MAX_RESULTS", agentWebSearch.DefaultMaxSearchResults),
			MaxResponseBytes: int64(getEnvPositiveInt("AGENT_WEB_SEARCH_MAX_RESPONSE_BYTES", int(agentWebSearch.DefaultMaxResponseBytes))),
			MaxConcurrent:    getEnvPositiveInt("AGENT_WEB_SEARCH_MAX_CONCURRENT", agentWebSearch.DefaultMaxConcurrent),
			EndpointPolicy:   policy,
		})
	default:
		return nil, fmt.Errorf("unsupported AGENT_WEB_SEARCH_PROVIDER %q", provider)
	}
}

func configureDefaultWebSearchProvider(
	factory *agentWebSearch.BraveProviderFactory,
) (agentWebSearch.Provider, error) {
	if factory == nil {
		return nil, nil
	}
	apiKey := strings.TrimSpace(getEnv("AGENT_WEB_SEARCH_API_KEY", ""))
	if apiKey == "" {
		return nil, nil
	}
	return factory.New(
		getEnv("AGENT_WEB_SEARCH_BASE_URL", agentWebSearch.DefaultBraveBaseURL),
		apiKey,
	)
}

type webAccessDependencies struct {
	search           agentWebSearch.Provider
	page             agentWebSearch.PageReader
	factory          *agentWebSearch.BraveProviderFactory
	providerResolver *agentWebSearch.AtomicProviderConfigResolver
}

func configureWebAccess(redisClient *redis.Client) (webAccessDependencies, error) {
	factory, err := configureWebSearchProviderFactory()
	if err != nil || factory == nil {
		return webAccessDependencies{}, err
	}
	searchProvider, err := configureDefaultWebSearchProvider(factory)
	if err != nil {
		return webAccessDependencies{}, err
	}
	cacheStore, err := repository.NewRedisWebCache(
		redisClient,
		getEnv("AGENT_WEB_CACHE_PREFIX", repository.DefaultWebCachePrefix),
	)
	if err != nil {
		return webAccessDependencies{}, fmt.Errorf("configure source cache: %w", err)
	}
	governor, err := repository.NewRedisWebAccessGovernor(
		redisClient,
		repository.RedisWebAccessGovernorConfig{
			Prefix:           getEnv("AGENT_WEB_GOVERNOR_PREFIX", repository.DefaultWebGovernorPrefix),
			UserWindow:       getEnvDuration("AGENT_WEB_USER_RATE_WINDOW", repository.DefaultWebUserWindow),
			UserMaxRequests:  getEnvPositiveInt("AGENT_WEB_USER_MAX_REQUESTS", repository.DefaultWebUserMaxRequests),
			RunTTL:           getEnvDuration("AGENT_WEB_RUN_TTL", repository.DefaultWebRunTTL),
			RunMaxRequests:   getEnvPositiveInt("AGENT_WEB_RUN_MAX_REQUESTS", repository.DefaultWebRunMaxRequests),
			RunMaxCostMicros: int64(getEnvPositiveInt("AGENT_WEB_RUN_MAX_COST_MICROS", int(repository.DefaultWebRunMaxCostMicros))),
		},
	)
	if err != nil {
		return webAccessDependencies{}, fmt.Errorf("configure web access governor: %w", err)
	}
	searchCacheTTL := getEnvDuration("AGENT_WEB_SEARCH_CACHE_TTL", 5*time.Minute)
	if searchProvider != nil {
		searchProvider = agentWebSearch.NewCachedProvider(searchProvider, cacheStore, searchCacheTTL)
	}
	providerResolver := agentWebSearch.NewAtomicProviderConfigResolver()
	searchProvider = agentWebSearch.NewTenantRoutingProvider(
		searchProvider,
		providerResolver,
		cacheStore,
		searchCacheTTL,
	)
	searchProvider = agentWebSearch.NewGovernedProvider(
		searchProvider,
		governor,
		getEnvNonNegativeInt64("AGENT_WEB_SEARCH_ESTIMATED_COST_MICROS", 1_000),
	)

	var pageReader agentWebSearch.PageReader
	if getEnvBool("AGENT_WEB_PAGE_READ_ENABLED", true) {
		pagePolicy := agentModel.NewEndpointPolicy(
			strings.Split(getEnv("AGENT_WEB_PAGE_ALLOWED_HOSTS", ""), ",")...,
		)
		pageReader, err = agentWebSearch.NewHTTPPageReader(agentWebSearch.HTTPPageReaderConfig{
			Timeout:          getEnvDuration("AGENT_WEB_PAGE_TIMEOUT", agentWebSearch.DefaultPageTimeout),
			MaxResponseBytes: int64(getEnvPositiveInt("AGENT_WEB_PAGE_MAX_RESPONSE_BYTES", int(agentWebSearch.DefaultMaxPageBytes))),
			MaxContentRunes:  getEnvPositiveInt("AGENT_WEB_PAGE_MAX_CONTENT_RUNES", agentWebSearch.DefaultMaxPageRunes),
			MaxConcurrent:    getEnvPositiveInt("AGENT_WEB_PAGE_MAX_CONCURRENT", agentWebSearch.DefaultMaxPageConcurrent),
			EndpointPolicy:   pagePolicy,
		})
		if err != nil {
			return webAccessDependencies{}, fmt.Errorf("configure page reader: %w", err)
		}
		pageReader = agentWebSearch.NewCachedPageReader(
			pageReader,
			cacheStore,
			getEnvDuration("AGENT_WEB_PAGE_CACHE_TTL", 15*time.Minute),
		)
		pageReader = agentWebSearch.NewGovernedPageReader(
			pageReader,
			governor,
			getEnvNonNegativeInt64("AGENT_WEB_PAGE_ESTIMATED_COST_MICROS", 100),
		)
	}
	return webAccessDependencies{
		search: searchProvider, page: pageReader,
		factory: factory, providerResolver: providerResolver,
	}, nil
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		log.Printf("⚠️ Invalid duration %s=%q, using %s", key, value, fallback)
		return fallback
	}
	return parsed
}

func getEnvPositiveInt(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func getEnvNonNegativeInt64(key string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		log.Printf("invalid %s=%q, using %d", key, value, fallback)
		return fallback
	}
	return parsed
}

func getEnvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		log.Printf("invalid %s=%q, using %t", key, value, fallback)
		return fallback
	}
	return parsed
}

func getEnvFloat64(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < 0 || parsed > 1 {
		log.Printf("invalid %s=%q, using %.2f", key, value, fallback)
		return fallback
	}
	return parsed
}

func parseProfileUserIDs(raw, role string) ([]uint64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	seen := make(map[uint64]struct{})
	result := make([]uint64, 0)
	for _, part := range strings.Split(raw, ",") {
		value := strings.TrimSpace(part)
		userID, err := strconv.ParseUint(value, 10, 64)
		if err != nil || userID == 0 {
			return nil, fmt.Errorf("invalid %s user id %q", role, value)
		}
		if _, exists := seen[userID]; exists {
			continue
		}
		seen[userID] = struct{}{}
		result = append(result, userID)
	}
	return result, nil
}

func resolveMCPAuthToken() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("AGENT_MCP_AUTH_TOKEN")); configured != "" {
		if len(configured) < 32 {
			return "", fmt.Errorf("AGENT_MCP_AUTH_TOKEN must contain at least 32 characters")
		}
		return configured, nil
	}
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate ephemeral MCP authentication token: %w", err)
	}
	log.Println("AGENT_MCP_AUTH_TOKEN is empty; generated an ephemeral process-local token")
	return hex.EncodeToString(value[:]), nil
}

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}
