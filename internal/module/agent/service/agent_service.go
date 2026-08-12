package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
	"twitter-clone/internal/module/agent/attribution"
	agentCredential "twitter-clone/internal/module/agent/credential"
	"twitter-clone/internal/module/agent/marketplace"
	externalmcp "twitter-clone/internal/module/agent/mcp/remote"
	agentMessage "twitter-clone/internal/module/agent/message"
	agentModel "twitter-clone/internal/module/agent/model"
	agentObservability "twitter-clone/internal/module/agent/observability"
	agentProduct "twitter-clone/internal/module/agent/product"
	"twitter-clone/internal/module/agent/profile"
	agentProject "twitter-clone/internal/module/agent/project"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	agentStrategy "twitter-clone/internal/module/agent/strategy"
	agentWebSearch "twitter-clone/internal/module/agent/websearch"
	"twitter-clone/internal/module/agent/workflow/dsl"
	"twitter-clone/internal/module/agent/workflow/rag"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"
	"twitter-clone/pkg/ai"
	"twitter-clone/pkg/config"
	"twitter-clone/pkg/logger"
	platformTrace "twitter-clone/pkg/trace"

	"github.com/go-redis/redis/v8"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sashabaranov/go-openai"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ========================== 常量 ==========================

const (
	// MaxContextMessages 多轮对话最多携带的历史消息数
	// 控制 token 消耗，20 条消息约覆盖最近 10 轮对话
	MaxContextMessages              = 20
	defaultWorkflowSnapshotInterval = 16
)

// ========================== 返回结构体 ==========================

type TweetResult struct {
	TweetID uint64
	URL     string
	Summary string
}

// ChatResult 统一的对话返回结构
type ChatResult struct {
	DialogueID string        // 对话会话 ID（十六进制字符串）
	RunID      string        // Runtime Run ID；Legacy 路径为空
	RunStatus  string        // Runtime lifecycle status；Legacy 路径为空
	Response   string        // AI 回复文本
	Tweets     []TweetResult // 推文搜索结果（模式二时有值）
}

// ========================== AgentService ==========================

// AgentService AI Agent 服务
type AgentService struct {
	llmClient    *openai.Client             // 对话模型客户端
	repo         repository.AgentRepository // 对话持久化仓储
	chatModel    string                     // 对话模型名称
	mcpAddr      string                     // MCP Server 地址
	mcpAuthToken string
	aiClient     *ai.Client    // 降级路由 AI 客户端
	rdb          *redis.Client // 🎯 注入 Redis 客户端支持调优配置写入、广播和冷却锁

	// 🆕 生命周期控制，防止 background context 导致的协程泄漏
	serviceCtx context.Context
	cancelFunc context.CancelFunc

	// 长连接与连接池复用
	mcpClient *client.Client
	mcpTools  []mcp.Tool
	mcpMu     sync.RWMutex

	memoryManager                        *rag.MemoryManager
	cascadeRouter                        *rag.CascadeRouter
	embeddingModel                       string
	runtimeRollout                       agentRuntime.Rollout
	profileResolver                      profile.Resolver
	runtimeRunner                        agentRuntime.AgentRunner
	runtimeTools                         RuntimeToolCatalog
	tweetWriteStateSource                tweetWriteStateSource
	runtimeMessages                      agentMessage.Builder
	runtimeTokens                        agentRuntime.TokenCounter
	runtimeAdmission                     agentRuntime.AdmissionController
	runtimeCostEstimator                 agentRuntime.CostEstimator
	goalRuntimeShadow                    GoalRuntimeShadowConfig
	goalRuntimeShadowObserver            GoalRuntimeShadowObserver
	traceRecorder                        agentObservability.Recorder
	traceReader                          agentObservability.Reader
	traceEventReader                     agentObservability.EventReader
	traceContentSampler                  agentObservability.ContentSampler
	workflowToolExecutor                 *workflowTool.Executor
	workflowToolPublicationStore         repository.WorkflowToolPublicationStore
	workflowAsToolEnabled                bool
	workflowToolCatalogLimit             int
	workflowToolTimeout                  time.Duration
	skillCatalogEnabled                  bool
	skillCatalogLimit                    int
	extensionCatalogEnabled              bool
	extensionCatalogLimit                int
	extensionSkillSource                 AgentExtensionSkillSource
	extensionMCPSource                   AgentExtensionMCPSource
	extensionMarketplaceEnabled          bool
	extensionMarketplaceLimit            int
	extensionMarketplaceStore            marketplace.CatalogStore
	confirmedDraftPublisher              ConfirmedDraftPublisher
	productOutcomeRecorder               ProductOutcomeRecorder
	contentAttributionStore              attribution.Store
	contentAttributionWindow             time.Duration
	workflowSnapshotInterval             uint64
	workflowBudgetDefaults               dsl.BudgetDSL
	workflowCancelPoll                   time.Duration
	workflowEventHeartbeat               time.Duration
	workflowEventWindow                  time.Duration
	providerConfigCipher                 agentCredential.SecretCipher
	providerEndpointPolicy               *agentModel.EndpointPolicy
	webSearchProviderFactory             *agentWebSearch.ProviderFactory
	externalMCPManager                   *externalmcp.Manager
	externalMCPEnabled                   bool
	externalMCPProjectScopeEnabled       bool
	externalMCPManagedCredentialsEnabled bool
	externalMCPManagedCredentials        externalmcp.ManagedCredentialResolver
	externalMCPEndpointPolicy            *agentModel.EndpointPolicy
	externalMCPPoolConfig                externalmcp.ClientPoolConfig
	externalMCPHealthConfig              externalmcp.HealthCheckConfig
	externalMCPPoolObserver              externalmcp.PoolObserver
	externalMCPHealthObserver            externalmcp.HealthObserver
	capabilityCatalog                    AgentCapabilityCatalog
	capabilityPlanner                    AgentCapabilityPlanner
	aiOpsReportSink                      AIOpsReportSink
	executionStrategyPlanner             agentStrategy.Planner
	multiAgentExecutionEnabled           bool
	agentExecutionRunStore               repository.AgentExecutionRunStore
	agentRunAccountingStore              repository.AgentRunAccountingStore
	recoverableAgentRuns                 bool
	agentTaskTemplateStore               repository.AgentTaskTemplateStore
	agentTaskTemplatesEnabled            bool
	agentTaskTemplateListLimit           int
	unifiedAgentProductObserver          UnifiedAgentProductObserver
	productEventStore                    agentProduct.Store
	externalMCPProductObserver           externalmcp.ProductObserver
	agentCheckpointCipher                agentCredential.SecretCipher
	agentCheckpointMaxBytes              int
	agentResumeLeaseDuration             time.Duration
	unifiedAgentApprovalRecovery         bool
	agentProjectManager                  *agentProject.Manager

	summaryWriter        SessionSummaryWriter
	summaryMinMessages   int64
	summaryIdleDelay     time.Duration
	summaryLeaseDuration time.Duration
	summaryMu            sync.Mutex
	summaryTimers        map[primitive.ObjectID]*time.Timer
	summaryJobs          map[primitive.ObjectID]map[string]context.CancelFunc
}

// Option configures AgentService without expanding its infrastructure-heavy
// constructor signature for every cross-cutting runtime concern.
type Option func(*AgentService)

// WithRuntimeRollout injects the immutable Runtime v2 rollout snapshot.
func WithRuntimeRollout(rollout agentRuntime.Rollout) Option {
	return func(service *AgentService) {
		service.runtimeRollout = rollout
	}
}

// WithProfileResolver injects an immutable Profile release snapshot. Profile
// selection stays independent from model/provider routing and Runtime rollout.
func WithProfileResolver(resolver profile.Resolver) Option {
	return func(service *AgentService) {
		service.profileResolver = resolver
	}
}

// WithAgentRunner replaces the default Runtime runner. It is primarily used
// for offline tests and allows future runners to be injected without changing
// the service API.
func WithAgentRunner(runner agentRuntime.AgentRunner) Option {
	return func(service *AgentService) {
		service.runtimeRunner = runner
	}
}

// WithGoalRuntimeShadow configures observation-only Goal verification over an
// already executed Runtime result. It does not replace the production runner.
func WithGoalRuntimeShadow(
	config GoalRuntimeShadowConfig,
	observer GoalRuntimeShadowObserver,
) Option {
	return func(service *AgentService) {
		service.goalRuntimeShadow = config
		service.goalRuntimeShadowObserver = observer
	}
}

// WithRuntimeToolCatalog replaces MCP tool discovery for tests or future
// registry implementations without changing Runtime execution contracts.
func WithRuntimeToolCatalog(catalog RuntimeToolCatalog) Option {
	return func(service *AgentService) {
		service.runtimeTools = catalog
	}
}

// WithRuntimeMessageBuilder replaces context assembly independently from the
// runner, allowing tokenizer- or provider-specific builders to be introduced.
func WithRuntimeMessageBuilder(builder agentMessage.Builder) Option {
	return func(service *AgentService) {
		service.runtimeMessages = builder
	}
}

// WithRuntimeAdmission replaces the run admission policy. Production uses a
// process-local limiter; a shared Redis implementation can be injected later.
func WithRuntimeAdmission(controller agentRuntime.AdmissionController) Option {
	return func(service *AgentService) {
		service.runtimeAdmission = controller
	}
}

// WithExecutionTraceStore injects persistence independently from Runtime,
// Workflow Engine and the Mongo-backed reader used by the control plane.
func WithExecutionTraceStore(recorder agentObservability.Recorder, reader agentObservability.Reader) Option {
	return func(service *AgentService) {
		if recorder != nil {
			service.traceRecorder = recorder
		}
		if reader != nil {
			service.traceReader = reader
		}
	}
}

// WithTraceContentSampler injects the independently governed Prompt and
// Completion preview policy. A nil sampler preserves hash-only tracing.
func WithTraceContentSampler(sampler agentObservability.ContentSampler) Option {
	return func(service *AgentService) {
		service.traceContentSampler = sampler
	}
}

// WithExecutionEventStore injects the bounded delivery channel used by run
// monitors. Durable trace queries remain owned by the execution trace reader.
func WithExecutionEventStore(reader agentObservability.EventReader) Option {
	return func(service *AgentService) {
		service.traceEventReader = reader
	}
}

// WithWorkflowEventStreamPolicy configures idle heartbeats and the maximum
// lifetime of one server stream. Clients resume with the last event cursor.
func WithWorkflowEventStreamPolicy(heartbeat, window time.Duration) Option {
	return func(service *AgentService) {
		if heartbeat > 0 {
			service.workflowEventHeartbeat = heartbeat
		}
		if window > 0 {
			service.workflowEventWindow = window
		}
	}
}

// WithWorkflowToolExecutor injects the single governed tool boundary shared
// by workflow nodes and model-driven MCP execution.
func WithWorkflowToolExecutor(executor *workflowTool.Executor) Option {
	return func(service *AgentService) {
		service.workflowToolExecutor = executor
	}
}

// WithWorkflowToolPublications configures the control-plane store separately
// from Runtime exposure. Disabling the feature preserves publication metadata
// so a rollback never mutates user configuration.
func WithWorkflowToolPublications(
	store repository.WorkflowToolPublicationStore,
	enabled bool,
	catalogLimit int,
	timeout time.Duration,
) Option {
	return func(service *AgentService) {
		service.workflowToolPublicationStore = store
		service.workflowAsToolEnabled = enabled
		if catalogLimit > 0 {
			service.workflowToolCatalogLimit = catalogLimit
		}
		if timeout > 0 {
			service.workflowToolTimeout = timeout
		}
	}
}

// WithWorkflowSkillCatalog exposes immutable Skill projections independently
// from Workflow-as-Tool publication. Disabling it removes discovery and
// execution routes without mutating any publication.
func WithWorkflowSkillCatalog(enabled bool, catalogLimit int) Option {
	return func(service *AgentService) {
		service.skillCatalogEnabled = enabled
		if catalogLimit > 0 {
			service.skillCatalogLimit = catalogLimit
		}
	}
}

// WithAgentExtensionCatalog exposes one credential-free directory assembled
// from the existing capability, Skill and governed MCP control planes. It does
// not grant execution rights or change any source configuration.
func WithAgentExtensionCatalog(enabled bool, catalogLimit int) Option {
	return func(service *AgentService) {
		service.extensionCatalogEnabled = enabled
		if catalogLimit > 0 && catalogLimit <= maxAgentExtensionCatalogLimit {
			service.extensionCatalogLimit = catalogLimit
		}
	}
}

// WithAgentExtensionSources replaces source adapters for isolated tests or a
// future catalog backend. Runtime execution continues to resolve every Skill
// and MCP tool through its authoritative service.
func WithAgentExtensionSources(
	skillSource AgentExtensionSkillSource,
	mcpSource AgentExtensionMCPSource,
) Option {
	return func(service *AgentService) {
		service.extensionSkillSource = skillSource
		service.extensionMCPSource = mcpSource
	}
}

// WithAgentExtensionMarketplace enables a credential-free public release
// catalog. It does not install packages or grant any Runtime permission.
func WithAgentExtensionMarketplace(
	store marketplace.CatalogStore,
	enabled bool,
	catalogLimit int,
) Option {
	return func(service *AgentService) {
		service.extensionMarketplaceStore = store
		service.extensionMarketplaceEnabled = enabled
		if catalogLimit > 0 && catalogLimit <= marketplace.MaxPageSize {
			service.extensionMarketplaceLimit = catalogLimit
		}
	}
}

// WithConfirmedDraftPublisher injects the explicit, user-confirmed publish
// boundary. It is intentionally separate from model-driven Tool execution.
func WithConfirmedDraftPublisher(publisher ConfirmedDraftPublisher) Option {
	return func(service *AgentService) {
		service.confirmedDraftPublisher = publisher
	}
}

// WithProductOutcomeRecorder connects explicit product actions to Profile
// experiment observations without making normal publishing depend on experiments.
func WithProductOutcomeRecorder(recorder ProductOutcomeRecorder) Option {
	return func(service *AgentService) {
		service.productOutcomeRecorder = recorder
	}
}

// WithContentAttribution enables short-lived tweet-to-Run attribution for
// trusted external engagement events. The store owns no tweet content.
func WithContentAttribution(store attribution.Store, window time.Duration) Option {
	return func(service *AgentService) {
		if store != nil {
			service.contentAttributionStore = store
		}
		if window > 0 {
			service.contentAttributionWindow = window
		}
	}
}

// WithWorkflowSnapshotInterval configures periodic materialization by applied
// state-event count. Zero disables periodic snapshots but final boundaries are
// still persisted.
func WithWorkflowSnapshotInterval(interval uint64) Option {
	return func(service *AgentService) {
		service.workflowSnapshotInterval = interval
	}
}

func WithWorkflowBudgetDefaults(budget dsl.BudgetDSL) Option {
	return func(service *AgentService) {
		service.workflowBudgetDefaults = budget
	}
}

func WithWorkflowCancellationPollInterval(interval time.Duration) Option {
	return func(service *AgentService) {
		if interval > 0 {
			service.workflowCancelPoll = interval
		}
	}
}

func WithMCPAuthToken(token string) Option {
	return func(service *AgentService) {
		service.mcpAuthToken = strings.TrimSpace(token)
	}
}

func WithProviderConfigCipher(cipher agentCredential.SecretCipher) Option {
	return func(service *AgentService) {
		service.providerConfigCipher = cipher
	}
}

func WithProviderEndpointPolicy(policy *agentModel.EndpointPolicy) Option {
	return func(service *AgentService) {
		service.providerEndpointPolicy = policy
	}
}

func WithWebSearchProviderFactory(factory *agentWebSearch.ProviderFactory) Option {
	return func(service *AgentService) {
		service.webSearchProviderFactory = factory
	}
}

// WithExternalMCPEnabled controls only remote MCP discovery and execution.
// Stored connection metadata remains available for rollback and inspection.
func WithExternalMCPEnabled(enabled bool) Option {
	return func(service *AgentService) {
		service.externalMCPEnabled = enabled
	}
}

func WithExternalMCPProjectScope(enabled bool) Option {
	return func(service *AgentService) {
		service.externalMCPProjectScopeEnabled = enabled
	}
}

func WithExternalMCPManagedCredentials(
	enabled bool,
	resolver externalmcp.ManagedCredentialResolver,
) Option {
	return func(service *AgentService) {
		service.externalMCPManagedCredentialsEnabled = enabled
		service.externalMCPManagedCredentials = resolver
	}
}

func WithAgentProjectManager(manager *agentProject.Manager) Option {
	return func(service *AgentService) {
		service.agentProjectManager = manager
	}
}

func WithExternalMCPEndpointPolicy(policy *agentModel.EndpointPolicy) Option {
	return func(service *AgentService) {
		service.externalMCPEndpointPolicy = policy
	}
}

func WithExternalMCPClientPool(config externalmcp.ClientPoolConfig, observer externalmcp.PoolObserver) Option {
	return func(service *AgentService) {
		service.externalMCPPoolConfig = config
		service.externalMCPPoolObserver = observer
	}
}

func WithExternalMCPHealthChecks(config externalmcp.HealthCheckConfig, observer externalmcp.HealthObserver) Option {
	return func(service *AgentService) {
		service.externalMCPHealthConfig = config
		service.externalMCPHealthObserver = observer
	}
}

// WithExternalMCPManager supports isolated tests and future connector
// implementations without coupling AgentService to the MCP SDK.
func WithExternalMCPManager(manager *externalmcp.Manager) Option {
	return func(service *AgentService) {
		service.externalMCPManager = manager
	}
}

// WithSessionSummaryPolicy configures the incremental threshold and idle
// boundary used to crystallize a dialogue. It is primarily useful for tests
// and deployments with a different conversation cadence.
func WithSessionSummaryPolicy(minPendingMessages int64, idleDelay time.Duration) Option {
	return func(service *AgentService) {
		if minPendingMessages > 0 {
			service.summaryMinMessages = minPendingMessages
		}
		if idleDelay > 0 {
			service.summaryIdleDelay = idleDelay
		}
	}
}

// WithSessionSummaryWriter replaces the memory sink independently from the
// scheduler. Production uses MemoryManager; tests or future event adapters can
// provide another implementation without changing dialogue persistence.
func WithSessionSummaryWriter(writer SessionSummaryWriter) Option {
	return func(service *AgentService) {
		service.summaryWriter = writer
	}
}

// WithAgentCapabilityPlanner replaces the explicit capability planner.
// Planner output remains a preference decision; tool authorization continues
// to be enforced by the catalog, profile, policy, budget and approval layers.
func WithAgentCapabilityPlanner(planner AgentCapabilityPlanner) Option {
	return func(service *AgentService) {
		service.capabilityPlanner = planner
	}
}

// WithAgentExecutionStrategyPlanner replaces the deterministic strategy
// admission planner. It cannot grant tools or bypass Runtime budgets.
func WithAgentExecutionStrategyPlanner(planner agentStrategy.Planner) Option {
	return func(service *AgentService) {
		service.executionStrategyPlanner = planner
	}
}

// WithMultiAgentExecution enables the bounded aggregate executor separately
// from strategy admission. Disabling it leaves all requests on the existing
// single-Agent execution paths.
func WithMultiAgentExecution(enabled bool) Option {
	return func(service *AgentService) {
		service.multiAgentExecutionEnabled = enabled
	}
}

// WithAgentCapabilityCatalog replaces the immutable product capability
// snapshot. It controls executable routes, not tool authorization.
func WithAgentCapabilityCatalog(catalog AgentCapabilityCatalog) Option {
	return func(service *AgentService) {
		service.capabilityCatalog = catalog
	}
}

// WithAgentExecutionRunStore injects the authoritative lifecycle store used
// by the Unified Agent. It is independent from trace and Workflow persistence.
func WithAgentExecutionRunStore(store repository.AgentExecutionRunStore) Option {
	return func(service *AgentService) {
		service.agentExecutionRunStore = store
	}
}

// WithAgentRunAccountingStore injects the narrow read model for direct child
// Workflow runs without coupling lifecycle writes to Workflow persistence.
func WithAgentRunAccountingStore(store repository.AgentRunAccountingStore) Option {
	return func(service *AgentService) {
		service.agentRunAccountingStore = store
	}
}

// WithRecoverableAgentRuns enables durable Unified Agent lifecycle writes.
// The first rollout persists lifecycle state only; resumable checkpoints stay
// disabled until their versioned schema and replay authorization are enabled.
func WithRecoverableAgentRuns(enabled bool) Option {
	return func(service *AgentService) {
		service.recoverableAgentRuns = enabled
	}
}

// WithUnifiedAgentProductObserver connects low-cardinality product metrics to
// successful authoritative Run transitions. Metrics remain outside lifecycle
// persistence and cannot make a Run commit fail.
func WithUnifiedAgentProductObserver(observer UnifiedAgentProductObserver) Option {
	return func(service *AgentService) {
		service.unifiedAgentProductObserver = observer
	}
}

// WithAgentProductEvents connects append-only idempotent product facts to
// their low-cardinality observers. Product persistence never grants runtime
// permissions and remains independent from execution traces.
func WithAgentProductEvents(
	store agentProduct.Store,
	externalMCPObserver externalmcp.ProductObserver,
) Option {
	return func(service *AgentService) {
		service.productEventStore = store
		service.externalMCPProductObserver = externalMCPObserver
	}
}

// WithAgentTaskTemplates configures explicit reusable task presets separately
// from Workflow DAGs. Disabling execution keeps stored templates available for
// read-only listing and archival during rollback.
func WithAgentTaskTemplates(
	store repository.AgentTaskTemplateStore,
	enabled bool,
	listLimit int,
) Option {
	return func(service *AgentService) {
		service.agentTaskTemplateStore = store
		service.agentTaskTemplatesEnabled = enabled
		if listLimit > 0 {
			service.agentTaskTemplateListLimit = listLimit
		}
	}
}

// WithUnifiedAgentApprovalRecovery enables governed risky/write MCP tools in
// the Unified Agent. It is deliberately independent and defaults to false so
// deployments can roll back without changing stored approvals or checkpoints.
func WithUnifiedAgentApprovalRecovery(enabled bool) Option {
	return func(service *AgentService) {
		service.unifiedAgentApprovalRecovery = enabled
	}
}

// WithAgentRunRecovery configures durable Runtime Checkpoint encryption and
// resume claims independently from Provider credential encryption.
func WithAgentRunRecovery(
	cipher agentCredential.SecretCipher,
	maxCheckpointBytes int,
	resumeLeaseDuration time.Duration,
) Option {
	return func(service *AgentService) {
		service.agentCheckpointCipher = cipher
		if maxCheckpointBytes > 0 {
			service.agentCheckpointMaxBytes = maxCheckpointBytes
		}
		if resumeLeaseDuration > 0 {
			service.agentResumeLeaseDuration = resumeLeaseDuration
		}
	}
}

// NewAgentService 创建 Agent 服务
func NewAgentService(
	llmBaseURL string,
	llmAPIKey string,
	chatModel string,
	mcpAddr string,
	repo repository.AgentRepository,
	aiClient *ai.Client,
	rdb *redis.Client,
	options ...Option,
) *AgentService {
	config := openai.DefaultConfig(llmAPIKey)
	config.BaseURL = llmBaseURL
	config.HTTPClient = platformTrace.InstrumentHTTPClient(nil, "agent.provider.http", nil)

	ctx, cancel := context.WithCancel(context.Background())

	service := &AgentService{
		llmClient:            openai.NewClientWithConfig(config),
		chatModel:            chatModel,
		mcpAddr:              mcpAddr,
		repo:                 repo,
		aiClient:             aiClient,
		rdb:                  rdb,
		serviceCtx:           ctx,
		cancelFunc:           cancel,
		summaryMinMessages:   defaultSessionSummaryMinMessages,
		summaryIdleDelay:     defaultSessionSummaryIdleDelay,
		summaryLeaseDuration: defaultSessionSummaryLeaseDuration,
		summaryTimers:        make(map[primitive.ObjectID]*time.Timer),
		summaryJobs:          make(map[primitive.ObjectID]map[string]context.CancelFunc),
		runtimeAdmission: agentRuntime.NewInMemoryConcurrencyLimiter(agentRuntime.ConcurrencyLimits{
			MaxPerUser:     envPositiveInt("AGENT_MAX_CONCURRENT_RUNS_PER_USER", 4),
			MaxPerWorkflow: envPositiveInt("AGENT_MAX_CONCURRENT_RUNS_PER_WORKFLOW", 2),
		}),
		workflowSnapshotInterval:   uint64(envPositiveInt("AGENT_WORKFLOW_SNAPSHOT_EVENT_INTERVAL", defaultWorkflowSnapshotInterval)),
		workflowCancelPoll:         envWorkflowDuration("AGENT_WORKFLOW_CANCEL_POLL_INTERVAL", defaultWorkflowCancellationPollInterval),
		workflowEventHeartbeat:     envWorkflowDuration("AGENT_WORKFLOW_EVENT_HEARTBEAT", defaultWorkflowEventHeartbeat),
		workflowEventWindow:        envWorkflowDuration("AGENT_WORKFLOW_EVENT_STREAM_WINDOW", defaultWorkflowEventStreamWindow),
		contentAttributionWindow:   DefaultContentAttributionWindow,
		agentCheckpointMaxBytes:    DefaultAgentRunCheckpointMaxBytes,
		agentResumeLeaseDuration:   DefaultAgentRunResumeLeaseDuration,
		workflowToolCatalogLimit:   defaultWorkflowToolCatalogLimit,
		workflowToolTimeout:        defaultWorkflowToolTimeout,
		skillCatalogLimit:          defaultAgentSkillCatalogLimit,
		extensionCatalogLimit:      defaultAgentExtensionCatalogLimit,
		extensionMarketplaceLimit:  marketplace.DefaultPageSize,
		agentTaskTemplateListLimit: defaultAgentTaskTemplateListLimit,
		workflowBudgetDefaults: dsl.BudgetDSL{
			MaxNodeExecutions:      envPositiveInt("AGENT_WORKFLOW_MAX_NODE_EXECUTIONS", 50),
			MaxParallelNodes:       envPositiveInt("AGENT_WORKFLOW_MAX_PARALLEL_NODES", 8),
			TimeoutSec:             envPositiveInt("AGENT_WORKFLOW_TIMEOUT_SEC", 300),
			MaxTotalTokens:         envPositiveInt("AGENT_WORKFLOW_MAX_TOTAL_TOKENS", 120_000),
			MaxEstimatedCostMicros: envNonNegativeInt64("AGENT_WORKFLOW_MAX_ESTIMATED_COST_MICROS", 0),
		},
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	if service.runtimeTokens == nil {
		service.runtimeTokens = agentRuntime.NewHeuristicTokenCounter()
	}
	if service.profileResolver == nil {
		resolver, err := NewBuiltInProfileResolver(nil)
		if err != nil {
			panic(fmt.Sprintf("invalid built-in agent profiles: %v", err))
		}
		service.profileResolver = resolver
	}
	if service.traceRecorder == nil {
		service.traceRecorder = agentObservability.NoopRecorder{}
	}
	if service.unifiedAgentProductObserver == nil {
		service.unifiedAgentProductObserver = noopUnifiedAgentProductObserver{}
	}
	if service.goalRuntimeShadowObserver == nil {
		service.goalRuntimeShadowObserver = noopGoalRuntimeShadowObserver{}
	}
	if service.providerConfigCipher == nil {
		service.providerConfigCipher, _ = agentCredential.NewAESGCMCipherFromEnv()
	}
	if service.providerEndpointPolicy == nil {
		service.providerEndpointPolicy = agentModel.NewEndpointPolicy(strings.Split(os.Getenv("AGENT_LLM_ALLOWED_HOSTS"), ",")...)
	}
	if service.externalMCPManager == nil {
		if store, ok := repo.(externalmcp.Store); ok {
			if service.externalMCPEndpointPolicy == nil {
				service.externalMCPEndpointPolicy = agentModel.NewEndpointPolicy(
					strings.Split(os.Getenv("AGENT_EXTERNAL_MCP_ALLOWED_HOSTS"), ",")...,
				)
			}
			service.externalMCPManager = externalmcp.NewManager(
				store,
				service.providerConfigCipher,
				service.externalMCPEndpointPolicy,
				externalmcp.NewSDKDiscoverer(
					service.externalMCPEndpointPolicy,
					20*time.Second,
					externalmcp.WithClientPool(service.externalMCPPoolConfig),
					externalmcp.WithPoolObserver(service.externalMCPPoolObserver),
				),
				externalmcp.WithEnabled(service.externalMCPEnabled),
				externalmcp.WithProjectScope(service.externalMCPProjectScopeEnabled, service.agentProjectManager),
				externalmcp.WithManagedCredentials(
					service.externalMCPManagedCredentialsEnabled,
					service.externalMCPManagedCredentials,
				),
				externalmcp.WithHealthChecks(service.externalMCPHealthConfig),
				externalmcp.WithHealthObserver(service.externalMCPHealthObserver),
			)
		}
	}
	if service.capabilityCatalog == nil {
		capabilityOptions := make([]BuiltInAgentCapabilityCatalogOption, 0, 1)
		if service.externalMCPEnabled && service.externalMCPManager != nil {
			capabilityOptions = append(capabilityOptions, WithAvailableExternalMCPCapability())
		}
		catalog, err := NewBuiltInAgentCapabilityCatalog(capabilityOptions...)
		if err != nil {
			panic(fmt.Sprintf("invalid built-in agent capability catalog: %v", err))
		}
		service.capabilityCatalog = catalog
	}
	if service.extensionSkillSource == nil {
		service.extensionSkillSource = workflowSkillExtensionSource{service: service}
	}
	if service.extensionMCPSource == nil && service.externalMCPManager != nil {
		service.extensionMCPSource = service.externalMCPManager
	}
	if service.capabilityPlanner == nil {
		service.capabilityPlanner = NewExplicitCapabilityPlanner(service.capabilityCatalog)
	}
	if service.executionStrategyPlanner == nil {
		planner, err := NewBuiltInAgentExecutionStrategyPlanner(agentStrategy.Policy{})
		if err != nil {
			panic(fmt.Sprintf("invalid built-in agent execution strategy planner: %v", err))
		}
		service.executionStrategyPlanner = planner
	}
	if service.runtimeMessages == nil {
		service.runtimeMessages = agentMessage.NewBuilder(service.runtimeTokens, nil)
	}
	if service.workflowToolExecutor == nil {
		service.workflowToolExecutor = workflowTool.NewExecutor(workflowTool.NewRegistry())
	}
	if service.runtimeTools == nil {
		service.runtimeTools = &mcpRuntimeToolCatalog{service: service}
	}
	if service.runtimeRunner == nil {
		modelClient := agentRuntime.ModelClient(agentModel.NewOpenAICompatibleClient(service.llmClient, chatModel, "openai-compatible"))
		runnerOptions := []agentRuntime.ReActRunnerOption{
			agentRuntime.WithTokenCounter(service.runtimeTokens),
			agentRuntime.WithAdmissionController(service.runtimeAdmission),
		}
		if router, err := buildDefaultProviderRouter(service.llmClient, chatModel); err != nil {
			log.Printf("warning: model catalog router unavailable, using compatibility client: %v", err)
		} else {
			modelClient = router
			service.runtimeCostEstimator = router
			runnerOptions = append(runnerOptions, agentRuntime.WithCostEstimator(router))
		}
		service.runtimeRunner = agentRuntime.NewReActRunner(
			&tracingModelClient{
				delegate: modelClient, recorder: service.traceRecorder,
				sampler: service.traceContentSampler, now: time.Now,
			},
			&mcpRuntimeToolExecutor{service: service},
			nil,
			runnerOptions...,
		)
	}
	if service.externalMCPManager != nil {
		service.externalMCPManager.Start(service.serviceCtx)
	}
	return service
}

func (s *AgentService) RuntimeCostEstimator() agentRuntime.CostEstimator {
	if s == nil {
		return nil
	}
	return s.runtimeCostEstimator
}

// RuntimeV2Enabled is the migration seam used by entry points as they move to
// the unified runtime. It is false for every mode unless explicitly enabled.
func (s *AgentService) RuntimeV2Enabled(mode agentRuntime.Mode) bool {
	return s != nil && s.runtimeRollout.Enabled(mode)
}

// Close 优雅关闭方法，通知所有绑定的长连接和协程安全退出
func (s *AgentService) Close() {
	if s.cancelFunc != nil {
		s.cancelFunc()
	}
	s.stopSessionSummaryTimers()
	if s.externalMCPManager != nil {
		_ = s.externalMCPManager.Close()
	}
	s.resetMCPClient()
}

func resolveDialogueKey(dialogueID uint64, dialogueKey string) string {
	dialogueKey = strings.TrimSpace(dialogueKey)
	if dialogueKey != "" && dialogueKey != "0" {
		return dialogueKey
	}
	if dialogueID > 0 {
		return fmt.Sprintf("%024x", dialogueID)
	}
	return ""
}

// ========================== 对话上下文辅助方法 ==========================

// getOrCreateDialogue 获取已有对话或创建新对话
// dialogueIDHex 为空字符串时创建新对话，否则加载已有对话
func (s *AgentService) getOrCreateDialogue(ctx context.Context, userID uint64, dialogueIDHex string, firstMessage string, mode repository.DialogueMode) (*repository.Dialogue, error) {
	if dialogueIDHex != "" && dialogueIDHex != "0" {
		oid, err := primitive.ObjectIDFromHex(dialogueIDHex)
		if err != nil {
			return nil, fmt.Errorf("invalid dialogue_id: %w", err)
		}
		dialogue, err := s.repo.GetDialogue(ctx, oid)
		if err != nil {
			return nil, fmt.Errorf("get dialogue failed: %w", err)
		}
		// 验证对话归属
		if dialogue.UserID != userID {
			return nil, fmt.Errorf("dialogue does not belong to user %d", userID)
		}
		return dialogue, nil
	}

	// 创建新对话
	title := repository.GenerateTitle(firstMessage)
	dialogue, err := s.repo.CreateDialogue(ctx, userID, title, mode)
	if err != nil {
		return nil, fmt.Errorf("create dialogue failed: %w", err)
	}
	return dialogue, nil
}

// loadContextMessages 加载历史消息并转换为 OpenAI 格式
func (s *AgentService) loadContextMessages(ctx context.Context, dialogueID primitive.ObjectID) ([]openai.ChatCompletionMessage, error) {
	recentMsgs, err := s.repo.GetRecentMessages(ctx, dialogueID, MaxContextMessages)
	if err != nil {
		return nil, fmt.Errorf("load context messages failed: %w", err)
	}

	messages := make([]openai.ChatCompletionMessage, 0, len(recentMsgs))
	for _, msg := range recentMsgs {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
		})
	}
	return messages, nil
}

// saveUserAndAssistantMessages 保存用户问题和 AI 回复到 MongoDB
func (s *AgentService) saveUserAndAssistantMessages(ctx context.Context, dialogueID primitive.ObjectID, userID uint64, userContent string, assistantContent string, metadata map[string]any) error {
	msgs := []*repository.DialogueMessage{
		{
			DialogueID: dialogueID,
			UserID:     userID,
			Role:       repository.RoleUser,
			Content:    userContent,
		},
		{
			DialogueID: dialogueID,
			UserID:     userID,
			Role:       repository.RoleAssistant,
			Content:    assistantContent,
			Metadata:   metadata,
		},
	}

	if err := s.repo.SaveMessages(ctx, msgs); err != nil {
		return fmt.Errorf("save messages failed: %w", err)
	}

	// 更新对话的 updated_at
	if err := s.repo.TouchDialogue(ctx, dialogueID); err != nil {
		logger.Warn(ctx, "touch dialogue failed", zap.Error(err))
	}
	s.scheduleSessionSummary(userID, dialogueID)

	return nil
}

func (s *AgentService) saveAssistantMessage(
	ctx context.Context,
	dialogueID primitive.ObjectID,
	userID uint64,
	content string,
	metadata map[string]any,
) error {
	if err := s.repo.SaveMessage(ctx, &repository.DialogueMessage{
		DialogueID: dialogueID,
		UserID:     userID,
		Role:       repository.RoleAssistant,
		Content:    content,
		Metadata:   metadata,
	}); err != nil {
		return fmt.Errorf("save assistant message failed: %w", err)
	}
	if err := s.repo.TouchDialogue(ctx, dialogueID); err != nil {
		logger.Warn(ctx, "touch dialogue failed", zap.Error(err))
	}
	s.scheduleSessionSummary(userID, dialogueID)
	return nil
}

// ========================== 对话历史查询 ==========================

// ListDialogues 获取用户对话列表
func (s *AgentService) ListDialogues(ctx context.Context, userID uint64, page, pageSize int) ([]*repository.Dialogue, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 20
	}
	return s.repo.ListDialogues(ctx, userID, page, pageSize)
}

// GetDialogueMessages 获取对话详细消息列表
func (s *AgentService) GetDialogueMessages(ctx context.Context, userID uint64, dialogueIDHex string) ([]*repository.DialogueMessage, error) {
	oid, err := primitive.ObjectIDFromHex(dialogueIDHex)
	if err != nil {
		return nil, fmt.Errorf("invalid dialogue_id: %w", err)
	}

	// 验证对话归属
	dialogue, err := s.repo.GetDialogue(ctx, oid)
	if err != nil {
		return nil, err
	}
	if dialogue.UserID != userID {
		return nil, fmt.Errorf("dialogue does not belong to user %d", userID)
	}

	return s.repo.GetMessages(ctx, oid)
}

// EndDialogueSession explicitly closes a chat session and synchronously
// crystallizes its pending durable memory. It is intentionally separate from
// DeleteDialogue: ending a session preserves the conversation and only stops
// its pending summary timers/jobs.
func (s *AgentService) EndDialogueSession(ctx context.Context, userID uint64, dialogueIDHex string) error {
	if ctx == nil {
		return fmt.Errorf("session end context is nil")
	}
	oid, err := primitive.ObjectIDFromHex(dialogueIDHex)
	if err != nil {
		return fmt.Errorf("invalid dialogue_id: %w", err)
	}
	dialogue, err := s.repo.GetDialogue(ctx, oid)
	if err != nil {
		return err
	}
	if dialogue.UserID != userID {
		return fmt.Errorf("dialogue does not belong to user %d", userID)
	}

	s.cancelSessionSummaryTimer(oid)
	if err := s.waitForSessionSummaryJobs(ctx, oid); err != nil {
		return fmt.Errorf("wait for session summary jobs: %w", err)
	}
	finalizeCtx, cancel := context.WithTimeout(ctx, defaultSessionSummaryTimeout)
	defer cancel()
	if err := s.crystallizeDialogueSummary(finalizeCtx, userID, oid, true); err != nil {
		return fmt.Errorf("finalize dialogue session summary: %w", err)
	}
	return nil
}

// DeleteDialogue 删除对话
func (s *AgentService) DeleteDialogue(ctx context.Context, userID uint64, dialogueIDHex string) error {
	oid, err := primitive.ObjectIDFromHex(dialogueIDHex)
	if err != nil {
		return fmt.Errorf("invalid dialogue_id: %w", err)
	}
	s.cancelSessionSummaryTimer(oid)
	return s.repo.DeleteDialogue(ctx, oid, userID)
}

// ========================== 模型信息 ==========================

// GetModelInfo 获取可用模型列表
func (s *AgentService) GetModelInfo() []ModelInfo {
	return GetAvailableModels()
}

// ========================== 文件解析 ==========================

// FileAnalysisResult 文件解析结果
type FileAnalysisResult struct {
	ParsedContent string // 解析出的文本内容
	FileKey       string // 存储 key，后续对话可通过此 key 引用
}

// AnalysisFile 解析上传的文件，提取文本内容并存入 MongoDB
// 存储后返回 file_key，用户可在后续对话中通过 file_key 引用文件内容
func (s *AgentService) AnalysisFile(ctx context.Context, userID uint64, fileKindID uint64, fileContent []byte) (*FileAnalysisResult, error) {
	// 1. 解析文件内容
	parsedText, err := ParseFile(fileKindID, fileContent)
	if err != nil {
		return nil, fmt.Errorf("parse file failed: %w", err)
	}

	// 2. 创建一个专门的对话来存储文件解析结果
	fileKindName := "未知文件"
	for _, fk := range SupportedFileKinds {
		if fk.ID == fileKindID {
			fileKindName = fk.Name
			break
		}
	}

	dialogue, err := s.repo.CreateDialogue(ctx, userID, fmt.Sprintf("[文件] %s", fileKindName), repository.ModeChat)
	if err != nil {
		return nil, fmt.Errorf("create file dialogue failed: %w", err)
	}

	// 3. 将解析结果作为 system 消息存入对话
	msg := &repository.DialogueMessage{
		DialogueID: dialogue.ID,
		UserID:     userID,
		Role:       repository.RoleSystem,
		Content:    fmt.Sprintf("用户上传了一个 %s 文件，以下是解析后的内容：\n\n%s", fileKindName, parsedText),
		Metadata: map[string]any{
			"file_kind_id":   fileKindID,
			"file_kind_name": fileKindName,
			"file_size":      len(fileContent),
		},
	}
	if err := s.repo.SaveMessage(ctx, msg); err != nil {
		return nil, fmt.Errorf("save file message failed: %w", err)
	}

	return &FileAnalysisResult{
		ParsedContent: parsedText,
		FileKey:       dialogue.ID.Hex(), // 使用对话 ID 作为 file_key，后续可直接加载此对话的上下文
	}, nil
}

// ========================== 模式一：直接 AI 对话 ==========================

// CallApiOfAi keeps the legacy chat contract while allowing the implementation
// to move to Runtime v2 through the existing per-mode rollout switch.
func (s *AgentService) CallApiOfAi(ctx context.Context, userID uint64, dialogueID uint64, dialogueKey string, content string) (*ChatResult, error) {
	if s.RuntimeV2Enabled(agentRuntime.ModeChat) {
		return s.callApiOfAiRuntime(ctx, userID, dialogueID, dialogueKey, content)
	}
	return s.callApiOfAiLegacy(ctx, userID, dialogueID, dialogueKey, content)
}

// callApiOfAiLegacy preserves the direct provider path for emergency rollback.
func (s *AgentService) callApiOfAiLegacy(ctx context.Context, userID uint64, dialogueID uint64, dialogueKey string, content string) (*ChatResult, error) {
	// 1. 获取或创建对话（dialogueID 作为十六进制传入时需要适配，这里兼容旧的 uint64 传参）
	dialogueIDHex := resolveDialogueKey(dialogueID, dialogueKey)

	dialogue, err := s.getOrCreateDialogue(ctx, userID, dialogueIDHex, content, repository.ModeChat)
	if err != nil {
		return nil, err
	}

	cognitive := s.buildCognitiveContext(ctx, userID, content)
	systemPrompt := s.decorateSystemPromptWithCognitiveContext(
		"你是一个专业的社交内容助手，请结合上下文给出具体、可执行、有质感的回复；不要沿用固定短字数限制，长度以用户要求和工具配置为准。",
		cognitive,
	)

	// 2. 构建消息列表：system + 历史上下文 + 当前用户消息
	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: systemPrompt,
		},
	}

	// 加载历史上下文
	contextMsgs, err := s.loadContextMessages(ctx, dialogue.ID)
	if err != nil {
		logger.Warn(ctx, "load context failed, proceeding without history", zap.Error(err))
	} else {
		messages = append(messages, contextMsgs...)
	}

	// 追加当前用户消息
	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: content,
	})

	// 3. 调用 LLM
	resp, err := s.llmClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:    s.selectedModel(ctx),
		Messages: messages,
	})
	if err != nil {
		return nil, fmt.Errorf("llm call failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("empty response from llm")
	}

	aiResponse := resp.Choices[0].Message.Content

	metadata := map[string]any{
		"cognitive_intent":          string(cognitive.Intent),
		"cognitive_rewritten_query": cognitive.RewrittenQuery,
		"cognitive_chunk_count":     cognitive.ChunkCount,
	}

	// 4. 持久化消息
	if err := s.saveUserAndAssistantMessages(ctx, dialogue.ID, userID, content, aiResponse, metadata); err != nil {
		logger.Error(ctx, "save messages failed", zap.Error(err))
	}
	return &ChatResult{
		DialogueID: dialogue.ID.Hex(),
		Response:   aiResponse,
	}, nil
}

// ========================== 模式二：RAG 语义搜索 ==========================

const (
	consultPromptTemplateID      = "consult.search.system"
	consultPromptTemplateVersion = "v1"
	consultSystemPrompt          = `你是一个推特内容检索助手。
规则：
1. 用户要查询推文、博主、趋势、历史内容时，必须调用对应工具获取真实数据。
2. 工具返回了结果，就按结果列表直接整理：给出推文 ID、作者/用户 ID、内容摘要和可继续追问的线索。
3. 工具返回“没有找到”或发生错误时，必须明确告诉用户当前没有拿到结果，并说明具体失败原因；禁止说“正在等待返回”“马上到达”“已成功发起请求”。
4. 不要编造不存在的推文、搜索结果、工具状态或后台异步进度。
5. 回答要短、准、可执行。`
)

// ConsultContent selects the Runtime v2 or legacy implementation using the
// per-mode rollout snapshot. The legacy path remains available for rollback.
func (s *AgentService) ConsultContent(ctx context.Context, userID uint64, dialogueID uint64, dialogueKey string, content string) (*ChatResult, error) {
	if s.RuntimeV2Enabled(agentRuntime.ModeConsult) {
		return s.consultContentRuntime(ctx, userID, dialogueID, dialogueKey, content)
	}
	return s.consultContentLegacy(ctx, userID, dialogueID, dialogueKey, content)
}

func (s *AgentService) consultContentRuntime(ctx context.Context, userID uint64, dialogueID uint64, dialogueKey string, content string) (*ChatResult, error) {
	dialogueIDHex := resolveDialogueKey(dialogueID, dialogueKey)
	dialogue, err := s.getOrCreateDialogue(ctx, userID, dialogueIDHex, content, repository.ModeConsult)
	if err != nil {
		return nil, err
	}
	noteAgentExecutionDialogue(ctx, dialogue.ID.Hex())
	if s.runtimeRunner == nil {
		return nil, errors.New("agent runtime runner is not configured")
	}

	if s.runtimeTools == nil {
		return nil, errors.New("agent runtime tool catalog is not configured")
	}
	tools, err := s.runtimeTools.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("list runtime tools failed: %w", err)
	}
	budget := agentRuntime.Budget{
		MaxSteps:               5,
		MaxInputTokens:         12000,
		MaxOutputTokens:        2048,
		MaxTotalTokens:         32000,
		MaxEstimatedCostMicros: 100_000,
		Timeout:                55 * time.Second,
	}
	contextMessages, err := s.loadContextMessages(ctx, dialogue.ID)
	if err != nil {
		logger.Warn(ctx, "load runtime context failed", zap.Error(err))
		contextMessages = nil
	}
	messageBuild, err := s.buildRuntimeMessages(consultSystemPrompt, content, contextMessages, budget)
	if err != nil {
		return nil, fmt.Errorf("build consult runtime messages failed: %w", err)
	}

	result, err := s.runRuntime(ctx, agentRuntime.RunRequest{
		Context: agentRuntime.RunContext{
			RunID: agentExecutionRunID(ctx), UserID: userID,
			Mode: agentRuntime.ModeConsult, Budget: budget,
			PromptTemplateID: consultPromptTemplateID, PromptTemplateVersion: consultPromptTemplateVersion,
		},
		Model:    s.selectedModel(ctx),
		Messages: messageBuild.Messages,
		Tools:    tools,
	})
	s.recordRuntimeResult(ctx, result, err, "consult")
	if err != nil {
		return nil, fmt.Errorf("consult runtime failed: %w", err)
	}
	response, err := runtimeUserVisibleResponse(result)
	if err != nil {
		return nil, fmt.Errorf("consult runtime response: %w", err)
	}

	metadata := map[string]any{
		"runtime_version":               "v2",
		"runtime_run_id":                result.Context.RunID,
		"runtime_steps":                 len(result.Steps),
		"runtime_tokens":                result.Usage.TotalTokens,
		"runtime_tokens_estimated":      result.Usage.Estimated,
		"runtime_estimated_cost_micros": result.Usage.EstimatedCostMicros,
		"runtime_cost_estimated":        result.Usage.CostEstimated,
		"runtime_pricing_version":       result.Usage.PricingVersion,
		"runtime_status":                string(result.Status),
		"runtime_context_tokens":        messageBuild.EstimatedTokens,
		"runtime_context_dropped":       stringifyDroppedSources(messageBuild.Dropped),
	}
	if err := s.saveUserAndAssistantMessages(ctx, dialogue.ID, userID, content, response, metadata); err != nil {
		logger.Error(ctx, "save runtime messages failed", zap.Error(err))
		return nil, fmt.Errorf("persist consult conversation failed: %w", err)
	}

	return &ChatResult{
		DialogueID: dialogue.ID.Hex(),
		RunID:      result.Context.RunID,
		RunStatus:  string(result.Status),
		Response:   response,
	}, nil
}

// consultContentLegacy preserves the pre-Runtime ReAct implementation for
// immediate rollback while the v2 path is being evaluated.
func (s *AgentService) consultContentLegacy(ctx context.Context, userID uint64, dialogueID uint64, dialogueKey string, content string) (*ChatResult, error) {
	dialogueIDHex := resolveDialogueKey(dialogueID, dialogueKey)

	dialogue, err := s.getOrCreateDialogue(ctx, userID, dialogueIDHex, content, repository.ModeConsult)
	if err != nil {
		return nil, err
	}

	// 1. 初始化 MCP Client
	mcpClient, tools, err := s.getOrInitMCPClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("init mcp client failed: %w", err)
	}

	// 2. 把 MCP Tools 转换成 OpenAI Function Calling 格式
	openaiTools := mcpToolsToOpenAI(tools)

	// 3. 构建初始消息（含历史上下文）
	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: consultSystemPrompt,
		},
	}

	// 加载历史上下文
	contextMsgs, err := s.loadContextMessages(ctx, dialogue.ID)
	if err != nil {
		logger.Warn(ctx, "load context failed", zap.Error(err))
	} else {
		messages = append(messages, contextMsgs...)
	}

	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: content,
	})

	// 4. ReAct 循环：LLM 决策 → 调 Tool → 把结果喂回 LLM → 直到 LLM 不再调 Tool
	for i := 0; i < 5; i++ { // 最多循环 5 次，防止死循环
		resp, err := s.llmClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:    s.selectedModel(ctx),
			Messages: messages,
			Tools:    openaiTools,
		})
		if err != nil {
			return nil, fmt.Errorf("llm call failed: %w", err)
		}

		if len(resp.Choices) == 0 {
			return nil, fmt.Errorf("empty response from llm")
		}

		choice := resp.Choices[0]

		// 5. LLM 不再调 Tool，直接返回最终回答
		if choice.FinishReason != openai.FinishReasonToolCalls {
			aiResponse := choice.Message.Content

			// 持久化消息
			if err := s.saveUserAndAssistantMessages(ctx, dialogue.ID, userID, content, aiResponse, nil); err != nil {
				logger.Error(ctx, "save messages failed", zap.Error(err))
			}

			return &ChatResult{
				DialogueID: dialogue.ID.Hex(),
				Response:   aiResponse,
			}, nil
		}

		// 6. LLM 要调 Tool，执行它
		messages = append(messages, choice.Message)

		for _, toolCall := range choice.Message.ToolCalls {
			logger.Info(ctx, "mcp tool call", zap.String("tool", toolCall.Function.Name))

			// 解析参数
			var args map[string]any
			if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
				return nil, fmt.Errorf("parse tool args failed: %w", err)
			}

			// 调用 MCP Server 执行 Tool，并进行身份鉴权注入
			toolResult, err := s.executeMCPToolGoverned(ctx, mcpClient, workflowTool.ExecutionRequest{
				ToolName:       toolCall.Function.Name,
				Inputs:         args,
				Identity:       workflowTool.CallerIdentity{UserID: userID},
				RunID:          dialogue.ID.Hex(),
				StepID:         toolCall.ID,
				Source:         workflowTool.SourceLegacy,
				IdempotencyKey: toolIdempotencyKey(dialogue.ID.Hex(), toolCall.ID, toolCall.Function.Name),
			}, mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      toolCall.Function.Name,
					Arguments: args,
				},
			})
			if err != nil {
				s.resetMCPClient() // 异常断连重置
				return nil, fmt.Errorf("call tool failed: %w", err)
			}

			// 提取 Tool 返回的文本结果
			resultText := extractTextFromToolResult(toolResult)

			// 把 Tool 结果追加到消息历史
			messages = append(messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    resultText,
				ToolCallID: toolCall.ID,
			})
		}
	}

	return nil, fmt.Errorf("max iterations reached without final answer")
}

// ========================== 模式三：AI 辅助写推文 ==========================

// AssistPublishTwitter selects the Runtime v2 or legacy drafting path. Actual
// publication remains an explicit ConfirmPublishTwitter operation.
func (s *AgentService) AssistPublishTwitter(ctx context.Context, userID uint64, dialogueID uint64, dialogueKey string, content string) (*ChatResult, error) {
	if s.RuntimeV2Enabled(agentRuntime.ModeAssist) {
		return s.assistPublishTwitterRuntime(ctx, userID, dialogueID, dialogueKey, content)
	}
	return s.assistPublishTwitterLegacy(ctx, userID, dialogueID, dialogueKey, content)
}

func (s *AgentService) assistPublishTwitterRuntime(ctx context.Context, userID uint64, dialogueID uint64, dialogueKey string, content string) (*ChatResult, error) {
	dialogueIDHex := resolveDialogueKey(dialogueID, dialogueKey)
	dialogue, err := s.getOrCreateDialogue(ctx, userID, dialogueIDHex, content, repository.ModeAssist)
	if err != nil {
		return nil, err
	}
	noteAgentExecutionDialogue(ctx, dialogue.ID.Hex())
	if s.runtimeRunner == nil {
		return nil, errors.New("agent runtime runner is not configured")
	}
	if s.runtimeTools == nil {
		return nil, errors.New("agent runtime tool catalog is not configured")
	}

	tools, err := s.runtimeTools.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("list runtime tools failed: %w", err)
	}
	profile, err := s.resolveAgentProfile(ctx, profileAssistDraft, userID)
	if err != nil {
		return nil, fmt.Errorf("resolve assist profile failed: %w", err)
	}
	contextMessages, err := s.loadContextMessages(ctx, dialogue.ID)
	if err != nil {
		logger.Warn(ctx, "load runtime context failed", zap.Error(err))
		contextMessages = nil
	}
	messageBuild, err := s.buildRuntimeMessages(profile.Prompt.SystemPrompt, content, contextMessages, profile.Budget)
	if err != nil {
		return nil, fmt.Errorf("build assist runtime messages failed: %w", err)
	}

	result, err := s.runRuntime(ctx, agentRuntime.RunRequest{
		Context: agentRuntime.RunContext{
			RunID: agentExecutionRunID(ctx), UserID: userID,
			Mode: agentRuntime.ModeAssist, Budget: profile.Budget,
			AgentProfileID: profile.ID, AgentProfileVersion: profile.Version,
			PromptTemplateID: profile.Prompt.ID, PromptTemplateVersion: profile.Prompt.Version,
		},
		Model:    s.selectedModel(ctx),
		Messages: messageBuild.Messages,
		Tools:    profile.FilterTools(tools),
	})
	s.recordRuntimeResult(ctx, result, err, profile.ID)
	if err != nil {
		return nil, fmt.Errorf("assist runtime failed: %w", err)
	}
	response, err := runtimeUserVisibleResponse(result)
	if err != nil {
		return nil, fmt.Errorf("assist runtime response: %w", err)
	}

	metadata := runtimeResultMetadata(result, profile.ID, profile.Version, profile.Prompt.Version)
	metadata["execution_profile"] = ExecutionProfileRuntimeDraft
	metadata["capability_ids"] = []string{CapabilityContentDraft}
	metadata["runtime_context_tokens"] = messageBuild.EstimatedTokens
	metadata["runtime_context_dropped"] = stringifyDroppedSources(messageBuild.Dropped)
	if err := s.saveUserAndAssistantMessages(ctx, dialogue.ID, userID, content, response, metadata); err != nil {
		return nil, fmt.Errorf("persist assist draft conversation failed: %w", err)
	}
	return &ChatResult{
		DialogueID: dialogue.ID.Hex(),
		RunID:      result.Context.RunID,
		RunStatus:  string(result.Status),
		Response:   response,
	}, nil
}

// assistPublishTwitterLegacy preserves the pre-Runtime ReAct loop for rollback.
func (s *AgentService) assistPublishTwitterLegacy(ctx context.Context, userID uint64, dialogueID uint64, dialogueKey string, content string) (*ChatResult, error) {
	dialogueIDHex := resolveDialogueKey(dialogueID, dialogueKey)

	dialogue, err := s.getOrCreateDialogue(ctx, userID, dialogueIDHex, content, repository.ModeAssist)
	if err != nil {
		return nil, err
	}

	// 1. 初始化 MCP Client
	mcpClient, tools, err := s.getOrInitMCPClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("init mcp client failed: %w", err)
	}

	// 2. 把 MCP Tools 转换成 OpenAI Function Calling 格式
	openaiTools := mcpToolsToOpenAI(tools)

	// 3. 构建初始消息（含系统设定）
	systemPrompt := fmt.Sprintf(`你是一个资深社交媒体内容策划助手，当前服务于 user_id: %d。
工作原则：
1. 当用户要你写草稿时，不要直接发布；先给出 3 条高质量候选，每条都要有清晰角度、完整表达和适合发布的正文。
2. 正文优先：分析可以短，但候选正文不能薄。除非用户明确要求极简，否则每条正文默认不少于 180 个中文字符；适合长文时可以写到 300-600 个中文字符。
3. 不再默认使用固定短字数限制；长度遵循平台和发布工具配置。如果用户指定 1000 字、长文、线程等形式，就按用户要求组织。
4. 候选内容要避免空泛口号，优先提供具体观点、语气差异、可传播的表达和必要的上下文。
5. 当用户明确说“发布/发出去/就用这条”时，再调用 create_tweet 工具完成发布。调用时只传 content，系统会绑定当前用户身份。`, userID)

	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: systemPrompt,
		},
	}

	// 加载历史上下文
	contextMsgs, err := s.loadContextMessages(ctx, dialogue.ID)
	if err != nil {
		logger.Warn(ctx, "load context failed", zap.Error(err))
	} else {
		messages = append(messages, contextMsgs...)
	}

	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: content,
	})

	// 4. ReAct 循环
	for i := 0; i < 5; i++ {
		resp, err := s.llmClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:    s.selectedModel(ctx),
			Messages: messages,
			Tools:    openaiTools,
		})
		if err != nil {
			return nil, fmt.Errorf("llm call failed: %w", err)
		}

		if len(resp.Choices) == 0 {
			return nil, fmt.Errorf("empty response from llm")
		}

		choice := resp.Choices[0]

		// 5. LLM 不再调 Tool，直接返回最终回答
		if choice.FinishReason != openai.FinishReasonToolCalls {
			aiResponse := choice.Message.Content

			// 持久化消息
			if err := s.saveUserAndAssistantMessages(ctx, dialogue.ID, userID, content, aiResponse, nil); err != nil {
				logger.Error(ctx, "save messages failed", zap.Error(err))
			}

			return &ChatResult{
				DialogueID: dialogue.ID.Hex(),
				Response:   aiResponse,
			}, nil
		}

		// 6. LLM 要调 Tool，执行它
		messages = append(messages, choice.Message)

		for _, toolCall := range choice.Message.ToolCalls {
			logger.Info(ctx, "mcp tool call", zap.String("tool", toolCall.Function.Name))

			var args map[string]any
			if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
				return nil, fmt.Errorf("parse tool args failed: %w", err)
			}

			// 调用 MCP Server 执行 Tool，并进行身份鉴权注入
			toolResult, err := s.executeMCPToolGoverned(ctx, mcpClient, workflowTool.ExecutionRequest{
				ToolName:       toolCall.Function.Name,
				Inputs:         args,
				Identity:       workflowTool.CallerIdentity{UserID: userID},
				RunID:          dialogue.ID.Hex(),
				StepID:         toolCall.ID,
				Source:         workflowTool.SourceLegacy,
				IdempotencyKey: toolIdempotencyKey(dialogue.ID.Hex(), toolCall.ID, toolCall.Function.Name),
			}, mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      toolCall.Function.Name,
					Arguments: args,
				},
			})
			if err != nil {
				s.resetMCPClient() // 异常断连重置
				return nil, fmt.Errorf("call tool failed: %w", err)
			}

			resultText := extractTextFromToolResult(toolResult)

			messages = append(messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    resultText,
				ToolCallID: toolCall.ID,
			})
		}
	}

	return nil, fmt.Errorf("max iterations reached without final answer")
}

// ========================== 模式四：多 Agent 协作写推文 ==========================

// MultiAgentPublishTwitter 模式四：多 Agent 协作写推文
func (s *AgentService) MultiAgentPublishTwitter(ctx context.Context, userID uint64, domain string, authorUserID uint64, styleRatio float32, referenceTweetIDs []uint64, dialogueKey string, content string) (*ChatResult, error) {

	dialogue, err := s.getOrCreateDialogue(ctx, userID, resolveDialogueKey(0, dialogueKey), content, repository.ModeMulti)
	if err != nil {
		return nil, err
	}

	mcpClient, _, err := s.getOrInitMCPClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("init mcp client failed: %w", err)
	}

	// ======== Agent 1: Search Agent 查阅相关领域推文 ========
	searchResult, err := s.executeMCPToolGoverned(ctx, mcpClient, workflowTool.ExecutionRequest{
		ToolName: multiSearchAgentProfile.PrimaryTool(),
		Inputs:   map[string]any{"query": domain, "size": float64(5)},
		Identity: workflowTool.CallerIdentity{UserID: userID},
		RunID:    dialogue.ID.Hex(), StepID: "search", Source: workflowTool.SourceLegacy,
	}, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: multiSearchAgentProfile.PrimaryTool(),
			Arguments: map[string]any{
				"query": domain,
				"size":  float64(5),
			},
		},
	})
	if err != nil {
		s.resetMCPClient() // 异常断连重置
		return nil, fmt.Errorf("search agent failed: %w", err)
	}
	domainTweets := extractTextFromToolResult(searchResult)

	// ======== Agent 2: Style Agent 分析作者风格 ========
	// 根据 style_ratio 计算读取推文数量，比如 0.7 对应读 35 条（最多50条）
	styleLimit := int(styleRatio * 50)
	if styleLimit < 1 {
		styleLimit = 1
	}

	styleInputs := map[string]any{"user_id": fmt.Sprintf("%d", authorUserID), "limit": float64(styleLimit)}
	styleResult, err := s.executeMCPToolGoverned(ctx, mcpClient, workflowTool.ExecutionRequest{
		ToolName: multiStyleAgentProfile.PrimaryTool(), Inputs: styleInputs,
		Identity: workflowTool.CallerIdentity{UserID: userID},
		RunID:    dialogue.ID.Hex(), StepID: "style", Source: workflowTool.SourceLegacy,
	}, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: multiStyleAgentProfile.PrimaryTool(),
			Arguments: map[string]any{
				"user_id": fmt.Sprintf("%d", authorUserID),
				"limit":   float64(styleLimit),
			},
		},
	})
	if err != nil {
		s.resetMCPClient() // 异常断连重置
		return nil, fmt.Errorf("style agent failed: %w", err)
	}
	authorTweets := extractTextFromToolResult(styleResult)

	// 获取用户指定的参考推文
	referenceTweets := ""
	if len(referenceTweetIDs) > 0 {
		ids := make([]string, len(referenceTweetIDs))
		for i, id := range referenceTweetIDs {
			ids[i] = fmt.Sprintf("%d", id)
		}
		refInputs := map[string]any{"tweet_ids": strings.Join(ids, ",")}
		refResult, err := s.executeMCPToolGoverned(ctx, mcpClient, workflowTool.ExecutionRequest{
			ToolName: "get_tweets_by_ids", Inputs: refInputs,
			Identity: workflowTool.CallerIdentity{UserID: userID},
			RunID:    dialogue.ID.Hex(), StepID: "references", Source: workflowTool.SourceLegacy,
		}, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "get_tweets_by_ids",
				Arguments: map[string]any{
					"tweet_ids": strings.Join(ids, ","),
				},
			},
		})
		if err == nil {
			referenceTweets = extractTextFromToolResult(refResult)
		} else {
			s.resetMCPClient() // 异常断连重置
		}
	}

	// ======== Agent 3: Writer Agent 综合生成推文 ========
	prompt := fmt.Sprintf(`你是一个专业的推文写作助手，现在需要综合以下信息写一条推文：

【用户要求】
%s

【领域参考推文】（来自 %s 领域的热门推文，供参考内容方向）
%s

【目标作者风格】（以下是目标作者的历史推文，请模仿其写作风格）
%s

【用户指定参考推文】（以下是用户特别指定的推文，请重点参考）
%s

请综合以上信息，生成3个推文草稿，要求：
1. 内容方向贴合用户要求和领域参考
2. 写作风格模仿目标作者
3. 正文优先，分析从简：简短研究摘要不超过 120 字，风格判断不超过 100 字，把主要 token 留给 3 条候选正文
4. 除非用户明确要求极简/一句话，否则每条「正文」不少于 180 个中文字符；如果主题需要氛围、观点或故事感，每条正文写到 300-600 个中文字符
5. 即使目标作者历史推文很短，也不要只输出一句话；可以保留其节奏、符号和标签，但要扩展成完整可发布内容
6. 相关性是硬约束：如果领域参考、作者历史或指定参考里出现与用户当前主题无关的内容，必须忽略；禁止把其他领域的概念、事实、标签或术语混入当前主题
7. 不再默认使用固定短字数限制；长度遵循用户要求与发布工具配置，必要时可以写成长文或线程式内容
8. 输出必须包含：简短研究摘要、风格判断、3 条候选正文、每条候选的适用场景
9. 格式如下：

【草稿一】
角度：
正文：
适用场景：

【草稿二】
角度：
正文：
适用场景：

【草稿三】
角度：
正文：
适用场景：`,
		content, domain, domainTweets, authorTweets, referenceTweets)

	resp, err := s.llmClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: s.selectedModel(ctx),
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: multiWriterSystemPrompt(),
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: prompt,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("writer agent failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("empty response from writer agent")
	}

	aiResponse := resp.Choices[0].Message.Content

	// 持久化消息，附带 metadata 记录调用参数
	metadata := map[string]any{
		"domain":              domain,
		"author_user_id":      authorUserID,
		"style_ratio":         styleRatio,
		"reference_tweet_ids": referenceTweetIDs,
		"agent_profiles": []string{
			multiSearchAgentProfile.ID,
			multiStyleAgentProfile.ID,
			multiWriterAgentProfile.ID,
			multiReviewAgentProfile.ID,
		},
	}
	if err := s.saveUserAndAssistantMessages(ctx, dialogue.ID, userID, content, aiResponse, metadata); err != nil {
		logger.Error(ctx, "save messages failed", zap.Error(err))
	}

	return &ChatResult{
		DialogueID: dialogue.ID.Hex(),
		Response:   aiResponse,
	}, nil
}

// ========================== MCP 辅助方法 ==========================

// getOrInitMCPClient 并发安全地获取已有的长连接客户端及缓存的 Tools 列表
func (s *AgentService) getOrInitMCPClient(ctx context.Context) (*client.Client, []mcp.Tool, error) {
	s.mcpMu.RLock()
	if s.mcpClient != nil {
		cli := s.mcpClient
		tools := s.mcpTools
		s.mcpMu.RUnlock()
		return cli, tools, nil
	}
	s.mcpMu.RUnlock()

	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()

	// 双重检查
	if s.mcpClient != nil {
		return s.mcpClient, s.mcpTools, nil
	}

	addr := s.mcpAddr
	// ⚠️ 本地进程内以 goroutine 形式启动 MCP Server 并监听 0.0.0.0。
	// 在容器内部回环连接时，必须转换为 127.0.0.1 拨号，规避容器环境路由解析问题。
	if strings.HasPrefix(addr, "0.0.0.0:") {
		addr = "127.0.0.1:" + strings.TrimPrefix(addr, "0.0.0.0:")
	}

	if len(s.mcpAuthToken) < 32 {
		return nil, nil, errors.New("MCP authentication token is not configured")
	}
	logger.Info(ctx, "initializing MCP long-connection client", zap.String("addr", addr))
	mcpClient, err := client.NewSSEMCPClient(
		fmt.Sprintf("http://%s/sse", addr),
		client.WithHeaders(map[string]string{"Authorization": "Bearer " + s.mcpAuthToken}),
		client.WithHTTPClient(platformTrace.InstrumentHTTPClient(nil, "agent.mcp.http", nil)),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create mcp client failed: %w", err)
	}

	// 🎯 核心防坑：使用绑定了服务生命周期的 Context，既防止短路断连，又防止内存泄露
	if err := mcpClient.Start(s.serviceCtx); err != nil {
		return nil, nil, fmt.Errorf("mcp client start failed: %w", err)
	}

	// 初始化握手，附加超时控制以避免挂起
	initCtx, initCancel := context.WithTimeout(ctx, 5*time.Second)
	defer initCancel()
	if _, err := mcpClient.Initialize(initCtx, mcp.InitializeRequest{}); err != nil {
		mcpClient.Close()
		return nil, nil, fmt.Errorf("mcp initialize failed: %w", err)
	}

	// 获取所有可用 Tools 并缓存
	listCtx, listCancel := context.WithTimeout(ctx, 5*time.Second)
	defer listCancel()
	toolsResp, err := mcpClient.ListTools(listCtx, mcp.ListToolsRequest{})
	if err != nil {
		mcpClient.Close()
		return nil, nil, fmt.Errorf("list tools failed: %w", err)
	}

	s.mcpClient = mcpClient
	s.mcpTools = toolsResp.Tools
	return s.mcpClient, s.mcpTools, nil
}

// resetMCPClient 清理失效的长连接客户端并置空，以便下次调用时重新握手
func (s *AgentService) resetMCPClient() {
	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()
	if s.mcpClient != nil {
		s.mcpClient.Close()
		s.mcpClient = nil
		s.mcpTools = nil
	}
}

// callToolWithAuth 封装 CallTool 请求，强制在客户端注入与校验 user_id，实现身份鉴权隔离
func (s *AgentService) callToolWithAuth(ctx context.Context, mcpClient mcpToolCaller, userID uint64, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, ok := req.Params.Arguments.(map[string]any)
	if !ok {
		args = make(map[string]any)
	}
	args = injectTrustedToolArguments(ctx, userID, req.Params.Name, args)

	req.Params.Arguments = args
	return mcpClient.CallTool(ctx, req)
}

func injectTrustedToolArguments(
	ctx context.Context,
	userID uint64,
	toolName string,
	arguments map[string]any,
) map[string]any {
	if arguments == nil {
		arguments = make(map[string]any)
	}
	metadata := workflowTool.ExecutionMetadataFromContext(ctx)
	// Sensitive write and metered web-access identities are always overwritten
	// from the authenticated execution context.
	if toolName == "create_tweet" {
		arguments["user_id"] = fmt.Sprintf("%d", userID)
		if metadata.IdempotencyKey != "" {
			arguments["idempotency_key"] = metadata.IdempotencyKey
		}
	}
	if toolName == "web_search" || toolName == "page_read" {
		arguments[agentWebSearch.InternalUserIDArgument] = userID
		arguments[agentWebSearch.InternalRunIDArgument] = metadata.RunID
	}
	if toolName == "web_search" {
		delete(arguments, agentWebSearch.InternalProviderConfigIDArgument)
		if configID := webSearchProviderConfigFromContext(ctx); configID != "" {
			arguments[agentWebSearch.InternalProviderConfigIDArgument] = configID
		}
	}
	return arguments
}

// mcpToolsToOpenAI 把 MCP Tools 格式转换成 OpenAI Function Calling 格式
func mcpToolsToOpenAI(tools []mcp.Tool) []openai.Tool {
	openaiTools := make([]openai.Tool, 0, len(tools))
	for _, t := range tools {
		// 把 MCP Tool 的 InputSchema 转成 openai 需要的 map
		schemaBytes, err := modelVisibleMCPInputSchema(t)
		if err != nil {
			continue
		}
		var schemaMap map[string]any
		if err := json.Unmarshal(schemaBytes, &schemaMap); err != nil {
			continue
		}

		openaiTools = append(openaiTools, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  schemaMap,
			},
		})
	}
	return openaiTools
}

// extractTextFromToolResult 提取 Tool 返回的文本内容
func extractTextFromToolResult(result *mcp.CallToolResult) string {
	text := ""
	for _, c := range result.Content {
		if textContent, ok := c.(mcp.TextContent); ok {
			text += textContent.Text
		}
	}
	if result.IsError && text == "" {
		return "工具调用失败，未返回可用结果。"
	}
	if result.IsError {
		return "工具调用失败：" + text
	}
	return text
}

// AnalyzeAlert 引入持续性能剖析火焰图简报并触发自愈调优闭环
func (s *AgentService) AnalyzeAlert(ctx context.Context, alertPayload string, errorLogs []string) (string, string, error) {
	log.Printf("🔔 [AIOps] Analyzing alert with LLM...")

	// 1. 启动 OTel Root Cause Analysis 主 Span
	tracer := otel.Tracer("agent-service")
	ctx, span := tracer.Start(ctx, "AIOps: Root Cause Analysis", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	// 🆕 获取持续性能剖析火焰图简报并送入 AI 诊断上下文
	analyzer := NewProfilingAnalyzer()
	gatewayProfile, _ := analyzer.GetFlamegraphSummary(ctx, "api-gateway")
	tweetProfile, _ := analyzer.GetFlamegraphSummary(ctx, "tweet-service")

	// 1. 组装输入上下文
	systemPrompt := `你是一个世界顶级的 AIOps 智能诊断专家。你的职责是根据微服务系统的告警详情、报错日志以及最新的 CPU 剖析火焰图简报，进行深度分析，找出根本原因（Root Cause），并提供自愈配置调优。请以专业的 Markdown 格式回复。

[CRITICAL INSTRUCTION]
在你的报告的最末尾，你必须输出且仅输出一段由 [STRUCT_START] 和 [STRUCT_END] 包裹的 JSON 格式的自愈指令，指定推荐的自愈调优措施。不要包含任何多余文本。例如：
1. 本地熔断隔离：
[STRUCT_START]
{
  "root_cause": "RedisDown",
  "action": "TriggerCircuitBreaker",
  "resource": "GET:/api/v1/feeds"
}
[STRUCT_END]

2. 灰度切流自愈：
[STRUCT_START]
{
  "root_cause": "TweetV2Bug",
  "action": "UpdateGrayTraffic",
  "resource": "tweet-service-vs",
  "weights": {
    "v1": 100,
    "v2": 0
  }
}
[STRUCT_END]

3. 缓存自适应参数调优：若通过 CPU 火焰图判定 CPU 负载高或排序函数分配开销高，可微调 L1/L2 缓存的 TTL 阻挡并发流量，并调整预热深度 (Preload Depth)：
[STRUCT_START]
{
  "root_cause": "TimelineCacheCPUOverload",
  "action": "TuneCacheConfig",
  "resource": "tweet-service",
  "l1_cache_ttl_seconds": 30,
  "l2_cache_ttl_seconds": 600,
  "preload_depth": 5
}
[STRUCT_END]
(注意：L1 TTL 限制为 [1, 3600]，PreloadDepth 限制为 [0, 10])`

	var sb strings.Builder
	sb.WriteString("### 1. Alert Details\n```json\n")
	sb.WriteString(alertPayload)
	sb.WriteString("\n```\n\n")

	sb.WriteString("### 2. Context Error Logs (From API Gateway Blackbox)\n")
	if len(errorLogs) == 0 {
		sb.WriteString("*No error logs captured near the alert timeframe.*\n")
	} else {
		sb.WriteString("```log\n")
		for _, l := range errorLogs {
			sb.WriteString(l)
			sb.WriteString("\n")
		}
		sb.WriteString("```\n")
	}

	// 注入 CPU 性能简报
	sb.WriteString("\n### 3. Continuous Profiling CPU Flamegraph Hotspots\n")
	sb.WriteString("#### API Gateway CPU Hotspots:\n```\n")
	sb.WriteString(gatewayProfile)
	sb.WriteString("\n```\n")
	sb.WriteString("#### Tweet Service CPU Hotspots:\n```\n")
	sb.WriteString(tweetProfile)
	sb.WriteString("\n```\n\n")

	sb.WriteString("\n请基于上述信息，进行关联根因分析 (RCA)，并按照以下格式输出 Markdown 报告：\n" +
		"- **告警现状与影响评估**\n" +
		"- **疑似根本原因 (Root Cause)**\n" +
		"- **推荐紧急止血与根治措施**\n")

	// 2. 调用支持容灾降级的大模型路由客户端 (开启 OTel 子 Span 记录推理开销)
	cheapModel := os.Getenv("LM_STUDIO_MODEL_CHAT")
	if cheapModel == "" {
		cheapModel = "qwen2.5-3b-instruct"
	}
	premiumModel := os.Getenv("PREMIUM_AI_MODEL_CHAT")
	if premiumModel == "" {
		premiumModel = "qwen-max"
	}

	if s.aiClient == nil {
		span.RecordError(fmt.Errorf("ai client is nil"))
		return "", "", fmt.Errorf("ai client is nil")
	}

	llmCtx, llmSpan := tracer.Start(ctx, "AIOps: LLM Inference")
	report, err := s.aiClient.GetChatCompletionWithRouting(
		llmCtx,
		systemPrompt,
		sb.String(),
		cheapModel,
		premiumModel,
		"high",
		nil,
	)
	llmSpan.End()

	if err != nil {
		span.RecordError(err)
		return "", "", fmt.Errorf("failed to generate RCA report via LLM: %w", err)
	}

	// 3. 提取结构化自愈元数据
	var structuredRCA string
	startIdx := strings.Index(report, "[STRUCT_START]")
	endIdx := strings.Index(report, "[STRUCT_END]")
	if startIdx != -1 && endIdx != -1 && endIdx > startIdx {
		jsonStr := report[startIdx+len("[STRUCT_START]") : endIdx]
		structuredRCA = strings.TrimSpace(jsonStr)
		// 从 report 中裁剪掉结构化标记部分，保持人读报告的纯净
		report = report[:startIdx] + report[endIdx+len("[STRUCT_END]"):]
		report = strings.TrimSpace(report)
	} else {
		structuredRCA = "{}"
	}

	// 4. Persist only through an injected governed sink. The default service
	// deliberately has no local-file fallback.
	persistCtx, persistSpan := tracer.Start(ctx, "AIOps: Persist Report")
	defer persistSpan.End()

	if s.aiOpsReportSink != nil {
		record := normalizeAIOpsReport(AIOpsReport{
			Report: report, StructuredRCA: structuredRCA, CreatedAt: time.Now().UTC(),
		})
		if persistErr := s.aiOpsReportSink.AppendAIOpsReport(persistCtx, record); persistErr != nil {
			log.Printf("⚠️ [AIOps] Failed to persist RCA report: %v", persistErr)
			persistSpan.RecordError(persistErr)
		} else {
			log.Printf("💾 [AIOps] Persisted RCA report through configured sink")
		}
	}

	// 🆕 5. 自动执行缓存自适应调优闭环 (AI 决策反馈回路)
	if structuredRCA != "" && structuredRCA != "{}" {
		var directive struct {
			Action            string `json:"action"`
			L1CacheTTLSeconds int    `json:"l1_cache_ttl_seconds"`
			L2CacheTTLSeconds int    `json:"l2_cache_ttl_seconds"`
			PreloadDepth      int    `json:"preload_depth"`
		}
		if jsonErr := json.Unmarshal([]byte(structuredRCA), &directive); jsonErr == nil {
			if directive.Action == "TuneCacheConfig" {
				log.Printf("🛡️ [AIOps] Auto-tuning Cache detected. Instantiating TuneCacheConfig...")
				res, tuneErr := s.TuneCacheConfig(ctx, directive.L1CacheTTLSeconds, directive.L2CacheTTLSeconds, directive.PreloadDepth)
				if tuneErr != nil {
					log.Printf("🚨 [AIOps] TuneCacheConfig failed: %v", tuneErr)
				} else {
					log.Printf("🛡️ [AIOps] TuneCacheConfig executed: %s", res)
				}
			}
		}
	}

	return report, structuredRCA, nil
}

// TuneCacheConfig 大模型调优 L1/L2 缓存配置的工具，包含防 Flapping 冷却锁和 Guardrails
func (s *AgentService) TuneCacheConfig(ctx context.Context, l1TTL int, l2TTL int, preloadDepth int) (string, error) {
	// 🆕 启动 OTel 子 Span 记录配置调优细节
	tracer := otel.Tracer("agent-service")
	var span trace.Span
	ctx, span = tracer.Start(ctx, "AIOps: Apply TuneCacheConfig")
	defer span.End()

	span.SetAttributes(
		attribute.Int("tuning.l1_ttl_seconds", l1TTL),
		attribute.Int("tuning.l2_ttl_seconds", l2TTL),
		attribute.Int("tuning.preload_depth", preloadDepth),
	)

	// 🎯 核心防坑：防止 AI 调优震荡的防 Flapping 冷却机制 (3分钟内同一调优行为限制)
	cooldownKey := "aiops:cooldown:tune_cache"
	locked, err := s.rdb.SetNX(ctx, cooldownKey, "locked", 3*time.Minute).Result()
	if err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("failed to acquire cooldown lock: %w", err)
	}
	if !locked {
		span.SetAttributes(attribute.Bool("tuning.bypassed_cooldown", true))
		return "Optimization bypassed: System is currently in a 3-minute cooldown observation period.", nil
	}

	// 1. 构造新配置
	newCfg := config.DynamicCacheConfig{
		L1CacheTTLSeconds: l1TTL,
		L2CacheTTLSeconds: l2TTL,
		PreloadDepth:      preloadDepth,
	}

	payload, err := json.Marshal(newCfg)
	if err != nil {
		return "", fmt.Errorf("failed to marshal cache config: %w", err)
	}

	// 🎯 严格的安全护栏 (Guardrails) - 防止大模型传入极端数值“自残”
	if err := config.ReloadConfig(payload); err != nil {
		// 校验失败，提前释放冷却锁以允许重试
		s.rdb.Del(ctx, cooldownKey)
		return fmt.Sprintf("Rejected by guardrail: %v", err), nil
	}

	// 2. 先持久化 (写入 Redis KV 供新建 Pod 或重启 Pod 初始化拉取自举)
	configKey := "system:cache:dynamic_config"
	if err := s.rdb.Set(ctx, configKey, payload, 0).Err(); err != nil {
		s.rdb.Del(ctx, cooldownKey)
		return "", fmt.Errorf("failed to persist dynamic config in Redis: %w", err)
	}

	// 3. 后广播 (发布 Redis PubSub 广播给现有微服务节点执行热重载)
	pubsubChannel := "channel:dynamic-cache-config"
	if err := s.rdb.Publish(ctx, pubsubChannel, payload).Err(); err != nil {
		return fmt.Sprintf("Config persisted but failed to broadcast: %v", err), nil
	}

	log.Printf("🛡️ [Agent Healer] Dynamically tuned cache config: L1 TTL=%ds, L2 TTL=%ds, PreloadDepth=%d", l1TTL, l2TTL, preloadDepth)
	return fmt.Sprintf("Cache configuration successfully updated and broadcasted. Current L1 TTL: %ds, L2 TTL: %ds, Preload Depth: %d", l1TTL, l2TTL, preloadDepth), nil
}

// sanitizeMarkdownTable 格式清洗，确保 Markdown 表格边界严丝合缝，前后端契约绝不崩塌
func (s *AgentService) sanitizeMarkdownTable(rawReport string) string {
	if !strings.Contains(rawReport, "|") {
		return "" // 非合法表格，宁可不要，触发降级
	}
	// 清洗掉大模型偶尔自带的 ```markdown ... ``` 标签包围圈
	cleaned := strings.ReplaceAll(rawReport, "```markdown", "")
	cleaned = strings.ReplaceAll(cleaned, "```", "")
	return strings.TrimSpace(cleaned)
}
