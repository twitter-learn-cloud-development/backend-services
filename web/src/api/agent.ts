import request from '../utils/request'
import { useUserStore } from '../stores/user'

// 获取所有可用的大模型以及它们支持的文件类型
export const getModels = () => {
    return request({
        url: '/agent/models',
        method: 'get'
    })
}

// 获取历史对话会话列表
export const getDialogues = () => {
    return request({
        url: '/agent/dialogues',
        method: 'get'
    })
}

// 获取某个特定对话的详细消息上下文
export const getDialogueMessages = (dialogueId: string) => {
    return request({
        url: `/agent/dialogues/${dialogueId}/messages`,
        method: 'get'
    })
}

// P8 统一 Agent 入口；能力列表是偏好，不代表客户端授权。
export interface AgentToolActivity {
    step_index: number
    tool_name: string
    status: 'succeeded' | 'failed' | 'pending'
    result_count: number
}

export interface AgentCitation {
    citation_id: string
    source_type: string
    source_id: string
    url: string
    title: string
    snippet: string
}

export interface AgentArtifact {
    artifact_id: string
    type: string
    status: string
    content_type: string
    content: string
    source_run_id: string
    requires_confirmation: boolean
}

export interface AgentApprovalState {
    status: string
    approval_id: string
    run_id: string
    action: string
    revision: number
    expires_at: number
    resume_supported: boolean
}

export interface AgentExecutionStrategyPlan {
    version: string
    template_id: string
    candidate_strategy: 'single_agent' | 'multi_agent'
    selected_strategy: 'single_agent' | 'multi_agent'
    decision: 'selected' | 'fallback' | 'disabled'
    reason_code: string
    complexity_score: number
    complexity_class: 'low' | 'medium' | 'high'
    complexity_signals: string[]
    estimated_latency_millis: number
    estimated_total_tokens: number
    estimated_cost_micros: number
    max_parallel_roles: number
    roles: Array<{
        role_id: string
        capability_ids: string[]
        allowed_tools: string[]
        max_steps: number
        max_total_tokens: number
        max_estimated_cost_micros: number
        timeout_millis: number
    }>
    plan_digest: string
}

export interface RunAgentResponse {
    response: string
    dialogue_key: string
    run_id: string
    run_status: string
    execution_profile: string
    capability_ids: string[]
    tweet_list: Array<{ tweet_id: string, url: string, summary: string }>
    publishable_draft: boolean
    tool_activities: AgentToolActivity[]
    citations: AgentCitation[]
    artifacts: AgentArtifact[]
    approval_state: AgentApprovalState
    selected_skill_id: string
    selected_skill_version: string
    selected_task_template_id: string
    selected_task_template_revision: number
    execution_strategy_plan?: AgentExecutionStrategyPlan
}

export interface AgentSkill {
    contract_version: string
    skill_id: string
    version: string
    display_name: string
    description: string
    instructions: string
    source: string
    allowed_tools: string[]
    knowledge: Array<{ kind: string, reference: string, version: string }>
    profile: {
        profile_id: string
        profile_version: string
        prompt_id: string
        prompt_version: string
    }
    budget: {
        max_steps: number
        max_input_tokens: number
        max_output_tokens: number
        max_total_tokens: number
        max_estimated_cost_micros: number
        timeout_seconds: number
    }
    output: {
        schema_id: string
        content_type: string
        schema_json: string
    }
    workflow: {
        publication_id: string
        publication_revision: number
        workflow_id: string
        workflow_revision_id: string
        workflow_revision_number: number
        workflow_dsl_hash: string
        tool_name: string
        input_schema_json: string
    }
}

export const listAgentSkills = (limit = 20) => {
    return request<{ skills: AgentSkill[] }>({
        url: '/agent/skills',
        method: 'get',
        params: { limit },
    })
}

export const getAgentSkill = (skillId: string, version: string) => {
    return request<{ skill: AgentSkill }>({
        url: `/agent/skills/${encodeURIComponent(skillId)}`,
        method: 'get',
        params: { version },
    })
}

export type AgentExtensionKind = 'capability' | 'skill' | 'mcp_tool'
export type AgentExtensionCategory = 'general' | 'workflow' | 'read' | 'write' | 'risky'
export type AgentExtensionScope = 'platform' | 'user' | 'project'
export type AgentExtensionStatus = 'available' | 'planned'

export interface AgentExtension {
    contract_version: string
    extension_id: string
    kind: AgentExtensionKind
    name: string
    display_name: string
    description: string
    version: string
    source: 'built_in' | 'workflow' | 'external_mcp'
    capability_id: string
    category: AgentExtensionCategory
    scope: AgentExtensionScope
    status: AgentExtensionStatus
    approval_mode: 'none' | 'required' | 'inherited'
    health_status: 'not_applicable' | 'unknown' | 'healthy' | 'degraded' | 'unhealthy'
    skill?: { skill_id: string, version: string }
    mcp?: {
        connection_id: string
        server_id: string
        snapshot_id: string
        qualified_tool_name: string
    }
}

export interface AgentExtensionSourceStatus {
    source: 'built_in' | 'workflow' | 'external_mcp'
    state: 'ready' | 'disabled'
    entry_count: number
}

export interface AgentExtensionPage {
    contract_version: string
    extensions: AgentExtension[]
    sources: AgentExtensionSourceStatus[]
    next_cursor: string
    has_more: boolean
}

export const listAgentExtensions = (params?: {
    kind?: AgentExtensionKind
    category?: AgentExtensionCategory
    scope?: AgentExtensionScope
    status?: AgentExtensionStatus
    search?: string
    after_cursor?: string
    page_size?: number
}) => request<AgentExtensionPage>({
    url: '/agent/extensions',
    method: 'get',
    params,
})

export type AgentMarketplaceExtensionKind = 'skill' | 'mcp_server'

export interface AgentMarketplacePublisher {
    publisher_id: string
    display_name: string
    verification: 'verified'
}

export interface AgentMarketplaceExtension {
    contract_version: string
    release_id: string
    package_id: string
    kind: AgentMarketplaceExtensionKind
    version: string
    display_name: string
    description: string
    publisher: AgentMarketplacePublisher
    artifact_digest_sha256: string
    signature_key_id: string
    capability_ids: string[]
    requested_permissions: string[]
    published_at_unix_ms: number
    signature_verified: boolean
}

export interface AgentMarketplaceExtensionPage {
    contract_version: string
    releases: AgentMarketplaceExtension[]
    next_cursor: string
    has_more: boolean
}

export const listAgentMarketplaceExtensions = (params?: {
    kind?: AgentMarketplaceExtensionKind
    publisher_id?: string
    search?: string
    after_cursor?: string
    page_size?: number
}) => request<AgentMarketplaceExtensionPage>({
    url: '/agent/marketplace/extensions',
    method: 'get',
    params,
})

export type AgentMarketplacePublisherVerification = 'verified' | 'suspended'
export type AgentMarketplaceSigningKeyStatus = 'active' | 'retired' | 'revoked'
export type AgentMarketplaceReleaseStatus = 'published' | 'withdrawn'

export interface AgentMarketplaceManagementAccess {
    contract_version: string
    enabled: boolean
    platform_admin: boolean
    owned_publisher_ids: string[]
}

export interface AgentMarketplaceSigningKey {
    key_id: string
    algorithm: 'ed25519'
    public_key_base64: string
    status: AgentMarketplaceSigningKeyStatus
}

export interface AgentMarketplaceManagedPublisher {
    contract_version: string
    publisher_id: string
    display_name: string
    verification: AgentMarketplacePublisherVerification
    signing_keys: AgentMarketplaceSigningKey[]
    owner_user_ids: string[]
    revision: number
    created_by: string
    updated_by: string
    verified_at_unix_ms: number
    created_at_unix_ms: number
    updated_at_unix_ms: number
}

export interface AgentMarketplaceManifest {
    contract_version: string
    package_id: string
    kind: AgentMarketplaceExtensionKind
    version: string
    publisher_id: string
    display_name: string
    description: string
    artifact_digest_sha256: string
    capability_ids: string[]
    requested_permissions: string[]
}

export interface AgentMarketplaceManagedRelease {
    contract_version: string
    release_id: string
    manifest: AgentMarketplaceManifest
    signature_key_id: string
    status: AgentMarketplaceReleaseStatus
    revision: number
    published_by: string
    withdrawn_by: string
    withdrawal_reason_code: string
    published_at_unix_ms: number
    withdrawn_at_unix_ms: number
    created_at_unix_ms: number
    updated_at_unix_ms: number
}

export interface AgentMarketplaceAuditEvent {
    contract_version: string
    event_id: string
    operation_id: string
    action: string
    outcome: 'requested' | 'succeeded' | 'failed'
    actor_user_id: string
    publisher_id: string
    package_id: string
    version: string
    key_id: string
    revision: number
    reason_code: string
    error_code: string
    created_at_unix_ms: number
}

export const getAgentMarketplaceManagementAccess = () => request<AgentMarketplaceManagementAccess>({
    url: '/agent/marketplace/manage/access',
    method: 'get',
})

export const listAgentMarketplacePublishers = (params?: { page?: number, page_size?: number }) => request<{
    publishers: AgentMarketplaceManagedPublisher[]
    total: number
}>({
    url: '/agent/marketplace/manage/publishers',
    method: 'get',
    params,
})

export const registerAgentMarketplacePublisher = (data: {
    publisher_id: string
    display_name: string
    owner_user_ids: string[]
    initial_key_id: string
    public_key_base64: string
}) => request<{ publisher: AgentMarketplaceManagedPublisher }>({
    url: '/agent/marketplace/manage/publishers',
    method: 'post',
    data,
})

export const rotateAgentMarketplacePublisherKey = (publisherId: string, data: {
    key_id: string
    public_key_base64: string
    expected_revision: number
}) => request<{ publisher: AgentMarketplaceManagedPublisher }>({
    url: `/agent/marketplace/manage/publishers/${encodeURIComponent(publisherId)}/keys/rotate`,
    method: 'post',
    data,
})

export const revokeAgentMarketplacePublisherKey = (publisherId: string, keyId: string, expectedRevision: number) => request<{
    publisher: AgentMarketplaceManagedPublisher
}>({
    url: `/agent/marketplace/manage/publishers/${encodeURIComponent(publisherId)}/keys/${encodeURIComponent(keyId)}/revoke`,
    method: 'post',
    data: { expected_revision: expectedRevision },
})

export const setAgentMarketplacePublisherVerification = (
    publisherId: string,
    verification: AgentMarketplacePublisherVerification,
    expectedRevision: number,
) => request<{ publisher: AgentMarketplaceManagedPublisher }>({
    url: `/agent/marketplace/manage/publishers/${encodeURIComponent(publisherId)}/verification`,
    method: 'put',
    data: { verification, expected_revision: expectedRevision },
})

export const listAgentMarketplaceManagedReleases = (params?: {
    publisher_id?: string
    status?: AgentMarketplaceReleaseStatus
    page?: number
    page_size?: number
}) => request<{ releases: AgentMarketplaceManagedRelease[], total: number }>({
    url: '/agent/marketplace/manage/releases',
    method: 'get',
    params,
})

export const publishAgentMarketplaceRelease = (data: {
    manifest: AgentMarketplaceManifest
    signature_key_id: string
    signature_base64: string
    expected_publisher_revision: number
}) => request<{ release: AgentMarketplaceManagedRelease }>({
    url: '/agent/marketplace/manage/releases',
    method: 'post',
    data,
})

export const withdrawAgentMarketplaceRelease = (releaseId: string, data: {
    reason_code: string
    expected_revision: number
}) => request<{ release: AgentMarketplaceManagedRelease }>({
    url: `/agent/marketplace/manage/releases/${encodeURIComponent(releaseId)}/withdraw`,
    method: 'post',
    data,
})

export const listAgentMarketplaceAuditEvents = (params?: {
    publisher_id?: string
    action?: string
    outcome?: string
    page?: number
    page_size?: number
}) => request<{ events: AgentMarketplaceAuditEvent[], total: number }>({
    url: '/agent/marketplace/manage/audits',
    method: 'get',
    params,
})

export interface AgentTaskTemplate {
    contract_version: string
    template_id: string
    name: string
    description: string
    instruction_template: string
    status: 'active' | 'archived'
    revision: number
    source_run_id: string
    source_run_revision: number
    source_result_digest: string
    source_execution_profile: string
    capability_ids: string[]
    skill_id: string
    skill_version: string
    source_model: string
    agent_profile_id: string
    agent_profile_version: string
    prompt_template_id: string
    prompt_template_version: string
    created_at: number
    updated_at: number
    archived_at: number
}

export const createAgentTaskTemplate = (runId: string, data: {
    expected_source_run_revision: number
    name: string
    description?: string
    instruction_template: string
    idempotency_key: string
}) => {
    return request<{ task_template: AgentTaskTemplate }>({
        url: `/agent/runs/${encodeURIComponent(runId)}/task-templates`,
        method: 'post',
        data,
    })
}

export const listAgentTaskTemplates = (limit = 20) => {
    return request<{ execution_enabled: boolean, task_templates: AgentTaskTemplate[] }>({
        url: '/agent/task-templates',
        method: 'get',
        params: { limit },
    })
}

export const archiveAgentTaskTemplate = (templateId: string, expectedRevision: number) => {
    return request<{ task_template: AgentTaskTemplate }>({
        url: `/agent/task-templates/${encodeURIComponent(templateId)}`,
        method: 'delete',
        params: { expected_revision: expectedRevision },
    })
}

export const runAgentTaskTemplate = (templateId: string, data: {
    expected_revision: number
    input: string
    dialogue_id: number | string
    dialogue_key?: string
    model_kind_id: number | string
    web_search_provider_config_id?: string
}) => {
    return request<RunAgentResponse>({
        url: `/agent/task-templates/${encodeURIComponent(templateId)}/run`,
        method: 'post',
        data,
        timeout: 120000,
    })
}

export const runAgent = (data: {
    content: string
    dialogue_id: number | string
    dialogue_key?: string
    model_kind_id: number | string
    preferred_capability_ids?: string[]
    web_search_provider_config_id?: string
    skill_id?: string
    skill_version?: string
}) => {
    return request<RunAgentResponse>({
        url: '/agent/run',
        method: 'post',
        data,
        timeout: 120000
    })
}

export interface AgentExecutionRunResponse {
    run_id: string
    dialogue_key: string
    execution_profile: string
    capability_ids: string[]
    skill_id: string
    skill_version: string
    task_template_id: string
    task_template_revision: number
    execution_strategy_plan?: AgentExecutionStrategyPlan
    status: string
    revision: number
    resume_supported: boolean
    pending_action_type: string
    pending_action_name: string
    pending_action_id: string
    approval_id: string
    approval_expires_at: number
    step_count: number
    input_tokens: number
    output_tokens: number
    total_tokens: number
    estimated_cost_micros: number
    pricing_version: string
    failure_code: string
    started_at: number
    updated_at: number
    suspended_at: number
    finished_at: number
}

export const getAgentRun = (runId: string) => {
    return request<AgentExecutionRunResponse>({
        url: `/agent/runs/${encodeURIComponent(runId)}`,
        method: 'get',
    })
}

export interface ExecutionTokenUsageResponse {
    input_tokens: number
    output_tokens: number
    total_tokens: number
    estimated: boolean
    estimated_cost_micros: number
    cost_estimated: boolean
    pricing_version: string
}

export interface ExecutionBudgetResponse {
    max_steps: number
    max_total_tokens: number
    max_estimated_cost_micros: number
    consumed_steps: number
    consumed_tokens: number
    consumed_cost_micros: number
}

export interface WorkflowRunAccountingResponse {
    run_id: string
    workflow_id: string
    parent_action_id: string
    status: string
    state: string
    accounting_version: string
    usage: ExecutionTokenUsageResponse
    budget: ExecutionBudgetResponse
    started_at_ms: number
    suspended_at_ms: number
    finished_at_ms: number
}

export interface AgentRunAccountingResponse {
    run_id: string
    run_status: string
    scope: string
    state: 'unavailable' | 'partial' | 'complete'
    complete: boolean
    truncated: boolean
    child_run_count: number
    included_child_run_count: number
    accounting_version: string
    parent_usage: ExecutionTokenUsageResponse
    parent_budget: ExecutionBudgetResponse
    child_usage: ExecutionTokenUsageResponse
    total_usage: ExecutionTokenUsageResponse
    children: WorkflowRunAccountingResponse[]
}

export const getAgentRunAccounting = (runId: string, childLimit = 50) => {
    return request<AgentRunAccountingResponse>({
        url: `/agent/runs/${encodeURIComponent(runId)}/accounting`,
        method: 'get',
        params: { child_limit: childLimit },
    })
}

export const resumeAgentRun = (runId: string, data: {
    expected_revision: number
    human_response?: string
    approval_id?: string
    resume_token?: string
}) => {
    return request<RunAgentResponse>({
        url: `/agent/runs/${encodeURIComponent(runId)}/resume`,
        method: 'post',
        data,
        timeout: 120000,
    })
}

// 模式一：普通直接对话
export const chat = (data: { content: string, dialogue_id: number | string, dialogue_key?: string, model_kind_id: number | string }) => {
    return request({
        url: '/agent/chat',
        method: 'post',
        data,
        timeout: 60000
    })
}

// 模式二：推文推荐与资讯咨询
export const consult = (data: { content: string, dialogue_id: number | string, dialogue_key?: string, model_kind_id: number | string }) => {
    return request({
        url: '/agent/consult',
        method: 'post',
        data,
        timeout: 60000
    })
}

// 模式三：AI 辅助发推 (生成草稿候选)
export const assistPublish = (data: { content: string, dialogue_id: number | string, dialogue_key?: string, model_kind_id: number | string }) => {
    return request({
        url: '/agent/assist',
        method: 'post',
        data,
        timeout: 60000
    })
}

// 模式三确认发布
export const confirmPublish = (data: { content: string, source_run_id?: string }) => {
    return request({
        url: '/agent/confirm',
        method: 'post',
        data
    })
}

// 模式四：多智能体协作自动生成推文
export const multiAgentPublish = (data: {
    domain: string,
    author_user_id: string,
    style_ratio: number,
    reference_tweet_ids: string[],
    dialogue_key?: string,
    content: string
}) => {
    return request({
        url: '/agent/multi',
        method: 'post',
        data,
        timeout: 120000 // 多体协作可能更耗时
    })
}

// 上传文件并由 Agent 解析（作为系统提示进入对话上下文）
export const uploadFileAnalysis = (file: File, fileKindId: string) => {
    const formData = new FormData()
    formData.append('file', file)
    formData.append('file_kind_id', fileKindId)
    
    return request({
        url: '/agent/files/analysis',
        method: 'post',
        data: formData,
        headers: {
            'Content-Type': 'multipart/form-data'
        },
        timeout: 60000
    })
}

export const createWorkflow = (data: { name: string, dsl: Record<string, any> }) => {
    return request({
        url: '/agent/workflows',
        method: 'post',
        data,
        timeout: 30000
    })
}

export const updateWorkflow = (workflowId: string, data: { name: string, dsl: Record<string, any> }) => {
    return request({
        url: `/agent/workflows/${workflowId}`,
        method: 'put',
        data,
        timeout: 30000
    })
}

export const listWorkflows = (params?: { page?: number, page_size?: number }) => {
    return request({
        url: '/agent/workflows',
        method: 'get',
        params
    })
}

export const getWorkflow = (workflowId: string) => {
    return request({
        url: `/agent/workflows/${workflowId}`,
        method: 'get'
    })
}

export const listWorkflowRevisions = (workflowId: string, params?: { page?: number, page_size?: number }) => {
    return request({
        url: `/agent/workflows/${workflowId}/revisions`,
        method: 'get',
        params
    })
}

export const getWorkflowRevision = (workflowId: string, revisionId: string) => {
    return request({
        url: `/agent/workflows/${workflowId}/revisions/${revisionId}`,
        method: 'get'
    })
}

export interface WorkflowToolPublication {
    publication_id: string
    user_id: string
    workflow_id: string
    workflow_revision_id: string
    workflow_revision_number: number
    workflow_dsl_hash: string
    tool_name: string
    display_name: string
    description: string
    input_schema?: Record<string, any>
    input_schema_json: string
    status: 'active' | 'disabled'
    revision: number
    created_at: number
    updated_at: number
}

export const publishWorkflowTool = (workflowId: string, data: {
    workflow_revision_id?: string
    description?: string
    input_schema?: Record<string, any>
    input_schema_json?: string
    expected_revision?: number
}) => {
    return request<{ publication: WorkflowToolPublication }>({
        url: `/agent/workflows/${workflowId}/tool-publication`,
        method: 'put',
        data,
        timeout: 30000,
    })
}

export const getWorkflowToolPublication = (workflowId: string) => {
    return request<{ publication: WorkflowToolPublication }>({
        url: `/agent/workflows/${workflowId}/tool-publication`,
        method: 'get',
    })
}

export const unpublishWorkflowTool = (workflowId: string, expectedRevision: number) => {
    return request<{ publication: WorkflowToolPublication }>({
        url: `/agent/workflows/${workflowId}/tool-publication`,
        method: 'delete',
        params: { expected_revision: expectedRevision },
        timeout: 30000,
    })
}

export const runWorkflow = (workflowId: string, data: { input?: Record<string, any>, input_json?: string, workflow_revision_id?: string }) => {
    return request({
        url: `/agent/workflows/${workflowId}/run`,
        method: 'post',
        data,
        timeout: 120000
    })
}

export const getWorkflowRun = (runId: string) => {
    return request({
        url: `/agent/workflow-runs/${runId}`,
        method: 'get'
    })
}

export const getWorkflowRunTrace = (runId: string) => {
    return request({
        url: `/agent/workflow-runs/${runId}/traces`,
        method: 'get'
    })
}

export interface WorkflowBlackboardSearchParams {
    state_version?: number
    query?: string
    path_prefix?: string
    after_cursor?: string
    page_size?: number
}

export const searchWorkflowBlackboard = (runId: string, params?: WorkflowBlackboardSearchParams) => {
    return request({
        url: `/agent/workflow-runs/${runId}/blackboard`,
        method: 'get',
        params,
    })
}

export interface WorkflowRunEvent {
    cursor: string
    kind: 'run' | 'step' | 'llm_call' | 'tool_call' | 'control'
    reset?: boolean
    heartbeat?: boolean
    terminal?: boolean
    reason?: string
    created_at_ms?: number
    run?: Record<string, any>
    step?: Record<string, any>
    llm_call?: Record<string, any>
    tool_call?: Record<string, any>
}

export const watchWorkflowRunEvents = async (
    runId: string,
    options: {
        afterCursor?: string
        signal?: AbortSignal
        onEvent: (event: WorkflowRunEvent) => void | Promise<void>
    },
) => {
    const userStore = useUserStore()
    const query = new URLSearchParams()
    if (options.afterCursor) query.set('after_cursor', options.afterCursor)
    const response = await fetch(`/api/v1/agent/workflow-runs/${encodeURIComponent(runId)}/events?${query.toString()}`, {
        method: 'GET',
        headers: {
            Accept: 'text/event-stream',
            ...(userStore.token ? { Authorization: `Bearer ${userStore.token}` } : {}),
        },
        cache: 'no-store',
        credentials: 'same-origin',
        signal: options.signal,
    })
    if (response.status === 401) {
        userStore.logout()
        throw new Error('unauthorized')
    }
    if (!response.ok || !response.body) {
        throw new Error(`workflow run event stream failed (${response.status})`)
    }

    const reader = response.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    const consumeFrame = async (frame: string) => {
        let eventID = ''
        const dataLines: string[] = []
        for (const line of frame.split(/\r?\n/)) {
            if (line.startsWith(':')) continue
            if (line.startsWith('id:')) eventID = line.slice(3).trim()
            if (line.startsWith('data:')) dataLines.push(line.slice(5).trimStart())
        }
        if (dataLines.length === 0) return
        const event = JSON.parse(dataLines.join('\n')) as WorkflowRunEvent
        if (!event.cursor && eventID) event.cursor = eventID
        await options.onEvent(event)
    }

    try {
        while (true) {
            const { value, done } = await reader.read()
            buffer += decoder.decode(value, { stream: !done })
            let boundary = buffer.search(/\r?\n\r?\n/)
            while (boundary >= 0) {
                const frame = buffer.slice(0, boundary)
                const separator = buffer.slice(boundary).match(/^\r?\n\r?\n/)?.[0] || '\n\n'
                buffer = buffer.slice(boundary + separator.length)
                await consumeFrame(frame)
                boundary = buffer.search(/\r?\n\r?\n/)
            }
            if (done) {
                if (buffer.trim()) await consumeFrame(buffer)
                return
            }
        }
    } finally {
        reader.releaseLock()
    }
}

export const listWorkflowRuns = (params?: {
    workflow_id?: string
    status?: string
    page?: number
    page_size?: number
}) => {
    return request({
        url: '/agent/workflow-runs',
        method: 'get',
        params
    })
}

export const cancelWorkflowRun = (runId: string, reason = '') => {
    return request({
        url: `/agent/workflow-runs/${runId}/cancel`,
        method: 'post',
        data: { reason }
    })
}

export interface ToolApproval {
    approval_id: string
    user_id: string
    run_id: string
    step_id: string
    tool_name: string
    source: string
    category: string
    status: string
    redacted_inputs: Record<string, any>
    idempotency_key: string
    reason: string
    revision: number
    created_at: number
    expires_at: number
    decided_at: number
}

export const listToolApprovals = (params?: { status?: string, page?: number, page_size?: number }) => {
    return request({
        url: '/agent/tool-approvals',
        method: 'get',
        params
    })
}

export const decideToolApproval = (approvalId: string, data: {
    decision: 'approved' | 'rejected'
    reason?: string
    expected_revision: number
}) => {
    return request({
        url: `/agent/tool-approvals/${approvalId}/decision`,
        method: 'post',
        data
    })
}

export const issueWorkflowResumeGrant = (approvalId: string, data: {
    expected_run_revision: number
}) => {
    return request({
        url: `/agent/tool-approvals/${approvalId}/resume-grant`,
        method: 'post',
        data
    })
}

export const issueAgentResumeGrant = (approvalId: string, data: {
    expected_run_revision: number
}) => {
    return request<{
        run: AgentExecutionRunResponse
        resume_token: string
        expires_at: number
    }>({
        url: `/agent/tool-approvals/${approvalId}/agent-resume-grant`,
        method: 'post',
        data,
    })
}

export const resumeWorkflowRun = (runId: string, data: {
    approval_id?: string
    resume_token?: string
    input?: Record<string, any>
    input_json?: string
}) => {
    return request({
        url: `/agent/workflow-runs/${runId}/resume`,
        method: 'post',
        data,
        timeout: 120000
    })
}

export const getWorkflowRunReplay = (runId: string) => {
    return request({
        url: `/agent/workflow-runs/${runId}/replay`,
        method: 'get',
        timeout: 30000
    })
}

export const getWorkflowCompensationJournal = (runId: string) => {
    return request({
        url: `/agent/workflow-runs/${runId}/compensations`,
        method: 'get',
        timeout: 30000
    })
}

export const retryWorkflowCompensation = (runId: string) => {
    return request({
        url: `/agent/workflow-runs/${runId}/compensations/retry`,
        method: 'post',
        data: {},
        timeout: 120000
    })
}

export type ProviderConfigKind = 'llm' | 'web_search'

export interface ProviderConfigView {
    provider_config_id: string
    kind: ProviderConfigKind
    name: string
    provider: string
    base_url: string
    model: string
    status: 'active' | 'revoked'
    has_secret: boolean
    credential_version: number
    revision: number
    created_at: number
    updated_at: number
}

export interface ProviderConfigPayload {
    kind?: ProviderConfigKind
    name: string
    provider: string
    base_url: string
    model?: string
    api_key?: string
    revision?: number
}

export const createProviderConfig = (data: ProviderConfigPayload) => {
    return request({
        url: '/agent/provider-configs',
        method: 'post',
        data
    })
}

export const updateProviderConfig = (configId: string, data: ProviderConfigPayload) => {
    return request({
        url: `/agent/provider-configs/${configId}`,
        method: 'put',
        data
    })
}

export const listProviderConfigs = (params?: { page?: number, page_size?: number, kind?: ProviderConfigKind }) => {
    return request<{ provider_configs: ProviderConfigView[], total: number }>({
        url: '/agent/provider-configs',
        method: 'get',
        params
    })
}

export const getProviderConfig = (configId: string) => {
    return request({
        url: `/agent/provider-configs/${configId}`,
        method: 'get'
    })
}

export const revokeProviderConfig = (configId: string, revision?: number) => {
    return request({
        url: `/agent/provider-configs/${configId}`,
        method: 'delete',
        params: revision ? { revision } : undefined
    })
}

export type ExternalMCPTransport = 'streamable_http' | 'sse'
export type ExternalMCPAuthType = 'none' | 'bearer'
export type AgentProjectRole = 'owner' | 'editor' | 'viewer'

export interface AgentProjectMemberView {
    user_id: string
    role: AgentProjectRole
    added_by: string
    created_at: number
    updated_at: number
}

export interface AgentProjectView {
    project_id: string
    name: string
    owner_id: string
    members: AgentProjectMemberView[]
    revision: number
    created_at: number
    updated_at: number
    current_role: AgentProjectRole
}

export const createAgentProject = (name: string) => request<{ project: AgentProjectView }>({
    url: '/agent/projects', method: 'post', data: { name }
})

export const listAgentProjects = (params?: { page?: number, page_size?: number }) => request<{
    projects: AgentProjectView[]
    total: number
}>({
    url: '/agent/projects', method: 'get', params
})

export const listAllAgentProjects = async () => {
    const projects: AgentProjectView[] = []
    const pageSize = 100
    const maxPages = 3
    for (let page = 1; page <= maxPages; page += 1) {
        const response = await listAgentProjects({ page, page_size: pageSize })
        const batch = response.data?.projects || []
        projects.push(...batch)
        const total = Number(response.data?.total || 0)
        if (batch.length < pageSize || projects.length >= total) break
    }
    return projects.slice(0, 256)
}

export const getAgentProject = (projectId: string) => request<{ project: AgentProjectView }>({
    url: `/agent/projects/${projectId}`, method: 'get'
})

export const upsertAgentProjectMember = (
    projectId: string,
    userId: string,
    role: Exclude<AgentProjectRole, 'owner'>,
    expectedRevision: number,
) => request<{ project: AgentProjectView }>({
    url: `/agent/projects/${projectId}/members/${encodeURIComponent(userId)}`,
    method: 'put',
    data: { role, expected_revision: expectedRevision },
})

export const removeAgentProjectMember = (
    projectId: string,
    userId: string,
    expectedRevision: number,
) => request<{ project: AgentProjectView }>({
    url: `/agent/projects/${projectId}/members/${encodeURIComponent(userId)}`,
    method: 'delete',
    data: { expected_revision: expectedRevision },
})

export interface ExternalMCPConnectionView {
    connection_id: string
    owner_user_id: string
    scope: 'user' | 'project'
    project_id: string
    server_id: string
    name: string
    transport: ExternalMCPTransport
    endpoint: string
    auth_type: ExternalMCPAuthType
    credential_source: 'user' | 'managed'
    managed_credential_ref: string
    managed_credential_version: number
    status: 'active' | 'revoked'
    has_secret: boolean
    credential_version: number
    latest_snapshot_id: string
    pending_snapshot_id: string
    active_snapshot_id: string
    discovery_status: 'unchecked' | 'ready' | 'review_required' | 'failed'
    last_error_code: string
    last_checked_at: number
    health_status: 'unknown' | 'healthy' | 'degraded' | 'unhealthy'
    health_error_code: string
    health_failure_count: number
    last_health_checked_at: number
    last_healthy_at: number
    next_health_check_at: number
    revision: number
    created_at: number
    updated_at: number
}

export interface ExternalMCPToolSchemaView {
    name: string
    qualified_name: string
    description: string
    input_schema_json: string
    output_schema_json: string
    declared_read_only: boolean
    declared_idempotent: boolean
    idempotency_key_argument: string
    supports_write_idempotency: boolean
}

export interface ExternalMCPToolPolicyView {
    snapshot_id: string
    tool_name: string
    qualified_name: string
    category: 'read' | 'write' | 'risky'
    enabled: boolean
    updated_at: number
}

export interface ExternalMCPToolView {
    schema: ExternalMCPToolSchemaView
    policy: ExternalMCPToolPolicyView
}

export interface ExternalMCPToolSnapshotView {
    snapshot_id: string
    connection_id: string
    server_id: string
    schema_hash: string
    version: number
    tools: ExternalMCPToolSchemaView[]
    created_at: number
}

export interface ExternalMCPConnectionPayload {
    scope?: 'user' | 'project'
    project_id?: string
    name: string
    transport: ExternalMCPTransport
    endpoint: string
    auth_type?: ExternalMCPAuthType
    credential_source?: 'user' | 'managed'
    managed_credential_ref?: string
    bearer_token?: string
    expected_revision?: number
}

export const createExternalMCPConnection = (data: ExternalMCPConnectionPayload) => request({
    url: '/agent/mcp-connections', method: 'post', data
})

export const updateExternalMCPConnection = (connectionId: string, data: ExternalMCPConnectionPayload) => request({
    url: `/agent/mcp-connections/${connectionId}`, method: 'put', data
})

export const listExternalMCPConnections = (params?: { page?: number, page_size?: number }) => request<{
    connections: ExternalMCPConnectionView[]
    total: number
}>({
    url: '/agent/mcp-connections', method: 'get', params
})

export const getExternalMCPConnection = (connectionId: string) => request<{
    connection: ExternalMCPConnectionView
}>({
    url: `/agent/mcp-connections/${connectionId}`, method: 'get'
})

export const revokeExternalMCPConnection = (connectionId: string, expectedRevision: number) => request({
    url: `/agent/mcp-connections/${connectionId}`, method: 'delete',
    data: { expected_revision: expectedRevision }
})

export const discoverExternalMCPTools = (connectionId: string, expectedRevision: number) => request<{
    connection: ExternalMCPConnectionView
    snapshot: ExternalMCPToolSnapshotView
}>({
    url: `/agent/mcp-connections/${connectionId}/discover`, method: 'post',
    data: { expected_revision: expectedRevision }, timeout: 30000
})

export const approveExternalMCPSnapshot = (
    connectionId: string,
    snapshotId: string,
    expectedRevision: number
) => request<{
    connection: ExternalMCPConnectionView
    snapshot: ExternalMCPToolSnapshotView
}>({
    url: `/agent/mcp-connections/${connectionId}/snapshots/${snapshotId}/approve`, method: 'post',
    data: { expected_revision: expectedRevision }
})

export const listExternalMCPTools = (connectionId: string) => request<{
    connection: ExternalMCPConnectionView
    snapshot: ExternalMCPToolSnapshotView
    tools: ExternalMCPToolView[]
}>({
    url: `/agent/mcp-connections/${connectionId}/tools`, method: 'get'
})

export const configureExternalMCPTool = (
    connectionId: string,
    toolName: string,
    data: {
        snapshot_id: string
        category: 'read' | 'write' | 'risky'
        enabled: boolean
        expected_revision: number
    }
) => request<{
    connection: ExternalMCPConnectionView
    tool: ExternalMCPToolView
}>({
    url: `/agent/mcp-connections/${connectionId}/tools/${encodeURIComponent(toolName)}/policy`,
    method: 'put',
    data
})

export interface AgentProfileSpecPayload {
    profile_id: string
    version: string
    prompt_id: string
    prompt_version: string
    system_prompt: string
    max_steps: number
    max_input_tokens: number
    max_output_tokens: number
    max_total_tokens: number
    max_estimated_cost_micros: number
    timeout_millis: number
    allowed_tools: string[]
}

export interface AgentProfileExperimentPolicyPayload {
    min_samples_per_arm: number
    target_samples_per_arm: number
    max_error_rate_increase_basis_points: number
    max_p95_latency_increase_basis_points: number
    max_average_cost_increase_basis_points: number
    outcome_signal?: '' | 'response_accepted' | 'draft_published' | 'content_engaged'
    min_outcome_samples_per_arm?: number
    max_outcome_rate_decrease_basis_points?: number
}

export interface AgentEvalEvidenceReferencePayload {
    storage: string
    bucket: string
    key: string
    version_id: string
    etag?: string
    report_sha256: string
    length: number
    content_type: string
    retention_mode: string
    retain_until: number
    archived_at: number
    dataset_version: string
    dataset_sha256: string
    execution_config_sha256: string
    integrity_key_id: string
}

export const getAgentProfileCatalogAccess = () => request({
    url: '/agent/profile-catalog/access',
    method: 'get',
})

export const listAgentProfileRoleBindings = (params?: { page?: number; page_size?: number }) => request({
    url: '/agent/profile-catalog/role-bindings',
    method: 'get',
    params,
})

export const upsertAgentProfileRoleBinding = (userId: string, data: { roles: string[]; expected_revision: number }) => request({
    url: `/agent/profile-catalog/role-bindings/${encodeURIComponent(userId)}`,
    method: 'put',
    data,
})

export const deleteAgentProfileRoleBinding = (userId: string, expectedRevision: number) => request({
    url: `/agent/profile-catalog/role-bindings/${encodeURIComponent(userId)}`,
    method: 'delete',
    params: { expected_revision: expectedRevision },
})

export const listAgentProfileRoleAuditEvents = (params?: { page?: number; page_size?: number }) => request({
    url: '/agent/profile-catalog/role-audits',
    method: 'get',
    params,
})

export const createAgentProfileDraft = (data: AgentProfileSpecPayload) => request({
    url: '/agent/profile-catalog/versions',
    method: 'post',
    data,
})

export const listAgentProfileVersions = (params?: { profile_id?: string, page?: number, page_size?: number }) => request({
    url: '/agent/profile-catalog/versions',
    method: 'get',
    params,
})

export const requestAgentProfilePublishApproval = (
    profileId: string,
    version: string,
    expectedVersionRevision: number,
    qualityEvidence?: AgentEvalEvidenceReferencePayload,
) => request({
    url: `/agent/profile-catalog/versions/${encodeURIComponent(profileId)}/${encodeURIComponent(version)}/publish-requests`,
    method: 'post',
    data: {
        expected_version_revision: expectedVersionRevision,
        quality_evidence: qualityEvidence,
    },
})

export const listAgentProfilePublishApprovals = (params?: {
    profile_id?: string
    status?: string
    page?: number
    page_size?: number
}) => request({
    url: '/agent/profile-catalog/publish-approvals',
    method: 'get',
    params,
})

export const decideAgentProfilePublishApproval = (approvalId: string, data: {
    decision: 'approved' | 'rejected'
    reason?: string
    expected_revision: number
}) => request({
    url: `/agent/profile-catalog/publish-approvals/${encodeURIComponent(approvalId)}/decision`,
    method: 'post',
    data,
})

export const retryAgentProfilePublishApproval = (approvalId: string, expectedRevision: number) => request({
    url: `/agent/profile-catalog/publish-approvals/${encodeURIComponent(approvalId)}/retry`,
    method: 'post',
    data: { expected_revision: expectedRevision },
})

export const getAgentProfileRelease = (profileId: string) => request({
    url: `/agent/profile-catalog/releases/${encodeURIComponent(profileId)}`,
    method: 'get',
})

export const upsertAgentProfileRelease = (profileId: string, data: {
    stable_version: string
    candidate_version: string
    candidate_basis_points: number
    salt?: string
    expected_revision: number
}) => request({
    url: `/agent/profile-catalog/releases/${encodeURIComponent(profileId)}`,
    method: 'put',
    data,
})

export const listAgentProfileAuditEvents = (params?: { profile_id?: string, page?: number, page_size?: number }) => request({
    url: '/agent/profile-catalog/audits',
    method: 'get',
    params,
})

export const startAgentProfileExperiment = (data: {
    profile_id: string
    expected_release_revision: number
    policy: AgentProfileExperimentPolicyPayload
}) => request({
    url: '/agent/profile-catalog/experiments',
    method: 'post',
    data,
})

export const listAgentProfileExperiments = (params?: {
    profile_id?: string
    status?: string
    page?: number
    page_size?: number
}) => request({
    url: '/agent/profile-catalog/experiments',
    method: 'get',
    params,
})

export const getAgentProfileExperiment = (experimentId: string) => request({
    url: `/agent/profile-catalog/experiments/${encodeURIComponent(experimentId)}`,
    method: 'get',
})

export const evaluateAgentProfileExperiment = (experimentId: string, expectedRevision: number) => request({
    url: `/agent/profile-catalog/experiments/${encodeURIComponent(experimentId)}/evaluate`,
    method: 'post',
    data: { expected_revision: expectedRevision },
})

export const stopAgentProfileExperiment = (experimentId: string, expectedRevision: number) => request({
    url: `/agent/profile-catalog/experiments/${encodeURIComponent(experimentId)}/stop`,
    method: 'post',
    data: { expected_revision: expectedRevision },
})

export const recordAgentProfileExperimentOutcome = (experimentId: string, data: {
    event_id: string
    signal: 'response_accepted' | 'draft_published' | 'content_engaged'
    positive: boolean
}) => request({
    url: `/agent/profile-catalog/experiments/${encodeURIComponent(experimentId)}/outcomes`,
    method: 'post',
    data,
})
