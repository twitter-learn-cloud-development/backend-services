<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { PlusIcon, XMarkIcon } from '@heroicons/vue/24/outline'
import {
  listExternalMCPConnections,
  listExternalMCPTools,
  listProviderConfigs,
  type ExternalMCPToolView,
} from '../../api/agent'

interface GraphNode {
  id: string
  type: string
  label?: string
  data?: any
}

interface GraphEdge {
  source: string
  target: string
}

const props = defineProps<{
  node: GraphNode | null
  nodes: GraphNode[]
  edges: GraphEdge[]
}>()

const emit = defineEmits<{
  (e: 'update:properties', nodeId: string, properties: any): void
  (e: 'update:execution', nodeId: string, execution: any): void
  (e: 'close'): void
}>()

const localProps = ref<any>({})
const localExecution = ref<any>({})
const providerConfigs = ref<any[]>([])
const webSearchProviderConfigs = ref<any[]>([])
const compensationInputs = ref<{ key: string; value: string }[]>([])
const externalMCPTools = ref<(ExternalMCPToolView & { connection_name: string })[]>([])
const externalMCPToolsLoading = ref(false)
const externalMCPArgumentsText = ref('{}')
const externalMCPArgumentsError = ref('')

interface StateWriteConfig {
  path: string
  source: string
  reducer: string
}

const reducerOptions = [
  { value: '', label: '直接写入' },
  { value: 'append', label: '追加 (Append)' },
  { value: 'sum', label: '求和 (Sum)' },
  { value: 'min', label: '最小值 (Min)' },
  { value: 'max', label: '最大值 (Max)' },
  { value: 'merge', label: '对象合并 (Merge)' },
  { value: 'first', label: '保留首值 (First)' },
  { value: 'last', label: '保留末值 (Last)' },
]

const loadProviderConfigs = async () => {
  try {
    const response = await listProviderConfigs({ page: 1, page_size: 100, kind: 'llm' })
    providerConfigs.value = (response.data?.provider_configs || []).filter((item: any) => item.status === 'active')
  } catch {
    providerConfigs.value = []
  }
}

const loadWebSearchProviderConfigs = async () => {
  try {
    const response = await listProviderConfigs({ page: 1, page_size: 100, kind: 'web_search' })
    webSearchProviderConfigs.value = (response.data?.provider_configs || []).filter((item: any) => item.status === 'active')
  } catch {
    webSearchProviderConfigs.value = []
  }
}

const loadExternalMCPTools = async () => {
  externalMCPToolsLoading.value = true
  try {
    const response = await listExternalMCPConnections({ page: 1, page_size: 100 })
    const connections = (response.data?.connections || [])
      .filter(connection => connection.status === 'active' && connection.discovery_status === 'ready')
      .slice(0, 20)
    const catalogs = await Promise.all(connections.map(async connection => {
      try {
        const catalog = await listExternalMCPTools(connection.connection_id)
        return (catalog.data?.tools || [])
          .filter(tool => tool.policy.enabled && ['read', 'write', 'risky'].includes(tool.policy.category))
          .map(tool => ({ ...tool, connection_name: connection.name }))
      } catch {
        return []
      }
    }))
    externalMCPTools.value = catalogs
      .flat()
      .sort((left, right) => left.schema.qualified_name.localeCompare(right.schema.qualified_name))
  } catch {
    externalMCPTools.value = []
  } finally {
    externalMCPToolsLoading.value = false
  }
}

const platformLLMDefaults = {
  provider: 'dashscope',
  base_url: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
  credential_ref: '',
  model: 'qwen-plus',
}

const lmStudioLLMDefaults = {
  provider: 'lmstudio',
  base_url: 'http://localhost:1234/v1',
  credential_ref: '',
  model: 'qwen2.5-3b-instruct',
}

const applyInlineLLMProviderDefaults = (provider: string) => {
  const defaults = provider === 'lmstudio'
    ? lmStudioLLMDefaults
    : provider === 'dashscope'
      ? platformLLMDefaults
      : { provider: 'custom', base_url: '', credential_ref: '', model: '' }
  Object.assign(localProps.value, defaults)
}

const chatDefaults = {
  mode: 'chat',
  system_prompt: '你是一个通用对话助手。只回答当前用户输入，保持准确、简洁、可执行；不要主动发布内容，不要引入与当前问题无关的领域素材。',
  prompt: '{{start.user_input}}',
  max_tokens: 1024,
}

const writerDefaults = {
  mode: 'writer',
  system_prompt: '你是一个专业内容创作助手。只围绕用户当前主题写作；如果上游参考内容与主题不相关，必须忽略，禁止混入无关领域概念。',
  prompt: `请根据用户输入写一组可直接发布的高质量内容草稿。

用户输入：{{start.user_input}}

要求：
1. 输出 3 条候选，每条都包含「角度」「正文」「适用场景」。
2. 除非用户明确要求极短，否则每条「正文」不少于 180 个中文字符。
3. 只围绕用户主题展开，禁止混入与主题无关的素材。`,
  max_tokens: 2048,
}

const plannerDefaults = {
  mode: 'planner',
  system_prompt: '你是任务规划器。把目标拆成有序、可执行、可验证的步骤；只制定计划，不声称已经执行。',
  prompt: `请为以下目标制定执行计划：

{{start.user_input}}

每一步包含目标、所需输入、建议工具、依赖关系和验收标准。`,
  max_tokens: 1024,
}

const toolNameOf = (node: GraphNode | null) => String(localProps.value.tool_name || node?.data?.properties?.tool_name || '')

const isPublishTweet = (node: GraphNode | null) => {
  if (!node || node.type !== 'tool') return false
  const title = `${node.data?.title || ''} ${node.label || ''}`.toLowerCase()
  const toolName = `${node.data?.properties?.tool_name || localProps.value.tool_name || ''}`.toLowerCase()
  return title.includes('publishtweet') || toolName === 'publishtweet'
}

const isWebSearch = (node: GraphNode | null) => {
  if (!node || node.type !== 'tool') return false
  const title = `${node.data?.title || ''} ${node.label || ''}`.toLowerCase()
  const toolName = `${node.data?.properties?.tool_name || localProps.value.tool_name || ''}`.toLowerCase()
  return title.includes('websearch') || toolName === 'websearch'
}

const isPageRead = (node: GraphNode | null) => {
  if (!node || node.type !== 'tool') return false
  const title = `${node.data?.title || ''} ${node.label || ''}`.toLowerCase()
  const toolName = `${node.data?.properties?.tool_name || localProps.value.tool_name || ''}`.toLowerCase()
  return title.includes('pageread') || title.includes('page read') || toolName === 'pageread'
}

const isAgentStrategy = (node: GraphNode | null) => node?.type === 'agent'
const isPlanExecutor = (node: GraphNode | null) => toolNameOf(node).toLowerCase() === 'planexecutor'
const isHybridTweetSearch = (node: GraphNode | null) => toolNameOf(node).toLowerCase() === 'hybridtweetsearch'
const isSemanticTweetSearch = (node: GraphNode | null) => toolNameOf(node).toLowerCase() === 'semantictweetsearch'
const isSearchUsers = (node: GraphNode | null) => toolNameOf(node).toLowerCase() === 'searchusers'
const isGetUserTweets = (node: GraphNode | null) => toolNameOf(node).toLowerCase() === 'getusertweets'
const isGetTweetsByIDs = (node: GraphNode | null) => toolNameOf(node).toLowerCase() === 'gettweetsbyids'
const isExternalMCPTool = (node: GraphNode | null) => {
  if (!node || node.type !== 'tool') return false
  return localProps.value.external_mcp === true || /^mcp_[A-Za-z0-9_-]+\.[A-Za-z0-9_.-]+$/.test(toolNameOf(node))
}
const selectedExternalMCPTool = computed(() => (
  externalMCPTools.value.find(tool => tool.schema.qualified_name === localProps.value.tool_name) || null
))
const externalMCPPolicyLabel = (category: ExternalMCPToolView['policy']['category']) => {
  if (category === 'read') return '只读'
  if (category === 'write') return '幂等写入 · 逐次审批'
  return '高风险 · 逐次审批'
}
const externalMCPPolicyClass = (category: ExternalMCPToolView['policy']['category']) => {
  if (category === 'read') return 'text-emerald-300'
  if (category === 'write') return 'text-blue-300'
  return 'text-amber-300'
}
const isMCPTool = (node: GraphNode | null) => (
  isHybridTweetSearch(node) ||
  isSemanticTweetSearch(node) ||
  isSearchUsers(node) ||
  isGetUserTweets(node) ||
  isGetTweetsByIDs(node)
)

watch(() => props.node, (newNode) => {
  if (!newNode) {
    localProps.value = {}
    localExecution.value = {}
    externalMCPArgumentsText.value = '{}'
    externalMCPArgumentsError.value = ''
    return
  }

  localProps.value = JSON.parse(JSON.stringify(newNode.data?.properties || {}))
	localExecution.value = JSON.parse(JSON.stringify(newNode.data?.execution || {}))
	if (!Array.isArray(localProps.value.state_writes)) localProps.value.state_writes = []
	const compensationProperties = localExecution.value.compensation?.properties
	compensationInputs.value = compensationProperties && typeof compensationProperties === 'object' && !Array.isArray(compensationProperties)
		? Object.entries(compensationProperties).map(([key, value]) => ({
			key,
			value: typeof value === 'string' ? value : JSON.stringify(value),
		}))
		: []

  if (newNode.type === 'llm') {
	localProps.value.provider_config_id ||= ''
	delete localProps.value.api_key
	if (localProps.value.provider_config_id) {
	  delete localProps.value.provider
	  delete localProps.value.base_url
	  delete localProps.value.credential_ref
	  delete localProps.value.model
	} else {
	  localProps.value.provider ||= platformLLMDefaults.provider
	  localProps.value.base_url ||= platformLLMDefaults.base_url
	  localProps.value.credential_ref ||= ''
	  localProps.value.model ||= platformLLMDefaults.model
	}
	void loadProviderConfigs()
    localProps.value.mode ||= 'chat'
    if (localProps.value.mode === 'planner') {
      localProps.value.system_prompt ||= plannerDefaults.system_prompt
      localProps.value.prompt ||= plannerDefaults.prompt
      localProps.value.max_tokens ||= plannerDefaults.max_tokens
    } else if (localProps.value.mode === 'writer') {
      localProps.value.system_prompt ||= writerDefaults.system_prompt
      localProps.value.prompt ||= writerDefaults.prompt
      localProps.value.max_tokens ||= writerDefaults.max_tokens
    } else {
      localProps.value.system_prompt ||= chatDefaults.system_prompt
      localProps.value.prompt ||= chatDefaults.prompt
      localProps.value.max_tokens ||= chatDefaults.max_tokens
    }
  }

  if (isPublishTweet(newNode)) {
    localProps.value.tool_name ||= 'PublishTweet'
    localProps.value.content ||= ''
    localProps.value.max_chars ||= 10000
    localProps.value.overflow_strategy ||= 'error'
  }

  if (isWebSearch(newNode)) {
    localProps.value.tool_name ||= 'WebSearch'
    localProps.value.query ||= '{{start.user_input}}'
    localProps.value.count ||= 5
	localProps.value.provider_config_id ||= ''
	void loadWebSearchProviderConfigs()
  }

  if (isPageRead(newNode)) {
    localProps.value.tool_name ||= 'PageRead'
    localProps.value.url ||= ''
    localProps.value.max_runes ||= 16000
  }

  if (isExternalMCPTool(newNode)) {
    localProps.value.external_mcp = true
    if (!localProps.value.mcp_arguments || typeof localProps.value.mcp_arguments !== 'object' || Array.isArray(localProps.value.mcp_arguments)) {
      localProps.value.mcp_arguments = {}
    }
    externalMCPArgumentsText.value = JSON.stringify(localProps.value.mcp_arguments, null, 2)
    externalMCPArgumentsError.value = ''
    void loadExternalMCPTools()
  }

  if (isHybridTweetSearch(newNode) || isSemanticTweetSearch(newNode)) {
    localProps.value.query ||= '{{start.user_input}}'
    localProps.value.size ||= 5
  }
  if (isSearchUsers(newNode)) {
    localProps.value.keyword ||= '{{start.user_input}}'
    localProps.value.limit ||= 5
  }
  if (isGetUserTweets(newNode)) {
    localProps.value.user_id ||= ''
    localProps.value.limit ||= 10
  }
  if (isGetTweetsByIDs(newNode)) {
    localProps.value.tweet_ids ||= ''
  }
  if (isAgentStrategy(newNode)) {
    localProps.value.tool_name ||= 'ReActAgent'
    localProps.value.objective ||= '{{start.user_input}}'
    localProps.value.allowed_tools ||= 'hybrid_search_tweets,search_users,get_user_tweets,web_search,page_read'
    localProps.value.max_iterations ||= 5
    localProps.value.model ||= 'qwen-plus'
    localProps.value.max_tokens ||= 2048
  }

  if (newNode.type === 'wait') {
    localProps.value.resume_mode ||= 'external_callback'
    localProps.value.reason ||= localProps.value.resume_mode === 'human_input'
      ? '请补充继续执行所需的信息'
      : '等待外部回调'
    if (localProps.value.resume_mode === 'human_input') {
      delete localProps.value.resume_token
    }
  }
}, { immediate: true })

const updateProps = () => {
  if (props.node) emit('update:properties', props.node.id, localProps.value)
}

const updateWaitProperties = () => {
  if (localProps.value.resume_mode === 'human_input') {
    delete localProps.value.resume_token
  }
  updateProps()
}

const updateExecution = () => {
  if (props.node) emit('update:execution', props.node.id, localExecution.value)
}

const updateExternalMCPArguments = () => {
  try {
    const value = JSON.parse(externalMCPArgumentsText.value || '{}')
    if (!value || typeof value !== 'object' || Array.isArray(value)) {
      throw new Error('arguments must be an object')
    }
    localProps.value.mcp_arguments = value
    externalMCPArgumentsError.value = ''
    updateProps()
  } catch {
    externalMCPArgumentsError.value = '参数必须是 JSON 对象'
  }
}

const selectExternalMCPTool = () => {
  localProps.value.external_mcp = true
  updateProps()
}

const retryEnabled = computed({
  get: () => !!localExecution.value.retry,
  set: (enabled: boolean) => {
    if (enabled) {
      localExecution.value.retry = {
        max_attempts: 3,
        initial_backoff_ms: 100,
        max_backoff_ms: 2000,
        multiplier: 2,
        jitter: 0,
      }
    } else {
      delete localExecution.value.retry
    }
    updateExecution()
  },
})

const compensationEnabled = computed({
	get: () => props.node?.type === 'tool' && !isExternalMCPTool(props.node) && !!localExecution.value.compensation,
	set: (enabled: boolean) => {
		if (enabled) {
			localExecution.value.compensation = {
				tool_name: '',
				properties: {},
				timeout_sec: 30,
			}
			compensationInputs.value = []
		} else {
			delete localExecution.value.compensation
			compensationInputs.value = []
		}
		updateExecution()
	},
})

const compensationRetryEnabled = computed({
	get: () => !!localExecution.value.compensation?.retry,
	set: (enabled: boolean) => {
		if (!localExecution.value.compensation) return
		if (enabled) {
			localExecution.value.compensation.retry = {
				max_attempts: 3,
				initial_backoff_ms: 100,
				max_backoff_ms: 2000,
				multiplier: 2,
				jitter: 0,
			}
		} else {
			delete localExecution.value.compensation.retry
		}
		updateExecution()
	},
})

const decodeCompensationInputValue = (value: string) => {
	const normalized = value.trim()
	if (!normalized) return ''
	if (!/^(?:\{|\[|true$|false$|null$|-?\d)/.test(normalized)) return value
	try {
		return JSON.parse(normalized)
	} catch {
		return value
	}
}

const syncCompensationInputs = () => {
	if (!localExecution.value.compensation) return
	const properties: Record<string, any> = {}
	compensationInputs.value.forEach(input => {
		const key = input.key.trim()
		if (key) properties[key] = decodeCompensationInputValue(input.value)
	})
	localExecution.value.compensation.properties = properties
	updateExecution()
}

const addCompensationInput = () => {
	compensationInputs.value.push({ key: '', value: '' })
}

const removeCompensationInput = (index: number) => {
	compensationInputs.value.splice(index, 1)
	syncCompensationInputs()
}

const addStateWrite = () => {
  const writes = localProps.value.state_writes as StateWriteConfig[]
  writes.push({ path: '', source: '', reducer: '' })
  updateProps()
}

const removeStateWrite = (index: string | number) => {
  const writes = localProps.value.state_writes as StateWriteConfig[]
  writes.splice(Number(index), 1)
  updateProps()
}

const changeProviderConfig = () => {
  if (localProps.value.provider_config_id) {
    delete localProps.value.provider
    delete localProps.value.base_url
    delete localProps.value.credential_ref
    delete localProps.value.model
  } else {
    applyInlineLLMProviderDefaults('dashscope')
  }
  updateProps()
}

const changeInlineProvider = () => {
  applyInlineLLMProviderDefaults(String(localProps.value.provider || 'dashscope'))
  updateProps()
}

const switchLLMMode = () => {
  if (localProps.value.mode === 'planner') {
    localProps.value.system_prompt = plannerDefaults.system_prompt
    localProps.value.prompt = plannerDefaults.prompt
    localProps.value.max_tokens = plannerDefaults.max_tokens
  } else if (localProps.value.mode === 'writer') {
    localProps.value.system_prompt = writerDefaults.system_prompt
    localProps.value.prompt = writerDefaults.prompt
    localProps.value.max_tokens = Math.max(Number(localProps.value.max_tokens || 0), writerDefaults.max_tokens)
  } else {
    localProps.value.system_prompt = chatDefaults.system_prompt
    localProps.value.prompt = chatDefaults.prompt
    localProps.value.max_tokens = chatDefaults.max_tokens
  }
  updateProps()
}

const ancestorNodes = computed(() => {
  if (!props.node) return []
  const visited = new Set<string>()
  const result: GraphNode[] = []

  const dfs = (currId: string) => {
    for (const edge of props.edges) {
      if (edge.target === currId && !visited.has(edge.source)) {
        visited.add(edge.source)
        const srcNode = props.nodes.find(n => n.id === edge.source)
        if (srcNode) result.push(srcNode)
        dfs(edge.source)
      }
    }
  }

  dfs(props.node.id)
  return result
})

const availableVariables = computed(() => {
  const vars: { label: string; value: string }[] = []
  for (const node of ancestorNodes.value) {
    const nodeLabel = node.data?.title || node.label || node.id
    if (node.type === 'start') {
      vars.push({ label: `${nodeLabel} (用户输入)`, value: `{{${node.id}.user_input}}` })
    } else if (node.type === 'llm') {
      vars.push({ label: `${nodeLabel} (大模型输出)`, value: `{{${node.id}.text}}` })
    } else if (node.type === 'agent') {
      vars.push({ label: `${nodeLabel} (智能体输出)`, value: `{{${node.id}.text}}` })
    } else if (node.type === 'tool' && String(node.data?.properties?.tool_name || '').toLowerCase() === 'websearch') {
      vars.push({ label: `${nodeLabel} (搜索结果)`, value: `{{${node.id}.results}}` })
    } else if (node.type === 'tool' && String(node.data?.properties?.tool_name || '').toLowerCase() === 'publishtweet') {
      vars.push({ label: `${nodeLabel} (推文 ID)`, value: `{{${node.id}.tweet_id}}` })
    } else if (node.type === 'tool') {
      vars.push({ label: `${nodeLabel} (工具结果)`, value: `{{${node.id}.result}}` })
    }
  }
  return vars
})

const insertVariable = (variable: string, field: string) => {
  if (!localProps.value[field]) localProps.value[field] = ''
  localProps.value[field] += variable
  updateProps()
}

const primaryInputField = () => {
  if (props.node?.type === 'llm') return 'prompt'
  if (isAgentStrategy(props.node)) return isPlanExecutor(props.node) ? 'plan' : 'objective'
  if (isPublishTweet(props.node)) return 'content'
  if (isSearchUsers(props.node)) return 'keyword'
  if (isGetUserTweets(props.node)) return 'user_id'
  if (isGetTweetsByIDs(props.node)) return 'tweet_ids'
  return 'query'
}
</script>

<template>
  <div
    v-if="node"
    class="fixed inset-y-0 right-0 z-40 flex h-full w-[min(100vw,20rem)] flex-col overflow-y-auto border-l border-white/10 bg-slate-900 p-4 text-white shadow-2xl md:static md:z-auto md:w-80 md:flex-shrink-0 md:shadow-none"
  >
    <div class="flex items-center justify-between border-b border-white/5 pb-3 mb-4">
      <div>
        <h3 class="text-sm font-bold">节点属性配置</h3>
        <p class="text-[10px] text-gray-500 mt-0.5">ID: {{ node.id }}</p>
      </div>
      <button @click="emit('close')" class="text-gray-400 hover:text-white text-xs">×</button>
    </div>

    <div class="flex-1 space-y-4">
      <div v-if="node.type === 'start'" class="space-y-2">
        <label class="text-xs font-semibold text-gray-300">起始配置</label>
        <p class="text-xs text-gray-500 leading-normal" v-pre>
          工作流启动时会将用户输入绑定到 <code>{{start.user_input}}</code>。
        </p>
      </div>

      <div v-if="node.type === 'llm'" class="space-y-3">
        <div class="grid grid-cols-2 gap-2">
          <div class="flex flex-col gap-1">
            <label class="text-xs font-semibold text-gray-300">组件模式</label>
            <select
              v-model="localProps.mode"
              @change="switchLLMMode"
              class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500"
            >
              <option value="chat">对话模式</option>
              <option value="writer">创作模式</option>
              <option value="planner">规划模式</option>
            </select>
          </div>
          <div class="flex flex-col gap-1">
            <label class="text-xs font-semibold text-gray-300">Max Tokens</label>
            <input
              v-model.number="localProps.max_tokens"
              @input="updateProps"
              type="number"
              placeholder="1024"
              class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500"
            />
          </div>
        </div>

        <div class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">Provider Config</label>
          <select
            v-model="localProps.provider_config_id"
            @change="changeProviderConfig"
            class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500"
          >
            <option value="">平台默认（DashScope）</option>
            <option v-for="config in providerConfigs" :key="config.provider_config_id" :value="config.provider_config_id">
              {{ config.name }} / {{ config.provider }} / {{ config.model }}
            </option>
          </select>
        </div>

        <div class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">System Prompt</label>
          <textarea
            v-model="localProps.system_prompt"
            @input="updateProps"
            rows="4"
            class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500 resize-y"
          ></textarea>
        </div>

        <div class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">Prompt 模板</label>
          <textarea
            v-model="localProps.prompt"
            @input="updateProps"
            rows="6"
            placeholder="例如：{{start.user_input}}"
            class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500 resize-y"
          ></textarea>
        </div>

        <div class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">模型名称</label>
          <input
            v-model="localProps.model"
			:disabled="!!localProps.provider_config_id"
            @input="updateProps"
            type="text"
            placeholder="例如: qwen-plus"
            class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500"
          />
        </div>

        <div v-if="!localProps.provider_config_id" class="grid grid-cols-2 gap-2">
          <div class="flex flex-col gap-1">
            <label class="text-xs font-semibold text-gray-300">Provider</label>
            <select
              v-model="localProps.provider"
              @change="changeInlineProvider"
              class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500"
            >
              <option value="dashscope">DashScope</option>
              <option value="lmstudio">LM Studio</option>
              <option value="custom">Custom</option>
            </select>
          </div>
          <div class="flex flex-col gap-1">
            <label class="text-xs font-semibold text-gray-300">Credential Ref</label>
            <input
              v-model="localProps.credential_ref"
              @input="updateProps"
              type="text"
              placeholder="dashscope.default"
              class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500"
            />
          </div>
        </div>

        <div v-if="!localProps.provider_config_id" class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">Base URL</label>
          <input
            v-model="localProps.base_url"
            @input="updateProps"
            type="text"
            :placeholder="localProps.provider === 'lmstudio' ? 'http://localhost:1234/v1' : 'https://dashscope.aliyuncs.com/compatible-mode/v1'"
            class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500"
          />
        </div>
      </div>

      <div v-if="isExternalMCPTool(node)" class="space-y-3">
        <div class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">外部 MCP 工具</label>
          <select
            v-model="localProps.tool_name"
            :disabled="externalMCPToolsLoading"
            @change="selectExternalMCPTool"
            class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-amber-400 disabled:opacity-60"
          >
            <option value="">{{ externalMCPToolsLoading ? '正在加载…' : '选择已启用工具' }}</option>
            <option v-for="tool in externalMCPTools" :key="tool.schema.qualified_name" :value="tool.schema.qualified_name">
              {{ tool.connection_name }} / {{ tool.schema.name }} / {{ externalMCPPolicyLabel(tool.policy.category) }}
            </option>
          </select>
        </div>
        <div v-if="selectedExternalMCPTool" class="border-y border-white/10 py-2">
          <div class="flex items-center justify-between gap-2">
            <code class="min-w-0 truncate text-[10px] text-amber-200">{{ selectedExternalMCPTool.schema.qualified_name }}</code>
            <span
              class="shrink-0 text-[10px] font-semibold"
              :class="externalMCPPolicyClass(selectedExternalMCPTool.policy.category)"
            >
              {{ externalMCPPolicyLabel(selectedExternalMCPTool.policy.category) }}
            </span>
          </div>
          <p v-if="selectedExternalMCPTool.schema.description" class="mt-1 text-[10px] leading-4 text-gray-400">
            {{ selectedExternalMCPTool.schema.description }}
          </p>
          <p
            v-if="selectedExternalMCPTool.policy.category === 'write'"
            class="mt-1 break-all text-[10px] leading-4 text-blue-300"
          >
            幂等键由平台注入：{{ selectedExternalMCPTool.schema.idempotency_key_argument }}
          </p>
        </div>
        <div class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">参数 JSON</label>
          <textarea
            v-model="externalMCPArgumentsText"
            @input="updateExternalMCPArguments"
            rows="7"
            spellcheck="false"
            class="w-full resize-y rounded-lg border bg-slate-800 p-2 font-mono text-[11px] text-white focus:outline-none"
            :class="externalMCPArgumentsError ? 'border-red-400 focus:border-red-400' : 'border-white/10 focus:border-amber-400'"
          ></textarea>
          <span v-if="externalMCPArgumentsError" class="text-[10px] text-red-300">{{ externalMCPArgumentsError }}</span>
        </div>
      </div>

      <div v-if="isPublishTweet(node)" class="space-y-3">
        <div class="rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-[11px] text-amber-100">
          PublishTweet 会真实发推；运行测试前会再次确认。
        </div>
        <div class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">推文内容</label>
          <textarea
            v-model="localProps.content"
            @input="updateProps"
            rows="4"
            placeholder="例如：{{node_llm_01.text}}"
            class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500"
          ></textarea>
        </div>
        <div class="grid grid-cols-2 gap-2">
          <div class="flex flex-col gap-1">
            <label class="text-xs font-semibold text-gray-300">Max Chars</label>
            <input
              v-model.number="localProps.max_chars"
              @input="updateProps"
              type="number"
              min="1"
              placeholder="10000"
              class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500"
            />
          </div>
          <div class="flex flex-col gap-1">
            <label class="text-xs font-semibold text-gray-300">Overflow</label>
            <select
              v-model="localProps.overflow_strategy"
              @change="updateProps"
              class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500"
            >
              <option value="error">Error</option>
              <option value="truncate">Truncate</option>
            </select>
          </div>
        </div>
      </div>

      <div v-if="isWebSearch(node)" class="space-y-3">
        <div class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">搜索 Provider</label>
          <select
            v-model="localProps.provider_config_id"
            @change="updateProps"
            class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500"
          >
            <option value="">平台默认</option>
            <option v-for="config in webSearchProviderConfigs" :key="config.provider_config_id" :value="config.provider_config_id">
              {{ config.name }} / {{ config.provider }}
            </option>
          </select>
        </div>
        <div class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">搜索 Query</label>
          <input
            v-model="localProps.query"
            @input="updateProps"
            type="text"
            placeholder="例如：{{start.user_input}}"
            class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500"
          />
        </div>
        <div class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">返回数量</label>
          <input
            v-model.number="localProps.count"
            @input="updateProps"
            type="number"
            min="1"
            max="10"
            class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white"
          />
        </div>
      </div>

      <div v-if="isPageRead(node)" class="space-y-3">
        <div class="rounded-lg border border-emerald-500/30 bg-emerald-500/10 px-3 py-2 text-[11px] text-emerald-100">
          页面内容按不可信外部证据处理；私网地址、重定向和隐藏指令不会进入模型上下文。
        </div>
        <div class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">网页 URL</label>
          <input
            v-model="localProps.url"
            @input="updateProps"
            type="url"
            placeholder="https://example.com/article"
            class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500"
          />
        </div>
        <div class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">最大正文字符</label>
          <input
            v-model.number="localProps.max_runes"
            @input="updateProps"
            type="number"
            min="1"
            max="32000"
            class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white"
          />
        </div>
      </div>

      <div v-if="isMCPTool(node)" class="space-y-3">
        <div class="rounded-lg border border-indigo-500/30 bg-indigo-500/10 px-3 py-2 text-[11px] text-indigo-100">
          MCP 能力：{{ toolNameOf(node) }}。调用结果会写入当前节点的 result 字段。
        </div>

        <div v-if="isHybridTweetSearch(node) || isSemanticTweetSearch(node)" class="space-y-3">
          <div class="flex flex-col gap-1">
            <label class="text-xs font-semibold text-gray-300">检索内容</label>
            <textarea
              v-model="localProps.query"
              @input="updateProps"
              rows="3"
              placeholder="例如：{{start.user_input}}"
              class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500"
            ></textarea>
          </div>
          <div class="flex flex-col gap-1">
            <label class="text-xs font-semibold text-gray-300">返回数量</label>
            <input v-model.number="localProps.size" @input="updateProps" type="number" min="1" max="20" class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white" />
          </div>
        </div>

        <div v-if="isSearchUsers(node)" class="space-y-3">
          <div class="flex flex-col gap-1">
            <label class="text-xs font-semibold text-gray-300">用户关键词</label>
            <input v-model="localProps.keyword" @input="updateProps" type="text" placeholder="用户名、简介关键词或上游变量" class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white" />
          </div>
          <div class="flex flex-col gap-1">
            <label class="text-xs font-semibold text-gray-300">返回数量</label>
            <input v-model.number="localProps.limit" @input="updateProps" type="number" min="1" max="20" class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white" />
          </div>
        </div>

        <div v-if="isGetUserTweets(node)" class="space-y-3">
          <div class="flex flex-col gap-1">
            <label class="text-xs font-semibold text-gray-300">用户 ID</label>
            <input v-model="localProps.user_id" @input="updateProps" type="text" placeholder="用户 ID 或上游变量" class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white" />
          </div>
          <div class="flex flex-col gap-1">
            <label class="text-xs font-semibold text-gray-300">推文数量</label>
            <input v-model.number="localProps.limit" @input="updateProps" type="number" min="1" max="50" class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white" />
          </div>
        </div>

        <div v-if="isGetTweetsByIDs(node)" class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">推文 ID 列表</label>
          <input v-model="localProps.tweet_ids" @input="updateProps" type="text" placeholder="例如：123,456,789" class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white" />
        </div>
      </div>

      <div v-if="isAgentStrategy(node)" class="space-y-3">
        <div class="rounded-lg border border-sky-500/30 bg-sky-500/10 px-3 py-2 text-[11px] text-sky-100">
          该策略只能调用白名单内的只读 MCP 工具，最多执行 8 轮。发布等写操作必须使用独立节点。
        </div>
        <div class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">执行策略</label>
          <select v-model="localProps.tool_name" @change="updateProps" class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white">
            <option value="ReActAgent">ReAct</option>
            <option value="PlanExecutor">Plan-Execute</option>
          </select>
        </div>
        <div class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">目标</label>
          <textarea v-model="localProps.objective" @input="updateProps" rows="3" placeholder="{{start.user_input}}" class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white"></textarea>
        </div>
        <div v-if="isPlanExecutor(node)" class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">执行计划</label>
          <textarea v-model="localProps.plan" @input="updateProps" rows="4" placeholder="连接 Planner 后插入其 text 输出" class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white"></textarea>
        </div>
        <div class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">允许的 MCP 工具</label>
          <input v-model="localProps.allowed_tools" @input="updateProps" type="text" placeholder="hybrid_search_tweets,search_users,get_user_tweets" class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white" />
        </div>
        <div class="grid grid-cols-2 gap-2">
          <div class="flex flex-col gap-1">
            <label class="text-xs font-semibold text-gray-300">最大迭代</label>
            <input v-model.number="localProps.max_iterations" @input="updateProps" type="number" min="1" max="8" class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white" />
          </div>
          <div class="flex flex-col gap-1">
            <label class="text-xs font-semibold text-gray-300">Max Tokens</label>
            <input v-model.number="localProps.max_tokens" @input="updateProps" type="number" min="1" class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white" />
          </div>
        </div>
        <div class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">模型名称</label>
          <input v-model="localProps.model" @input="updateProps" type="text" placeholder="qwen-plus" class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white" />
        </div>
        <div class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">额外约束</label>
          <textarea v-model="localProps.system_prompt" @input="updateProps" rows="3" class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white"></textarea>
        </div>
      </div>

      <div v-if="node.type === 'router'" class="space-y-3">
        <div class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">判定表达式</label>
          <input
            v-model="localProps.expression"
            @input="updateProps"
            type="text"
            placeholder="例如: len({{start.user_input}}) > 10"
            class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500"
          />
        </div>
      </div>

      <div v-if="node.type === 'wait'" class="space-y-3">
        <div class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">等待类型</label>
          <select
            v-model="localProps.resume_mode"
            @change="updateWaitProperties"
            class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500"
          >
            <option value="human_input">人工输入</option>
            <option value="external_callback">外部回调</option>
          </select>
        </div>
        <div class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">
            {{ localProps.resume_mode === 'human_input' ? '向用户展示的问题' : '挂起原因' }}
          </label>
          <input
            v-model="localProps.reason"
            @input="updateWaitProperties"
            type="text"
            :placeholder="localProps.resume_mode === 'human_input' ? '请补充继续执行所需的信息' : '等待外部回调'"
            class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500"
          />
        </div>
      </div>

      <div class="flex flex-col gap-1 border-t border-white/5 pt-3 mt-3">
        <label class="text-xs font-semibold text-gray-300">单节点超时 (SLA)</label>
        <input
          v-model.number="localProps.timeout_sec"
          @input="updateProps"
          type="number"
          placeholder="超时秒数，如 30"
          class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500"
        />
      </div>

      <div v-if="!['start', 'end', 'wait'].includes(node.type) && !isExternalMCPTool(node)" class="border-t border-white/5 pt-3 mt-3 space-y-2">
        <div class="flex items-center justify-between">
          <label class="text-xs font-semibold text-gray-300">失败重试</label>
          <input
            v-model="retryEnabled"
            type="checkbox"
            aria-label="启用失败重试"
            class="h-4 w-4 accent-indigo-500"
          />
        </div>
        <div v-if="retryEnabled" class="grid grid-cols-2 gap-2">
          <div class="flex flex-col gap-1">
            <label class="text-[10px] text-gray-400">最大尝试次数</label>
            <input
              v-model.number="localExecution.retry.max_attempts"
              @input="updateExecution"
              type="number"
              min="1"
              max="10"
              class="min-w-0 bg-slate-800 border border-white/10 rounded-md p-2 text-xs text-white focus:outline-none focus:border-blue-500"
            />
          </div>
          <div class="flex flex-col gap-1">
            <label class="text-[10px] text-gray-400">初始退避 (ms)</label>
            <input
              v-model.number="localExecution.retry.initial_backoff_ms"
              @input="updateExecution"
              type="number"
              min="0"
              class="min-w-0 bg-slate-800 border border-white/10 rounded-md p-2 text-xs text-white focus:outline-none focus:border-blue-500"
            />
          </div>
          <div class="flex flex-col gap-1">
            <label class="text-[10px] text-gray-400">最大退避 (ms)</label>
            <input
              v-model.number="localExecution.retry.max_backoff_ms"
              @input="updateExecution"
              type="number"
              min="0"
              class="min-w-0 bg-slate-800 border border-white/10 rounded-md p-2 text-xs text-white focus:outline-none focus:border-blue-500"
            />
          </div>
          <div class="flex flex-col gap-1">
            <label class="text-[10px] text-gray-400">退避倍数</label>
            <input
              v-model.number="localExecution.retry.multiplier"
              @input="updateExecution"
              type="number"
              min="0.1"
              step="0.1"
              class="min-w-0 bg-slate-800 border border-white/10 rounded-md p-2 text-xs text-white focus:outline-none focus:border-blue-500"
            />
          </div>
          <div class="col-span-2 flex flex-col gap-1">
            <label class="text-[10px] text-gray-400">确定性抖动 (0-1)</label>
            <input
              v-model.number="localExecution.retry.jitter"
              @input="updateExecution"
              type="number"
              min="0"
              max="1"
              step="0.05"
              class="min-w-0 bg-slate-800 border border-white/10 rounded-md p-2 text-xs text-white focus:outline-none focus:border-blue-500"
            />
          </div>
        </div>
      </div>

		<div v-if="node.type === 'tool' && !isExternalMCPTool(node)" class="border-t border-white/5 pt-3 mt-3 space-y-3">
			<div class="flex items-center justify-between">
				<label class="text-xs font-semibold text-gray-300">失败补偿</label>
				<input
					v-model="compensationEnabled"
					type="checkbox"
					aria-label="启用失败补偿"
					class="h-4 w-4 accent-amber-500"
				/>
			</div>
			<div v-if="compensationEnabled" class="space-y-3 rounded-md border border-amber-500/20 bg-amber-500/5 p-3">
				<div class="flex flex-col gap-1">
					<label class="text-[10px] text-gray-400">补偿工具</label>
					<input
						v-model.trim="localExecution.compensation.tool_name"
						@input="updateExecution"
						type="text"
						placeholder="例如 DeleteDraft"
						class="w-full rounded-md border border-white/10 bg-slate-800 p-2 text-xs text-white focus:border-amber-400 focus:outline-none"
					/>
				</div>
				<div class="flex flex-col gap-1">
					<label class="text-[10px] text-gray-400">总超时（秒）</label>
					<input
						v-model.number="localExecution.compensation.timeout_sec"
						@input="updateExecution"
						type="number"
						min="0"
						class="w-full rounded-md border border-white/10 bg-slate-800 p-2 text-xs text-white focus:border-amber-400 focus:outline-none"
					/>
				</div>

				<div class="space-y-2">
					<div class="flex items-center justify-between">
						<label class="text-[10px] text-gray-400">输入映射</label>
						<button
							type="button"
							title="添加补偿输入"
							aria-label="添加补偿输入"
							@click="addCompensationInput"
							class="inline-flex h-7 w-7 items-center justify-center rounded-md border border-white/10 bg-slate-800 text-gray-300 hover:text-white"
						>
							<PlusIcon class="h-4 w-4" />
						</button>
					</div>
					<div v-for="(input, index) in compensationInputs" :key="index" class="grid grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)_2rem] gap-2">
						<input
							v-model="input.key"
							@input="syncCompensationInputs"
							type="text"
							placeholder="resource_id"
							class="min-w-0 rounded-md border border-white/10 bg-slate-900 p-2 text-xs text-white focus:border-amber-400 focus:outline-none"
						/>
						<input
							v-model="input.value"
							@input="syncCompensationInputs"
							type="text"
							:placeholder="`{{${node.id}.id}}`"
							class="min-w-0 rounded-md border border-white/10 bg-slate-900 p-2 text-xs text-white focus:border-amber-400 focus:outline-none"
						/>
						<button
							type="button"
							title="删除补偿输入"
							aria-label="删除补偿输入"
							@click="removeCompensationInput(index)"
							class="inline-flex h-8 w-8 items-center justify-center rounded-md text-gray-400 hover:bg-red-500/10 hover:text-red-300"
						>
							<XMarkIcon class="h-4 w-4" />
						</button>
					</div>
				</div>

				<div class="flex items-center justify-between">
					<label class="text-[10px] text-gray-400">补偿重试</label>
					<input v-model="compensationRetryEnabled" type="checkbox" aria-label="启用补偿重试" class="h-4 w-4 accent-amber-500" />
				</div>
				<div v-if="compensationRetryEnabled" class="grid grid-cols-2 gap-2">
					<input v-model.number="localExecution.compensation.retry.max_attempts" @input="updateExecution" type="number" min="1" max="10" title="最大尝试次数" class="min-w-0 rounded-md border border-white/10 bg-slate-800 p-2 text-xs text-white" />
					<input v-model.number="localExecution.compensation.retry.initial_backoff_ms" @input="updateExecution" type="number" min="0" title="初始退避毫秒" class="min-w-0 rounded-md border border-white/10 bg-slate-800 p-2 text-xs text-white" />
					<input v-model.number="localExecution.compensation.retry.max_backoff_ms" @input="updateExecution" type="number" min="0" title="最大退避毫秒" class="min-w-0 rounded-md border border-white/10 bg-slate-800 p-2 text-xs text-white" />
					<input v-model.number="localExecution.compensation.retry.multiplier" @input="updateExecution" type="number" min="0.1" step="0.1" title="退避倍数" class="min-w-0 rounded-md border border-white/10 bg-slate-800 p-2 text-xs text-white" />
				</div>
			</div>
		</div>

      <div v-if="!['start', 'end', 'wait'].includes(node.type)" class="border-t border-white/5 pt-3 mt-3 space-y-2">
        <div class="flex items-center justify-between">
          <label class="text-xs font-semibold text-gray-300">全局状态写入</label>
          <button
            type="button"
            title="添加状态写入"
            aria-label="添加状态写入"
            @click="addStateWrite"
            class="h-7 w-7 inline-flex items-center justify-center rounded-md border border-white/10 bg-slate-800 text-gray-300 hover:bg-slate-700 hover:text-white"
          >
            <PlusIcon class="h-4 w-4" />
          </button>
        </div>
        <div
          v-for="(write, index) in localProps.state_writes"
          :key="index"
          class="border border-white/10 bg-slate-800/60 rounded-md p-2 space-y-2"
        >
          <div class="flex items-center gap-2">
            <input
              v-model="write.path"
              @input="updateProps"
              type="text"
              placeholder="shared.summary"
              class="min-w-0 flex-1 bg-slate-900 border border-white/10 rounded-md p-2 text-xs text-white focus:outline-none focus:border-blue-500"
            />
            <button
              type="button"
              title="删除状态写入"
              aria-label="删除状态写入"
              @click="removeStateWrite(index)"
              class="h-8 w-8 shrink-0 inline-flex items-center justify-center rounded-md text-gray-400 hover:bg-red-500/10 hover:text-red-300"
            >
              <XMarkIcon class="h-4 w-4" />
            </button>
          </div>
          <div class="grid grid-cols-2 gap-2">
            <input
              v-model="write.source"
              @input="updateProps"
              type="text"
              placeholder="输出字段"
              class="min-w-0 bg-slate-900 border border-white/10 rounded-md p-2 text-xs text-white focus:outline-none focus:border-blue-500"
            />
            <select
              v-model="write.reducer"
              @change="updateProps"
              class="min-w-0 bg-slate-900 border border-white/10 rounded-md p-2 text-xs text-white focus:outline-none focus:border-blue-500"
            >
              <option v-for="option in reducerOptions" :key="option.value" :value="option.value">
                {{ option.label }}
              </option>
            </select>
          </div>
        </div>
      </div>

      <div class="border-t border-white/5 pt-3 mt-4">
        <h4 class="text-xs font-bold text-gray-300 mb-2">可用上游变量</h4>
        <div v-if="availableVariables.length === 0" class="text-[10px] text-gray-500 leading-normal">
          当前节点没有上游连接，请连线后引用。
        </div>
        <div v-else class="space-y-1.5">
          <div
            v-for="v in availableVariables"
            :key="v.value"
            @click="insertVariable(v.value, primaryInputField())"
            class="flex items-center justify-between p-1.5 rounded bg-slate-800 hover:bg-slate-700 cursor-pointer transition-all border border-white/5"
          >
            <span class="text-[10px] text-gray-200">{{ v.label }}</span>
            <code class="text-[9px] text-indigo-400 bg-black/30 px-1 py-0.5 rounded">{{ v.value }}</code>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
