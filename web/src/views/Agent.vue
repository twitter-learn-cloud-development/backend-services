<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import {
  ArrowLeftIcon,
  ArrowPathIcon,
  ChatBubbleLeftIcon,
  PaperAirplaneIcon,
  PaperClipIcon,
  PlusIcon,
  SparklesIcon,
} from '@heroicons/vue/24/outline'
import {
  assistPublish,
  chat,
  consult,
  getDialogueMessages,
  getDialogues,
  getModels,
  listWorkflows,
  multiAgentPublish,
  runWorkflow,
  uploadFileAnalysis,
} from '../api/agent'

const router = useRouter()

const dialogues = ref<any[]>([])
const activeDialogueId = ref('')
const messages = ref<any[]>([])
const models = ref<any[]>([])
const activeModelId = ref('')
const inputContent = ref('')
const isLoading = ref(false)
const fileInputRef = ref<HTMLInputElement | null>(null)
const chatContainerRef = ref<HTMLElement | null>(null)

const modes = [
  { id: 'chat', name: '直接对话' },
  { id: 'consult', name: '资讯/搜索' },
  { id: 'assist', name: '辅助发推' },
  { id: 'multi', name: '智能体协作发推' },
  { id: 'workflow', name: '自定义工作流' },
]
const activeMode = ref('chat')

const emptyHint = computed(() => {
  if (activeMode.value === 'workflow') return '输入内容后，将调用你保存的自定义工作流'
  return '在下方输入内容，开始与 AI 交流'
})

onMounted(async () => {
  await fetchModels()
  await fetchDialogues()
})

const goBack = () => {
  if (window.history.length > 1) {
    router.back()
    return
  }
  router.push('/')
}

const dialogueKeyOf = (dialogue: any) => String(dialogue?.dialogue_key || dialogue?.id || '')

const normalizeDialogueMessages = (rawMessages: any[]) => {
  return rawMessages.flatMap((message: any) => {
    if (message?.role && message?.content) {
      return [{ role: message.role, content: message.content }]
    }

    const legacyMessages = []
    if (message?.question) legacyMessages.push({ role: 'user', content: message.question })
    if (message?.response) legacyMessages.push({ role: 'assistant', content: message.response })
    return legacyMessages
  })
}

const fetchModels = async () => {
  try {
    const res = await getModels()
    models.value = res.data.model_kind_list || []
    if (models.value.length > 0) {
      activeModelId.value = String(models.value[0].id)
    }
  } catch (err) {
    console.error('Failed to load models:', err)
  }
}

const fetchDialogues = async () => {
  try {
    const res = await getDialogues()
    dialogues.value = res.data.repository_dialogue_list || []
  } catch (err) {
    console.error('Failed to load dialogues:', err)
  }
}

const selectDialogue = async (id: string) => {
  if (!id) return
  activeDialogueId.value = id
  messages.value = []
  isLoading.value = true
  try {
    const res = await getDialogueMessages(id)
    const msgs = res.data.messages || []
    messages.value = normalizeDialogueMessages(msgs)
    scrollToBottom()
  } catch (err) {
    console.error('Failed to load messages:', err)
  } finally {
    isLoading.value = false
  }
}

const createNewDialogue = () => {
  activeDialogueId.value = ''
  messages.value = []
}

const triggerFileUpload = () => {
  fileInputRef.value?.click()
}

const handleFileUpload = async (event: Event) => {
  const target = event.target as HTMLInputElement
  const file = target.files?.[0]
  if (!file) return

  const currentModel = models.value.find(model => String(model.id) === activeModelId.value)
  if (!currentModel?.file_kind_list?.length) {
    alert('当前模型不支持文件解析')
    return
  }

  isLoading.value = true
  try {
    const fileKindId = String(currentModel.file_kind_list[0].id)
    const res = await uploadFileAnalysis(file, fileKindId)
    alert(`文件解析成功，FileKey: ${res.data.file_key}`)
    await fetchDialogues()
    if (res.data.file_key) {
      await selectDialogue(res.data.file_key)
    }
  } catch (err) {
    console.error('Upload failed:', err)
    alert('文件上传失败')
  } finally {
    isLoading.value = false
    if (fileInputRef.value) fileInputRef.value.value = ''
  }
}

const stringifyWorkflowValue = (value: any) => {
  if (value === undefined || value === null) return ''
  if (typeof value === 'string') return value
  return JSON.stringify(value, null, 2)
}

const pickWorkflowText = (value: any, depth = 0): string => {
  if (depth > 4 || value === undefined || value === null) return ''
  if (typeof value === 'string') return value.trim()
  if (typeof value !== 'object') return ''

  const preferredKeys = ['text', 'response', 'result', 'content', 'answer', 'final', 'summary']
  for (const key of preferredKeys) {
    const picked = pickWorkflowText(value[key], depth + 1)
    if (picked) return picked
  }

  for (const [key, nested] of Object.entries(value)) {
    if (['traces', 'blackboard', 'checkpoint'].includes(key)) continue
    const picked = pickWorkflowText(nested, depth + 1)
    if (picked) return picked
  }
  return ''
}

const formatWorkflowRunOutput = (run: any) => {
  if (!run) return '工作流没有返回运行结果。'
  if (run.status && run.status !== 'success') {
    return `工作流执行失败：${run.error_message || run.status}`
  }

  const output = run.output || {}
  const best = pickWorkflowText(output)
  if (best) return best

  const fallback = stringifyWorkflowValue(output)
  return fallback ? `工作流执行完成：\n${fallback}` : '工作流执行完成。'
}

const runSavedWorkflow = async (content: string) => {
  const listResp = await listWorkflows({ page: 1, page_size: 1 })
  const workflow = listResp.data.workflows?.[0]
  if (!workflow?.workflow_id) {
    return {
      answer: '还没有可用的自定义工作流。请先进入「定制工作流」创建并保存。',
      dialogueKey: '',
    }
  }

  const runResp = await runWorkflow(workflow.workflow_id, {
    input: {
      user_input: content,
      dialogue_key: activeDialogueId.value || '',
      persist_dialogue: true,
    },
  })
  return {
    answer: runResp.data.response || formatWorkflowRunOutput(runResp.data.run),
    dialogueKey: String(runResp.data.dialogue_key || ''),
  }
}

const sendMessage = async () => {
  const content = inputContent.value.trim()
  if (!content || isLoading.value) return

  inputContent.value = ''
  messages.value.push({ role: 'user', content })
  scrollToBottom()

  isLoading.value = true
  try {
    const reqData = {
      content,
      dialogue_id: '0',
      dialogue_key: activeDialogueId.value || '',
      model_kind_id: activeModelId.value || '0',
    }

    let res: any
    if (activeMode.value === 'chat') {
      res = await chat(reqData)
      messages.value.push({ role: 'assistant', content: res.data.response })
    } else if (activeMode.value === 'consult') {
      res = await consult(reqData)
      let answer = res.data.response || ''
      if (res.data.tweet_list?.length) {
        answer += '\n\n**推荐推文:**\n'
        res.data.tweet_list.forEach((tweet: any, index: number) => {
          answer += `${index + 1}. ${tweet.summary || tweet.content || tweet.url || '相关推文'}\n`
        })
      }
      messages.value.push({ role: 'assistant', content: answer })
    } else if (activeMode.value === 'assist') {
      res = await assistPublish(reqData)
      let answer = res.data.response || ''
      if (res.data.tweet_list?.length) {
        answer += '\n\n**草稿候选:**\n'
        res.data.tweet_list.forEach((tweet: any, index: number) => {
          answer += `${index + 1}. ${tweet.content || tweet.summary || '草稿'}\n`
        })
      }
      messages.value.push({ role: 'assistant', content: answer })
    } else if (activeMode.value === 'multi') {
      res = await multiAgentPublish({
        domain: 'general',
        author_user_id: '0',
        style_ratio: 0.5,
        reference_tweet_ids: [],
        dialogue_key: activeDialogueId.value || '',
        content,
      })
      messages.value.push({ role: 'assistant', content: res.data.response })
    } else if (activeMode.value === 'workflow') {
      const workflowResult = await runSavedWorkflow(content)
      messages.value.push({ role: 'assistant', content: workflowResult.answer })
      if (workflowResult.dialogueKey) {
        activeDialogueId.value = workflowResult.dialogueKey
      }
    }

    if (res?.data?.dialogue_key) {
      activeDialogueId.value = res.data.dialogue_key
    }

    scrollToBottom()
    await fetchDialogues()
    if (!activeDialogueId.value && dialogues.value.length > 0) {
      activeDialogueId.value = dialogueKeyOf(dialogues.value[0])
    }
  } catch (err: any) {
    console.error('Send failed:', err)
    const errorMessage = err?.response?.data?.error || err?.message || '请求失败，请稍后重试。'
    messages.value.push({ role: 'assistant', content: `请求失败：${errorMessage}` })
  } finally {
    isLoading.value = false
  }
}

const scrollToBottom = () => {
  nextTick(() => {
    if (chatContainerRef.value) {
      chatContainerRef.value.scrollTop = chatContainerRef.value.scrollHeight
    }
  })
}
</script>

<template>
  <div class="flex h-screen w-full bg-gray-50 text-gray-900 dark:bg-black dark:text-white">
    <div class="hidden w-64 flex-col border-r border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900 md:flex">
      <div class="p-4">
        <button
          @click="createNewDialogue"
          class="flex w-full items-center justify-center space-x-2 rounded-xl bg-blue-50 py-2.5 font-medium text-primary transition-colors hover:bg-blue-100 dark:bg-gray-800 dark:text-blue-400 dark:hover:bg-gray-700"
        >
          <PlusIcon class="h-5 w-5" />
          <span>新建对话</span>
        </button>
      </div>

      <div class="flex-1 space-y-1 overflow-y-auto px-2">
        <div
          v-for="dialogue in dialogues"
          :key="dialogue.id"
          @click="selectDialogue(dialogueKeyOf(dialogue))"
          :class="[
            'flex cursor-pointer items-center space-x-3 rounded-lg p-3 transition-colors',
            activeDialogueId === dialogueKeyOf(dialogue) ? 'bg-primary text-white' : 'hover:bg-gray-100 dark:hover:bg-gray-800',
          ]"
        >
          <ChatBubbleLeftIcon class="h-5 w-5 flex-shrink-0" />
          <span class="truncate text-sm font-medium">{{ dialogue.title || '新对话' }}</span>
        </div>
      </div>
    </div>

    <div class="relative flex h-full flex-1 flex-col bg-white dark:bg-black">
      <div class="absolute top-0 z-10 flex h-16 w-full items-center justify-between border-b border-gray-200 bg-white/80 px-6 backdrop-blur-md dark:border-gray-800 dark:bg-black/80">
        <div class="flex min-w-0 items-center space-x-3">
          <button
            @click="goBack"
            class="flex h-9 w-9 items-center justify-center rounded-full text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white"
            title="返回上一页"
          >
            <ArrowLeftIcon class="h-5 w-5" />
          </button>
          <span class="text-lg font-bold">AI 助手</span>
          <select v-model="activeModelId" class="cursor-pointer rounded-lg border-none bg-gray-100 px-3 py-1.5 text-sm outline-none dark:bg-gray-800">
            <option v-for="model in models" :key="model.id" :value="String(model.id)">
              {{ model.name }}
            </option>
          </select>
        </div>

        <RouterLink
          to="/agent/workflow"
          class="flex items-center space-x-1.5 rounded-lg bg-gradient-to-r from-blue-500 to-indigo-600 px-3 py-1.5 text-xs font-semibold text-white shadow-md transition-all duration-300 hover:from-blue-600 hover:to-indigo-700"
        >
          <SparklesIcon class="h-4 w-4" />
          <span>定制工作流</span>
        </RouterLink>
      </div>

      <div ref="chatContainerRef" class="flex-1 space-y-6 overflow-y-auto p-6 pb-32 pt-24">
        <div v-if="messages.length === 0" class="flex h-full flex-col items-center justify-center space-y-4 text-gray-400">
          <SparklesIcon class="h-16 w-16 opacity-50" />
          <p class="text-lg">{{ emptyHint }}</p>
        </div>

        <div v-for="(msg, index) in messages" :key="index" class="flex w-full" :class="msg.role === 'user' ? 'justify-end' : 'justify-start'">
          <div
            class="max-w-[80%] whitespace-pre-wrap rounded-2xl px-5 py-3 shadow-sm"
            :class="msg.role === 'user' ? 'rounded-br-none bg-primary text-white' : 'rounded-bl-none bg-gray-100 dark:bg-gray-800'"
          >
            {{ msg.content }}
          </div>
        </div>

        <div v-if="isLoading && (!messages.length || messages[messages.length - 1].role === 'user')" class="flex w-full justify-start">
          <div class="flex items-center space-x-2 rounded-2xl rounded-bl-none bg-gray-100 px-5 py-4 dark:bg-gray-800">
            <div class="h-2 w-2 animate-bounce rounded-full bg-gray-400"></div>
            <div class="h-2 w-2 animate-bounce rounded-full bg-gray-400" style="animation-delay: 0.2s"></div>
            <div class="h-2 w-2 animate-bounce rounded-full bg-gray-400" style="animation-delay: 0.4s"></div>
          </div>
        </div>
      </div>

      <div class="absolute bottom-0 w-full border-t border-gray-100 bg-gradient-to-t from-white via-white to-transparent px-6 pb-6 pt-10 dark:border-gray-800 dark:from-black dark:via-black">
        <div class="relative mx-auto flex max-w-4xl flex-col space-y-3">
          <div class="flex flex-wrap gap-2">
            <button
              v-for="mode in modes"
              :key="mode.id"
              @click="activeMode = mode.id"
              :class="[
                'rounded-full px-3 py-1 text-xs font-medium transition-colors',
                activeMode === mode.id
                  ? 'bg-gray-800 text-white dark:bg-gray-200 dark:text-black'
                  : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-gray-800 dark:text-gray-400 dark:hover:bg-gray-700',
              ]"
            >
              {{ mode.name }}
            </button>
          </div>

          <div class="relative flex items-end rounded-2xl border border-gray-300 bg-white shadow-sm transition-all focus-within:border-primary focus-within:ring-2 focus-within:ring-primary dark:border-gray-700 dark:bg-gray-900">
            <input ref="fileInputRef" type="file" class="hidden" @change="handleFileUpload" />
            <button @click="triggerFileUpload" class="p-3 text-gray-400 transition-colors hover:text-primary" title="解析文件">
              <PaperClipIcon class="h-6 w-6" />
            </button>

            <textarea
              v-model="inputContent"
              @keydown.enter.exact.prevent="sendMessage"
              placeholder="输入消息，Enter 发送，Shift+Enter 换行"
              class="max-h-48 min-h-[52px] flex-1 resize-none border-none bg-transparent px-2 py-3 text-gray-900 focus:ring-0 dark:text-white"
              rows="1"
            ></textarea>

            <button
              @click="sendMessage"
              :disabled="!inputContent.trim() || isLoading"
              class="m-1.5 rounded-xl p-3 text-white transition-colors"
              :class="inputContent.trim() && !isLoading ? 'bg-primary hover:bg-blue-600' : 'cursor-not-allowed bg-gray-300 dark:bg-gray-700'"
            >
              <ArrowPathIcon v-if="isLoading" class="h-5 w-5 animate-spin" />
              <PaperAirplaneIcon v-else class="h-5 w-5" />
            </button>
          </div>

          <div class="text-center text-xs text-gray-400">
            AI Agent 可能会犯错。请核查重要信息。
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
