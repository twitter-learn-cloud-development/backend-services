<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import {
  ArchiveBoxXMarkIcon,
  ArrowLeftIcon,
  ArrowPathIcon,
  ArrowTopRightOnSquareIcon,
  BuildingStorefrontIcon,
  BookmarkSquareIcon,
  ChartBarSquareIcon,
  ChatBubbleLeftIcon,
  CheckCircleIcon,
  ExclamationCircleIcon,
  GlobeAltIcon,
  KeyIcon,
  MagnifyingGlassIcon,
  PaperAirplaneIcon,
  PaperClipIcon,
  PlusIcon,
  PuzzlePieceIcon,
  RectangleStackIcon,
  ServerStackIcon,
  SparklesIcon,
  XMarkIcon,
} from '@heroicons/vue/24/outline'
import ApprovalInbox from '../components/agent/ApprovalInbox.vue'
import AgentExtensionCatalogDialog from '../components/agent/AgentExtensionCatalogDialog.vue'
import AgentExtensionMarketplaceDialog from '../components/agent/AgentExtensionMarketplaceDialog.vue'
import ExternalMCPDialog from '../components/agent/ExternalMCPDialog.vue'
import WebSearchProviderDialog from '../components/agent/WebSearchProviderDialog.vue'
import {
  archiveAgentTaskTemplate,
  confirmPublish,
  createAgentTaskTemplate,
  getAgentRun,
  getAgentRunAccounting,
  getAgentMarketplaceManagementAccess,
  getDialogueMessages,
  getDialogues,
  getModels,
  listAgentExtensions,
  listAgentMarketplaceExtensions,
  listAgentSkills,
  listAgentTaskTemplates,
  runAgent,
  runAgentTaskTemplate,
  resumeAgentRun,
  uploadFileAnalysis,
  type AgentApprovalState,
  type AgentArtifact,
  type AgentCitation,
  type AgentExecutionRunResponse,
  type AgentRunAccountingResponse,
  type AgentSkill,
  type AgentTaskTemplate,
  type AgentToolActivity,
  type RunAgentResponse,
} from '../api/agent'

const router = useRouter()

type MessageRole = 'user' | 'assistant'

interface ChatMessage {
  id: string
  role: MessageRole
  content: string
  runId?: string
  runStatus?: string
  executionProfile?: string
  capabilityIds?: string[]
  publishableDraft?: boolean
  draftCandidates?: string[]
  toolActivities?: AgentToolActivity[]
  citations?: AgentCitation[]
  artifacts?: AgentArtifact[]
  approvalState?: AgentApprovalState
  selectedSkillId?: string
  selectedSkillVersion?: string
  selectedTaskTemplateId?: string
  selectedTaskTemplateRevision?: number
  publishedTweetId?: string
}

interface CapabilityPreset {
  id: 'auto' | 'conversation' | 'search' | 'web-search' | 'external-mcp' | 'draft' | 'research-draft' | 'web-research-draft'
  name: string
  capabilityIds: string[]
}

const capabilityPresets: CapabilityPreset[] = [
  { id: 'auto', name: '自动选择能力', capabilityIds: [] },
  { id: 'conversation', name: '直接回答', capabilityIds: ['conversation.reply'] },
  { id: 'search', name: '站内搜索', capabilityIds: ['platform.search'] },
  { id: 'web-search', name: '联网搜索', capabilityIds: ['web.search'] },
  { id: 'external-mcp', name: '外部 MCP 工具', capabilityIds: ['connector.mcp'] },
  { id: 'draft', name: '内容草拟', capabilityIds: ['content.draft'] },
  { id: 'research-draft', name: '站内搜索并草拟', capabilityIds: ['platform.search', 'content.draft'] },
  { id: 'web-research-draft', name: '联网研究并草拟', capabilityIds: ['web.search', 'content.draft'] },
]

const dialogues = ref<any[]>([])
const activeDialogueId = ref('')
const messages = ref<ChatMessage[]>([])
const models = ref<any[]>([])
const activeModelId = ref('')
const activeCapabilityPresetId = ref<CapabilityPreset['id']>('auto')
const inputContent = ref('')
const isSending = ref(false)
const isDialogueLoading = ref(false)
const isUploading = ref(false)
const dialogueLoadError = ref('')
const fileInputRef = ref<HTMLInputElement | null>(null)
const chatContainerRef = ref<HTMLElement | null>(null)
const publishDialogOpen = ref(false)
const publishDraft = ref('')
const publishCandidates = ref<string[]>([])
const publishCandidateIndex = ref(0)
const publishSourceRunId = ref('')
const publishingMessage = ref<ChatMessage | null>(null)
const isPublishingDraft = ref(false)
const publishDraftError = ref('')
const webProviderDialogOpen = ref(false)
const externalMCPDialogOpen = ref(false)
const externalMCPInitialConnectionId = ref('')
const extensionCatalogDialogOpen = ref(false)
const extensionMarketplaceDialogOpen = ref(false)
const extensionCatalogAvailable = ref(false)
const extensionMarketplaceAvailable = ref(false)
const extensionManagementAvailable = ref(false)
const activeWebSearchProviderConfigId = ref('')
const agentSkills = ref<AgentSkill[]>([])
const activeSkillKey = ref('')
const agentTaskTemplates = ref<AgentTaskTemplate[]>([])
const activeTaskTemplateId = ref('')
const taskTemplateExecutionEnabled = ref(false)
const taskTemplateDialogOpen = ref(false)
const taskTemplateSourceMessage = ref<ChatMessage | null>(null)
const taskTemplateName = ref('')
const taskTemplateDescription = ref('')
const taskTemplateInstruction = ref('{{input}}')
const taskTemplateIdempotencyKey = ref('')
const taskTemplateError = ref('')
const isSavingTaskTemplate = ref(false)
const isArchivingTaskTemplate = ref(false)
const resolvingTaskTemplateRunId = ref('')
const accountingDialogOpen = ref(false)
const accountingLoading = ref(false)
const accountingError = ref('')
const runAccounting = ref<AgentRunAccountingResponse | null>(null)

const agentSkillKey = (version: AgentSkill) => `${version.skill_id}\u001f${version.version}`
const selectedSkill = computed(() => (
  agentSkills.value.find(version => agentSkillKey(version) === activeSkillKey.value)
))
const selectedTaskTemplate = computed(() => (
  agentTaskTemplates.value.find(template => template.template_id === activeTaskTemplateId.value)
))
const selectedCapabilityPreset = computed(() => (
  capabilityPresets.find(item => item.id === activeCapabilityPresetId.value) || capabilityPresets[0]!
))
const preferredCapabilityIds = computed(() => (
  selectedTaskTemplate.value
    ? [...selectedTaskTemplate.value.capability_ids]
    : selectedSkill.value
      ? ['skill.run']
      : [...selectedCapabilityPreset.value.capabilityIds]
))
const lastMessageIsUser = computed(() => (
  messages.value[messages.value.length - 1]?.role === 'user'
))
const canSend = computed(() => Boolean(
  inputContent.value.trim()
  && activeModelId.value
  && !isSending.value
  && !isUploading.value,
))
const canSubmitTaskTemplate = computed(() => Boolean(
  taskTemplateName.value.trim()
  && taskTemplateInstruction.value.trim()
  && taskTemplateInstruction.value.split('{{input}}').length === 2
  && !isSavingTaskTemplate.value,
))

let localMessageSequence = 0
let dialogueLoadSequence = 0
const dialogueViewRevision = ref(0)
const sendingViewRevision = ref<number | null>(null)
const isSendingForCurrentView = computed(() => (
  isSending.value && sendingViewRevision.value === dialogueViewRevision.value
))
const resumableRunMessage = computed(() => {
  const latestAssistant = [...messages.value].reverse().find(message => (
    message.role === 'assistant' && message.runId
  ))
  if (
    latestAssistant?.runStatus === 'awaiting_human'
    && latestAssistant.approvalState?.resume_supported
    && Number(latestAssistant.approvalState.revision) > 0
    && latestAssistant.runId
  ) {
    return latestAssistant
  }
  return null
})

const applyExecutionRunState = (message: ChatMessage, run: AgentExecutionRunResponse) => {
  message.runStatus = String(run.status || '')
  message.executionProfile = String(run.execution_profile || '')
  message.capabilityIds = Array.isArray(run.capability_ids) ? run.capability_ids : []
  message.selectedSkillId = String(run.skill_id || '')
  message.selectedSkillVersion = String(run.skill_version || '')
  message.selectedTaskTemplateId = String(run.task_template_id || '')
  message.selectedTaskTemplateRevision = Number(run.task_template_revision || 0)
  message.approvalState = {
    status: run.status === 'awaiting_human' || run.status === 'approval_required'
      ? 'input_required'
      : 'not_required',
    approval_id: run.approval_id || '',
    run_id: run.run_id,
    action: run.pending_action_type,
    revision: Number(run.revision || 0),
    expires_at: Number(run.approval_expires_at || 0),
    resume_supported: Boolean(run.resume_supported),
  }
}

const nextMessageId = (prefix: string) => {
  localMessageSequence += 1
  return `${prefix}-${Date.now()}-${localMessageSequence}`
}

const detectExtensionSurfaces = async () => {
  const [catalog, marketplace, management] = await Promise.allSettled([
    listAgentExtensions({ page_size: 1 }),
    listAgentMarketplaceExtensions({ page_size: 1 }),
    getAgentMarketplaceManagementAccess(),
  ])
  extensionCatalogAvailable.value = catalog.status === 'fulfilled'
  extensionMarketplaceAvailable.value = marketplace.status === 'fulfilled'
  if (management.status === 'fulfilled') {
    const access = management.value.data
    extensionManagementAvailable.value = Boolean(
      access?.enabled
      && (
        access.platform_admin
        || (Array.isArray(access.owned_publisher_ids) && access.owned_publisher_ids.length > 0)
      ),
    )
  } else {
    extensionManagementAvailable.value = false
  }
}

onMounted(async () => {
  await Promise.all([
    fetchModels(),
    fetchDialogues(),
    fetchAgentSkills(),
    fetchAgentTaskTemplates(),
    detectExtensionSurfaces(),
  ])
})

watch(activeSkillKey, (value) => {
  if (value) {
    activeCapabilityPresetId.value = 'auto'
    activeTaskTemplateId.value = ''
  }
})

watch(activeCapabilityPresetId, (value) => {
  if (value !== 'auto') {
    activeSkillKey.value = ''
    activeTaskTemplateId.value = ''
  }
})

watch(activeTaskTemplateId, (value) => {
  if (value) {
    activeCapabilityPresetId.value = 'auto'
    activeSkillKey.value = ''
  }
})

const goBack = () => {
  if (window.history.length > 1) {
    router.back()
    return
  }
  router.push('/')
}

const dialogueKeyOf = (dialogue: any) => String(dialogue?.dialogue_key || dialogue?.id || '')

const upsertDialogueSummary = (dialogueKey: string, content: string) => {
  const normalizedKey = String(dialogueKey || '').trim()
  if (!normalizedKey) return
  const existing = dialogues.value.find(dialogue => dialogueKeyOf(dialogue) === normalizedKey)
  const title = Array.from(String(content || '').trim()).slice(0, 30).join('') || '新对话'
  const summary = {
    ...(existing || {}),
    id: existing?.id || normalizedKey,
    dialogue_key: normalizedKey,
    title: existing?.title || title,
  }
  dialogues.value = [
    summary,
    ...dialogues.value.filter(dialogue => dialogueKeyOf(dialogue) !== normalizedKey),
  ]
}

const normalizeDialogueMessages = (rawMessages: any[]): ChatMessage[] => {
  return rawMessages.flatMap((message: any, index: number): ChatMessage[] => {
    const sourceId = String(message?.id || index)
    if (message?.role && message?.content) {
      return [{
        id: `history-${sourceId}-${message.role}`,
        role: message.role === 'user' ? 'user' : 'assistant',
        content: String(message.content),
        runId: String(message.run_id || ''),
        publishableDraft: Boolean(message.publishable_draft && message.run_id),
      }]
    }

    const legacyMessages: ChatMessage[] = []
    if (message?.question) {
      legacyMessages.push({
        id: `history-${sourceId}-user`,
        role: 'user',
        content: String(message.question),
      })
    }
    if (message?.response) {
      legacyMessages.push({
        id: `history-${sourceId}-assistant`,
        role: 'assistant',
        content: String(message.response),
      })
    }
    return legacyMessages
  })
}

const extractDraftCandidates = (content: string) => {
  const normalized = String(content || '').replace(/\r\n/g, '\n').trim()
  if (!normalized) return []

  const marker = /^(?:#{1,6}\s*)?(?:【草稿[^】]*】|(?:草稿|候选)\s*(?:\d+|[一二三四五六七八九十]+))\s*$/mu
  const blocks = normalized.split(new RegExp(`(?=${marker.source})`, 'gmu')).filter(block => marker.test(block))
  const candidates = blocks.map((block) => {
    const body = block.match(/\*{0,2}正文\*{0,2}\s*[:：]\s*([\s\S]*?)(?=\n\s*(?:\*{0,2}适用场景\*{0,2}\s*[:：]|【草稿)|$)/u)?.[1]
    return String(body || '').replace(/^```[^\n]*\n?|```$/g, '').trim()
  }).filter(Boolean)

  return candidates.length > 0 ? [...new Set(candidates)] : [normalized]
}

const choosePublishCandidate = (index: number) => {
  publishCandidateIndex.value = index
  publishDraft.value = publishCandidates.value[index] || ''
  publishDraftError.value = ''
}

const openPublishDialog = (message: ChatMessage) => {
  if (!message?.runId || !message?.publishableDraft) return
  const structuredCandidates: string[] = Array.isArray(message.draftCandidates)
    ? message.draftCandidates.map((candidate: unknown) => String(candidate || '').trim()).filter(Boolean)
    : []
  const candidates: string[] = structuredCandidates.length > 0
    ? [...new Set<string>(structuredCandidates)]
    : extractDraftCandidates(message.content)
  if (candidates.length === 0) return
  publishingMessage.value = message
  publishSourceRunId.value = String(message.runId)
  publishCandidates.value = candidates
  publishCandidateIndex.value = 0
  publishDraft.value = candidates[0] || ''
  publishDraftError.value = ''
  publishDialogOpen.value = true
}

const resetPublishDialog = () => {
  publishDialogOpen.value = false
  publishDraft.value = ''
  publishCandidates.value = []
  publishSourceRunId.value = ''
  publishingMessage.value = null
  publishDraftError.value = ''
}

const closePublishDialog = () => {
  if (isPublishingDraft.value) return
  resetPublishDialog()
}

const submitConfirmedDraft = async () => {
  const content = publishDraft.value.trim()
  if (!content || !publishSourceRunId.value || isPublishingDraft.value) return
  isPublishingDraft.value = true
  publishDraftError.value = ''
  let published = false
  try {
    const response = await confirmPublish({ content, source_run_id: publishSourceRunId.value })
    if (publishingMessage.value) {
      publishingMessage.value.publishedTweetId = String(response.data.tweet_id || '')
    }
    published = true
  } catch (error: any) {
    publishDraftError.value = error?.response?.data?.error || error?.message || '发布失败，请稍后重试。'
  } finally {
    isPublishingDraft.value = false
    if (published) resetPublishDialog()
  }
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

const fetchAgentSkills = async () => {
  try {
    const res = await listAgentSkills()
    agentSkills.value = Array.isArray(res.data.skills) ? res.data.skills : []
    if (
      activeSkillKey.value
      && !agentSkills.value.some(version => agentSkillKey(version) === activeSkillKey.value)
    ) {
      activeSkillKey.value = ''
    }
  } catch (err: any) {
    agentSkills.value = []
    if (err?.response?.status !== 404 && err?.response?.status !== 422) {
      console.warn('Failed to load Agent Skills:', err)
    }
  }
}

const useExtensionSkill = (version: AgentSkill) => {
  const key = agentSkillKey(version)
  const index = agentSkills.value.findIndex(item => agentSkillKey(item) === key)
  if (index >= 0) agentSkills.value.splice(index, 1, version)
  else agentSkills.value.push(version)
  activeSkillKey.value = key
  extensionCatalogDialogOpen.value = false
}

const openExternalMCPDialog = (connectionId = '') => {
  externalMCPInitialConnectionId.value = connectionId
  externalMCPDialogOpen.value = true
}

const manageExtensionMCP = (connectionId: string) => {
  extensionCatalogDialogOpen.value = false
  openExternalMCPDialog(connectionId)
}

const fetchAgentTaskTemplates = async () => {
  try {
    const res = await listAgentTaskTemplates()
    taskTemplateExecutionEnabled.value = Boolean(res.data.execution_enabled)
    agentTaskTemplates.value = Array.isArray(res.data.task_templates)
      ? res.data.task_templates
      : []
    if (
      activeTaskTemplateId.value
      && !agentTaskTemplates.value.some(template => template.template_id === activeTaskTemplateId.value)
    ) {
      activeTaskTemplateId.value = ''
    }
  } catch (err: any) {
    taskTemplateExecutionEnabled.value = false
    agentTaskTemplates.value = []
    if (err?.response?.status !== 404 && err?.response?.status !== 422) {
      console.warn('Failed to load Agent task templates:', err)
    }
  }
}

const newTaskTemplateIdempotencyKey = () => {
  if (typeof window.crypto?.randomUUID === 'function') {
    return window.crypto.randomUUID()
  }
  return `task-template-${Date.now()}-${Math.random().toString(16).slice(2)}`
}

const canSaveTaskTemplate = (message: ChatMessage) => Boolean(
  taskTemplateExecutionEnabled.value
  && message.role === 'assistant'
  && message.runId,
)

const openRunAccounting = async (message: ChatMessage) => {
  if (!message.runId || accountingLoading.value) return
  accountingDialogOpen.value = true
  accountingLoading.value = true
  accountingError.value = ''
  runAccounting.value = null
  try {
    const response = await getAgentRunAccounting(message.runId)
    runAccounting.value = response.data
  } catch (err: any) {
    accountingError.value = err?.response?.data?.error || err?.message || '读取运行用量失败'
  } finally {
    accountingLoading.value = false
  }
}

const closeRunAccounting = () => {
  accountingDialogOpen.value = false
  accountingError.value = ''
  runAccounting.value = null
}

const accountingStateLabel = (state: string) => {
  if (state === 'complete') return '核算完整'
  if (state === 'partial') return '部分核算'
  return '暂无可信核算'
}

const formatUsageNumber = (value: number | undefined) => (
  new Intl.NumberFormat('zh-CN').format(Number(value || 0))
)

const formatCostMicros = (value: number | undefined) => (
  `${formatUsageNumber(value)} 微单位`
)

const openTaskTemplateDialog = async (message: ChatMessage) => {
  if (!canSaveTaskTemplate(message) || !message.runId || resolvingTaskTemplateRunId.value) return
  resolvingTaskTemplateRunId.value = message.runId
  try {
    const run = await getAgentRun(message.runId)
    applyExecutionRunState(message, run.data)
    if (run.data.status !== 'completed' || Number(run.data.revision || 0) <= 0) {
      window.alert('只有已完成的任务可以保存为模板')
      return
    }
    taskTemplateSourceMessage.value = message
    taskTemplateName.value = ''
    taskTemplateDescription.value = ''
    taskTemplateInstruction.value = '{{input}}'
    taskTemplateIdempotencyKey.value = newTaskTemplateIdempotencyKey()
    taskTemplateError.value = ''
    taskTemplateDialogOpen.value = true
  } catch (err: any) {
    window.alert(err?.response?.data?.error || err?.message || '读取任务运行状态失败')
  } finally {
    resolvingTaskTemplateRunId.value = ''
  }
}

const closeTaskTemplateDialog = () => {
  if (isSavingTaskTemplate.value) return
  taskTemplateDialogOpen.value = false
  taskTemplateSourceMessage.value = null
  taskTemplateError.value = ''
}

const submitTaskTemplate = async () => {
  const source = taskTemplateSourceMessage.value
  if (
    !source?.runId
    || !canSubmitTaskTemplate.value
    || !taskTemplateIdempotencyKey.value
  ) return
  isSavingTaskTemplate.value = true
  taskTemplateError.value = ''
  try {
    const res = await createAgentTaskTemplate(source.runId, {
      expected_source_run_revision: Number(source.approvalState?.revision || 0),
      name: taskTemplateName.value.trim(),
      description: taskTemplateDescription.value.trim(),
      instruction_template: taskTemplateInstruction.value.trim(),
      idempotency_key: taskTemplateIdempotencyKey.value,
    })
    const saved = res.data.task_template
    agentTaskTemplates.value = [
      saved,
      ...agentTaskTemplates.value.filter(template => template.template_id !== saved.template_id),
    ]
    activeTaskTemplateId.value = saved.template_id
    isSavingTaskTemplate.value = false
    closeTaskTemplateDialog()
  } catch (err: any) {
    taskTemplateError.value = err?.response?.data?.error || err?.message || '保存任务模板失败'
  } finally {
    isSavingTaskTemplate.value = false
  }
}

const archiveSelectedTaskTemplate = async () => {
  const template = selectedTaskTemplate.value
  if (!template || isArchivingTaskTemplate.value) return
  if (!window.confirm(`归档任务模板“${template.name}”？`)) return
  isArchivingTaskTemplate.value = true
  try {
    await archiveAgentTaskTemplate(template.template_id, template.revision)
    agentTaskTemplates.value = agentTaskTemplates.value.filter(
      item => item.template_id !== template.template_id,
    )
    activeTaskTemplateId.value = ''
  } catch (err: any) {
    window.alert(err?.response?.data?.error || err?.message || '归档任务模板失败')
  } finally {
    isArchivingTaskTemplate.value = false
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
  const requestSequence = ++dialogueLoadSequence
  dialogueViewRevision.value += 1
  activeDialogueId.value = id
  messages.value = []
  dialogueLoadError.value = ''
  isDialogueLoading.value = true
  try {
    const res = await getDialogueMessages(id)
    if (requestSequence !== dialogueLoadSequence || activeDialogueId.value !== id) return
    const msgs = res.data.messages || []
    messages.value = normalizeDialogueMessages(msgs)
    if (messages.value.length === 0) {
      dialogueLoadError.value = '该对话没有已保存的消息，可能在模型或工具返回前失败。你可以在下方重新发送，或新建对话。'
    }
    const latestRunMessage = [...messages.value].reverse().find(message => (
      message.role === 'assistant' && message.runId
    ))
    if (latestRunMessage?.runId) {
      try {
        const run = await getAgentRun(latestRunMessage.runId)
        if (requestSequence !== dialogueLoadSequence || activeDialogueId.value !== id) return
        applyExecutionRunState(latestRunMessage, run.data)
      } catch (runErr: any) {
        if (runErr?.response?.status !== 404 && runErr?.response?.status !== 422) {
          console.warn('Failed to load latest agent run state:', runErr)
        }
      }
    }
    scrollToBottom()
  } catch (err: any) {
    if (requestSequence !== dialogueLoadSequence || activeDialogueId.value !== id) return
    console.error('Failed to load messages:', err)
    dialogueLoadError.value = err?.response?.data?.error || err?.message || '对话加载失败，请稍后重试。'
  } finally {
    if (requestSequence === dialogueLoadSequence) {
      isDialogueLoading.value = false
    }
  }
}

const createNewDialogue = () => {
  dialogueLoadSequence += 1
  dialogueViewRevision.value += 1
  activeDialogueId.value = ''
  messages.value = []
  dialogueLoadError.value = ''
  isDialogueLoading.value = false
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

  isUploading.value = true
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
    isUploading.value = false
    if (fileInputRef.value) fileInputRef.value.value = ''
  }
}

const handleWorkflowResumed = async () => {
  await fetchDialogues()
  if (activeDialogueId.value) {
    await selectDialogue(activeDialogueId.value)
  }
}

const capabilityLabel = (capabilityId: string) => {
  const labels: Record<string, string> = {
    'conversation.reply': '对话',
    'platform.search': '站内搜索',
    'web.search': '联网搜索',
    'connector.mcp': '外部 MCP 工具',
    'content.draft': '内容草拟',
    'skill.run': '自定义技能',
  }
  return labels[capabilityId] || capabilityId
}

const toolLabel = (toolName: string) => {
  const labels: Record<string, string> = {
    hybrid_search_tweets: '站内搜索',
    web_search: '联网搜索',
  }
  return labels[toolName] || toolName
}

const toolStatusLabel = (status: string) => {
  const labels: Record<string, string> = {
    succeeded: '已完成',
    failed: '失败',
    pending: '执行中',
  }
  return labels[status] || status
}

const runStatusLabel = (status: string) => {
  const labels: Record<string, string> = {
    success: '已完成',
    succeeded: '已完成',
    completed: '已完成',
    failed: '失败',
    pending: '执行中',
    suspended: '已挂起',
    awaiting_human: '等待你的回复',
    approval_required: '等待工具审批',
    running: '执行中',
  }
  return labels[status] || status
}

const approvalStatusLabel = (approval?: AgentApprovalState) => {
  if (approval?.status === 'input_required' && approval.action === 'tool_call') {
    return '等待工具审批'
  }
  const labels: Record<string, string> = {
    pending: '等待审批',
    approved: '已批准',
    rejected: '已拒绝',
    expired: '已过期',
    input_required: '请回复上方问题后继续',
    responded: '已提交回复',
  }
  return labels[approval?.status || ''] || approval?.status || ''
}

const shouldShowApproval = (approval?: AgentApprovalState) => Boolean(
  approval?.status && approval.status !== 'not_required',
)

const buildRunMessage = (data: RunAgentResponse): ChatMessage => {
  const artifacts = Array.isArray(data.artifacts) ? data.artifacts : []
  const draftArtifacts = artifacts.filter(artifact => (
    artifact.type === 'content.draft' && String(artifact.content || '').trim()
  ))
  const artifactCandidates = draftArtifacts.map(artifact => artifact.content.trim())
  const sourceRunId = String(draftArtifacts[0]?.source_run_id || data.run_id || '')
  const citations = Array.isArray(data.citations) && data.citations.length > 0
    ? data.citations
    : (data.tweet_list || []).map(tweet => ({
      citation_id: `tweet:${tweet.tweet_id}`,
      source_type: 'tweet',
      source_id: tweet.tweet_id,
      url: tweet.url,
      title: tweet.summary || `推文 ${tweet.tweet_id}`,
      snippet: tweet.summary || '',
    }))

  return {
    id: nextMessageId('assistant'),
    role: 'assistant',
    content: String(data.response || ''),
    runId: sourceRunId,
    runStatus: String(data.run_status || ''),
    executionProfile: String(data.execution_profile || ''),
    capabilityIds: Array.isArray(data.capability_ids) ? data.capability_ids : [],
    publishableDraft: Boolean(data.publishable_draft && sourceRunId),
    draftCandidates: artifactCandidates.length > 0
      ? [...new Set(artifactCandidates)]
      : extractDraftCandidates(data.response || ''),
    toolActivities: Array.isArray(data.tool_activities) ? data.tool_activities : [],
    citations,
    artifacts,
    approvalState: data.approval_state,
    selectedSkillId: String(data.selected_skill_id || ''),
    selectedSkillVersion: String(data.selected_skill_version || ''),
    selectedTaskTemplateId: String(data.selected_task_template_id || ''),
    selectedTaskTemplateRevision: Number(data.selected_task_template_revision || 0),
  }
}

const renderTaskTemplateInput = (template: AgentTaskTemplate, input: string) => (
  template.instruction_template.replace('{{input}}', input)
)

const normalizeAgentRequestError = (err: any) => {
  const rawMessage = String(
    err?.response?.data?.error || err?.message || '请求失败，请稍后重试。',
  )
  if (
    rawMessage.includes('required capability evidence is missing')
    && rawMessage.includes('did not complete its bound workflow tool')
  ) {
    return '所选工作流未完成执行，系统没有生成可用结果。请确认工作流包含可达的结束节点后重试。'
  }
  return rawMessage
}

const sendMessage = async () => {
  const content = inputContent.value.trim()
  if (!content || !canSend.value) return

  const requestDialogueKey = activeDialogueId.value
  const requestViewRevision = dialogueViewRevision.value
  const resumeTarget = resumableRunMessage.value
  const requestedTaskTemplate = resumeTarget ? undefined : selectedTaskTemplate.value
  const resumeTargetSnapshot = resumeTarget?.approvalState ? {
    runStatus: resumeTarget.runStatus,
    approvalState: { ...resumeTarget.approvalState },
  } : undefined
  const optimisticMessageId = nextMessageId('user')
  inputContent.value = ''
  messages.value.push({
    id: optimisticMessageId,
    role: 'user',
    content: requestedTaskTemplate
      ? renderTaskTemplateInput(requestedTaskTemplate, content)
      : content,
  })
  scrollToBottom()

  isSending.value = true
  sendingViewRevision.value = requestViewRevision
  try {
    let res
    if (resumeTarget?.runId && resumeTarget.approvalState) {
      resumeTarget.runStatus = 'running'
      resumeTarget.approvalState.status = 'responded'
      res = await resumeAgentRun(resumeTarget.runId, {
        expected_revision: resumeTarget.approvalState.revision,
        human_response: content,
      })
    } else if (requestedTaskTemplate) {
      res = await runAgentTaskTemplate(requestedTaskTemplate.template_id, {
        expected_revision: requestedTaskTemplate.revision,
        input: content,
        dialogue_id: '0',
        dialogue_key: requestDialogueKey,
        model_kind_id: activeModelId.value || '0',
        web_search_provider_config_id: activeWebSearchProviderConfigId.value || undefined,
      })
    } else {
      const preferredCapabilities = preferredCapabilityIds.value
      const requestedSkill = selectedSkill.value
      res = await runAgent({
        content,
        dialogue_id: '0',
        dialogue_key: requestDialogueKey,
        model_kind_id: activeModelId.value || '0',
        preferred_capability_ids: preferredCapabilities.length > 0 ? preferredCapabilities : undefined,
        web_search_provider_config_id: activeWebSearchProviderConfigId.value || undefined,
        skill_id: requestedSkill?.skill_id,
        skill_version: requestedSkill?.version,
      })
    }
    const returnedDialogueKey = String(res.data.dialogue_key || requestDialogueKey)
    const viewIsCurrent = (
      requestViewRevision === dialogueViewRevision.value
      && activeDialogueId.value === requestDialogueKey
    )
    if (viewIsCurrent) {
      activeDialogueId.value = returnedDialogueKey
      messages.value.push(buildRunMessage(res.data))
      scrollToBottom()
    }
    await fetchDialogues()
    if (returnedDialogueKey) {
      upsertDialogueSummary(returnedDialogueKey, content)
    }
  } catch (err: any) {
    console.error('Send failed:', err)
    const errorMessage = normalizeAgentRequestError(err)
    let authoritativeRun: AgentExecutionRunResponse | undefined
    if (resumeTarget?.runId) {
      try {
        const run = await getAgentRun(resumeTarget.runId)
        authoritativeRun = run.data
      } catch (runErr) {
        console.warn('Failed to refresh agent run after resume error:', runErr)
      }
    }
    const viewIsCurrent = (
      requestViewRevision === dialogueViewRevision.value
      && activeDialogueId.value === requestDialogueKey
    )
    if (viewIsCurrent && authoritativeRun?.status === 'completed' && requestDialogueKey) {
      await fetchDialogues()
      if (
        requestViewRevision === dialogueViewRevision.value
        && activeDialogueId.value === requestDialogueKey
      ) {
        await selectDialogue(requestDialogueKey)
      }
      return
    }
    if (viewIsCurrent) {
      messages.value = messages.value.filter(message => message.id !== optimisticMessageId)
      inputContent.value = content
      if (resumeTarget) {
        if (authoritativeRun) {
          applyExecutionRunState(resumeTarget, authoritativeRun)
        } else if (resumeTargetSnapshot) {
          resumeTarget.runStatus = resumeTargetSnapshot.runStatus
          resumeTarget.approvalState = resumeTargetSnapshot.approvalState
        }
      }
      messages.value.push({
        id: nextMessageId('error'),
        role: 'assistant',
        content: `请求失败：${errorMessage}`,
        runStatus: 'failed',
      })
      scrollToBottom()
    }
  } finally {
    isSending.value = false
    sendingViewRevision.value = null
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
      <div class="absolute top-0 z-10 flex h-16 w-full items-center justify-between border-b border-gray-200 bg-white/80 px-3 backdrop-blur-md dark:border-gray-800 dark:bg-black/80 sm:px-6">
        <div class="flex min-w-0 items-center space-x-2 sm:space-x-3">
          <button
            @click="goBack"
            class="flex h-9 w-9 items-center justify-center rounded-full text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white"
            title="返回上一页"
          >
            <ArrowLeftIcon class="h-5 w-5" />
          </button>
          <span class="hidden text-lg font-bold sm:inline">智能体</span>
          <select
            v-model="activeModelId"
            :disabled="models.length === 0"
            title="对话模型"
            class="max-w-32 cursor-pointer rounded-lg border-none bg-gray-100 px-2 py-1.5 text-sm outline-none disabled:cursor-not-allowed disabled:opacity-60 dark:bg-gray-800 sm:max-w-none sm:px-3"
          >
            <option v-if="models.length === 0" value="">无可用对话模型</option>
            <option v-for="model in models" :key="model.id" :value="String(model.id)">
              {{ model.name }}
            </option>
          </select>
        </div>

        <div class="flex items-center gap-2">
          <ApprovalInbox @resumed="handleWorkflowResumed" />
          <button
            type="button"
            title="联网配置"
            class="relative flex h-9 w-9 items-center justify-center rounded-lg border border-gray-200 text-gray-600 transition-colors hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
            @click="webProviderDialogOpen = true"
          >
            <GlobeAltIcon class="h-4 w-4" />
            <span v-if="activeWebSearchProviderConfigId" class="absolute right-1 top-1 h-1.5 w-1.5 rounded-full bg-green-500"></span>
          </button>
          <button
            v-if="extensionCatalogAvailable"
            type="button"
            title="扩展目录"
            class="flex h-9 w-9 items-center justify-center rounded-lg border border-gray-200 text-gray-600 transition-colors hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
            @click="extensionCatalogDialogOpen = true"
          >
            <PuzzlePieceIcon class="h-4 w-4" />
          </button>
          <button
            v-if="extensionMarketplaceAvailable"
            type="button"
            title="扩展市场"
            class="flex h-9 w-9 items-center justify-center rounded-lg border border-gray-200 text-gray-600 transition-colors hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
            @click="extensionMarketplaceDialogOpen = true"
          >
            <BuildingStorefrontIcon class="h-4 w-4" />
          </button>
          <RouterLink
            v-if="extensionManagementAvailable"
            to="/agent/marketplace/manage"
            title="扩展发布管理"
            class="flex h-9 w-9 items-center justify-center rounded-lg border border-gray-200 text-gray-600 transition-colors hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
          >
            <KeyIcon class="h-4 w-4" />
          </RouterLink>
          <button
            type="button"
            title="外部 MCP"
            class="flex h-9 w-9 items-center justify-center rounded-lg border border-gray-200 text-gray-600 transition-colors hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
            @click="openExternalMCPDialog()"
          >
            <ServerStackIcon class="h-4 w-4" />
          </button>
          <RouterLink
            to="/agent/profiles"
            title="智能体配置"
            class="flex h-9 w-9 items-center justify-center rounded-lg border border-gray-200 text-gray-600 transition-colors hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
          >
            <RectangleStackIcon class="h-4 w-4" />
          </RouterLink>
          <RouterLink
            to="/agent/workflow"
            title="打开自动化工作流"
            class="flex items-center space-x-1.5 rounded-lg bg-primary px-3 py-2 text-xs font-semibold text-white shadow-sm transition-colors hover:bg-blue-600"
          >
            <SparklesIcon class="h-4 w-4" />
            <span class="hidden lg:inline">自动化</span>
          </RouterLink>
        </div>
      </div>

      <div ref="chatContainerRef" class="flex-1 space-y-6 overflow-y-auto p-4 pb-44 pt-24 sm:p-6 sm:pb-44 sm:pt-24">
        <div v-if="isDialogueLoading" class="flex h-full items-center justify-center text-gray-400">
          <ArrowPathIcon class="h-6 w-6 animate-spin" />
        </div>

        <div v-else-if="dialogueLoadError" class="flex h-full flex-col items-center justify-center gap-3 text-center">
          <ExclamationCircleIcon class="h-10 w-10 text-red-400" />
          <p class="max-w-md text-sm text-gray-500 dark:text-gray-400">{{ dialogueLoadError }}</p>
          <button
            type="button"
            class="text-sm font-medium text-primary hover:text-blue-600"
            @click="selectDialogue(activeDialogueId)"
          >
            重新加载
          </button>
        </div>

        <div v-else-if="messages.length === 0" class="flex h-full flex-col items-center justify-center space-y-4 text-gray-400">
          <SparklesIcon class="h-16 w-16 opacity-50" />
          <p class="text-lg">从一个问题、想法或任务开始</p>
        </div>

        <div v-for="msg in messages" :key="msg.id" class="flex w-full" :class="msg.role === 'user' ? 'justify-end' : 'justify-start'">
          <div
            class="max-w-[88%] whitespace-pre-wrap rounded-2xl px-5 py-3 shadow-sm sm:max-w-[80%]"
            :class="msg.role === 'user' ? 'rounded-br-none bg-primary text-white' : 'rounded-bl-none bg-gray-100 dark:bg-gray-800'"
          >
            <div v-if="msg.content">{{ msg.content }}</div>

            <div
              v-if="msg.role === 'assistant' && (msg.capabilityIds?.length || msg.runStatus || msg.selectedSkillId || msg.selectedTaskTemplateId)"
              class="mt-3 flex flex-wrap items-center gap-1.5 border-t border-gray-200 pt-2 dark:border-gray-700"
            >
              <span
                v-for="capabilityId in msg.capabilityIds"
                :key="capabilityId"
                class="rounded bg-white px-2 py-1 text-xs text-gray-600 dark:bg-gray-900 dark:text-gray-300"
              >
                {{ capabilityLabel(capabilityId) }}
              </span>
              <span
                v-if="msg.selectedSkillId"
                :title="`${msg.selectedSkillId}@${msg.selectedSkillVersion}`"
                class="inline-flex items-center gap-1 text-xs font-medium text-indigo-700 dark:text-indigo-300"
              >
                <RectangleStackIcon class="h-4 w-4" />
                {{ msg.selectedSkillId }}
              </span>
              <span
                v-if="msg.selectedTaskTemplateId"
                :title="`${msg.selectedTaskTemplateId}@${msg.selectedTaskTemplateRevision}`"
                class="inline-flex items-center gap-1 text-xs font-medium text-emerald-700 dark:text-emerald-300"
              >
                <BookmarkSquareIcon class="h-4 w-4" />
                任务模板
              </span>
              <span
                v-if="msg.runStatus"
                :title="msg.executionProfile || '执行状态'"
                class="inline-flex items-center gap-1 text-xs text-gray-500 dark:text-gray-400"
              >
                <CheckCircleIcon
                  v-if="['success', 'succeeded', 'completed'].includes(msg.runStatus)"
                  class="h-4 w-4 text-green-600 dark:text-green-400"
                />
                <ExclamationCircleIcon
                  v-else-if="msg.runStatus === 'failed'"
                  class="h-4 w-4 text-red-500"
                />
                <ArrowPathIcon v-else class="h-4 w-4" />
                {{ runStatusLabel(msg.runStatus) }}
              </span>
            </div>

            <div
              v-if="msg.role === 'assistant' && msg.toolActivities?.length"
              class="mt-3 space-y-1.5 border-t border-gray-200 pt-2 dark:border-gray-700"
            >
              <div
                v-for="activity in msg.toolActivities"
                :key="`${activity.step_index}-${activity.tool_name}`"
                class="flex items-center gap-2 text-xs text-gray-600 dark:text-gray-300"
              >
                <MagnifyingGlassIcon class="h-4 w-4 shrink-0 text-gray-400" />
                <span class="font-medium">{{ toolLabel(activity.tool_name) }}</span>
                <span class="text-gray-400">{{ toolStatusLabel(activity.status) }}</span>
                <span v-if="activity.result_count > 0" class="text-gray-400">
                  · {{ activity.result_count }} 条结果
                </span>
              </div>
            </div>

            <div
              v-if="msg.role === 'assistant' && msg.citations?.length"
              class="mt-3 space-y-2 border-t border-gray-200 pt-2 dark:border-gray-700"
            >
              <div class="text-xs font-medium text-gray-500 dark:text-gray-400">引用</div>
              <a
                v-for="citation in msg.citations"
                :key="citation.citation_id"
                :href="citation.url"
                :target="citation.url.startsWith('/') ? undefined : '_blank'"
                :rel="citation.url.startsWith('/') ? undefined : 'noopener noreferrer'"
                class="group flex items-start gap-2 rounded p-2 text-left transition-colors hover:bg-white dark:hover:bg-gray-900"
              >
                <span class="min-w-0 flex-1">
                  <span class="block truncate text-sm font-medium text-gray-800 group-hover:text-primary dark:text-gray-100">
                    {{ citation.title || `来源 ${citation.source_id}` }}
                  </span>
                  <span v-if="citation.snippet" class="mt-0.5 block line-clamp-2 whitespace-normal text-xs leading-5 text-gray-500 dark:text-gray-400">
                    {{ citation.snippet }}
                  </span>
                </span>
                <ArrowTopRightOnSquareIcon class="mt-0.5 h-4 w-4 shrink-0 text-gray-400 group-hover:text-primary" />
              </a>
            </div>

            <div
              v-if="msg.role === 'assistant' && shouldShowApproval(msg.approvalState)"
              class="mt-3 flex items-center gap-2 border-t border-gray-200 pt-2 text-xs text-amber-700 dark:border-gray-700 dark:text-amber-300"
            >
              <ExclamationCircleIcon class="h-4 w-4" />
              {{ approvalStatusLabel(msg.approvalState) }}
            </div>

            <div
              v-if="msg.role === 'assistant' && msg.runId"
              class="mt-3 flex flex-wrap items-center gap-4 border-t border-gray-200 pt-2 dark:border-gray-700"
            >
              <button
                type="button"
                class="inline-flex items-center gap-1.5 text-xs font-medium text-gray-600 hover:text-primary dark:text-gray-300"
                @click="openRunAccounting(msg)"
              >
                <ChartBarSquareIcon class="h-4 w-4" />
                用量
              </button>
              <button
                v-if="canSaveTaskTemplate(msg)"
                type="button"
                :disabled="Boolean(resolvingTaskTemplateRunId)"
                class="inline-flex items-center gap-1.5 text-xs font-medium text-primary hover:text-blue-600 disabled:cursor-not-allowed disabled:opacity-50"
                @click="openTaskTemplateDialog(msg)"
              >
                <ArrowPathIcon
                  v-if="resolvingTaskTemplateRunId === msg.runId"
                  class="h-4 w-4 animate-spin"
                />
                <BookmarkSquareIcon v-else class="h-4 w-4" />
                保存为任务模板
              </button>
            </div>

            <div v-if="msg.publishableDraft" class="mt-3 flex items-center border-t border-gray-200 pt-2 dark:border-gray-700">
              <span v-if="msg.publishedTweetId" class="text-xs text-green-600 dark:text-green-400">
                已发布 · {{ msg.publishedTweetId }}
              </span>
              <template v-else>
                <span class="mr-3 text-xs text-gray-500 dark:text-gray-400">可发布草稿</span>
                <button
                  type="button"
                  @click="openPublishDialog(msg)"
                  class="inline-flex items-center gap-1.5 text-xs font-medium text-primary hover:text-blue-600"
                >
                  <PaperAirplaneIcon class="h-4 w-4" />
                  检查并发布
                </button>
              </template>
            </div>
          </div>
        </div>

        <div v-if="isSendingForCurrentView && (!messages.length || lastMessageIsUser)" class="flex w-full justify-start">
          <div class="flex items-center space-x-2 rounded-2xl rounded-bl-none bg-gray-100 px-5 py-4 dark:bg-gray-800">
            <div class="h-2 w-2 animate-bounce rounded-full bg-gray-400"></div>
            <div class="h-2 w-2 animate-bounce rounded-full bg-gray-400" style="animation-delay: 0.2s"></div>
            <div class="h-2 w-2 animate-bounce rounded-full bg-gray-400" style="animation-delay: 0.4s"></div>
          </div>
        </div>
      </div>

      <div class="absolute bottom-0 w-full border-t border-gray-100 bg-gradient-to-t from-white via-white to-transparent px-6 pb-6 pt-10 dark:border-gray-800 dark:from-black dark:via-black">
        <div class="relative mx-auto flex max-w-4xl flex-col space-y-3">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <label class="flex min-w-0 items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
              <SparklesIcon class="h-4 w-4 shrink-0" />
              <span class="sr-only">能力偏好</span>
              <select
                v-model="activeCapabilityPresetId"
                title="能力偏好"
                class="min-w-0 cursor-pointer border-none bg-transparent p-0 pr-7 text-xs font-medium text-gray-600 outline-none focus:ring-0 dark:text-gray-300"
              >
                <option v-for="preset in capabilityPresets" :key="preset.id" :value="preset.id">
                  {{ preset.name }}
                </option>
              </select>
            </label>
            <label
              v-if="agentSkills.length > 0"
              class="flex min-w-0 items-center gap-2 text-xs text-gray-500 dark:text-gray-400"
            >
              <RectangleStackIcon class="h-4 w-4 shrink-0" />
              <span class="sr-only">自定义技能</span>
              <select
                v-model="activeSkillKey"
                title="选择一个固定版本的自定义技能"
                class="min-w-0 max-w-56 cursor-pointer border-none bg-transparent p-0 pr-7 text-xs font-medium text-gray-600 outline-none focus:ring-0 dark:text-gray-300"
              >
                <option value="">不使用自定义技能</option>
                <option
                  v-for="version in agentSkills"
                  :key="agentSkillKey(version)"
                  :value="agentSkillKey(version)"
                >
                  {{ version.display_name }} · {{ version.version.slice(0, 11) }}
                </option>
              </select>
            </label>
            <div
              v-if="taskTemplateExecutionEnabled && agentTaskTemplates.length > 0"
              class="flex min-w-0 items-center gap-1 text-xs text-gray-500 dark:text-gray-400"
            >
              <BookmarkSquareIcon class="h-4 w-4 shrink-0" />
              <select
                v-model="activeTaskTemplateId"
                title="选择任务模板"
                class="min-w-0 max-w-56 cursor-pointer border-none bg-transparent p-0 pr-7 text-xs font-medium text-gray-600 outline-none focus:ring-0 dark:text-gray-300"
              >
                <option value="">不使用任务模板</option>
                <option
                  v-for="taskTemplate in agentTaskTemplates"
                  :key="taskTemplate.template_id"
                  :value="taskTemplate.template_id"
                >
                  {{ taskTemplate.name }}
                </option>
              </select>
              <button
                v-if="selectedTaskTemplate"
                type="button"
                title="归档任务模板"
                :disabled="isArchivingTaskTemplate"
                class="flex h-7 w-7 shrink-0 items-center justify-center text-gray-400 hover:text-red-500 disabled:cursor-not-allowed disabled:opacity-50"
                @click="archiveSelectedTaskTemplate"
              >
                <ArrowPathIcon v-if="isArchivingTaskTemplate" class="h-4 w-4 animate-spin" />
                <ArchiveBoxXMarkIcon v-else class="h-4 w-4" />
              </button>
            </div>
            <button
              v-if="activeWebSearchProviderConfigId"
              type="button"
              class="inline-flex shrink-0 items-center gap-1 text-xs font-medium text-green-700 hover:text-green-800 dark:text-green-400"
              @click="webProviderDialogOpen = true"
            >
              <GlobeAltIcon class="h-4 w-4" />
              个人联网 API
            </button>
            <span v-else-if="activeDialogueId" class="truncate text-xs text-gray-400">连续对话</span>
          </div>

          <div class="relative flex items-end rounded-lg border border-gray-300 bg-white shadow-sm transition-all focus-within:border-primary focus-within:ring-2 focus-within:ring-primary dark:border-gray-700 dark:bg-gray-900">
            <input ref="fileInputRef" type="file" class="hidden" @change="handleFileUpload" />
            <button
              type="button"
              :disabled="isUploading || isSending"
              class="p-3 text-gray-400 transition-colors hover:text-primary disabled:cursor-not-allowed disabled:opacity-50"
              title="解析文件"
              @click="triggerFileUpload"
            >
              <ArrowPathIcon v-if="isUploading" class="h-6 w-6 animate-spin" />
              <PaperClipIcon v-else class="h-6 w-6" />
            </button>

            <textarea
              v-model="inputContent"
              @keydown.enter.exact.prevent="sendMessage"
              placeholder="输入消息"
              :disabled="models.length === 0"
              class="max-h-48 min-h-[52px] flex-1 resize-none border-none bg-transparent px-2 py-3 text-gray-900 focus:ring-0 dark:text-white"
              rows="1"
            ></textarea>

            <button
              @click="sendMessage"
              :disabled="!canSend"
              title="发送"
              class="m-1.5 rounded-lg p-3 text-white transition-colors"
              :class="canSend ? 'bg-primary hover:bg-blue-600' : 'cursor-not-allowed bg-gray-300 dark:bg-gray-700'"
            >
              <ArrowPathIcon v-if="isSending" class="h-5 w-5 animate-spin" />
              <PaperAirplaneIcon v-else class="h-5 w-5" />
            </button>
          </div>

          <div class="text-center text-xs text-gray-400">
            AI Agent 可能会犯错。请核查重要信息。
          </div>
        </div>
      </div>
    </div>

    <WebSearchProviderDialog
      v-model="activeWebSearchProviderConfigId"
      :open="webProviderDialogOpen"
      @close="webProviderDialogOpen = false"
    />

    <AgentExtensionCatalogDialog
      :open="extensionCatalogDialogOpen"
      @close="extensionCatalogDialogOpen = false"
      @use-skill="useExtensionSkill"
      @manage-mcp="manageExtensionMCP"
    />

    <AgentExtensionMarketplaceDialog
      :open="extensionMarketplaceDialogOpen"
      @close="extensionMarketplaceDialogOpen = false"
    />

    <ExternalMCPDialog
      :open="externalMCPDialogOpen"
      :initial-connection-id="externalMCPInitialConnectionId"
      @close="externalMCPDialogOpen = false"
    />

    <div
      v-if="accountingDialogOpen"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/45 p-4"
      @click.self="closeRunAccounting"
    >
      <div class="flex max-h-[85vh] w-full max-w-3xl flex-col overflow-hidden rounded-lg bg-white shadow-2xl dark:bg-gray-900">
        <div class="flex items-center justify-between border-b border-gray-200 px-5 py-4 dark:border-gray-700">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">运行用量</h2>
            <p v-if="runAccounting" class="mt-0.5 font-mono text-xs text-gray-400">
              {{ runAccounting.run_id }}
            </p>
          </div>
          <button
            type="button"
            title="关闭"
            class="flex h-8 w-8 items-center justify-center rounded-full text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-800"
            @click="closeRunAccounting"
          >
            <XMarkIcon class="h-5 w-5" />
          </button>
        </div>

        <div class="min-h-56 flex-1 overflow-y-auto">
          <div v-if="accountingLoading" class="flex min-h-56 items-center justify-center text-gray-400">
            <ArrowPathIcon class="h-6 w-6 animate-spin" />
          </div>
          <div v-else-if="accountingError" class="flex min-h-56 items-center justify-center p-6 text-sm text-red-600">
            {{ accountingError }}
          </div>
          <template v-else-if="runAccounting">
            <div class="flex flex-wrap items-center justify-between gap-3 px-5 py-4">
              <span
                class="text-sm font-medium"
                :class="runAccounting.state === 'complete'
                  ? 'text-green-700 dark:text-green-300'
                  : runAccounting.state === 'partial'
                    ? 'text-amber-700 dark:text-amber-300'
                    : 'text-gray-500 dark:text-gray-400'"
              >
                {{ accountingStateLabel(runAccounting.state) }}
              </span>
              <span class="text-xs text-gray-400">
                直接子工作流 {{ runAccounting.included_child_run_count }} / {{ runAccounting.child_run_count }}
              </span>
            </div>

            <div
              v-if="runAccounting.state !== 'complete'"
              class="border-y border-amber-200 bg-amber-50 px-5 py-3 text-sm text-amber-800 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-200"
            >
              运行尚未结束、存在旧版记录或查询已截断，当前合计只包含可验证的核算快照。
            </div>

            <div class="grid grid-cols-1 border-y border-gray-200 sm:grid-cols-3 sm:divide-x dark:border-gray-700 dark:sm:divide-gray-700">
              <div class="px-5 py-4">
                <div class="text-xs text-gray-400">总计 Token</div>
                <div class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">
                  {{ formatUsageNumber(runAccounting.total_usage.total_tokens) }}
                </div>
                <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                  {{ formatCostMicros(runAccounting.total_usage.estimated_cost_micros) }}
                  <span v-if="runAccounting.total_usage.estimated || runAccounting.total_usage.cost_estimated"> · 含估算</span>
                </div>
              </div>
              <div class="border-t border-gray-200 px-5 py-4 sm:border-t-0 dark:border-gray-700">
                <div class="text-xs text-gray-400">父 Agent 自身</div>
                <div class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">
                  {{ formatUsageNumber(runAccounting.parent_usage.total_tokens) }}
                </div>
                <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                  {{ runAccounting.parent_budget.consumed_steps }} / {{ runAccounting.parent_budget.max_steps }} 步
                </div>
              </div>
              <div class="border-t border-gray-200 px-5 py-4 sm:border-t-0 dark:border-gray-700">
                <div class="text-xs text-gray-400">直接子工作流</div>
                <div class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">
                  {{ formatUsageNumber(runAccounting.child_usage.total_tokens) }}
                </div>
                <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                  {{ formatCostMicros(runAccounting.child_usage.estimated_cost_micros) }}
                </div>
              </div>
            </div>

            <div class="px-5 py-4">
              <div class="mb-2 text-sm font-semibold text-gray-900 dark:text-white">直接子工作流明细</div>
              <div v-if="runAccounting.children.length === 0" class="py-6 text-center text-sm text-gray-400">
                本次运行没有调用工作流工具
              </div>
              <div
                v-for="child in runAccounting.children"
                :key="child.run_id"
                class="grid gap-2 border-t border-gray-200 py-3 text-sm first:border-t-0 sm:grid-cols-[minmax(0,1fr)_auto] dark:border-gray-700"
              >
                <div class="min-w-0">
                  <div class="truncate font-medium text-gray-800 dark:text-gray-100">
                    {{ child.workflow_id }}
                  </div>
                  <div class="mt-0.5 truncate font-mono text-xs text-gray-400">{{ child.run_id }}</div>
                </div>
                <div class="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-gray-500 sm:justify-end dark:text-gray-400">
                  <span>{{ accountingStateLabel(child.state) }}</span>
                  <span>{{ child.budget.consumed_steps }} / {{ child.budget.max_steps }} 节点</span>
                  <span>{{ formatUsageNumber(child.usage.total_tokens) }} Token</span>
                  <span>{{ formatCostMicros(child.usage.estimated_cost_micros) }}</span>
                </div>
              </div>
            </div>
          </template>
        </div>
      </div>
    </div>

    <div
      v-if="taskTemplateDialogOpen"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/45 p-4"
      @click.self="closeTaskTemplateDialog"
    >
      <div class="w-full max-w-xl rounded-lg bg-white shadow-2xl dark:bg-gray-900">
        <div class="flex items-center justify-between border-b border-gray-200 px-5 py-4 dark:border-gray-700">
          <h2 class="text-base font-semibold text-gray-900 dark:text-white">保存任务模板</h2>
          <button
            type="button"
            title="关闭"
            class="flex h-8 w-8 items-center justify-center rounded-full text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-800"
            @click="closeTaskTemplateDialog"
          >
            <XMarkIcon class="h-5 w-5" />
          </button>
        </div>

        <div class="space-y-4 p-5">
          <label class="block">
            <span class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-200">名称</span>
            <input
              v-model="taskTemplateName"
              maxlength="80"
              class="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 outline-none focus:border-primary focus:ring-2 focus:ring-blue-100 dark:border-gray-700 dark:bg-gray-950 dark:text-white dark:focus:ring-blue-950"
            />
          </label>
          <label class="block">
            <span class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-200">描述</span>
            <input
              v-model="taskTemplateDescription"
              maxlength="500"
              class="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 outline-none focus:border-primary focus:ring-2 focus:ring-blue-100 dark:border-gray-700 dark:bg-gray-950 dark:text-white dark:focus:ring-blue-950"
            />
          </label>
          <label class="block">
            <span class="mb-1.5 flex items-center justify-between gap-3 text-sm font-medium text-gray-700 dark:text-gray-200">
              <span>指令模板</span>
              <code class="text-xs font-normal text-gray-400">&#123;&#123;input&#125;&#125;</code>
            </span>
            <textarea
              v-model="taskTemplateInstruction"
              rows="7"
              maxlength="12288"
              placeholder="围绕以下输入完成任务：{{input}}"
              class="w-full resize-y rounded-md border border-gray-300 bg-white p-3 text-sm leading-6 text-gray-900 outline-none focus:border-primary focus:ring-2 focus:ring-blue-100 dark:border-gray-700 dark:bg-gray-950 dark:text-white dark:focus:ring-blue-950"
            ></textarea>
          </label>
          <p class="min-h-5 text-sm text-red-600">{{ taskTemplateError }}</p>
        </div>

        <div class="flex justify-end gap-2 border-t border-gray-200 px-5 py-4 dark:border-gray-700">
          <button
            type="button"
            class="rounded-md px-4 py-2 text-sm text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800"
            @click="closeTaskTemplateDialog"
          >
            取消
          </button>
          <button
            type="button"
            :disabled="!canSubmitTaskTemplate"
            class="inline-flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-white hover:bg-blue-600 disabled:cursor-not-allowed disabled:opacity-50"
            @click="submitTaskTemplate"
          >
            <ArrowPathIcon v-if="isSavingTaskTemplate" class="h-4 w-4 animate-spin" />
            <BookmarkSquareIcon v-else class="h-4 w-4" />
            保存
          </button>
        </div>
      </div>
    </div>

    <div v-if="publishDialogOpen" class="fixed inset-0 z-50 flex items-center justify-center bg-black/45 p-4" @click.self="closePublishDialog">
      <div class="w-full max-w-2xl rounded-lg bg-white shadow-2xl dark:bg-gray-900">
        <div class="flex items-center justify-between border-b border-gray-200 px-5 py-4 dark:border-gray-700">
          <h2 class="text-base font-semibold text-gray-900 dark:text-white">确认发布草稿</h2>
          <button
            type="button"
            title="关闭"
            class="flex h-8 w-8 items-center justify-center rounded-full text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-800"
            @click="closePublishDialog"
          >
            <XMarkIcon class="h-5 w-5" />
          </button>
        </div>

        <div class="space-y-4 p-5">
          <div v-if="publishCandidates.length > 1" class="flex flex-wrap gap-2">
            <button
              v-for="(_, index) in publishCandidates"
              :key="index"
              type="button"
              class="rounded-md border px-3 py-1.5 text-sm transition-colors"
              :class="publishCandidateIndex === index
                ? 'border-primary bg-blue-50 text-primary dark:bg-blue-950/40'
                : 'border-gray-200 text-gray-600 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800'"
              @click="choosePublishCandidate(index)"
            >
              草稿 {{ index + 1 }}
            </button>
          </div>

          <textarea
            v-model="publishDraft"
            rows="10"
            class="w-full resize-y rounded-lg border border-gray-300 bg-white p-3 text-sm leading-6 text-gray-900 outline-none focus:border-primary focus:ring-2 focus:ring-blue-100 dark:border-gray-700 dark:bg-gray-950 dark:text-white dark:focus:ring-blue-950"
          ></textarea>
          <div class="flex items-center justify-between gap-4">
            <p class="min-h-5 text-sm text-red-600">{{ publishDraftError }}</p>
            <span class="shrink-0 text-xs text-gray-400">{{ publishDraft.length }} 字符</span>
          </div>
        </div>

        <div class="flex justify-end gap-2 border-t border-gray-200 px-5 py-4 dark:border-gray-700">
          <button type="button" class="rounded-md px-4 py-2 text-sm text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800" @click="closePublishDialog">
            取消
          </button>
          <button
            type="button"
            :disabled="!publishDraft.trim() || isPublishingDraft"
            class="inline-flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-white hover:bg-blue-600 disabled:cursor-not-allowed disabled:opacity-50"
            @click="submitConfirmedDraft"
          >
            <ArrowPathIcon v-if="isPublishingDraft" class="h-4 w-4 animate-spin" />
            <PaperAirplaneIcon v-else class="h-4 w-4" />
            发布
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
