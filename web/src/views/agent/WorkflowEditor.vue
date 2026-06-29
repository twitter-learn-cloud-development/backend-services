<script setup lang="ts">
import { ref, markRaw, onMounted, onUnmounted, provide } from 'vue'
import { useRouter } from 'vue-router'
import { VueFlow, useVueFlow } from '@vue-flow/core'
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import { ArrowLeftIcon } from '@heroicons/vue/24/outline'
import { createWorkflow, getWorkflow, listWorkflows, runWorkflow, updateWorkflow } from '../../api/agent'

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
const lastRunId = ref('')
const lastRunStatus = ref('')
const lastError = ref('')

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

const defaultPropertiesForNode = (type: string, title: string, preset = '') => {
  if (type === 'llm') {
    const writer = preset === 'llm_writer' || title.toLowerCase().includes('writer') || title.includes('创作')
    const planner = preset === 'llm_planner' || title.toLowerCase().includes('planner') || title.includes('规划')
    return {
      mode: planner ? 'planner' : (writer ? 'writer' : 'chat'),
      provider: 'lmstudio',
      base_url: 'http://localhost:1234/v1',
      model: 'qwen2.5-3b-instruct',
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
  if (type === 'agent') {
    return {
      tool_name: preset === 'plan_executor' ? 'PlanExecutor' : 'ReActAgent',
      objective: '{{start.user_input}}',
      plan: '',
      allowed_tools: 'hybrid_search_tweets,search_users,get_user_tweets',
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
  if (type === 'wait') return { reason: 'waiting for external callback', resume_token: `resume_${Date.now()}`, timeout_sec: 0 }
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
  if (type === 'tool') return properties.tool_name ? `${properties.tool_name}` : '工具调用'
  if (type === 'agent') return properties.tool_name === 'PlanExecutor' ? '计划执行器' : 'ReAct 智能体'
  if (type === 'router') return '条件路由'
  if (type === 'wait') return '人工审批'
  return type.toUpperCase()
}

const nodeDescription = (type: string, properties: Record<string, any> = {}) => {
  if (properties.prompt) return `Prompt: ${String(properties.prompt).replace(/\s+/g, ' ').slice(0, 80)}`
  if (properties.content) return `推文: ${properties.content}`
  if (properties.query) return `Query: ${properties.query}`
  if (properties.objective) return `目标: ${properties.objective}`
  if (type === 'start') return '接收启动入参。'
  if (type === 'end') return '输出最终执行详情。'
  return '已从保存的 DSL 恢复。'
}

const makeNode = (id: string, type: string, position: { x: number; y: number }, saved: any = {}, properties: Record<string, any> = {}) => ({
  id,
  type,
  label: saved.label || nodeTitle(type, properties),
  position,
  data: {
    title: nodeTitle(type, properties),
    description: nodeDescription(type, properties),
    status: 'idle',
    properties,
  },
})

const normalizeRestoredProperties = (type: string, properties: Record<string, any>) => {
  const next = { ...properties }
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
  workflowName.value = workflow?.name || dsl.name || workflowName.value
  savedWorkflowId.value = workflow?.workflow_id || ''

  const uiNodes = Array.isArray(dsl.ui?.nodes) ? dsl.ui.nodes : []
  const uiNodeMap = new Map<string, any>(uiNodes.map((n: any) => [n.id, n]))
  const dslNodes = Array.isArray(dsl.nodes) && dsl.nodes.length > 0
    ? dsl.nodes
    : uiNodes.map((n: any) => ({ id: n.id, type: n.type, properties: n.data?.properties || {} }))

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
    return makeNode(n.id, n.type, positions.get(n.id) || { x: 120 + index * 320, y: 170 }, saved, properties)
  })

  selectedEdgeId.value = ''
  edges.value = dslEdges
    .filter((e: any) => e.source && e.target)
    .map((e: any) => createEdge(e.source, e.target, e))
}

const loadSavedWorkflow = async () => {
  try {
    const listResp = await listWorkflows({ page: 1, page_size: 1 })
    const first = listResp.data.workflows?.[0]
    if (!first?.workflow_id) {
      loadDefaultWorkflow()
      return
    }
    const detailResp = await getWorkflow(first.workflow_id)
    restoreWorkflowFromDSL(detailResp.data.workflow)
  } catch (err) {
    console.error('Failed to load saved workflow', err)
    loadDefaultWorkflow()
  }
}

const onKeyDown = (event: KeyboardEvent) => {
  if ((event.key === 'Delete' || event.key === 'Backspace') && selectedEdgeId.value) {
    event.preventDefault()
    deleteEdge(selectedEdgeId.value)
  }
}

onMounted(() => {
  loadSavedWorkflow()
  window.addEventListener('keydown', onKeyDown)
})

onUnmounted(() => {
  window.removeEventListener('keydown', onKeyDown)
})

const onDragOver = (event: DragEvent) => {
  event.preventDefault()
  if (event.dataTransfer) event.dataTransfer.dropEffect = 'move'
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
  const newNodeID = `node_${type}_${Date.now()}`
  const properties = defaultPropertiesForNode(type, title, preset)
  nodes.value.push(makeNode(newNodeID, type, position, { label: title, data: { title } }, properties))
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

const inferToolName = (node: any, properties: Record<string, any>) => {
  if (properties.tool_name) return properties.tool_name
  const title = `${node.data?.title || ''} ${node.label || ''}`.toLowerCase()
  if (title.includes('websearch') || title.includes('search') || properties.query) return 'WebSearch'
  if (title.includes('publishtweet') || title.includes('tweet') || properties.content) return 'PublishTweet'
  return ''
}

const buildWorkflowDSL = () => ({
  name: workflowName.value,
  nodes: nodes.value.map(n => {
    const properties = { ...(n.data?.properties || {}) }
    if (n.type === 'tool' || n.type === 'agent') {
      const toolName = inferToolName(n, properties)
      if (toolName) properties.tool_name = toolName
    }
    return {
      id: n.id,
      type: n.type,
      properties,
      timeout_sec: Number(properties.timeout_sec || 30),
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
  const traceMap = new Map(traces.map(trace => [trace.node_id, trace]))
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

const saveWorkflow = async () => {
  if (isSaving.value) return ''
  isSaving.value = true
  lastError.value = ''
  try {
    const dsl = buildWorkflowDSL()
    const resp = savedWorkflowId.value
      ? await updateWorkflow(savedWorkflowId.value, { name: workflowName.value, dsl })
      : await createWorkflow({ name: workflowName.value, dsl })
    savedWorkflowId.value = resp.data.workflow.workflow_id
    alert('工作流 DSL 已保存。')
    return savedWorkflowId.value
  } catch (err: any) {
    lastError.value = err?.response?.data?.error || err?.message || '保存工作流失败'
    alert(lastError.value)
    return ''
  } finally {
    isSaving.value = false
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
  try {
    const resp = await runWorkflow(workflowId, { input: { user_input: userInput } })
    const run = resp.data.run
    lastRunId.value = run.run_id
    lastRunStatus.value = run.status
    const hasTraces = applyNodeTraces(run.output?.traces || [])
    if (run.status === 'success') {
      if (!hasTraces) markAllNodes('success')
      alert('工作流执行成功。')
      return
    }
    if (!hasTraces) markAllNodes('failed')
    lastError.value = run.error_message || '工作流执行失败'
    alert(lastError.value)
  } catch (err: any) {
    markAllNodes('failed')
    lastRunStatus.value = 'failed'
    lastError.value = err?.response?.data?.error || err?.message || '工作流执行失败'
    alert(lastError.value)
  } finally {
    isRunning.value = false
  }
}
</script>

<template>
  <div class="flex h-screen bg-slate-950 font-sans overflow-hidden">
    <SidebarNodes />

    <div class="flex-1 flex flex-col relative" @dragover="onDragOver" @drop="onDrop">
      <div class="h-14 bg-slate-900 border-b border-white/10 flex items-center justify-between px-4 z-10">
        <div class="flex min-w-0 items-center gap-3">
          <button
            @click="goBack"
            class="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full text-slate-400 transition-colors hover:bg-slate-800 hover:text-white"
            title="返回上一页"
          >
            <ArrowLeftIcon class="h-4 w-4" />
          </button>
          <div class="min-w-0">
          <h1 class="text-sm font-bold text-white truncate">高定制化智能助手编排器</h1>
          <p class="text-[10px] text-gray-500 truncate">
            {{ savedWorkflowId ? `Workflow: ${savedWorkflowId}` : '默认对话工作流；发推需要显式接入 PublishTweet 工具' }}
          </p>
          </div>
        </div>
        <div class="flex items-center gap-3">
          <span v-if="lastRunStatus" class="text-[10px] text-gray-400">
            {{ lastRunId ? `${lastRunStatus} · ${lastRunId}` : lastRunStatus }}
          </span>
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
      @close="onPaneClick"
    />
  </div>
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
