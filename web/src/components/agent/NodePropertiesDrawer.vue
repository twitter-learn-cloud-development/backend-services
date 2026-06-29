<script setup lang="ts">
import { ref, computed, watch } from 'vue'

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
  (e: 'close'): void
}>()

const localProps = ref<any>({})

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

const isAgentStrategy = (node: GraphNode | null) => node?.type === 'agent'
const isPlanExecutor = (node: GraphNode | null) => toolNameOf(node).toLowerCase() === 'planexecutor'
const isHybridTweetSearch = (node: GraphNode | null) => toolNameOf(node).toLowerCase() === 'hybridtweetsearch'
const isSemanticTweetSearch = (node: GraphNode | null) => toolNameOf(node).toLowerCase() === 'semantictweetsearch'
const isSearchUsers = (node: GraphNode | null) => toolNameOf(node).toLowerCase() === 'searchusers'
const isGetUserTweets = (node: GraphNode | null) => toolNameOf(node).toLowerCase() === 'getusertweets'
const isGetTweetsByIDs = (node: GraphNode | null) => toolNameOf(node).toLowerCase() === 'gettweetsbyids'
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
    return
  }

  localProps.value = JSON.parse(JSON.stringify(newNode.data?.properties || {}))

  if (newNode.type === 'llm') {
    localProps.value.provider ||= 'lmstudio'
    localProps.value.base_url ||= 'http://localhost:1234/v1'
    localProps.value.model ||= 'qwen2.5-3b-instruct'
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
    localProps.value.allowed_tools ||= 'hybrid_search_tweets,search_users,get_user_tweets'
    localProps.value.max_iterations ||= 5
    localProps.value.model ||= 'qwen-plus'
    localProps.value.max_tokens ||= 2048
  }

  if (newNode.type === 'wait') {
    localProps.value.reason ||= 'waiting for external callback'
  }
}, { immediate: true })

const updateProps = () => {
  if (props.node) emit('update:properties', props.node.id, localProps.value)
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
    class="w-80 bg-slate-900 border-l border-white/10 h-full flex flex-col p-4 overflow-y-auto text-white"
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
            @input="updateProps"
            type="text"
            placeholder="例如: qwen2.5-3b-instruct"
            class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500"
          />
        </div>

        <div class="grid grid-cols-2 gap-2">
          <div class="flex flex-col gap-1">
            <label class="text-xs font-semibold text-gray-300">Provider</label>
            <select
              v-model="localProps.provider"
              @change="updateProps"
              class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500"
            >
              <option value="lmstudio">LM Studio</option>
              <option value="dashscope">DashScope</option>
              <option value="custom">Custom</option>
            </select>
          </div>
          <div class="flex flex-col gap-1">
            <label class="text-xs font-semibold text-gray-300">API Key</label>
            <input
              v-model="localProps.api_key"
              @input="updateProps"
              type="password"
              placeholder="LM Studio 可留空"
              class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500"
            />
          </div>
        </div>

        <div class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">Base URL</label>
          <input
            v-model="localProps.base_url"
            @input="updateProps"
            type="text"
            placeholder="http://localhost:1234/v1"
            class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500"
          />
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
          <label class="text-xs font-semibold text-gray-300">搜索 Query</label>
          <input
            v-model="localProps.query"
            @input="updateProps"
            type="text"
            placeholder="例如：{{start.user_input}}"
            class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500"
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
          <label class="text-xs font-semibold text-gray-300">Suspend Reason</label>
          <input
            v-model="localProps.reason"
            @input="updateProps"
            type="text"
            placeholder="waiting for external callback"
            class="w-full bg-slate-800 border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-blue-500"
          />
        </div>
        <div class="flex flex-col gap-1">
          <label class="text-xs font-semibold text-gray-300">Resume Token</label>
          <input
            v-model="localProps.resume_token"
            @input="updateProps"
            type="text"
            placeholder="callback token or approval id"
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
