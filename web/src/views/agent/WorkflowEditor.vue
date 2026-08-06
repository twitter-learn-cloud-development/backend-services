<script setup lang="ts">
import { computed, ref, markRaw, onMounted, onUnmounted, provide } from 'vue'
import { useRouter } from 'vue-router'
import { VueFlow, useVueFlow } from '@vue-flow/core'
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import { AdjustmentsHorizontalIcon, ArrowLeftIcon, ArrowPathIcon, ChevronLeftIcon, ChevronRightIcon, ClockIcon, ListBulletIcon, MagnifyingGlassIcon, QueueListIcon, ShieldCheckIcon, Squares2X2Icon, StopIcon, XMarkIcon } from '@heroicons/vue/24/outline'
import {
  cancelWorkflowRun,
  createWorkflow,
  getWorkflow,
  getWorkflowCompensationJournal,
  getWorkflowRevision,
  getWorkflowRun,
  getWorkflowRunTrace,
  getWorkflowRunReplay,
  getWorkflowToolPublication,
  listWorkflowRuns,
  listWorkflowRevisions,
  listWorkflows,
  publishWorkflowTool,
  retryWorkflowCompensation,
  runWorkflow,
  searchWorkflowBlackboard,
  unpublishWorkflowTool,
  updateWorkflow,
  watchWorkflowRunEvents,
  type WorkflowRunEvent,
  type WorkflowToolPublication,
} from '../../api/agent'

import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'

import SidebarNodes from '../../components/agent/SidebarNodes.vue'
import NodePropertiesDrawer from '../../components/agent/NodePropertiesDrawer.vue'
import CustomNodeWrapper from '../../components/agent/nodes/CustomNodeWrapper.vue'

const nodeTypes = {
  start: markRaw(CustomNodeWrapper) as any,
  end: markRaw(CustomNodeWrapper) as any,
  llm: markRaw(CustomNodeWrapper) as any,
  tool: markRaw(CustomNodeWrapper) as any,
  agent: markRaw(CustomNodeWrapper) as any,
  router: markRaw(CustomNodeWrapper) as any,
  wait: markRaw(CustomNodeWrapper) as any,
}

const baseEdgeStyle = { stroke: '#6366f1', strokeWidth: 2.25 }
const selectedEdgeStyle = { stroke: '#38bdf8', strokeWidth: 3 }
const router = useRouter()
const { project } = useVueFlow()

const nodes = ref<any[]>([])
const edges = ref<any[]>([])
const selectedNode = ref<any | null>(null)
const selectedEdgeId = ref('')
const isSaving = ref(false)
const isRunning = ref(false)
const workflowName = ref('高定制化 AI 工作流')
const savedWorkflowId = ref('')
const selectedRevisionId = ref('')
const workflowOptions = ref<any[]>([])
const workflowRevisionOptions = ref<any[]>([])
const isLoadingCatalog = ref(false)
const workflowToolPublication = ref<WorkflowToolPublication | null>(null)
const workflowToolPublicationLoading = ref(false)
const workflowToolPublicationSaving = ref(false)
const lastRunId = ref('')
const lastRunStatus = ref('')
const lastError = ref('')
const lastNotice = ref('')
const replayOpen = ref(false)
const replayLoading = ref(false)
const replayError = ref('')
const replayData = ref<any | null>(null)
const compensationJournalOpen = ref(false)
const compensationJournalLoading = ref(false)
const compensationJournalError = ref('')
const compensationJournalData = ref<any | null>(null)
const runConsoleOpen = ref(false)
const runConsoleLoading = ref(false)
const runConsoleError = ref('')
const runStatusFilter = ref('')
const runPage = ref(1)
const runPageSize = 10
const runTotal = ref(0)
const runItems = ref<any[]>([])
const selectedRunDetail = ref<any | null>(null)
const selectedRunTrace = ref<any | null>(null)
const selectedRunLoading = ref(false)
const selectedBlackboard = ref<any | null>(null)
const blackboardLoading = ref(false)
const blackboardError = ref('')
const blackboardQuery = ref('')
const blackboardPathPrefix = ref('')
const blackboardStateVersion = ref(0)
const blackboardCursor = ref('')
const blackboardCursorHistory = ref<string[]>([])
const runStreamConnected = ref(false)
const runStreamReconnecting = ref(false)
const budgetOpen = ref(false)
const mobilePaletteOpen = ref(false)

let runStreamAbortController: AbortController | null = null
let runStreamReconnectTimer: ReturnType<typeof setTimeout> | null = null
let runStreamGeneration = 0
let runSelectionGeneration = 0
let runEventCursor = '0-0'

const selectedRunIsTerminal = computed(() => new Set([
  'suspended', 'success', 'failed', 'rejected', 'compensated', 'compensation_failed', 'canceled',
]).has(String(selectedRunDetail.value?.status || '')))

const runStreamIndicatorTitle = computed(() => {
  if (selectedRunIsTerminal.value) return '事件流已结束'
  if (runStreamConnected.value) return '运行事件流已连接'
  if (runStreamReconnecting.value) return '正在恢复运行事件流'
  return '运行事件流未连接'
})

const selectedExecutionSteps = computed(() => {
  const records = selectedRunTrace.value?.steps
  if (Array.isArray(records) && records.length > 0) return records
  return (selectedRunDetail.value?.output?.traces || []).map((trace: any, index: number) => ({
    ...trace,
    step_id: trace.node_id,
    step_type: trace.node_type,
    sequence: index + 1,
    error_class: trace.error || '',
  }))
})

const defaultWorkflowBudget = {
  max_node_executions: 50,
  max_parallel_nodes: 8,
  timeout_sec: 300,
  max_total_tokens: 120000,
  max_estimated_cost_micros: 0,
}

const normalizeWorkflowBudget = (value: Record<string, any> = {}) => ({
  max_node_executions: Math.min(1000, Math.max(1, Number(value.max_node_executions || defaultWorkflowBudget.max_node_executions))),
  max_parallel_nodes: Math.min(64, Math.max(1, Number(value.max_parallel_nodes || defaultWorkflowBudget.max_parallel_nodes))),
  timeout_sec: Math.min(3600, Math.max(1, Number(value.timeout_sec || defaultWorkflowBudget.timeout_sec))),
  max_total_tokens: Math.min(10000000, Math.max(1, Number(value.max_total_tokens || defaultWorkflowBudget.max_total_tokens))),
  max_estimated_cost_micros: Math.min(1000000000000, Math.max(0, Number(value.max_estimated_cost_micros || 0))),
})

const workflowBudget = ref(normalizeWorkflowBudget())

const goBack = () => {
  if (window.history.length > 1) {
    router.back()
    return
  }
  router.push('/')
}

const defaultChatSystemPrompt = '你是一个通用对话助手。只回答当前用户输入，保持准确、简洁、可执行；不要主动发布内容，不要引入与当前问题无关的领域素材。'
const defaultChatPrompt = '{{start.user_input}}'
const defaultPlannerSystemPrompt = '你是任务规划器。把目标拆成有序、可执行、可验证的步骤；只制定计划，不声称已经执行。'
const defaultPlannerPrompt = `请为以下目标制定执行计划：

{{start.user_input}}

输出要求：
1. 每一步包含目标、所需输入、建议工具和验收标准。
2. 标明步骤依赖关系。
3. 不执行工具，不编造执行结果。`

const defaultWriterSystemPrompt = '你是一个专业内容创作助手。只围绕用户当前主题写作；如果上游参考内容与主题不相关，必须忽略，禁止混入无关领域概念。'
const defaultWriterPrompt = `请根据用户输入写一组可直接发布的高质量内容草稿。

用户输入：{{start.user_input}}

要求：
1. 输出 3 条候选，每条都包含「角度」「正文」「适用场景」。
2. 除非用户明确要求极短，否则每条「正文」不少于 180 个中文字符；如果主题适合长文，可以写到 300-600 个中文字符。
3. 正文要有完整观点、细节、节奏和记忆点，不要只写一句口号。
4. 分析部分要短，把主要篇幅留给正文。
5. 只围绕用户主题展开，禁止混入与主题无关的科技、生物、财经等素材。
6. 不要默认套用 280 字限制，长度以用户要求和发布工具配置为准。`

const createEdge = (source: string, target: string, extra: Record<string, any> = {}) => {
  const id = extra.id || `e_${source}_${target}_${Date.now()}`
  const selected = id === selectedEdgeId.value
  return {
    id,
    source,
    target,
    sourceHandle: extra.sourceHandle || extra.source_handle || 'output',
    targetHandle: extra.targetHandle || extra.target_handle || 'input',
    type: 'straight',
    animated: false,
    selectable: true,
    interactionWidth: 24,
    style: selected ? selectedEdgeStyle : baseEdgeStyle,
  }
}

const refreshEdgeStyles = () => {
  edges.value = edges.value.map(edge => createEdge(edge.source, edge.target, edge))
}

const deleteEdge = (edgeId: string) => {
  edges.value = edges.value.filter(edge => edge.id !== edgeId)
  if (selectedEdgeId.value === edgeId) selectedEdgeId.value = ''
}

const platformLLMDefaults = {
  provider: 'dashscope',
  base_url: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
  model: 'qwen-plus',
}

const defaultPropertiesForNode = (type: string, title: string, preset = '') => {
  if (type === 'llm') {
    const writer = preset === 'llm_writer' || title.toLowerCase().includes('writer') || title.includes('创作')
    const planner = preset === 'llm_planner' || title.toLowerCase().includes('planner') || title.includes('规划')
    return {
      mode: planner ? 'planner' : (writer ? 'writer' : 'chat'),
      provider_config_id: '',
      provider: platformLLMDefaults.provider,
      base_url: platformLLMDefaults.base_url,
	  credential_ref: '',
      model: platformLLMDefaults.model,
      system_prompt: planner ? defaultPlannerSystemPrompt : (writer ? defaultWriterSystemPrompt : defaultChatSystemPrompt),
      prompt: planner ? defaultPlannerPrompt : (writer ? defaultWriterPrompt : defaultChatPrompt),
      max_tokens: writer ? 2048 : 1024,
      timeout_sec: 30,
    }
  }
  if (preset === 'mcp_hybrid_tweet_search') {
    return { tool_name: 'HybridTweetSearch', query: '{{start.user_input}}', size: 5, timeout_sec: 20 }
  }
  if (preset === 'mcp_semantic_tweet_search') {
    return { tool_name: 'SemanticTweetSearch', query: '{{start.user_input}}', size: 5, timeout_sec: 20 }
  }
  if (preset === 'mcp_search_users') {
    return { tool_name: 'SearchUsers', keyword: '{{start.user_input}}', limit: 5, timeout_sec: 20 }
  }
  if (preset === 'mcp_get_user_tweets') {
    return { tool_name: 'GetUserTweets', user_id: '', limit: 10, timeout_sec: 20 }
  }
  if (preset === 'mcp_get_tweets_by_ids') {
    return { tool_name: 'GetTweetsByIDs', tweet_ids: '', timeout_sec: 20 }
  }
  if (preset === 'web_search') {
    return { tool_name: 'WebSearch', query: '{{start.user_input}}', count: 5, provider_config_id: '', timeout_sec: 20 }
  }
  if (preset === 'page_read') {
    return { tool_name: 'PageRead', url: '', max_runes: 16000, timeout_sec: 20 }
  }
  if (preset === 'external_mcp') {
    return { external_mcp: true, tool_name: '', mcp_arguments: {}, timeout_sec: 20 }
  }
  if (type === 'agent') {
    return {
      tool_name: preset === 'plan_executor' ? 'PlanExecutor' : 'ReActAgent',
      objective: '{{start.user_input}}',
      plan: '',
      allowed_tools: 'hybrid_search_tweets,search_users,get_user_tweets,web_search,page_read',
      max_iterations: 5,
      model: 'qwen-plus',
      max_tokens: 2048,
      timeout_sec: 90,
    }
  }
  if (type === 'tool') {
    return {
      tool_name: 'PublishTweet',
      content: '{{node_llm_01.text}}',
      max_chars: 10000,
      overflow_strategy: 'error',
      timeout_sec: 20,
    }
  }
  if (type === 'router') return { branch: 'true', timeout_sec: 5 }
  if (type === 'wait') {
    return {
      resume_mode: 'human_input',
      reason: '请补充继续执行所需的信息',
      timeout_sec: 0,
    }
  }
  return {}
}

const nodeTitle = (type: string, properties: Record<string, any> = {}) => {
  if (type === 'start') return '开始输入'
  if (type === 'end') return '输出结果'
  if (type === 'llm') {
    if (properties.mode === 'writer') return 'LLM 创作生成'
    if (properties.mode === 'planner') return '任务规划器'
    return 'LLM 对话生成'
  }
  if (type === 'tool') {
    if (properties.external_mcp && !properties.tool_name) return '外部 MCP 工具'
    return properties.tool_name ? `${properties.tool_name}` : '工具调用'
  }
  if (type === 'agent') return properties.tool_name === 'PlanExecutor' ? '计划执行器' : 'ReAct 智能体'
  if (type === 'router') return '条件路由'
  if (type === 'wait') {
    return properties.resume_mode === 'external_callback' ? '外部回调等待' : '人工输入'
  }
  return type.toUpperCase()
}

const nodeDescription = (type: string, properties: Record<string, any> = {}) => {
  if (properties.prompt) return `Prompt: ${String(properties.prompt).replace(/\s+/g, ' ').slice(0, 80)}`
  if (properties.content) return `推文: ${properties.content}`
  if (properties.url) return `URL: ${properties.url}`
  if (properties.query) return `Query: ${properties.query}`
  if (properties.objective) return `目标: ${properties.objective}`
  if (type === 'wait' && properties.reason) return String(properties.reason)
  if (type === 'start') return '接收启动入参。'
  if (type === 'end') return '输出最终执行详情。'
  return '已从保存的 DSL 恢复。'
}

const executionMetadataKeys = [
  'input_schema',
  'output_schema',
  'retry',
	'compensation',
  'policy',
  'profile_ref',
  'provider_ref',
] as const

const copyExecutionMetadata = (source: Record<string, any> = {}) => {
  const metadata: Record<string, any> = {}
  executionMetadataKeys.forEach(key => {
    const value = source?.[key]
    if (value !== undefined && value !== null && value !== '') {
      metadata[key] = typeof value === 'object'
        ? JSON.parse(JSON.stringify(value))
        : value
    }
  })
  return metadata
}

const makeNode = (
  id: string,
  type: string,
  position: { x: number; y: number },
  saved: any = {},
  properties: Record<string, any> = {},
  execution: Record<string, any> = {},
) => ({
  id,
  type,
  label: saved.label || nodeTitle(type, properties),
  position,
  data: {
    title: nodeTitle(type, properties),
    description: nodeDescription(type, properties),
    status: 'idle',
    properties,
    execution: copyExecutionMetadata(execution),
  },
})

const normalizeRestoredProperties = (type: string, properties: Record<string, any>) => {
  const next = { ...properties }
	delete next.api_key
  if (type === 'llm') {
    const prompt = String(next.prompt || '')
    const legacyWriterPrompt = prompt.includes('重新润色') || prompt.includes('高质量推文草稿') || prompt.includes('可直接发布')
    next.mode ||= legacyWriterPrompt ? 'writer' : 'chat'
    if (next.mode === 'planner') {
      next.system_prompt ||= defaultPlannerSystemPrompt
      next.prompt ||= defaultPlannerPrompt
      next.max_tokens = Math.max(Number(next.max_tokens || 0), 1024)
    } else if (next.mode === 'writer') {
      next.system_prompt ||= defaultWriterSystemPrompt
      if (!prompt || legacyWriterPrompt) next.prompt = defaultWriterPrompt
      next.max_tokens = Math.max(Number(next.max_tokens || 0), 2048)
    } else {
      next.system_prompt ||= defaultChatSystemPrompt
      if (!prompt || legacyWriterPrompt) next.prompt = defaultChatPrompt
      next.max_tokens = Math.max(Number(next.max_tokens || 0), 1024)
    }
  }
  return next
}

const loadDefaultWorkflow = () => {
  workflowBudget.value = normalizeWorkflowBudget()
  nodes.value = [
    makeNode('start', 'start', { x: 120, y: 170 }),
    makeNode('node_llm_01', 'llm', { x: 440, y: 170 }, {}, defaultPropertiesForNode('llm', '', 'llm_chat')),
    makeNode('end', 'end', { x: 760, y: 170 }),
  ]

  edges.value = [
    createEdge('start', 'node_llm_01', { id: 'e_start_llm' }),
    createEdge('node_llm_01', 'end', { id: 'e_llm_end' }),
  ]
}

const isLegacyAutoPublishDefault = (dslNodes: any[], dslEdges: any[]) => {
  if (dslNodes.length !== 4 || dslEdges.length !== 3) return false
  const ids = new Set(dslNodes.map(node => node.id))
  if (!ids.has('start') || !ids.has('node_llm_01') || !ids.has('node_tweet_01') || !ids.has('end')) return false

  const tweetNode = dslNodes.find(node => node.id === 'node_tweet_01')
  const llmNode = dslNodes.find(node => node.id === 'node_llm_01')
  const toolName = String(tweetNode?.properties?.tool_name || '').toLowerCase()
  const prompt = String(llmNode?.properties?.prompt || '')
  return toolName === 'publishtweet' && (
    prompt.includes('重新润色') ||
    prompt.includes('高质量推文草稿') ||
    prompt.includes('可直接发布') ||
    prompt.includes('{{start.user_input}}')
  )
}

const orderNodeIds = (dslNodes: any[], dslEdges: any[]) => {
  const ids = dslNodes.map(n => n.id)
  const indegree = new Map(ids.map(id => [id, 0]))
  const children = new Map<string, string[]>()

  dslEdges.forEach(edge => {
    if (!indegree.has(edge.source) || !indegree.has(edge.target)) return
    indegree.set(edge.target, (indegree.get(edge.target) || 0) + 1)
    children.set(edge.source, [...(children.get(edge.source) || []), edge.target])
  })

  const queue = ids.filter(id => (indegree.get(id) || 0) === 0)
  const ordered: string[] = []
  while (queue.length > 0) {
    const id = queue.shift()!
    ordered.push(id)
    for (const child of children.get(id) || []) {
      indegree.set(child, (indegree.get(child) || 0) - 1)
      if ((indegree.get(child) || 0) === 0) queue.push(child)
    }
  }
  return ordered.length === ids.length ? ordered : ids
}

const calculatePositions = (dslNodes: any[], dslEdges: any[]) => {
  const ordered = orderNodeIds(dslNodes, dslEdges)
  const levels = new Map<string, number>()
  ordered.forEach(id => levels.set(id, 0))
  ordered.forEach(id => {
    const current = levels.get(id) || 0
    dslEdges
      .filter(edge => edge.source === id)
      .forEach(edge => levels.set(edge.target, Math.max(levels.get(edge.target) || 0, current + 1)))
  })

  const lanes = new Map<number, number>()
  const positions = new Map<string, { x: number; y: number }>()
  ordered.forEach(id => {
    const level = levels.get(id) || 0
    const lane = lanes.get(level) || 0
    lanes.set(level, lane + 1)
    positions.set(id, { x: 120 + level * 320, y: 170 + lane * 150 })
  })
  return positions
}

const restoreWorkflowFromDSL = (workflow: any) => {
  const dsl = workflow?.dsl || {}
	workflowBudget.value = normalizeWorkflowBudget(dsl.budget || {})
  workflowName.value = workflow?.name || dsl.name || workflowName.value
  savedWorkflowId.value = workflow?.workflow_id || ''

  const uiNodes = Array.isArray(dsl.ui?.nodes) ? dsl.ui.nodes : []
  const uiNodeMap = new Map<string, any>(uiNodes.map((n: any) => [n.id, n]))
  const dslNodes = Array.isArray(dsl.nodes) && dsl.nodes.length > 0
    ? dsl.nodes
    : uiNodes.map((n: any) => ({
        id: n.id,
        type: n.type,
        properties: n.data?.properties || {},
        ...copyExecutionMetadata(n.data?.execution || {}),
      }))

  const dslEdges = Array.isArray(dsl.edges) && dsl.edges.length > 0
    ? dsl.edges
    : (Array.isArray(dsl.ui?.edges) ? dsl.ui.edges : [])

  if (dslNodes.length === 0) {
    loadDefaultWorkflow()
    return
  }

  if (isLegacyAutoPublishDefault(dslNodes, dslEdges)) {
    loadDefaultWorkflow()
    return
  }

  const positions = calculatePositions(dslNodes, dslEdges)
  nodes.value = dslNodes.map((n: any, index: number) => {
    const saved = uiNodeMap.get(n.id)
    const properties = normalizeRestoredProperties(n.type, n.properties || saved?.data?.properties || {})
    if (n.timeout_sec !== undefined && n.timeout_sec !== null) {
      properties.timeout_sec = Number(n.timeout_sec)
    }
    properties.state_writes = Array.isArray(n.writes)
      ? JSON.parse(JSON.stringify(n.writes))
      : (Array.isArray(properties.state_writes) ? properties.state_writes : [])
    const execution = {
      ...copyExecutionMetadata(saved?.data?.execution || {}),
      ...copyExecutionMetadata(n),
    }
    const savedPosition = saved?.position
    const position = Number.isFinite(Number(savedPosition?.x)) && Number.isFinite(Number(savedPosition?.y))
      ? { x: Number(savedPosition.x), y: Number(savedPosition.y) }
      : (positions.get(n.id) || { x: 120 + index * 320, y: 170 })
    return makeNode(
      n.id,
      n.type,
      position,
      saved,
      properties,
      execution,
    )
  })

  selectedEdgeId.value = ''
  edges.value = dslEdges
    .filter((e: any) => e.source && e.target)
    .map((e: any) => createEdge(e.source, e.target, e))
}

const refreshWorkflowCatalog = async () => {
	const listResp = await listWorkflows({ page: 1, page_size: 50 })
	workflowOptions.value = listResp.data.workflows || []
}

const loadWorkflowRevisionOptions = async (workflowId: string, preferredRevisionId = '') => {
	workflowRevisionOptions.value = []
	selectedRevisionId.value = ''
	if (!workflowId) return
	const revisionsResp = await listWorkflowRevisions(workflowId, { page: 1, page_size: 50 })
	workflowRevisionOptions.value = revisionsResp.data.revisions || []
	const preferred = workflowRevisionOptions.value.find(item => item.revision_id === preferredRevisionId)
		|| workflowRevisionOptions.value[0]
	selectedRevisionId.value = String(preferred?.revision_id || '')
}

const loadWorkflowToolPublication = async () => {
  workflowToolPublication.value = null
  if (!savedWorkflowId.value) return
  workflowToolPublicationLoading.value = true
  try {
    const response = await getWorkflowToolPublication(savedWorkflowId.value)
    workflowToolPublication.value = response.data.publication
  } catch (err: any) {
    if (Number(err?.response?.status || 0) !== 404) {
      console.warn('Failed to load workflow tool publication', err)
    }
  } finally {
    workflowToolPublicationLoading.value = false
  }
}

const loadSelectedWorkflow = async () => {
	if (!savedWorkflowId.value) {
		selectedRevisionId.value = ''
		workflowRevisionOptions.value = []
		workflowToolPublication.value = null
		workflowName.value = '高定制化 AI 工作流'
		loadDefaultWorkflow()
		return
	}
	lastError.value = ''
	try {
		const detailResp = await getWorkflow(savedWorkflowId.value)
		const workflow = detailResp.data.workflow
		restoreWorkflowFromDSL(workflow)
		await loadWorkflowRevisionOptions(savedWorkflowId.value, String(workflow?.current_revision_id || ''))
		await loadWorkflowToolPublication()
	} catch (err: any) {
		lastError.value = err?.response?.data?.error || err?.message || '加载工作流失败'
	}
}

const loadSelectedRevision = async () => {
	if (!savedWorkflowId.value || !selectedRevisionId.value) return
	lastError.value = ''
	try {
		const resp = await getWorkflowRevision(savedWorkflowId.value, selectedRevisionId.value)
		const revision = resp.data.revision
		restoreWorkflowFromDSL({
			workflow_id: savedWorkflowId.value,
			name: workflowName.value,
			dsl: revision?.dsl,
		})
	} catch (err: any) {
		lastError.value = err?.response?.data?.error || err?.message || '加载工作流版本失败'
	}
}

const loadWorkflowCatalog = async () => {
	isLoadingCatalog.value = true
	try {
		await refreshWorkflowCatalog()
		loadDefaultWorkflow()
	} catch (err) {
		console.error('Failed to load workflow catalog', err)
		workflowOptions.value = []
		loadDefaultWorkflow()
	} finally {
		isLoadingCatalog.value = false
	}
}

const onKeyDown = (event: KeyboardEvent) => {
  if (event.key === 'Escape' && mobilePaletteOpen.value) {
    mobilePaletteOpen.value = false
    return
  }
  if ((event.key === 'Delete' || event.key === 'Backspace') && selectedEdgeId.value) {
    event.preventDefault()
    deleteEdge(selectedEdgeId.value)
  }
}

onMounted(() => {
	loadWorkflowCatalog()
  window.addEventListener('keydown', onKeyDown)
})

onUnmounted(() => {
  window.removeEventListener('keydown', onKeyDown)
  stopWorkflowRunStream()
})

const onDragOver = (event: DragEvent) => {
  event.preventDefault()
  if (event.dataTransfer) event.dataTransfer.dropEffect = 'move'
}

const appendPaletteNode = (
  type: string,
  title: string,
  preset: string,
  position: { x: number; y: number },
) => {
  const newNodeID = `node_${type}_${Date.now()}`
  const properties = defaultPropertiesForNode(type, title, preset)
  nodes.value.push(makeNode(newNodeID, type, position, { label: title, data: { title } }, properties))
}

const onPaletteAdd = (node: { type: string; title: string; preset: string }) => {
  const container = document.querySelector('.vue-flow')
  if (!container) return

  const rect = container.getBoundingClientRect()
  const offset = (nodes.value.length % 5) * 24
  const position = project({
    x: Math.max(80, rect.width / 2 - 120 + offset),
    y: Math.max(80, rect.height / 2 - 60 + offset),
  })
  appendPaletteNode(node.type, node.title, node.preset, position)
  mobilePaletteOpen.value = false
}

const onDrop = (event: DragEvent) => {
  event.preventDefault()
  if (!event.dataTransfer) return
  const type = event.dataTransfer.getData('application/vueflow-type')
  const title = event.dataTransfer.getData('application/vueflow-title')
  const preset = event.dataTransfer.getData('application/vueflow-preset')
  if (!type) return

  const container = document.querySelector('.vue-flow')
  if (!container) return

  const rect = container.getBoundingClientRect()
  const position = project({ x: event.clientX - rect.left, y: event.clientY - rect.top })
  appendPaletteNode(type, title, preset, position)
}

const onConnectHandler = (params: any) => {
  const sourceHandle = params.sourceHandle || 'output'
  const targetHandle = params.targetHandle || 'input'
  edges.value.push(createEdge(params.source, params.target, {
    id: `e_${params.source}_${sourceHandle}_${params.target}_${targetHandle}_${Date.now()}`,
    sourceHandle,
    targetHandle,
  }))
}

const onEdgeClick = (event: any) => {
  selectedNode.value = null
  selectedEdgeId.value = event.edge?.id || ''
  refreshEdgeStyles()
}

const onEdgeDoubleClick = (event: any) => {
  const edgeId = event.edge?.id
  if (edgeId) deleteEdge(edgeId)
}

const deleteNode = (nodeId: string) => {
  nodes.value = nodes.value.filter(n => n.id !== nodeId)
  edges.value = edges.value.filter(e => e.source !== nodeId && e.target !== nodeId)
  if (selectedNode.value?.id === nodeId) selectedNode.value = null
}
provide('deleteNode', deleteNode)

const onNodeClick = (event: any) => {
  selectedEdgeId.value = ''
  selectedNode.value = event.node
  refreshEdgeStyles()
}

const onPaneClick = () => {
  selectedNode.value = null
  selectedEdgeId.value = ''
  refreshEdgeStyles()
}

const updateNodeProperties = (nodeId: string, properties: any) => {
  const node = nodes.value.find(n => n.id === nodeId)
  if (!node) return
  const normalized = normalizeRestoredProperties(node.type, properties)
  node.data.properties = normalized
  node.data.title = nodeTitle(node.type, normalized)
  node.label = node.data.title
  node.data.description = nodeDescription(node.type, normalized)
}

const updateNodeExecution = (nodeId: string, execution: any) => {
  const node = nodes.value.find(n => n.id === nodeId)
  if (!node) return
  node.data.execution = copyExecutionMetadata(execution || {})
}

const inferToolName = (node: any, properties: Record<string, any>) => {
  if (properties.tool_name) return properties.tool_name
  const title = `${node.data?.title || ''} ${node.label || ''}`.toLowerCase()
  if (title.includes('pageread') || title.includes('page read') || properties.url) return 'PageRead'
  if (title.includes('websearch') || title.includes('search') || properties.query) return 'WebSearch'
  if (title.includes('publishtweet') || title.includes('tweet') || properties.content) return 'PublishTweet'
  return ''
}

const buildWorkflowDSL = () => ({
  name: workflowName.value,
  budget: normalizeWorkflowBudget(workflowBudget.value),
  nodes: nodes.value.map(n => {
    const properties = { ...(n.data?.properties || {}) }
    const configuredTimeout = properties.timeout_sec
    const timeoutSec = configuredTimeout === undefined || configuredTimeout === null || configuredTimeout === ''
      ? 30
      : Number(configuredTimeout)
    const stateWrites = (Array.isArray(properties.state_writes) ? properties.state_writes : [])
      .map((write: any) => ({
        path: String(write?.path || '').trim(),
        source: String(write?.source || '').trim(),
        reducer: String(write?.reducer || '').trim().toLowerCase(),
      }))
      .filter((write: any) => write.path)
    delete properties.state_writes
    if (n.type === 'tool' || n.type === 'agent') {
      const toolName = inferToolName(n, properties)
      if (toolName) properties.tool_name = toolName
    }
    return {
      ...copyExecutionMetadata(n.data?.execution || {}),
      id: n.id,
      type: n.type,
      properties,
      timeout_sec: timeoutSec,
      ...(stateWrites.length > 0 ? { writes: stateWrites } : {}),
    }
  }),
  edges: edges.value.map(e => ({
    id: e.id,
    source: e.source,
    target: e.target,
    source_handle: e.sourceHandle || 'output',
    target_handle: e.targetHandle || 'input',
  })),
  ui: {
    nodes: nodes.value.map(n => ({
      id: n.id,
      type: n.type,
      label: n.label,
      position: n.position,
      data: {
        title: n.data?.title,
        description: n.data?.description,
        status: 'idle',
        properties: n.data?.properties || {},
        execution: copyExecutionMetadata(n.data?.execution || {}),
      },
    })),
    edges: edges.value.map(e => ({
      id: e.id,
      source: e.source,
      target: e.target,
      sourceHandle: e.sourceHandle || 'output',
      targetHandle: e.targetHandle || 'input',
      type: 'straight',
    })),
  },
})

const markAllNodes = (status: string) => {
  nodes.value.forEach(n => {
    n.data.status = status
  })
}

const normalizeTraceStatus = (status: string) => {
  if (status === 'failed') return 'failed'
  if (status === 'skipped') return 'skipped'
  if (status === 'suspended') return 'suspended'
  if (status === 'success') return 'success'
  if (status === 'running') return 'running'
  return 'idle'
}

const applyNodeTraces = (traces: any[]) => {
  if (!Array.isArray(traces) || traces.length === 0) return false
  const traceMap = new Map(traces.map(trace => [trace.node_id || trace.step_id, trace]))
  nodes.value.forEach(node => {
    const trace = traceMap.get(node.id)
    node.data.status = trace ? normalizeTraceStatus(trace.status) : 'idle'
  })
  return true
}

const workflowContainsPublishTweet = () => nodes.value.some(node => {
  const props = node.data?.properties || {}
  return node.type === 'tool' && String(props.tool_name || '').toLowerCase() === 'publishtweet'
})

const normalizeWorkflowError = (value: unknown, fallback: string) => {
  const message = String(value || fallback).trim() || fallback
  const normalized = message.toLowerCase()
  if (
    normalized.includes('localhost:1234') ||
    normalized.includes('127.0.0.1:1234') ||
    (normalized.includes('lm studio') && normalized.includes('connection refused'))
  ) {
    return '当前 LLM 节点使用 LM Studio，但本地模型服务未连接。请选择该节点并切换为 DashScope，或启动 LM Studio 后重试。'
  }
  if (normalized.includes('workflow-as-tool runtime is disabled')) {
    return '当前部署未启用“发布给 Agent”。请开启 AGENT_WORKFLOW_AS_TOOL_ENABLED 和 AGENT_SKILL_CATALOG_ENABLED 后重启 Agent Service。'
  }
  if (normalized.includes('web search provider is unavailable')) {
    return '联网搜索尚未配置。当前演示请改用“站内混合检索”组件；启用联网搜索后还需配置受支持的搜索 Provider。'
  }
  return message
}

const setWorkflowError = (value: unknown, fallback: string) => {
  lastNotice.value = ''
  lastError.value = normalizeWorkflowError(value, fallback)
}

const setWorkflowNotice = (message: string) => {
  lastError.value = ''
  lastNotice.value = message
}

const saveWorkflow = async () => {
  if (isSaving.value) return ''
  isSaving.value = true
  lastError.value = ''
  lastNotice.value = ''
  try {
    const dsl = buildWorkflowDSL()
    const resp = savedWorkflowId.value
      ? await updateWorkflow(savedWorkflowId.value, { name: workflowName.value, dsl })
      : await createWorkflow({ name: workflowName.value, dsl })
    const workflow = resp.data.workflow
    savedWorkflowId.value = workflow.workflow_id
    await refreshWorkflowCatalog()
    await loadWorkflowRevisionOptions(savedWorkflowId.value, String(workflow.current_revision_id || ''))
    await loadWorkflowToolPublication()
    setWorkflowNotice('工作流已保存，节点、连线和配置将在下次进入时恢复。')
    return savedWorkflowId.value
  } catch (err: any) {
    setWorkflowError(err?.response?.data?.error || err?.message, '保存工作流失败')
    return ''
  } finally {
    isSaving.value = false
  }
}

const publishSelectedWorkflowTool = async () => {
  if (!savedWorkflowId.value || !selectedRevisionId.value || workflowToolPublicationSaving.value) return
  const current = workflowToolPublication.value
  const description = window.prompt(
    '说明这个工作流适合在什么情况下由 AI 助手调用',
    current?.description || `运行“${workflowName.value}”只读工作流。`,
  )
  if (description === null || !description.trim()) return

  workflowToolPublicationSaving.value = true
  lastError.value = ''
  lastNotice.value = ''
  try {
    const response = await publishWorkflowTool(savedWorkflowId.value, {
      workflow_revision_id: selectedRevisionId.value,
      description: description.trim(),
      expected_revision: current?.revision || 0,
    })
    workflowToolPublication.value = response.data.publication
    setWorkflowNotice(`已发布为 AI 助手工具，并固定到 v${response.data.publication.workflow_revision_number}。`)
  } catch (err: any) {
    setWorkflowError(err?.response?.data?.error || err?.message, '发布 AI 助手工具失败')
    await loadWorkflowToolPublication()
  } finally {
    workflowToolPublicationSaving.value = false
  }
}

const unpublishSelectedWorkflowTool = async () => {
  const current = workflowToolPublication.value
  if (!savedWorkflowId.value || !current || workflowToolPublicationSaving.value) return
  if (!window.confirm('停用后，AI 助手将不再发现或调用这个工作流。确认停用吗？')) return

  workflowToolPublicationSaving.value = true
  lastError.value = ''
  try {
  lastNotice.value = ''
    const response = await unpublishWorkflowTool(savedWorkflowId.value, current.revision)
    workflowToolPublication.value = response.data.publication
  } catch (err: any) {
    setWorkflowNotice('已停用该 Agent 工具，既有工作流版本仍然保留。')
    setWorkflowError(err?.response?.data?.error || err?.message, '停用 AI 助手工具失败')
    await loadWorkflowToolPublication()
  } finally {
    workflowToolPublicationSaving.value = false
  }
}

const runWorkflowSimulation = async () => {
  if (isRunning.value) return
  if (workflowContainsPublishTweet()) {
    const confirmed = window.confirm('当前工作流包含 PublishTweet，会真实发布推文。确认继续运行吗？')
    if (!confirmed) return
  }

  const workflowId = await saveWorkflow()
  if (!workflowId) return

  const userInput = window.prompt('请输入本次工作流启动内容', '你好，请帮我分析这个问题') || ''
  if (!userInput.trim()) return

  isRunning.value = true
  lastError.value = ''
  lastRunStatus.value = 'running'
  markAllNodes('running')
  lastNotice.value = ''
  try {
    const resp = await runWorkflow(workflowId, {
      workflow_revision_id: selectedRevisionId.value,
      input: { user_input: userInput },
    })
    const run = resp.data.run
    lastRunId.value = run.run_id
    lastRunStatus.value = run.status
    const hasTraces = applyNodeTraces(run.output?.traces || [])
    if (run.status === 'success') {
      if (!hasTraces) markAllNodes('success')
      setWorkflowNotice('工作流执行成功，可在“运行记录”中查看节点和工具调用。')
      return
    }
		if (run.status === 'compensated') {
			if (!hasTraces) markAllNodes('failed')
			setWorkflowError(run.error_message, '工作流主流程失败，但已完成全部补偿操作。')
			return
		}
		if (run.status === 'suspended' && run.run_id && run.approval_request_id) {
			setWorkflowNotice('工作流已安全挂起，请在审批中心处理后继续。')
			return
		}
    if (!hasTraces) markAllNodes('failed')
    setWorkflowError(run.error_message, '工作流执行失败')
  } catch (err: any) {
    markAllNodes('failed')
    lastRunStatus.value = 'failed'
    setWorkflowError(err?.response?.data?.error || err?.message, '工作流执行失败')
  } finally {
    isRunning.value = false
  }
}

const retryFailedCompensation = async () => {
  if (!lastRunId.value || isRunning.value) return
  isRunning.value = true
  lastError.value = ''
  try {
    const resp = await retryWorkflowCompensation(lastRunId.value)
    const run = resp.data.run || {}
  lastNotice.value = ''
    lastRunStatus.value = run.status || ''
    applyNodeTraces(run.output?.traces || [])
    if (compensationJournalOpen.value) await loadCompensationJournal()
    if (run.status === 'compensated') {
      setWorkflowNotice('补偿操作已恢复并全部完成。')
      return
    }
    if (run.status === 'suspended' && run.run_id && run.approval_request_id) {
      setWorkflowNotice('补偿操作需要再次审批，工作流已安全挂起。')
      return
    }
    setWorkflowError(run.error_message, '补偿操作仍未完成。')
  } catch (err: any) {
    setWorkflowError(err?.response?.data?.error || err?.message, '重试补偿失败。')
  } finally {
    isRunning.value = false
  }
}

const loadCompensationJournal = async () => {
  if (!lastRunId.value || compensationJournalLoading.value) return
  compensationJournalLoading.value = true
  compensationJournalError.value = ''
  try {
    const resp = await getWorkflowCompensationJournal(lastRunId.value)
    compensationJournalData.value = resp.data
    lastRunStatus.value = resp.data.run?.status || lastRunStatus.value
  } catch (err: any) {
    compensationJournalError.value = err?.response?.data?.error || err?.message || '加载补偿日志失败。'
  } finally {
    compensationJournalLoading.value = false
  }
}

const openCompensationJournal = async () => {
  if (!lastRunId.value) return
  compensationJournalOpen.value = true
  compensationJournalData.value = null
  await loadCompensationJournal()
}

const closeCompensationJournal = () => {
  compensationJournalOpen.value = false
}

const resetSelectedBlackboard = () => {
  selectedBlackboard.value = null
  blackboardLoading.value = false
  blackboardError.value = ''
  blackboardQuery.value = ''
  blackboardPathPrefix.value = ''
  blackboardStateVersion.value = 0
  blackboardCursor.value = ''
  blackboardCursorHistory.value = []
}

const loadSelectedBlackboard = async (cursor = '', resetHistory = false) => {
  const runId = String(selectedRunDetail.value?.run_id || '')
  if (!runId || blackboardLoading.value) return false
  const selection = runSelectionGeneration
  if (resetHistory) blackboardCursorHistory.value = []
  blackboardLoading.value = true
  blackboardError.value = ''
  try {
    const resp = await searchWorkflowBlackboard(runId, {
      state_version: Math.max(0, Number(blackboardStateVersion.value || 0)),
      query: blackboardQuery.value.trim() || undefined,
      path_prefix: blackboardPathPrefix.value.trim() || undefined,
      after_cursor: cursor || undefined,
      page_size: 20,
    })
    if (selection !== runSelectionGeneration || String(selectedRunDetail.value?.run_id || '') !== runId) return false
    selectedBlackboard.value = resp.data
    blackboardCursor.value = cursor
    return true
  } catch (err: any) {
    if (selection === runSelectionGeneration) {
      blackboardError.value = err?.response?.data?.error || err?.message || '加载 Blackboard 快照失败。'
    }
    return false
  } finally {
    if (selection === runSelectionGeneration) blackboardLoading.value = false
  }
}

const loadNextBlackboardPage = async () => {
  const nextCursor = String(selectedBlackboard.value?.next_cursor || '')
  if (!nextCursor || blackboardLoading.value) return
  const previousCursor = blackboardCursor.value
  blackboardCursorHistory.value = [...blackboardCursorHistory.value, previousCursor]
  if (!await loadSelectedBlackboard(nextCursor)) {
    blackboardCursorHistory.value = blackboardCursorHistory.value.slice(0, -1)
  }
}

const loadPreviousBlackboardPage = async () => {
  if (!blackboardCursorHistory.value.length || blackboardLoading.value) return
  const previous = blackboardCursorHistory.value[blackboardCursorHistory.value.length - 1]
  const remaining = blackboardCursorHistory.value.slice(0, -1)
  if (await loadSelectedBlackboard(previous)) {
    blackboardCursorHistory.value = remaining
  }
}

const loadWorkflowRuns = async (page = runPage.value) => {
  if (runConsoleLoading.value) return
  runConsoleLoading.value = true
  runConsoleError.value = ''
  try {
    const resp = await listWorkflowRuns({
      workflow_id: savedWorkflowId.value || undefined,
      status: runStatusFilter.value || undefined,
      page,
      page_size: runPageSize,
    })
    runItems.value = resp.data.runs || []
    runTotal.value = Number(resp.data.total || 0)
    runPage.value = Number(resp.data.page || page)
    if (selectedRunDetail.value && !runItems.value.some(item => item.run_id === selectedRunDetail.value.run_id)) {
	  runSelectionGeneration += 1
	  stopWorkflowRunStream()
      selectedRunDetail.value = null
      selectedRunTrace.value = null
      resetSelectedBlackboard()
    }
  } catch (err: any) {
    runConsoleError.value = err?.response?.data?.error || err?.message || '加载运行记录失败。'
  } finally {
    runConsoleLoading.value = false
  }
}

const openRunConsole = async () => {
	runSelectionGeneration += 1
	stopWorkflowRunStream()
  runConsoleOpen.value = true
  selectedRunDetail.value = null
  selectedRunTrace.value = null
  resetSelectedBlackboard()
  runPage.value = 1
  await loadWorkflowRuns(1)
}

const closeRunConsole = () => {
	runSelectionGeneration += 1
	stopWorkflowRunStream()
  runConsoleOpen.value = false
  resetSelectedBlackboard()
}

const stopWorkflowRunStream = () => {
  runStreamGeneration += 1
  runStreamAbortController?.abort()
  runStreamAbortController = null
  if (runStreamReconnectTimer) clearTimeout(runStreamReconnectTimer)
  runStreamReconnectTimer = null
  runStreamConnected.value = false
  runStreamReconnecting.value = false
}

const refreshSelectedRunSnapshot = async (runId: string, summary: any, selection: number) => {
  const [detailResponse, traceResponse] = await Promise.all([
    getWorkflowRun(runId),
    getWorkflowRunTrace(runId).catch(() => null),
  ])
  if (selection !== runSelectionGeneration || String(selectedRunDetail.value?.run_id || '') !== runId) return false
  selectedRunDetail.value = detailResponse.data.run || summary
  selectedRunTrace.value = traceResponse?.data || null
  lastRunId.value = runId
  lastRunStatus.value = String(selectedRunDetail.value.status || summary?.status || '')
  applyNodeTraces(selectedRunTrace.value?.steps || selectedRunDetail.value.output?.traces || [])
  return true
}

const upsertSelectedTraceRecord = (key: 'steps' | 'llm_calls' | 'tool_calls', record: Record<string, any>) => {
  if (!record?.record_id) return
  const current = selectedRunTrace.value || { run: null, steps: [], llm_calls: [], tool_calls: [] }
  const records = Array.isArray(current[key]) ? current[key] : []
  const index = records.findIndex((item: any) => item.record_id === record.record_id)
  const next = index >= 0
    ? records.map((item: any, itemIndex: number) => itemIndex === index ? record : item)
    : [...records, record]
  selectedRunTrace.value = { ...current, [key]: next }
}

const applyWorkflowRunEvent = async (runId: string, selection: number, event: WorkflowRunEvent) => {
  if (selection !== runSelectionGeneration || String(selectedRunDetail.value?.run_id || '') !== runId) return
  if (event.cursor) runEventCursor = event.cursor
  let refreshSnapshot = Boolean(event.reset)

  if (event.run) {
    const currentTrace = selectedRunTrace.value || { steps: [], llm_calls: [], tool_calls: [] }
    selectedRunTrace.value = { ...currentTrace, run: event.run }
    selectedRunDetail.value = { ...selectedRunDetail.value, status: event.run.status || selectedRunDetail.value.status }
    runItems.value = runItems.value.map(item => item.run_id === runId ? { ...item, status: event.run?.status || item.status } : item)
    lastRunStatus.value = String(event.run.status || lastRunStatus.value)
  }
  if (event.step) {
    upsertSelectedTraceRecord('steps', event.step)
    applyNodeTraces(selectedRunTrace.value?.steps || [])
  }
  if (event.llm_call) upsertSelectedTraceRecord('llm_calls', event.llm_call)
  if (event.tool_call) upsertSelectedTraceRecord('tool_calls', event.tool_call)

  if (event.terminal) {
    const terminalStatuses = new Set(['suspended', 'success', 'failed', 'rejected', 'compensated', 'compensation_failed', 'canceled'])
    if (event.reason && terminalStatuses.has(event.reason)) {
      selectedRunDetail.value = { ...selectedRunDetail.value, status: event.reason }
      lastRunStatus.value = event.reason
    }
    refreshSnapshot = true
  }
  if (refreshSnapshot) {
    await refreshSelectedRunSnapshot(runId, selectedRunDetail.value, selection)
  }
}

const startWorkflowRunStream = (runId: string, selection: number, cursor = '0-0') => {
  stopWorkflowRunStream()
  runEventCursor = cursor
  const generation = runStreamGeneration
  const controller = new AbortController()
  runStreamAbortController = controller
  let terminal = false

  void (async () => {
    try {
      runStreamReconnecting.value = false
      runStreamConnected.value = true
      await watchWorkflowRunEvents(runId, {
        afterCursor: runEventCursor,
        signal: controller.signal,
        onEvent: async event => {
          if (generation !== runStreamGeneration || controller.signal.aborted) return
          terminal = terminal || Boolean(event.terminal)
          await applyWorkflowRunEvent(runId, selection, event)
        },
      })
    } catch (err: any) {
      if (controller.signal.aborted || err?.name === 'AbortError') return
      if (generation === runStreamGeneration) runStreamReconnecting.value = true
    } finally {
      if (generation === runStreamGeneration) runStreamConnected.value = false
    }
    if (terminal || generation !== runStreamGeneration || !runConsoleOpen.value || selection !== runSelectionGeneration) return
    runStreamReconnecting.value = true
    runStreamReconnectTimer = setTimeout(() => {
      if (generation === runStreamGeneration && runConsoleOpen.value && selection === runSelectionGeneration) {
        startWorkflowRunStream(runId, selection, runEventCursor)
      }
    }, 750)
  })()
}

const selectWorkflowRun = async (summary: any) => {
  if (!summary?.run_id) return
  const runId = String(summary.run_id)
  const selection = ++runSelectionGeneration
  stopWorkflowRunStream()
  selectedRunDetail.value = summary
  selectedRunTrace.value = null
  resetSelectedBlackboard()
  selectedRunLoading.value = true
  runConsoleError.value = ''
  try {
    if (await refreshSelectedRunSnapshot(runId, summary, selection)) {
      await loadSelectedBlackboard('', true)
      startWorkflowRunStream(runId, selection)
    }
  } catch (err: any) {
    if (selection === runSelectionGeneration) {
      runConsoleError.value = err?.response?.data?.error || err?.message || '加载运行详情失败。'
    }
  } finally {
    if (selection === runSelectionGeneration) selectedRunLoading.value = false
  }
}

const changeRunPage = async (offset: number) => {
  const target = runPage.value + offset
  const maxPage = Math.max(1, Math.ceil(runTotal.value / runPageSize))
  if (target < 1 || target > maxPage) return
  await loadWorkflowRuns(target)
}

const runStatusOptions = [
  { value: '', label: '全部状态' },
  { value: 'running', label: '运行中' },
  { value: 'suspended', label: '已挂起' },
  { value: 'success', label: '成功' },
  { value: 'failed', label: '失败' },
  { value: 'rejected', label: '已拒绝' },
  { value: 'compensating', label: '补偿中' },
  { value: 'compensated', label: '已补偿' },
  { value: 'compensation_failed', label: '补偿失败' },
  { value: 'canceling', label: '取消中' },
  { value: 'canceled', label: '已取消' },
]

const cancelSelectedRun = async () => {
  const run = selectedRunDetail.value
  if (!run?.run_id || run.status !== 'running' || isRunning.value) return
  if (!window.confirm('确认停止这次工作流运行？')) return
  isRunning.value = true
  runConsoleError.value = ''
  try {
    const resp = await cancelWorkflowRun(String(run.run_id), '用户请求停止')
    const canceled = resp.data.run || {}
    selectedRunDetail.value = { ...run, ...canceled }
    runItems.value = runItems.value.map(item => item.run_id === canceled.run_id ? { ...item, ...canceled } : item)
    lastRunId.value = String(canceled.run_id || run.run_id)
    lastRunStatus.value = String(canceled.status || 'canceling')
  } catch (err: any) {
    runConsoleError.value = err?.response?.data?.error || err?.message || '停止工作流失败。'
  } finally {
    isRunning.value = false
  }
}

const openRunReplay = async () => {
  if (!lastRunId.value || replayLoading.value) return
  replayOpen.value = true
  replayLoading.value = true
  replayError.value = ''
  replayData.value = null
  try {
    const resp = await getWorkflowRunReplay(lastRunId.value)
    replayData.value = resp.data
  } catch (err: any) {
    replayError.value = err?.response?.data?.error || err?.message || '加载运行回放失败。'
  } finally {
    replayLoading.value = false
  }
}

const closeRunReplay = () => {
  replayOpen.value = false
}

const openBudgetSettings = () => {
  workflowBudget.value = normalizeWorkflowBudget(workflowBudget.value)
  budgetOpen.value = true
}

const closeBudgetSettings = () => {
  workflowBudget.value = normalizeWorkflowBudget(workflowBudget.value)
  budgetOpen.value = false
}

const formatReplayTime = (seconds: number) => {
  if (!seconds) return '—'
  return new Date(seconds * 1000).toLocaleString('zh-CN', { hour12: false })
}

const shortHash = (value: string) => value ? `${value.slice(0, 10)}…${value.slice(-6)}` : '—'
</script>

<template>
  <div class="flex h-screen bg-slate-950 font-sans overflow-hidden">
    <SidebarNodes class="hidden md:flex" @add="onPaletteAdd" />

    <div class="flex-1 flex flex-col relative" @dragover="onDragOver" @drop="onDrop">
      <div class="min-h-14 bg-slate-900 border-b border-white/10 flex flex-wrap items-center justify-between gap-2 px-4 py-2 z-10">
        <div class="flex min-w-0 items-center gap-3">
          <button
            @click="goBack"
            class="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full text-slate-400 transition-colors hover:bg-slate-800 hover:text-white"
            title="返回上一页"
          >
            <ArrowLeftIcon class="h-4 w-4" />
          </button>
          <button
            @click="mobilePaletteOpen = true"
            class="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-md border border-white/10 text-slate-300 transition-colors hover:bg-slate-800 hover:text-white md:hidden"
            title="打开组件库"
          >
            <Squares2X2Icon class="h-4 w-4" />
          </button>
          <div class="min-w-0">
          <h1 class="text-sm font-bold text-white truncate">高定制化智能助手编排器</h1>
          <p class="text-[10px] text-gray-500 truncate">
            {{ savedWorkflowId ? `Workflow: ${savedWorkflowId}` : '默认对话工作流；发推需要显式接入 PublishTweet 工具' }}
          </p>
          </div>
        </div>
        <div class="order-3 flex w-full min-w-0 items-center gap-2 sm:order-none sm:w-auto sm:flex-1 sm:justify-center">
          <select
            v-model="savedWorkflowId"
            @change="loadSelectedWorkflow"
            :disabled="isLoadingCatalog || isSaving || isRunning"
            class="h-8 min-w-0 flex-1 rounded-md border border-white/10 bg-slate-800 px-2 text-xs text-slate-200 outline-none focus:border-indigo-400 sm:max-w-56"
          >
            <option value="">新建工作流</option>
            <option v-for="workflow in workflowOptions" :key="workflow.workflow_id" :value="workflow.workflow_id">
              {{ workflow.name }}
            </option>
          </select>
          <select
            v-model="selectedRevisionId"
            @change="loadSelectedRevision"
            :disabled="!savedWorkflowId || workflowRevisionOptions.length === 0 || isSaving || isRunning"
            class="h-8 w-32 rounded-md border border-white/10 bg-slate-800 px-2 text-xs text-slate-200 outline-none focus:border-indigo-400 disabled:cursor-not-allowed disabled:opacity-50"
          >
            <option value="">选择版本</option>
            <option v-for="revision in workflowRevisionOptions" :key="revision.revision_id" :value="revision.revision_id">
              v{{ revision.revision_number }} · {{ String(revision.dsl_hash || '').slice(0, 8) }}
            </option>
          </select>
        </div>
        <div class="order-2 flex flex-wrap items-center justify-end gap-2 sm:order-none">
          <span
            v-if="workflowToolPublication?.status === 'active'"
            class="text-[10px] font-medium text-emerald-300"
            :title="workflowToolPublication.tool_name"
          >
            Agent 工具 · v{{ workflowToolPublication.workflow_revision_number }}
          </span>
          <span v-if="lastRunStatus" class="text-[10px] text-gray-400">
            {{ lastRunId ? `${lastRunStatus} · ${lastRunId}` : lastRunStatus }}
          </span>
          <button
            v-if="lastRunStatus === 'compensation_failed'"
            @click="retryFailedCompensation"
            :disabled="isRunning"
            title="重试失败的补偿操作"
            class="inline-flex h-8 items-center gap-1 rounded-md border border-amber-500/30 bg-amber-500/10 px-2 text-xs font-semibold text-amber-200 hover:bg-amber-500/20 disabled:opacity-50"
          >
            <ArrowPathIcon class="h-4 w-4" />
            <span>重试补偿</span>
          </button>
          <button
            @click="openRunConsole"
            :disabled="runConsoleLoading"
            title="查看运行记录"
            class="inline-flex h-8 items-center gap-1 rounded-md border border-white/10 bg-slate-800 px-2 text-xs font-semibold text-slate-200 hover:bg-slate-700 disabled:opacity-50"
          >
            <ListBulletIcon class="h-4 w-4" />
            <span>运行记录</span>
          </button>
          <button
            v-if="lastRunId"
            @click="openCompensationJournal"
            :disabled="compensationJournalLoading"
            title="查看补偿日志"
            class="inline-flex h-8 items-center gap-1 rounded-md border border-amber-500/30 bg-amber-500/10 px-2 text-xs font-semibold text-amber-200 hover:bg-amber-500/20 disabled:opacity-50"
          >
            <QueueListIcon class="h-4 w-4" />
            <span>补偿日志</span>
          </button>
          <button
            v-if="lastRunId"
            @click="openRunReplay"
            :disabled="replayLoading"
            title="查看只读运行回放"
            class="inline-flex h-8 items-center gap-1 rounded-md border border-sky-500/30 bg-sky-500/10 px-2 text-xs font-semibold text-sky-200 hover:bg-sky-500/20 disabled:opacity-50"
          >
            <ClockIcon class="h-4 w-4" />
            <span>运行回放</span>
          </button>
          <button
            @click="openBudgetSettings"
            :disabled="isSaving || isRunning"
            title="配置运行预算"
            class="inline-flex h-8 items-center gap-1 rounded-md border border-white/10 bg-slate-800 px-2 text-xs font-semibold text-slate-200 hover:bg-slate-700 disabled:opacity-50"
          >
            <AdjustmentsHorizontalIcon class="h-4 w-4" />
            <span>运行预算</span>
          </button>
          <button
            v-if="
              workflowToolPublication?.status === 'active' &&
              selectedRevisionId &&
              selectedRevisionId !== workflowToolPublication.workflow_revision_id
            "
            @click="publishSelectedWorkflowTool"
            :disabled="workflowToolPublicationSaving || workflowToolPublicationLoading || isSaving || isRunning"
            title="将 Agent 工具显式更新到当前选中的不可变版本"
            class="inline-flex h-8 items-center gap-1 rounded-md border border-emerald-500/30 bg-emerald-500/10 px-2 text-xs font-semibold text-emerald-200 hover:bg-emerald-500/20 disabled:opacity-50"
          >
            <ShieldCheckIcon class="h-4 w-4" />
            <span>{{ workflowToolPublicationSaving ? '更新中...' : '更新 Agent 版本' }}</span>
          </button>
          <button
            v-if="workflowToolPublication?.status === 'active'"
            @click="unpublishSelectedWorkflowTool"
            :disabled="workflowToolPublicationSaving || workflowToolPublicationLoading || isSaving || isRunning"
            title="停止向 AI 助手提供这个工作流"
            class="inline-flex h-8 items-center gap-1 rounded-md border border-rose-500/30 bg-rose-500/10 px-2 text-xs font-semibold text-rose-200 hover:bg-rose-500/20 disabled:opacity-50"
          >
            <XMarkIcon class="h-4 w-4" />
            <span>停用工具</span>
          </button>
          <button
            v-else
            @click="publishSelectedWorkflowTool"
            :disabled="!savedWorkflowId || !selectedRevisionId || workflowToolPublicationSaving || workflowToolPublicationLoading || isSaving || isRunning"
            title="将当前选中的不可变版本发布给 AI 助手"
            class="inline-flex h-8 items-center gap-1 rounded-md border border-emerald-500/30 bg-emerald-500/10 px-2 text-xs font-semibold text-emerald-200 hover:bg-emerald-500/20 disabled:cursor-not-allowed disabled:opacity-50"
          >
            <ShieldCheckIcon class="h-4 w-4" />
            <span>{{ workflowToolPublicationSaving ? '发布中...' : '发布给 Agent' }}</span>
          </button>
          <button
            @click="runWorkflowSimulation"
            :disabled="isRunning || isSaving"
            class="px-3 py-1.5 rounded-lg text-xs font-semibold bg-gradient-to-r from-blue-500 to-indigo-600 hover:from-blue-600 hover:to-indigo-700 text-white shadow-lg transition-all duration-300 disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-1.5"
          >
            <span class="relative flex h-2 w-2" v-if="isRunning">
              <span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-sky-400 opacity-75"></span>
              <span class="relative inline-flex rounded-full h-2 w-2 bg-sky-500"></span>
            </span>
            {{ isRunning ? '执行中...' : '运行测试 (Run)' }}
          </button>
          <button
            @click="saveWorkflow"
            :disabled="isSaving || isRunning"
            class="px-3 py-1.5 rounded-lg text-xs font-semibold bg-slate-800 hover:bg-slate-700 text-gray-200 hover:text-white border border-white/5 transition-all disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {{ isSaving ? '保存中...' : '保存发布 (Save)' }}
          </button>
        </div>
      </div>

      <div
        v-if="lastError || lastNotice"
        role="status"
        class="flex min-h-10 items-center justify-between gap-3 border-b px-4 py-2 text-xs"
        :class="lastError
          ? 'border-rose-500/20 bg-rose-500/10 text-rose-200'
          : 'border-emerald-500/20 bg-emerald-500/10 text-emerald-200'"
      >
        <p class="min-w-0 flex-1 break-words">{{ lastError || lastNotice }}</p>
        <button
          type="button"
          class="flex h-7 w-7 flex-none items-center justify-center text-current opacity-70 hover:opacity-100"
          title="关闭提示"
          @click="lastError = ''; lastNotice = ''"
        >
          <XMarkIcon class="h-4 w-4" />
        </button>
      </div>

      <div class="flex-1 bg-slate-950">
        <VueFlow
          v-model:nodes="nodes"
          v-model:edges="edges"
          :node-types="nodeTypes"
          :fit-view-on-init="true"
          class="interaction-flow"
          @nodeClick="onNodeClick"
          @edgeClick="onEdgeClick"
          @edgeDoubleClick="onEdgeDoubleClick"
          @paneClick="onPaneClick"
          @connect="onConnectHandler"
        >
          <Background pattern-color="#475569" :gap="16" :size="1.2" />
          <Controls position="bottom-right" />
        </VueFlow>
      </div>
    </div>

    <NodePropertiesDrawer
      :node="selectedNode"
      :nodes="nodes"
      :edges="edges"
      @update:properties="updateNodeProperties"
      @update:execution="updateNodeExecution"
      @close="onPaneClick"
    />
  </div>

  <Teleport to="body">
    <div
      v-if="mobilePaletteOpen"
      class="fixed inset-0 z-50 bg-black/65 md:hidden"
      @click.self="mobilePaletteOpen = false"
    >
      <aside class="relative h-full w-[min(86vw,320px)] shadow-2xl">
        <button
          @click="mobilePaletteOpen = false"
          class="absolute right-3 top-3 z-10 flex h-8 w-8 items-center justify-center rounded-md border border-white/10 bg-slate-800 text-slate-300 hover:text-white"
          title="关闭组件库"
        >
          <XMarkIcon class="h-4 w-4" />
        </button>
        <SidebarNodes class="!w-full !border-r-0 !pr-12" @add="onPaletteAdd" />
      </aside>
    </div>
  </Teleport>

  <Teleport to="body">
    <div
      v-if="budgetOpen"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/65 p-3 sm:p-6"
      @click.self="closeBudgetSettings"
    >
      <section
        role="dialog"
        aria-modal="true"
        aria-label="工作流运行预算"
        class="flex max-h-[86vh] w-[min(94vw,620px)] flex-col overflow-hidden rounded-lg border border-white/10 bg-slate-950 shadow-2xl"
      >
        <header class="flex min-h-14 items-center justify-between border-b border-white/10 px-4">
          <div>
            <h2 class="text-sm font-semibold text-white">运行预算</h2>
            <p class="mt-0.5 text-[11px] text-slate-500">限制单次工作流的节点、并发、时间与模型消耗</p>
          </div>
          <button
            @click="closeBudgetSettings"
            title="关闭运行预算"
            class="flex h-8 w-8 items-center justify-center text-slate-400 hover:text-white"
          >
            <XMarkIcon class="h-5 w-5" />
          </button>
        </header>

        <div class="grid overflow-y-auto px-4 py-4 text-xs sm:grid-cols-2 sm:gap-x-4">
          <label class="mb-4 block">
            <span class="mb-1.5 block font-medium text-slate-300">最大节点执行次数</span>
            <input v-model.number="workflowBudget.max_node_executions" type="number" min="1" max="1000" step="1" class="h-9 w-full rounded-md border border-white/10 bg-slate-900 px-3 text-slate-100 outline-none focus:border-sky-400" />
            <span class="mt-1 block text-[10px] text-slate-500">重试会计入执行次数</span>
          </label>
          <label class="mb-4 block">
            <span class="mb-1.5 block font-medium text-slate-300">最大并行节点数</span>
            <input v-model.number="workflowBudget.max_parallel_nodes" type="number" min="1" max="64" step="1" class="h-9 w-full rounded-md border border-white/10 bg-slate-900 px-3 text-slate-100 outline-none focus:border-sky-400" />
            <span class="mt-1 block text-[10px] text-slate-500">同一波次超过上限时排队执行</span>
          </label>
          <label class="mb-4 block">
            <span class="mb-1.5 block font-medium text-slate-300">运行超时（秒）</span>
            <input v-model.number="workflowBudget.timeout_sec" type="number" min="1" max="3600" step="1" class="h-9 w-full rounded-md border border-white/10 bg-slate-900 px-3 text-slate-100 outline-none focus:border-sky-400" />
            <span class="mt-1 block text-[10px] text-slate-500">超时会取消仍在执行的节点</span>
          </label>
          <label class="mb-4 block">
            <span class="mb-1.5 block font-medium text-slate-300">模型 Token 总上限</span>
            <input v-model.number="workflowBudget.max_total_tokens" type="number" min="1" max="10000000" step="1000" class="h-9 w-full rounded-md border border-white/10 bg-slate-900 px-3 text-slate-100 outline-none focus:border-sky-400" />
            <span class="mt-1 block text-[10px] text-slate-500">并行模型请求会先预留额度</span>
          </label>
          <label class="mb-2 block sm:col-span-2">
            <span class="mb-1.5 block font-medium text-slate-300">估算成本上限（微单位）</span>
            <input v-model.number="workflowBudget.max_estimated_cost_micros" type="number" min="0" max="1000000000000" step="1000" class="h-9 w-full rounded-md border border-white/10 bg-slate-900 px-3 text-slate-100 outline-none focus:border-sky-400" />
            <span class="mt-1 block text-[10px] text-slate-500">0 表示不启用；启用后模型必须存在可用定价</span>
          </label>
        </div>

        <footer class="flex min-h-14 items-center justify-end border-t border-white/10 px-4">
          <button @click="closeBudgetSettings" class="h-8 rounded-md bg-sky-500 px-4 text-xs font-semibold text-white hover:bg-sky-400">
            应用
          </button>
        </footer>
      </section>
    </div>
  </Teleport>

  <Teleport to="body">
    <div
      v-if="runConsoleOpen"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/65 p-3 sm:p-6"
      @click.self="closeRunConsole"
    >
      <section
        role="dialog"
        aria-modal="true"
        aria-label="工作流运行记录"
        class="flex h-[min(86vh,760px)] w-[min(96vw,960px)] flex-col overflow-hidden rounded-lg border border-white/10 bg-slate-950 shadow-2xl"
      >
        <header class="flex min-h-14 flex-wrap items-center justify-between gap-2 border-b border-white/10 px-4 py-2">
          <div class="min-w-0">
            <h2 class="truncate text-sm font-semibold text-white">运行记录</h2>
            <p class="truncate text-[11px] text-slate-500">{{ savedWorkflowId || '全部工作流' }}</p>
          </div>
          <div class="flex items-center gap-2">
            <select
              v-model="runStatusFilter"
              @change="loadWorkflowRuns(1)"
              class="h-8 rounded-md border border-white/10 bg-slate-900 px-2 text-xs text-slate-200 outline-none focus:border-sky-400"
            >
              <option v-for="option in runStatusOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
            </select>
            <button
              @click="loadWorkflowRuns(runPage)"
              :disabled="runConsoleLoading"
              title="刷新运行记录"
              class="flex h-8 w-8 items-center justify-center text-slate-400 hover:text-white disabled:opacity-50"
            >
              <ArrowPathIcon class="h-4 w-4" />
            </button>
            <button
              @click="closeRunConsole"
              title="关闭运行记录"
              class="flex h-8 w-8 items-center justify-center text-slate-400 hover:text-white"
            >
              <XMarkIcon class="h-5 w-5" />
            </button>
          </div>
        </header>

        <div v-if="runConsoleError" class="border-b border-rose-500/20 bg-rose-500/10 px-4 py-2 text-xs text-rose-300">
          {{ runConsoleError }}
        </div>
        <div class="grid min-h-0 flex-1 md:grid-cols-[320px_minmax(0,1fr)]">
          <section class="flex min-h-0 flex-col border-b border-white/10 md:border-b-0 md:border-r">
            <div v-if="runConsoleLoading" class="flex flex-1 items-center justify-center text-xs text-slate-400">正在加载运行记录…</div>
            <div v-else-if="!runItems.length" class="flex flex-1 items-center justify-center text-xs text-slate-500">暂无运行记录。</div>
            <div v-else class="min-h-0 flex-1 overflow-y-auto">
              <button
                v-for="run in runItems"
                :key="run.run_id"
                @click="selectWorkflowRun(run)"
                class="block w-full border-b border-white/10 px-4 py-3 text-left transition-colors hover:bg-slate-900"
                :class="selectedRunDetail?.run_id === run.run_id ? 'bg-slate-900' : ''"
              >
                <div class="flex items-center justify-between gap-2">
                  <span class="truncate text-xs font-semibold text-slate-100">{{ run.status }}</span>
                  <time class="flex-shrink-0 text-[10px] text-slate-500">{{ formatReplayTime(run.started_at) }}</time>
                </div>
                <p class="mt-1 truncate font-mono text-[10px] text-slate-500">{{ run.run_id }}</p>
                <p v-if="run.error_message" class="mt-1 line-clamp-2 text-[11px] text-rose-300">{{ run.error_message }}</p>
              </button>
            </div>
            <footer class="flex min-h-12 items-center justify-between border-t border-white/10 px-3 text-[11px] text-slate-500">
              <span>{{ runTotal }} 条</span>
              <div class="flex items-center gap-1">
                <button
                  @click="changeRunPage(-1)"
                  :disabled="runPage <= 1 || runConsoleLoading"
                  title="上一页"
                  class="flex h-7 w-7 items-center justify-center text-slate-400 hover:text-white disabled:opacity-30"
                >
                  <ChevronLeftIcon class="h-4 w-4" />
                </button>
                <span class="min-w-12 text-center">{{ runPage }} / {{ Math.max(1, Math.ceil(runTotal / runPageSize)) }}</span>
                <button
                  @click="changeRunPage(1)"
                  :disabled="runPage >= Math.max(1, Math.ceil(runTotal / runPageSize)) || runConsoleLoading"
                  title="下一页"
                  class="flex h-7 w-7 items-center justify-center text-slate-400 hover:text-white disabled:opacity-30"
                >
                  <ChevronRightIcon class="h-4 w-4" />
                </button>
              </div>
            </footer>
          </section>

          <section class="min-h-0 overflow-y-auto">
            <div v-if="selectedRunLoading" class="flex min-h-full items-center justify-center text-xs text-slate-400">正在加载运行详情…</div>
            <div v-else-if="!selectedRunDetail" class="flex min-h-full items-center justify-center text-xs text-slate-500">选择一条运行记录。</div>
            <div v-else>
              <div class="flex min-h-12 items-center justify-between gap-3 border-b border-white/10 px-4">
                <div class="flex min-w-0 items-center gap-2">
                  <span
                    class="h-2 w-2 flex-shrink-0 rounded-full"
                    :class="runStreamConnected ? 'bg-emerald-400' : runStreamReconnecting ? 'bg-amber-400' : 'bg-slate-600'"
                    :title="runStreamIndicatorTitle"
                  ></span>
                  <p class="truncate font-mono text-[10px] text-slate-500">{{ selectedRunDetail.run_id }}</p>
                </div>
                <button
                  v-if="selectedRunDetail.status === 'running'"
                  @click="cancelSelectedRun"
                  :disabled="isRunning"
                  class="inline-flex h-8 items-center gap-1 rounded-md border border-rose-500/30 bg-rose-500/10 px-3 text-xs font-semibold text-rose-200 hover:bg-rose-500/20 disabled:opacity-50"
                >
                  <StopIcon class="h-4 w-4" />
                  {{ isRunning ? '停止中…' : '停止运行' }}
                </button>
              </div>
              <div class="grid grid-cols-2 gap-4 border-b border-white/10 px-4 py-4 text-xs sm:grid-cols-4">
                <div>
                  <p class="text-slate-500">状态</p>
                  <p class="mt-1 font-semibold text-slate-100">{{ selectedRunDetail.status }}</p>
                </div>
                <div>
                  <p class="text-slate-500">工作流版本</p>
                  <p class="mt-1 font-semibold text-slate-100">v{{ selectedRunDetail.workflow_revision_number || 0 }}</p>
                </div>
                <div>
                  <p class="text-slate-500">状态版本</p>
                  <p class="mt-1 font-semibold text-slate-100">{{ selectedRunDetail.state_version || 0 }}</p>
                </div>
                <div>
                  <p class="text-slate-500">耗时</p>
                  <p class="mt-1 font-semibold text-slate-100">{{ selectedRunDetail.finished_at && selectedRunDetail.started_at ? `${Math.max(0, selectedRunDetail.finished_at - selectedRunDetail.started_at)} 秒` : '—' }}</p>
                </div>
                <p v-if="selectedRunDetail.error_message" class="col-span-2 break-words border-t border-white/10 pt-3 text-[11px] text-rose-300 sm:col-span-4">
                  {{ selectedRunDetail.error_message }}
                </p>
                <p v-if="selectedRunDetail.cancel_reason" class="col-span-2 break-words border-t border-white/10 pt-3 text-[11px] text-amber-300 sm:col-span-4">
                  {{ selectedRunDetail.cancel_reason }}
                </p>
              </div>

              <div v-if="selectedRunDetail.output?.budget" class="border-b border-white/10 px-4 py-3 text-[11px] text-slate-300">
                节点 {{ selectedRunDetail.output.budget.node_executions || 0 }} 次 ·
                Token {{ selectedRunDetail.output.budget.usage?.total_tokens || 0 }} ·
                成本 {{ selectedRunDetail.output.budget.usage?.estimated_cost_micros || 0 }} 微单位
              </div>

              <section class="border-b border-white/10 px-4 py-4">
                <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
                  <h3 class="text-xs font-semibold text-slate-200">Blackboard 快照</h3>
                  <span v-if="selectedBlackboard" class="text-[10px] text-slate-500">
                    v{{ selectedBlackboard.state_version || 0 }} · 基线 v{{ selectedBlackboard.base_snapshot_version || 0 }} ·
                    {{ selectedBlackboard.verified ? '已校验' : '未校验' }}
                  </span>
                </div>
                <form class="grid grid-cols-[minmax(0,1fr)_auto] gap-2 sm:grid-cols-[90px_minmax(120px,0.8fr)_minmax(160px,1fr)_auto]" @submit.prevent="loadSelectedBlackboard('', true)">
                  <input
                    v-model.number="blackboardStateVersion"
                    type="number"
                    min="0"
                    aria-label="状态版本"
                    title="状态版本，0 表示最新"
                    class="h-8 min-w-0 border border-white/10 bg-slate-900 px-2 text-xs text-slate-200 outline-none focus:border-sky-400"
                    placeholder="版本"
                  />
                  <input
                    v-model="blackboardPathPrefix"
                    aria-label="路径前缀"
                    class="h-8 min-w-0 border border-white/10 bg-slate-900 px-2 text-xs text-slate-200 outline-none focus:border-sky-400"
                    placeholder="路径前缀"
                  />
                  <input
                    v-model="blackboardQuery"
                    aria-label="搜索 Blackboard"
                    class="h-8 min-w-0 border border-white/10 bg-slate-900 px-2 text-xs text-slate-200 outline-none focus:border-sky-400"
                    placeholder="搜索路径或值"
                  />
                  <button
                    type="submit"
                    :disabled="blackboardLoading"
                    title="检索 Blackboard"
                    class="flex h-8 w-8 items-center justify-center border border-sky-500/30 bg-sky-500/10 text-sky-200 hover:bg-sky-500/20 disabled:opacity-50"
                  >
                    <MagnifyingGlassIcon class="h-4 w-4" />
                  </button>
                </form>
                <p v-if="blackboardError" class="mt-3 break-words text-[11px] text-rose-300">{{ blackboardError }}</p>
                <p v-else-if="blackboardLoading" class="py-6 text-center text-xs text-slate-500">正在读取状态快照…</p>
                <p v-else-if="!selectedBlackboard?.entries?.length" class="py-6 text-center text-xs text-slate-500">没有匹配的状态字段。</p>
                <ul v-else class="mt-3 divide-y divide-white/10 border-y border-white/10">
                  <li v-for="entry in selectedBlackboard.entries" :key="entry.path" class="py-3">
                    <div class="flex flex-wrap items-center justify-between gap-2">
                      <code class="break-all text-[11px] font-semibold text-sky-200">{{ entry.path }}</code>
                      <span class="text-[10px] text-slate-500">{{ entry.value_type }} · {{ entry.value_length || 0 }} 字节</span>
                    </div>
                    <pre v-if="entry.value_json" class="mt-2 max-h-32 overflow-auto whitespace-pre-wrap break-all bg-slate-900/60 p-2 text-[10px] leading-5 text-slate-300">{{ entry.value_json }}</pre>
                    <p v-else-if="entry.truncated" class="mt-2 text-[10px] text-amber-300">值超过预览上限</p>
                    <p class="mt-1 truncate font-mono text-[9px] text-slate-600" :title="entry.value_hash">{{ entry.value_hash }}</p>
                  </li>
                </ul>
                <footer v-if="selectedBlackboard" class="mt-3 flex items-center justify-between text-[10px] text-slate-500">
                  <span>{{ selectedBlackboard.matched_total || 0 }} 个字段</span>
                  <div class="flex items-center gap-1">
                    <button
                      @click="loadPreviousBlackboardPage"
                      :disabled="!blackboardCursorHistory.length || blackboardLoading"
                      title="上一页"
                      class="flex h-7 w-7 items-center justify-center text-slate-400 hover:text-white disabled:opacity-30"
                    >
                      <ChevronLeftIcon class="h-4 w-4" />
                    </button>
                    <button
                      @click="loadNextBlackboardPage"
                      :disabled="!selectedBlackboard.has_more || blackboardLoading"
                      title="下一页"
                      class="flex h-7 w-7 items-center justify-center text-slate-400 hover:text-white disabled:opacity-30"
                    >
                      <ChevronRightIcon class="h-4 w-4" />
                    </button>
                  </div>
                </footer>
              </section>

              <div class="px-4 py-4">
                <div class="mb-3 flex items-center justify-between">
                  <h3 class="text-xs font-semibold text-slate-200">节点 Trace</h3>
                  <span class="text-[10px] text-slate-500">{{ selectedExecutionSteps.length }} 个节点</span>
                </div>
                <p v-if="!selectedExecutionSteps.length" class="py-8 text-center text-xs text-slate-500">没有可用的节点 Trace。</p>
                <ol v-else class="border-l border-slate-700 pl-4">
                  <li v-for="trace in selectedExecutionSteps" :key="trace.record_id || trace.step_id" class="relative pb-5 last:pb-0">
                    <span class="absolute -left-[1.2rem] top-1 h-2 w-2 rounded-full" :class="trace.status === 'success' ? 'bg-emerald-400' : trace.status === 'failed' ? 'bg-rose-400' : 'bg-slate-400'"></span>
                    <div class="flex flex-wrap items-center justify-between gap-2">
                      <p class="text-xs font-semibold text-slate-100">{{ trace.step_id }} · {{ trace.step_type }}</p>
                      <span class="text-[10px] text-slate-500">{{ trace.status }} · {{ trace.duration_ms || 0 }} ms</span>
                    </div>
                    <p class="mt-1 text-[10px] text-slate-500">尝试 {{ trace.attempt || 0 }} / {{ trace.max_attempts || 0 }}</p>
                    <p v-if="trace.error_class" class="mt-1 break-words text-[11px] text-rose-300">{{ trace.error_class }}</p>
                  </li>
                </ol>

                <div class="mt-5 border-t border-white/10 pt-4">
                  <div class="mb-3 flex items-center justify-between">
                    <h3 class="text-xs font-semibold text-slate-200">模型调用</h3>
                    <span class="text-[10px] text-slate-500">{{ selectedRunTrace?.llm_calls?.length || 0 }} 次</span>
                  </div>
                  <p v-if="!selectedRunTrace?.llm_calls?.length" class="py-4 text-center text-xs text-slate-500">没有模型调用记录。</p>
                  <ul v-else class="divide-y divide-white/10">
                    <li v-for="call in selectedRunTrace.llm_calls" :key="call.record_id" class="py-3 first:pt-0">
                      <div class="flex flex-wrap items-center justify-between gap-2">
                        <p class="text-xs font-semibold text-slate-100">{{ call.step_id }} · {{ call.model || '未标记模型' }}</p>
                        <span class="text-[10px] text-slate-500">{{ call.status }} · {{ call.duration_ms || 0 }} ms</span>
                      </div>
                      <p class="mt-1 text-[10px] text-slate-500">
                        {{ call.provider || '默认 Provider' }} · Token {{ call.usage?.total_tokens || 0 }} · 输入 {{ call.prompt_length || 0 }} 字节 · 输出 {{ call.completion_length || 0 }} 字节
                      </p>
                      <p v-if="call.prompt_template_id || call.prompt_template_version" class="mt-1 break-all text-[10px] text-slate-500">
                        模板 {{ call.prompt_template_id || '未标记' }} · {{ call.prompt_template_version || '未标记版本' }}
                      </p>
                      <p class="mt-1 text-[10px] text-slate-600">
                        内容采样 {{ call.content_sample_policy || 'disabled' }} · Prompt {{ call.prompt_sample_status || 'disabled' }} · Completion {{ call.completion_sample_status || 'disabled' }}
                      </p>
                      <pre v-if="call.prompt_sample" class="mt-2 max-h-28 overflow-auto whitespace-pre-wrap break-words border-l-2 border-cyan-500/50 pl-2 text-[10px] text-slate-300">{{ call.prompt_sample }}</pre>
                      <pre v-if="call.completion_sample" class="mt-2 max-h-28 overflow-auto whitespace-pre-wrap break-words border-l-2 border-emerald-500/50 pl-2 text-[10px] text-slate-300">{{ call.completion_sample }}</pre>
                      <p v-if="call.error_class" class="mt-1 text-[11px] text-rose-300">{{ call.error_class }}</p>
                    </li>
                  </ul>
                </div>

                <div class="mt-5 border-t border-white/10 pt-4">
                  <div class="mb-3 flex items-center justify-between">
                    <h3 class="text-xs font-semibold text-slate-200">工具调用</h3>
                    <span class="text-[10px] text-slate-500">{{ selectedRunTrace?.tool_calls?.length || 0 }} 次</span>
                  </div>
                  <p v-if="!selectedRunTrace?.tool_calls?.length" class="py-4 text-center text-xs text-slate-500">没有工具调用记录。</p>
                  <ul v-else class="divide-y divide-white/10">
                    <li v-for="call in selectedRunTrace.tool_calls" :key="call.record_id" class="py-3 first:pt-0">
                      <div class="flex flex-wrap items-center justify-between gap-2">
                        <p class="text-xs font-semibold text-slate-100">{{ call.step_id }} · {{ call.tool_name }}</p>
                        <span class="text-[10px] text-slate-500">{{ call.status }} · {{ call.duration_ms || 0 }} ms</span>
                      </div>
                      <p class="mt-1 text-[10px] text-slate-500">{{ call.category || 'tool' }} · 尝试 {{ call.attempts || 0 }} 次 · 参数 {{ call.arguments_length || 0 }} 字节</p>
                      <p v-if="call.output_reference" class="mt-1 truncate text-[10px] text-amber-300/80" :title="call.output_reference">
                        {{ call.output_storage || 'object' }} 归档 · {{ call.output_length || 0 }} 字节
                      </p>
                      <p v-if="call.error_class" class="mt-1 text-[11px] text-rose-300">{{ call.error_class }}</p>
                    </li>
                  </ul>
                </div>
              </div>
            </div>
          </section>
        </div>
      </section>
    </div>
  </Teleport>

  <Teleport to="body">
    <div
      v-if="compensationJournalOpen"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/65 p-3 sm:p-6"
      @click.self="closeCompensationJournal"
    >
      <section
        role="dialog"
        aria-modal="true"
        aria-label="补偿日志"
        class="flex max-h-[82vh] w-[min(94vw,720px)] flex-col overflow-hidden rounded-lg border border-white/10 bg-slate-950 shadow-2xl"
      >
        <header class="flex min-h-14 items-center justify-between border-b border-white/10 px-4">
          <div class="min-w-0">
            <h2 class="truncate text-sm font-semibold text-white">补偿日志</h2>
            <p class="truncate text-[11px] text-slate-500">{{ lastRunId }}</p>
          </div>
          <button
            @click="closeCompensationJournal"
            title="关闭补偿日志"
            class="flex h-8 w-8 items-center justify-center text-slate-400 hover:text-white"
          >
            <XMarkIcon class="h-5 w-5" />
          </button>
        </header>

        <div v-if="compensationJournalLoading" class="flex min-h-56 items-center justify-center text-sm text-slate-400">
          正在读取补偿日志…
        </div>
        <div v-else-if="compensationJournalError" class="min-h-40 p-5 text-sm text-rose-300">
          {{ compensationJournalError }}
        </div>
        <div v-else-if="compensationJournalData" class="overflow-y-auto">
          <section class="grid grid-cols-2 gap-4 border-b border-white/10 px-4 py-4 text-xs sm:grid-cols-3">
            <div>
              <p class="text-slate-500">运行状态</p>
              <p class="mt-1 font-semibold text-slate-100">{{ compensationJournalData.run?.status || '—' }}</p>
            </div>
            <div>
              <p class="text-slate-500">下一步骤</p>
              <p class="mt-1 font-semibold text-slate-100">{{ compensationJournalData.next_sequence ? `#${compensationJournalData.next_sequence}` : '已完成' }}</p>
            </div>
            <div class="col-span-2 flex items-end justify-start sm:col-span-1 sm:justify-end">
              <button
                v-if="compensationJournalData.retry_available"
                @click="retryFailedCompensation"
                :disabled="isRunning"
                class="inline-flex h-8 items-center gap-1 rounded-md bg-amber-500 px-3 text-xs font-semibold text-slate-950 hover:bg-amber-400 disabled:opacity-50"
              >
                <ArrowPathIcon class="h-4 w-4" />
                {{ isRunning ? '重试中…' : '重试下一步' }}
              </button>
            </div>
            <p v-if="compensationJournalData.run?.error_message" class="col-span-2 break-words border-t border-white/10 pt-3 text-[11px] text-rose-300 sm:col-span-3">
              {{ compensationJournalData.run.error_message }}
            </p>
          </section>

          <section class="px-4 py-4">
            <p v-if="!compensationJournalData.entries?.length" class="py-8 text-center text-xs text-slate-500">没有生成补偿步骤。</p>
            <div v-else class="divide-y divide-white/10 border-y border-white/10">
              <div
                v-for="entry in compensationJournalData.entries"
                :key="entry.sequence"
                class="grid gap-3 py-3 text-xs sm:grid-cols-[3rem_minmax(0,1fr)_8rem] sm:items-center"
              >
                <span class="font-mono" :class="entry.is_next ? 'text-amber-300' : 'text-slate-500'">#{{ entry.sequence }}</span>
                <div class="min-w-0">
                  <p class="truncate font-semibold text-slate-100">{{ entry.tool_name }}</p>
                  <p class="truncate text-[10px] text-slate-500">{{ entry.source_node_id }} · {{ shortHash(entry.plan_hash) }}</p>
                  <p v-if="entry.error_message" class="mt-1 break-words text-[11px] text-rose-300">{{ entry.error_message }}</p>
                  <p v-if="entry.approval_request_id" class="mt-1 truncate font-mono text-[10px] text-amber-300">审批 {{ entry.approval_request_id }}</p>
                </div>
                <div class="sm:text-right">
                  <p class="font-semibold text-slate-300">{{ entry.status }}</p>
                  <p class="text-[10px] text-slate-500">尝试 {{ entry.attempt }}</p>
                  <p v-if="entry.lease_until" class="text-[10px] text-slate-500">{{ formatReplayTime(entry.lease_until) }}</p>
                </div>
              </div>
            </div>
          </section>
        </div>
      </section>
    </div>
  </Teleport>

  <Teleport to="body">
    <div
      v-if="replayOpen"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/65 p-3 sm:p-6"
      @click.self="closeRunReplay"
    >
      <section
        role="dialog"
        aria-modal="true"
        aria-label="工作流运行回放"
        class="flex max-h-[86vh] w-[min(94vw,780px)] flex-col overflow-hidden rounded-lg border border-white/10 bg-slate-950 shadow-2xl"
      >
        <header class="flex min-h-14 items-center justify-between border-b border-white/10 px-4">
          <div class="min-w-0">
            <h2 class="truncate text-sm font-semibold text-white">运行回放</h2>
            <p class="truncate text-[11px] text-slate-500">{{ lastRunId }}</p>
          </div>
          <button
            @click="closeRunReplay"
            title="关闭运行回放"
            class="flex h-8 w-8 items-center justify-center text-slate-400 hover:text-white"
          >
            <XMarkIcon class="h-5 w-5" />
          </button>
        </header>

        <div v-if="replayLoading" class="flex min-h-64 items-center justify-center text-sm text-slate-400">
          正在校验持久化证据…
        </div>
        <div v-else-if="replayError" class="min-h-40 p-5 text-sm text-rose-300">
          {{ replayError }}
        </div>
        <div v-else-if="replayData" class="overflow-y-auto">
          <section class="grid grid-cols-2 gap-x-4 gap-y-3 border-b border-white/10 px-4 py-4 text-xs sm:grid-cols-4">
            <div>
              <p class="text-slate-500">运行状态</p>
              <p class="mt-1 font-semibold text-slate-100">{{ replayData.run?.status || '—' }}</p>
            </div>
            <div>
              <p class="text-slate-500">工作流版本</p>
              <p class="mt-1 font-semibold text-slate-100">v{{ replayData.revision?.revision_number || replayData.run?.workflow_revision_number || 0 }}</p>
            </div>
            <div>
              <p class="text-slate-500">状态版本</p>
              <p class="mt-1 font-semibold text-slate-100">{{ replayData.integrity?.state_version ?? 0 }}</p>
            </div>
            <div>
              <p class="text-slate-500">完整性</p>
              <p class="mt-1 inline-flex items-center gap-1 font-semibold" :class="replayData.integrity?.verified ? 'text-emerald-300' : 'text-rose-300'">
                <ShieldCheckIcon class="h-4 w-4" />
                {{ replayData.integrity?.verified ? '已验证' : '未通过' }}
              </p>
            </div>
            <div v-if="replayData.snapshot" class="col-span-2 border-t border-white/10 pt-3 sm:col-span-4">
              <p class="text-slate-500">最近快照</p>
              <p class="mt-1 break-all font-mono text-[10px] text-slate-300">
                v{{ replayData.snapshot.state_version }} · {{ shortHash(replayData.snapshot.snapshot_hash) }} · {{ formatReplayTime(replayData.snapshot.created_at) }}
              </p>
            </div>
            <div v-if="replayData.run?.output?.budget" class="col-span-2 border-t border-white/10 pt-3 sm:col-span-4">
              <p class="text-slate-500">运行预算消耗</p>
              <p class="mt-1 text-[11px] text-slate-300">
                节点 {{ replayData.run.output.budget.node_executions || 0 }} 次 ·
                Token {{ replayData.run.output.budget.usage?.total_tokens || 0 }} ·
                成本 {{ replayData.run.output.budget.usage?.estimated_cost_micros || 0 }} 微单位
              </p>
            </div>
          </section>

          <section class="border-b border-white/10 px-4 py-4">
            <div class="mb-3 flex items-center justify-between gap-3">
              <h3 class="text-xs font-semibold text-slate-200">状态事件</h3>
              <span class="text-[11px] text-slate-500">{{ replayData.events?.length || 0 }} 条</span>
            </div>
            <p v-if="!replayData.events?.length" class="text-xs text-slate-500">本次运行没有持久状态事件。</p>
            <ol v-else class="border-l border-slate-700 pl-4">
              <li v-for="event in replayData.events" :key="event.sequence" class="relative pb-5 last:pb-0">
                <span class="absolute -left-[1.2rem] top-1 h-2 w-2 rounded-full bg-sky-400"></span>
                <div class="flex flex-wrap items-center justify-between gap-2">
                  <p class="text-xs font-semibold text-slate-100">#{{ event.sequence }} · {{ event.node_id }}</p>
                  <time class="text-[10px] text-slate-500">{{ formatReplayTime(event.applied_at) }}</time>
                </div>
                <p class="mt-1 font-mono text-[10px] text-slate-500" :title="event.event_hash">{{ shortHash(event.event_hash) }}</p>
                <pre class="mt-2 max-h-40 overflow-auto whitespace-pre-wrap break-all bg-slate-900 px-3 py-2 text-[11px] leading-5 text-slate-300">{{ JSON.stringify(event.delta, null, 2) }}</pre>
              </li>
            </ol>
          </section>

          <section class="px-4 py-4">
            <div class="mb-3 flex items-center justify-between gap-3">
              <h3 class="text-xs font-semibold text-slate-200">补偿步骤</h3>
              <span class="text-[11px] text-slate-500">{{ replayData.compensations?.length || 0 }} 条</span>
            </div>
            <p v-if="!replayData.compensations?.length" class="text-xs text-slate-500">本次运行没有生成补偿计划。</p>
            <div v-else class="divide-y divide-white/10 border-y border-white/10">
              <div v-for="item in replayData.compensations" :key="item.sequence" class="grid gap-2 py-3 text-xs sm:grid-cols-[3rem_minmax(0,1fr)_7rem] sm:items-center">
                <span class="font-mono text-slate-500">#{{ item.sequence }}</span>
                <div class="min-w-0">
                  <p class="truncate font-semibold text-slate-100">{{ item.tool_name }}</p>
                  <p class="truncate text-[10px] text-slate-500" :title="item.plan_hash">{{ item.source_node_id }} · {{ shortHash(item.plan_hash) }}</p>
                  <p v-if="item.error_message" class="mt-1 break-words text-[11px] text-rose-300">{{ item.error_message }}</p>
                </div>
                <div class="sm:text-right">
                  <p class="font-semibold text-slate-300">{{ item.status }}</p>
                  <p class="text-[10px] text-slate-500">尝试 {{ item.attempt }}</p>
                </div>
              </div>
            </div>
          </section>
        </div>
      </section>
    </div>
  </Teleport>
</template>

<style>
.vue-flow__edge-path {
  stroke: #6366f1 !important;
  stroke-width: 2.25 !important;
}

.vue-flow__edge.selected .vue-flow__edge-path,
.vue-flow__edge:focus .vue-flow__edge-path {
  stroke: #38bdf8 !important;
  stroke-width: 3 !important;
}

.vue-flow__edge {
  cursor: pointer;
}

.vue-flow__connection-path {
  stroke: #a5b4fc !important;
  stroke-width: 2 !important;
}

.vue-flow__controls {
  background: #0f172a !important;
  border: 1px solid rgba(255, 255, 255, 0.1) !important;
  border-radius: 8px !important;
  box-shadow: 0 4px 6px -1px rgb(0 0 0 / 0.1) !important;
}

.vue-flow__controls-button {
  border-bottom: 1px solid rgba(255, 255, 255, 0.05) !important;
  fill: #94a3b8 !important;
}

.vue-flow__controls-button:hover {
  background: #1e293b !important;
  fill: #ffffff !important;
}
</style>
