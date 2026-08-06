<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  ArrowLeftIcon,
  ArrowPathIcon,
  CheckIcon,
  ClockIcon,
  PencilSquareIcon,
  PaperAirplaneIcon,
  PlusIcon,
  ShieldCheckIcon,
  TrashIcon,
  UserGroupIcon,
  XMarkIcon,
} from '@heroicons/vue/24/outline'
import {
  createAgentProfileDraft,
  deleteAgentProfileRoleBinding,
  decideAgentProfilePublishApproval,
  evaluateAgentProfileExperiment,
  getAgentProfileCatalogAccess,
  getAgentProfileRelease,
  listAgentProfileAuditEvents,
  listAgentProfileExperiments,
  listAgentProfilePublishApprovals,
  listAgentProfileRoleAuditEvents,
  listAgentProfileRoleBindings,
  listAgentProfileVersions,
  requestAgentProfilePublishApproval,
  retryAgentProfilePublishApproval,
  startAgentProfileExperiment,
  stopAgentProfileExperiment,
  upsertAgentProfileRoleBinding,
  upsertAgentProfileRelease,
  type AgentEvalEvidenceReferencePayload,
  type AgentProfileSpecPayload,
} from '../../api/agent'

const router = useRouter()
const activeTab = ref<'versions' | 'approvals' | 'release' | 'experiments' | 'audits' | 'members'>('versions')
const profileFilter = ref('')
const approvalStatus = ref('')
const roles = ref<string[]>([])
const accessEnabled = ref(false)
const directPublishEnabled = ref(false)
const experimentsEnabled = ref(false)
const dynamicRBACEnabled = ref(false)
const rootAdmin = ref(false)
const staticRoles = ref<string[]>([])
const dynamicRoles = ref<string[]>([])
const loading = ref(false)
const mutating = ref(false)
const errorMessage = ref('')
const successMessage = ref('')
const versions = ref<any[]>([])
const approvals = ref<any[]>([])
const audits = ref<any[]>([])
const versionTotal = ref(0)
const approvalTotal = ref(0)
const auditTotal = ref(0)
const experiments = ref<any[]>([])
const experimentTotal = ref(0)
const experimentStatus = ref('')
const release = ref<any | null>(null)
const roleBindings = ref<any[]>([])
const roleAudits = ref<any[]>([])
const roleBindingTotal = ref(0)
const roleAuditTotal = ref(0)
const showRoleForm = ref(false)
const showDraftForm = ref(false)
const publishTarget = ref<any | null>(null)
const publishEvidenceText = ref('')
const decisionTarget = ref<any | null>(null)
const decision = ref<'approved' | 'rejected'>('approved')
const decisionReason = ref('')
const roleForm = reactive({ user_id: '', roles: [] as string[], expected_revision: 0 })
const roleOptions = ['viewer', 'editor', 'approver', 'admin']

const draft = reactive<AgentProfileSpecPayload>({
  profile_id: '',
  version: 'v1',
  prompt_id: '',
  prompt_version: 'v1',
  system_prompt: '',
  max_steps: 8,
  max_input_tokens: 12000,
  max_output_tokens: 3000,
  max_total_tokens: 15000,
  max_estimated_cost_micros: 0,
  timeout_millis: 60000,
  allowed_tools: [],
})
const allowedToolsText = ref('')

const releaseForm = reactive({
  stable_version: '',
  candidate_version: '',
  candidate_basis_points: 0,
  salt: '',
  expected_revision: 0,
})

const experimentForm = reactive({
  min_samples_per_arm: 50,
  target_samples_per_arm: 200,
  max_error_rate_increase_basis_points: 500,
  max_p95_latency_increase_basis_points: 2000,
  max_average_cost_increase_basis_points: 2000,
  outcome_signal: '' as '' | 'response_accepted' | 'draft_published' | 'content_engaged',
  min_outcome_samples_per_arm: 50,
  max_outcome_rate_decrease_basis_points: 1000,
})

const hasRole = (role: string) => roles.value.includes('admin') || roles.value.includes(role)
const canEdit = computed(() => hasRole('editor'))
const canApprove = computed(() => hasRole('approver'))
const canAdmin = computed(() => roles.value.includes('admin'))

const tabs = [
  { id: 'versions', label: '版本' },
  { id: 'approvals', label: '发布审批' },
  { id: 'release', label: 'Release' },
  { id: 'experiments', label: '实验门禁' },
  { id: 'audits', label: '审计' },
  { id: 'members', label: '成员权限' },
] as const
const visibleTabs = computed(() => tabs.filter(tab => {
  if (tab.id === 'members') return canAdmin.value
  if (tab.id === 'experiments') return experimentsEnabled.value
  return true
}))

const extractError = (error: any) => String(error?.response?.data?.error || error?.message || '操作失败')
const formatTime = (value: number) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-'
const shortHash = (value: string) => value ? `${value.slice(0, 10)}...${value.slice(-6)}` : '-'
const formatBasisPoints = (value: number) => `${(Number(value || 0) / 100).toFixed(2)}%`

const statusClass = (status: string) => {
  if (['published', 'applied', 'passed'].includes(status)) return 'bg-emerald-500/15 text-emerald-300'
  if (['pending', 'applying', 'running', 'collecting', 'continue'].includes(status)) return 'bg-amber-500/15 text-amber-300'
  if (['rejected', 'apply_failed', 'rolled_back', 'rollback'].includes(status)) return 'bg-rose-500/15 text-rose-300'
  return 'bg-slate-700 text-slate-300'
}

const goBack = () => {
  if (window.history.length > 1) router.back()
  else router.push('/agent')
}

const loadAccess = async () => {
  const response = await getAgentProfileCatalogAccess()
  accessEnabled.value = Boolean(response.data.enabled)
  roles.value = Array.isArray(response.data.roles) ? response.data.roles : []
  staticRoles.value = Array.isArray(response.data.static_roles) ? response.data.static_roles : []
  dynamicRoles.value = Array.isArray(response.data.dynamic_roles) ? response.data.dynamic_roles : []
  rootAdmin.value = Boolean(response.data.root_admin)
  dynamicRBACEnabled.value = Boolean(response.data.dynamic_rbac_enabled)
  directPublishEnabled.value = Boolean(response.data.direct_publish_enabled)
  experimentsEnabled.value = Boolean(response.data.experiments_enabled)
}

const loadRoleManagement = async () => {
  if (!canAdmin.value || !dynamicRBACEnabled.value) {
    roleBindings.value = []
    roleAudits.value = []
    roleBindingTotal.value = 0
    roleAuditTotal.value = 0
    return
  }
  const [bindingsResponse, auditsResponse] = await Promise.all([
    listAgentProfileRoleBindings({ page: 1, page_size: 100 }),
    listAgentProfileRoleAuditEvents({ page: 1, page_size: 50 }),
  ])
  roleBindings.value = bindingsResponse.data.role_bindings || []
  roleAudits.value = auditsResponse.data.audit_events || []
  roleBindingTotal.value = Number(bindingsResponse.data.total || 0)
  roleAuditTotal.value = Number(auditsResponse.data.total || 0)
}

const loadVersions = async () => {
  const response = await listAgentProfileVersions({ profile_id: profileFilter.value.trim() || undefined, page: 1, page_size: 50 })
  versions.value = response.data.profile_versions || []
  versionTotal.value = Number(response.data.total || 0)
}

const loadApprovals = async () => {
  const response = await listAgentProfilePublishApprovals({
    profile_id: profileFilter.value.trim() || undefined,
    status: approvalStatus.value || undefined,
    page: 1,
    page_size: 50,
  })
  approvals.value = response.data.approvals || []
  approvalTotal.value = Number(response.data.total || 0)
}

const loadAudits = async () => {
  const response = await listAgentProfileAuditEvents({ profile_id: profileFilter.value.trim() || undefined, page: 1, page_size: 50 })
  audits.value = response.data.audit_events || []
  auditTotal.value = Number(response.data.total || 0)
}

const loadExperiments = async () => {
  if (!experimentsEnabled.value) {
    experiments.value = []
    experimentTotal.value = 0
    return
  }
  const response = await listAgentProfileExperiments({
    profile_id: profileFilter.value.trim() || undefined,
    status: experimentStatus.value || undefined,
    page: 1,
    page_size: 50,
  })
  experiments.value = response.data.experiments || []
  experimentTotal.value = Number(response.data.total || 0)
}

const loadRelease = async () => {
  release.value = null
  const profileID = profileFilter.value.trim()
  if (!profileID) return
  try {
    const response = await getAgentProfileRelease(profileID)
    release.value = response.data.profile_release
    Object.assign(releaseForm, {
      stable_version: release.value?.stable_version || '',
      candidate_version: release.value?.candidate_version || '',
      candidate_basis_points: Number(release.value?.candidate_basis_points || 0),
      salt: release.value?.salt || '',
      expected_revision: Number(release.value?.revision || 0),
    })
  } catch (error: any) {
    if (error?.response?.status !== 404) throw error
    Object.assign(releaseForm, {
      stable_version: '', candidate_version: '', candidate_basis_points: 0,
      salt: '', expected_revision: 0,
    })
  }
}

const refresh = async () => {
  loading.value = true
  errorMessage.value = ''
  try {
    await Promise.all([loadVersions(), loadApprovals(), loadAudits(), loadExperiments(), loadRelease(), loadRoleManagement()])
  } catch (error) {
    errorMessage.value = extractError(error)
  } finally {
    loading.value = false
  }
}

const resetRoleForm = () => {
  Object.assign(roleForm, { user_id: '', roles: [], expected_revision: 0 })
  showRoleForm.value = false
}

const editRoleBinding = (item: any) => {
  Object.assign(roleForm, {
    user_id: String(item.user_id || ''),
    roles: Array.isArray(item.roles) ? [...item.roles] : [],
    expected_revision: Number(item.revision || 0),
  })
  showRoleForm.value = true
}

const saveRoleBinding = async () => {
  if (!/^\d+$/.test(roleForm.user_id) || roleForm.user_id === '0' || !roleForm.roles.length) return
  mutating.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    await upsertAgentProfileRoleBinding(roleForm.user_id, {
      roles: [...roleForm.roles], expected_revision: Number(roleForm.expected_revision),
    })
    successMessage.value = `用户 ${roleForm.user_id} 的动态角色已保存`
    resetRoleForm()
    await Promise.all([loadAccess(), loadRoleManagement()])
  } catch (error) {
    errorMessage.value = extractError(error)
  } finally {
    mutating.value = false
  }
}

const removeRoleBinding = async (item: any) => {
  if (!window.confirm(`删除用户 ${item.user_id} 的全部动态角色？`)) return
  mutating.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    await deleteAgentProfileRoleBinding(String(item.user_id), Number(item.revision))
    successMessage.value = `用户 ${item.user_id} 的动态角色已删除`
    await Promise.all([loadAccess(), loadRoleManagement()])
  } catch (error) {
    errorMessage.value = extractError(error)
  } finally {
    mutating.value = false
  }
}

const submitDraft = async () => {
  mutating.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    const payload = {
      ...draft,
      prompt_id: draft.prompt_id.trim() || `${draft.profile_id.trim()}.system`,
      allowed_tools: allowedToolsText.value.split(',').map(item => item.trim()).filter(Boolean),
    }
    await createAgentProfileDraft(payload)
    successMessage.value = `草稿 ${payload.profile_id}@${payload.version} 已创建`
    showDraftForm.value = false
    profileFilter.value = payload.profile_id
    await refresh()
  } catch (error) {
    errorMessage.value = extractError(error)
  } finally {
    mutating.value = false
  }
}

const openPublish = (item: any) => {
  publishTarget.value = item
  publishEvidenceText.value = ''
}

const closePublish = () => {
  publishTarget.value = null
  publishEvidenceText.value = ''
}

const parseEvidenceTime = (value: unknown, field: string) => {
  if (typeof value === 'number' && Number.isFinite(value) && value > 0) return value
  const parsed = Date.parse(String(value || ''))
  if (!Number.isFinite(parsed) || parsed <= 0) throw new Error(`${field} 不是有效时间`)
  return parsed
}

const parseQualityEvidence = (raw: string): AgentEvalEvidenceReferencePayload | undefined => {
  const value = raw.trim()
  if (!value) return undefined
  const receipt = JSON.parse(value)
  return {
    storage: String(receipt.storage || ''),
    bucket: String(receipt.bucket || ''),
    key: String(receipt.key || ''),
    version_id: String(receipt.version_id || ''),
    etag: String(receipt.etag || ''),
    report_sha256: String(receipt.report_sha256 || ''),
    length: Number(receipt.length || 0),
    content_type: String(receipt.content_type || ''),
    retention_mode: String(receipt.retention_mode || ''),
    retain_until: parseEvidenceTime(receipt.retain_until, 'retain_until'),
    archived_at: parseEvidenceTime(receipt.archived_at, 'archived_at'),
    dataset_version: String(receipt.dataset_version || ''),
    dataset_sha256: String(receipt.dataset_sha256 || ''),
    execution_config_sha256: String(receipt.execution_config_sha256 || ''),
    integrity_key_id: String(receipt.integrity_key_id || ''),
  }
}

const requestPublish = async () => {
  if (!publishTarget.value) return
  mutating.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    const item = publishTarget.value
    const qualityEvidence = parseQualityEvidence(publishEvidenceText.value)
    await requestAgentProfilePublishApproval(item.spec.profile_id, item.spec.version, Number(item.revision), qualityEvidence)
    successMessage.value = `已提交 ${item.spec.profile_id}@${item.spec.version} 的发布审批`
    closePublish()
    activeTab.value = 'approvals'
    approvalStatus.value = 'pending'
    await Promise.all([loadApprovals(), loadAudits()])
  } catch (error) {
    errorMessage.value = extractError(error)
  } finally {
    mutating.value = false
  }
}

const openDecision = (item: any, nextDecision: 'approved' | 'rejected') => {
  decisionTarget.value = item
  decision.value = nextDecision
  decisionReason.value = ''
}

const submitDecision = async () => {
  if (!decisionTarget.value) return
  mutating.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    await decideAgentProfilePublishApproval(decisionTarget.value.approval_id, {
      decision: decision.value,
      reason: decisionReason.value.trim(),
      expected_revision: Number(decisionTarget.value.revision),
    })
    successMessage.value = decision.value === 'approved' ? '审批通过，版本已进入发布流程' : '发布申请已拒绝'
    decisionTarget.value = null
    await refresh()
  } catch (error) {
    errorMessage.value = extractError(error)
  } finally {
    mutating.value = false
  }
}

const retryApproval = async (item: any) => {
  mutating.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    await retryAgentProfilePublishApproval(item.approval_id, Number(item.revision))
    successMessage.value = '发布执行已恢复'
    await refresh()
  } catch (error) {
    errorMessage.value = extractError(error)
  } finally {
    mutating.value = false
  }
}

const saveRelease = async () => {
  const profileID = profileFilter.value.trim()
  if (!profileID) return
  mutating.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    await upsertAgentProfileRelease(profileID, { ...releaseForm })
    successMessage.value = `Release ${profileID} 已更新`
    await Promise.all([loadRelease(), loadAudits()])
  } catch (error) {
    errorMessage.value = extractError(error)
  } finally {
    mutating.value = false
  }
}

const startExperiment = async () => {
  const profileID = profileFilter.value.trim()
  if (!profileID || !release.value || Number(release.value.candidate_basis_points || 0) <= 0) return
  mutating.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    await startAgentProfileExperiment({
      profile_id: profileID,
      expected_release_revision: Number(release.value.revision),
      policy: {
        ...experimentForm,
        min_outcome_samples_per_arm: experimentForm.outcome_signal ? experimentForm.min_outcome_samples_per_arm : 0,
        max_outcome_rate_decrease_basis_points: experimentForm.outcome_signal ? experimentForm.max_outcome_rate_decrease_basis_points : 0,
      },
    })
    successMessage.value = `实验 ${profileID} 已启动`
    await Promise.all([loadExperiments(), loadAudits()])
  } catch (error) {
    errorMessage.value = extractError(error)
  } finally {
    mutating.value = false
  }
}

const evaluateExperiment = async (item: any) => {
  mutating.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    await evaluateAgentProfileExperiment(item.experiment_id, Number(item.revision))
    successMessage.value = '实验统计已重新评估'
    await Promise.all([loadExperiments(), loadRelease(), loadAudits()])
  } catch (error) {
    errorMessage.value = extractError(error)
  } finally {
    mutating.value = false
  }
}

const stopExperiment = async (item: any) => {
  if (!window.confirm('停止后不会自动恢复，确认停止该实验？')) return
  mutating.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    await stopAgentProfileExperiment(item.experiment_id, Number(item.revision))
    successMessage.value = '实验已停止'
    await Promise.all([loadExperiments(), loadAudits()])
  } catch (error) {
    errorMessage.value = extractError(error)
  } finally {
    mutating.value = false
  }
}

onMounted(async () => {
  loading.value = true
  try {
    await loadAccess()
    if (accessEnabled.value) await refresh()
  } catch (error) {
    errorMessage.value = extractError(error)
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <main class="min-h-screen bg-slate-950 text-slate-100">
    <header class="sticky top-0 z-20 border-b border-white/10 bg-slate-950/95 backdrop-blur">
      <div class="flex min-h-16 items-center justify-between gap-4 px-4 sm:px-6">
        <div class="flex min-w-0 items-center gap-3">
          <button @click="goBack" title="返回" class="flex h-9 w-9 shrink-0 items-center justify-center rounded-md text-slate-400 hover:bg-slate-800 hover:text-white">
            <ArrowLeftIcon class="h-5 w-5" />
          </button>
          <div class="min-w-0">
            <h1 class="truncate text-base font-semibold">Agent Profile 管理</h1>
            <p class="truncate text-xs text-slate-500" :title="`静态：${staticRoles.join(', ') || '-'}；动态：${dynamicRoles.join(', ') || '-'}`">{{ roles.length ? roles.join(' / ') : '无管理角色' }}</p>
          </div>
        </div>
        <div class="flex items-center gap-2">
          <button @click="refresh" :disabled="loading || !accessEnabled" title="刷新" class="flex h-9 w-9 items-center justify-center rounded-md border border-white/10 text-slate-300 hover:bg-slate-800 disabled:opacity-40">
            <ArrowPathIcon class="h-5 w-5" :class="loading ? 'animate-spin' : ''" />
          </button>
          <button v-if="canEdit" @click="showDraftForm = !showDraftForm" class="inline-flex h-9 items-center gap-2 rounded-md bg-indigo-500 px-3 text-sm font-semibold text-white hover:bg-indigo-400">
            <PlusIcon class="h-4 w-4" />
            新建草稿
          </button>
        </div>
      </div>
      <div class="flex flex-wrap items-center gap-3 border-t border-white/5 px-4 py-3 sm:px-6">
        <input v-model="profileFilter" @keyup.enter="refresh" placeholder="筛选 Profile ID" class="h-9 w-full max-w-sm rounded-md border border-white/10 bg-slate-900 px-3 text-sm outline-none focus:border-indigo-400" />
        <button @click="refresh" class="h-9 rounded-md border border-white/10 px-3 text-sm text-slate-300 hover:bg-slate-800">查询</button>
        <span v-if="directPublishEnabled" class="text-xs text-amber-300">Break-glass 直发已启用</span>
      </div>
      <nav class="flex overflow-x-auto px-4 sm:px-6">
        <button v-for="tab in visibleTabs" :key="tab.id" @click="activeTab = tab.id" class="h-11 shrink-0 border-b-2 px-4 text-sm font-medium" :class="activeTab === tab.id ? 'border-indigo-400 text-white' : 'border-transparent text-slate-500 hover:text-slate-300'">
          {{ tab.label }}
        </button>
      </nav>
    </header>

    <div v-if="errorMessage" class="border-b border-rose-500/20 bg-rose-500/10 px-4 py-3 text-sm text-rose-300 sm:px-6">{{ errorMessage }}</div>
    <div v-if="successMessage" class="border-b border-emerald-500/20 bg-emerald-500/10 px-4 py-3 text-sm text-emerald-300 sm:px-6">{{ successMessage }}</div>

    <section v-if="!loading && !accessEnabled" class="flex min-h-[55vh] flex-col items-center justify-center gap-3 px-6 text-center">
      <ShieldCheckIcon class="h-10 w-10 text-slate-600" />
      <h2 class="text-base font-semibold">没有 Agent Profile 管理权限</h2>
    </section>

    <template v-else>
      <section v-if="showDraftForm && canEdit" class="border-b border-white/10 bg-slate-900/50 px-4 py-5 sm:px-6">
        <form @submit.prevent="submitDraft" class="grid max-w-6xl grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-4">
          <label class="text-xs text-slate-400">Profile ID<input v-model="draft.profile_id" required class="mt-1 h-9 w-full rounded-md border border-white/10 bg-slate-950 px-3 text-sm text-white" /></label>
          <label class="text-xs text-slate-400">版本<input v-model="draft.version" required class="mt-1 h-9 w-full rounded-md border border-white/10 bg-slate-950 px-3 text-sm text-white" /></label>
          <label class="text-xs text-slate-400">Prompt ID<input v-model="draft.prompt_id" :placeholder="`${draft.profile_id || 'profile'}.system`" class="mt-1 h-9 w-full rounded-md border border-white/10 bg-slate-950 px-3 text-sm text-white" /></label>
          <label class="text-xs text-slate-400">Prompt 版本<input v-model="draft.prompt_version" required class="mt-1 h-9 w-full rounded-md border border-white/10 bg-slate-950 px-3 text-sm text-white" /></label>
          <label class="text-xs text-slate-400 md:col-span-2 lg:col-span-4">System Prompt<textarea v-model="draft.system_prompt" required rows="5" class="mt-1 w-full rounded-md border border-white/10 bg-slate-950 px-3 py-2 text-sm leading-6 text-white" /></label>
          <label class="text-xs text-slate-400">最大步骤<input v-model.number="draft.max_steps" type="number" min="1" required class="mt-1 h-9 w-full rounded-md border border-white/10 bg-slate-950 px-3 text-sm" /></label>
          <label class="text-xs text-slate-400">输入 Token<input v-model.number="draft.max_input_tokens" type="number" min="1" required class="mt-1 h-9 w-full rounded-md border border-white/10 bg-slate-950 px-3 text-sm" /></label>
          <label class="text-xs text-slate-400">输出 Token<input v-model.number="draft.max_output_tokens" type="number" min="1" required class="mt-1 h-9 w-full rounded-md border border-white/10 bg-slate-950 px-3 text-sm" /></label>
          <label class="text-xs text-slate-400">总 Token<input v-model.number="draft.max_total_tokens" type="number" min="1" required class="mt-1 h-9 w-full rounded-md border border-white/10 bg-slate-950 px-3 text-sm" /></label>
          <label class="text-xs text-slate-400">成本上限（微单位）<input v-model.number="draft.max_estimated_cost_micros" type="number" min="0" class="mt-1 h-9 w-full rounded-md border border-white/10 bg-slate-950 px-3 text-sm" /></label>
          <label class="text-xs text-slate-400">超时（毫秒）<input v-model.number="draft.timeout_millis" type="number" min="1" required class="mt-1 h-9 w-full rounded-md border border-white/10 bg-slate-950 px-3 text-sm" /></label>
          <label class="text-xs text-slate-400 md:col-span-2">允许工具（逗号分隔）<input v-model="allowedToolsText" class="mt-1 h-9 w-full rounded-md border border-white/10 bg-slate-950 px-3 text-sm" /></label>
          <div class="flex items-end gap-2 md:col-span-2 lg:col-span-4">
            <button type="submit" :disabled="mutating" class="h-9 rounded-md bg-indigo-500 px-4 text-sm font-semibold hover:bg-indigo-400 disabled:opacity-50">保存草稿</button>
            <button type="button" @click="showDraftForm = false" class="h-9 rounded-md border border-white/10 px-4 text-sm text-slate-300 hover:bg-slate-800">取消</button>
          </div>
        </form>
      </section>

      <section v-if="activeTab === 'versions'" class="overflow-x-auto">
        <div class="flex items-center justify-between border-b border-white/10 px-4 py-3 text-xs text-slate-500 sm:px-6"><span>共 {{ versionTotal }} 个版本</span></div>
        <table class="w-full min-w-[920px] text-left text-sm">
          <thead class="border-b border-white/10 bg-slate-900/60 text-xs text-slate-500"><tr><th class="px-6 py-3">Profile</th><th class="px-4 py-3">状态</th><th class="px-4 py-3">预算</th><th class="px-4 py-3">工具</th><th class="px-4 py-3">Revision</th><th class="px-4 py-3">创建时间</th><th class="px-6 py-3 text-right">操作</th></tr></thead>
          <tbody class="divide-y divide-white/10">
            <tr v-for="item in versions" :key="item.id" class="hover:bg-white/[0.025]">
              <td class="px-6 py-4"><p class="font-semibold">{{ item.spec.profile_id }}@{{ item.spec.version }}</p><p class="mt-1 font-mono text-[10px] text-slate-500" :title="item.snapshot_hash">{{ shortHash(item.snapshot_hash) }}</p></td>
              <td class="px-4 py-4"><span class="rounded px-2 py-1 text-xs font-medium" :class="statusClass(item.status)">{{ item.status }}</span></td>
              <td class="px-4 py-4 text-xs text-slate-400">{{ item.spec.max_steps }} 步 · {{ item.spec.max_total_tokens }} Token</td>
              <td class="max-w-56 px-4 py-4 text-xs text-slate-400"><span class="line-clamp-2">{{ item.spec.allowed_tools?.join(', ') || '-' }}</span></td>
              <td class="px-4 py-4 font-mono text-xs">{{ item.revision }}</td>
              <td class="px-4 py-4 text-xs text-slate-400">{{ formatTime(item.created_at) }}</td>
              <td class="px-6 py-4 text-right"><button v-if="canEdit && item.status === 'draft'" @click="openPublish(item)" :disabled="mutating" class="inline-flex h-8 items-center gap-1 rounded-md border border-indigo-400/40 px-3 text-xs text-indigo-300 hover:bg-indigo-500/10 disabled:opacity-40"><PaperAirplaneIcon class="h-4 w-4" />提交审批</button></td>
            </tr>
            <tr v-if="!versions.length"><td colspan="7" class="px-6 py-12 text-center text-sm text-slate-500">暂无版本</td></tr>
          </tbody>
        </table>
      </section>

      <section v-if="activeTab === 'approvals'" class="overflow-x-auto">
        <div class="flex flex-wrap items-center justify-between gap-3 border-b border-white/10 px-4 py-3 sm:px-6"><span class="text-xs text-slate-500">共 {{ approvalTotal }} 条申请</span><select v-model="approvalStatus" @change="loadApprovals" class="h-8 rounded-md border border-white/10 bg-slate-900 px-2 text-xs"><option value="">全部状态</option><option value="pending">pending</option><option value="applying">applying</option><option value="applied">applied</option><option value="rejected">rejected</option><option value="apply_failed">apply_failed</option></select></div>
        <table class="w-full min-w-[1050px] text-left text-sm">
          <thead class="border-b border-white/10 bg-slate-900/60 text-xs text-slate-500"><tr><th class="px-6 py-3">目标版本</th><th class="px-4 py-3">状态</th><th class="px-4 py-3">申请人</th><th class="px-4 py-3">审批人</th><th class="px-4 py-3">Revision</th><th class="px-4 py-3">时间</th><th class="px-6 py-3 text-right">操作</th></tr></thead>
          <tbody class="divide-y divide-white/10">
            <tr v-for="item in approvals" :key="item.approval_id" class="hover:bg-white/[0.025]">
              <td class="px-6 py-4"><p class="font-semibold">{{ item.profile_id }}@{{ item.version }}</p><p class="mt-1 font-mono text-[10px] text-slate-500">{{ shortHash(item.snapshot_hash) }}</p><p v-if="item.quality_evidence" class="mt-2 text-xs text-emerald-300">Eval {{ item.quality_evidence.gate_status }} · {{ item.quality_evidence.passed }}/{{ item.quality_evidence.cases }}</p><p v-if="item.quality_evidence" class="mt-1 font-mono text-[10px] text-slate-500" :title="item.quality_evidence.reference?.report_sha256">{{ item.quality_evidence.reference?.dataset_version }} · {{ shortHash(item.quality_evidence.reference?.report_sha256) }}</p><p v-if="item.error_code" class="mt-1 text-xs text-rose-300">{{ item.error_code }}</p></td>
              <td class="px-4 py-4"><span class="rounded px-2 py-1 text-xs font-medium" :class="statusClass(item.status)">{{ item.status }}</span></td>
              <td class="px-4 py-4 font-mono text-xs">{{ item.requested_by }}</td>
              <td class="px-4 py-4 font-mono text-xs">{{ item.decided_by === '0' ? '-' : item.decided_by }}</td>
              <td class="px-4 py-4 font-mono text-xs">{{ item.revision }}</td>
              <td class="px-4 py-4 text-xs text-slate-400">{{ formatTime(item.requested_at) }}</td>
              <td class="px-6 py-4"><div v-if="canApprove" class="flex justify-end gap-2"><button v-if="item.status === 'pending'" @click="openDecision(item, 'approved')" title="批准" class="flex h-8 w-8 items-center justify-center rounded-md border border-emerald-400/40 text-emerald-300 hover:bg-emerald-500/10"><CheckIcon class="h-4 w-4" /></button><button v-if="item.status === 'pending'" @click="openDecision(item, 'rejected')" title="拒绝" class="flex h-8 w-8 items-center justify-center rounded-md border border-rose-400/40 text-rose-300 hover:bg-rose-500/10"><XMarkIcon class="h-4 w-4" /></button><button v-if="item.status === 'apply_failed' || (item.status === 'applying' && item.apply_lease_until < Date.now())" @click="retryApproval(item)" title="恢复发布" class="flex h-8 w-8 items-center justify-center rounded-md border border-amber-400/40 text-amber-300 hover:bg-amber-500/10"><ArrowPathIcon class="h-4 w-4" /></button></div></td>
            </tr>
            <tr v-if="!approvals.length"><td colspan="7" class="px-6 py-12 text-center text-sm text-slate-500">暂无发布申请</td></tr>
          </tbody>
        </table>
      </section>

      <section v-if="activeTab === 'release'" class="px-4 py-6 sm:px-6">
        <div v-if="!profileFilter.trim()" class="py-16 text-center text-sm text-slate-500">输入 Profile ID 后查询 Release</div>
        <form v-else @submit.prevent="saveRelease" class="grid max-w-4xl grid-cols-1 gap-4 md:grid-cols-2">
          <label class="text-xs text-slate-400">稳定版本<input v-model="releaseForm.stable_version" :disabled="!canAdmin" required class="mt-1 h-9 w-full rounded-md border border-white/10 bg-slate-900 px-3 text-sm disabled:opacity-60" /></label>
          <label class="text-xs text-slate-400">候选版本<input v-model="releaseForm.candidate_version" :disabled="!canAdmin" required class="mt-1 h-9 w-full rounded-md border border-white/10 bg-slate-900 px-3 text-sm disabled:opacity-60" /></label>
          <label class="text-xs text-slate-400">候选流量（基点）<input v-model.number="releaseForm.candidate_basis_points" :disabled="!canAdmin" type="number" min="0" max="10000" class="mt-1 h-9 w-full rounded-md border border-white/10 bg-slate-900 px-3 text-sm disabled:opacity-60" /></label>
          <label class="text-xs text-slate-400">分流 Salt<input v-model="releaseForm.salt" :disabled="!canAdmin" class="mt-1 h-9 w-full rounded-md border border-white/10 bg-slate-900 px-3 text-sm disabled:opacity-60" /></label>
          <div class="md:col-span-2"><p class="mb-3 text-xs text-slate-500">Revision {{ releaseForm.expected_revision }}</p><button v-if="canAdmin" type="submit" :disabled="mutating" class="h-9 rounded-md bg-indigo-500 px-4 text-sm font-semibold hover:bg-indigo-400 disabled:opacity-50">保存 Release</button></div>
        </form>
      </section>

      <section v-if="activeTab === 'experiments' && experimentsEnabled">
        <form v-if="canAdmin" @submit.prevent="startExperiment" class="grid grid-cols-1 gap-4 border-b border-white/10 bg-slate-900/40 px-4 py-5 sm:px-6 md:grid-cols-3 xl:grid-cols-8">
          <div class="md:col-span-3 xl:col-span-8">
            <p class="text-sm font-semibold">启动运行时安全实验</p>
            <p class="mt-1 text-xs text-slate-500">{{ profileFilter.trim() || '请先选择 Profile' }} · Release revision {{ release?.revision || '-' }}</p>
          </div>
          <label class="text-xs text-slate-400">最小样本/组<input v-model.number="experimentForm.min_samples_per_arm" type="number" min="1" max="5000" required class="mt-1 h-9 w-full rounded-md border border-white/10 bg-slate-950 px-3 text-sm" /></label>
          <label class="text-xs text-slate-400">目标样本/组<input v-model.number="experimentForm.target_samples_per_arm" type="number" min="1" max="5000" required class="mt-1 h-9 w-full rounded-md border border-white/10 bg-slate-950 px-3 text-sm" /></label>
          <label class="text-xs text-slate-400">错误率增量上限 (bps)<input v-model.number="experimentForm.max_error_rate_increase_basis_points" type="number" min="0" max="10000" required class="mt-1 h-9 w-full rounded-md border border-white/10 bg-slate-950 px-3 text-sm" /></label>
          <label class="text-xs text-slate-400">P95 延迟增幅上限 (bps)<input v-model.number="experimentForm.max_p95_latency_increase_basis_points" type="number" min="0" max="10000" required class="mt-1 h-9 w-full rounded-md border border-white/10 bg-slate-950 px-3 text-sm" /></label>
          <label class="text-xs text-slate-400">平均成本增幅上限 (bps)<input v-model.number="experimentForm.max_average_cost_increase_basis_points" type="number" min="0" max="10000" required class="mt-1 h-9 w-full rounded-md border border-white/10 bg-slate-950 px-3 text-sm" /></label>
          <label class="text-xs text-slate-400">业务结果<select v-model="experimentForm.outcome_signal" class="mt-1 h-9 w-full rounded-md border border-white/10 bg-slate-950 px-3 text-sm"><option value="">不启用</option><option value="response_accepted">回答被采纳</option><option value="draft_published">草稿被发布</option><option value="content_engaged">内容获互动</option></select></label>
          <label class="text-xs text-slate-400">结果最小样本/组<input v-model.number="experimentForm.min_outcome_samples_per_arm" :disabled="!experimentForm.outcome_signal" type="number" min="1" :max="experimentForm.target_samples_per_arm" class="mt-1 h-9 w-full rounded-md border border-white/10 bg-slate-950 px-3 text-sm disabled:opacity-40" /></label>
          <label class="text-xs text-slate-400">结果率下降上限 (bps)<input v-model.number="experimentForm.max_outcome_rate_decrease_basis_points" :disabled="!experimentForm.outcome_signal" type="number" min="0" max="10000" class="mt-1 h-9 w-full rounded-md border border-white/10 bg-slate-950 px-3 text-sm disabled:opacity-40" /></label>
          <div class="flex items-end"><button type="submit" :disabled="mutating || !profileFilter.trim() || !release || Number(release.candidate_basis_points || 0) <= 0" class="h-9 w-full rounded-md bg-indigo-500 px-4 text-sm font-semibold hover:bg-indigo-400 disabled:opacity-40">启动实验</button></div>
        </form>

        <div class="flex flex-wrap items-center justify-between gap-3 border-b border-white/10 px-4 py-3 sm:px-6">
          <span class="text-xs text-slate-500">共 {{ experimentTotal }} 个实验</span>
          <select v-model="experimentStatus" @change="loadExperiments" class="h-8 rounded-md border border-white/10 bg-slate-900 px-2 text-xs">
            <option value="">全部状态</option><option value="running">running</option><option value="passed">passed</option><option value="rolled_back">rolled_back</option><option value="stopped">stopped</option><option value="superseded">superseded</option>
          </select>
        </div>
        <div class="overflow-x-auto">
          <table class="w-full min-w-[1380px] text-left text-sm">
            <thead class="border-b border-white/10 bg-slate-900/60 text-xs text-slate-500"><tr><th class="px-6 py-3">实验</th><th class="px-4 py-3">状态</th><th class="px-4 py-3">稳定组</th><th class="px-4 py-3">候选组</th><th class="px-4 py-3">错误率</th><th class="px-4 py-3">P95 延迟</th><th class="px-4 py-3">平均成本</th><th class="px-4 py-3">业务结果</th><th class="px-4 py-3">更新时间</th><th class="px-6 py-3 text-right">操作</th></tr></thead>
            <tbody class="divide-y divide-white/10">
              <tr v-for="item in experiments" :key="item.experiment_id" class="hover:bg-white/[0.025]">
                <td class="px-6 py-4"><p class="font-semibold">{{ item.profile_id }}</p><p class="mt-1 text-xs text-slate-400">{{ item.stable_version }} → {{ item.candidate_version }} · {{ formatBasisPoints(item.candidate_basis_points) }}</p><p class="mt-1 font-mono text-[10px] text-slate-600">{{ item.experiment_id }}</p></td>
                <td class="px-4 py-4"><span class="rounded px-2 py-1 text-xs font-medium" :class="statusClass(item.status)">{{ item.status }}</span><p class="mt-2 text-xs text-slate-400">{{ item.decision }}</p><p class="mt-1 text-[11px] text-slate-500">{{ item.decision_reason }}</p></td>
                <td class="px-4 py-4 font-mono text-xs">{{ item.stats?.stable?.samples || 0 }} / {{ item.policy?.target_samples_per_arm || 0 }}</td>
                <td class="px-4 py-4 font-mono text-xs">{{ item.stats?.candidate?.samples || 0 }} / {{ item.policy?.target_samples_per_arm || 0 }}</td>
                <td class="px-4 py-4 text-xs"><p>S {{ formatBasisPoints(item.stats?.stable?.error_rate_basis_points) }}</p><p class="mt-1">C {{ formatBasisPoints(item.stats?.candidate?.error_rate_basis_points) }}</p></td>
                <td class="px-4 py-4 text-xs"><p>S {{ item.stats?.stable?.p95_latency_millis || 0 }} ms</p><p class="mt-1">C {{ item.stats?.candidate?.p95_latency_millis || 0 }} ms</p></td>
                <td class="px-4 py-4 text-xs"><p>S {{ item.stats?.stable?.average_cost_micros || 0 }}</p><p class="mt-1">C {{ item.stats?.candidate?.average_cost_micros || 0 }}</p></td>
                <td class="px-4 py-4 text-xs"><template v-if="item.policy?.outcome_signal"><p class="text-slate-400">{{ item.policy.outcome_signal }}</p><p class="mt-1">S {{ formatBasisPoints(item.stats?.stable?.outcome_rate_basis_points) }} · {{ item.stats?.stable?.outcome_samples || 0 }}</p><p class="mt-1">C {{ formatBasisPoints(item.stats?.candidate?.outcome_rate_basis_points) }} · {{ item.stats?.candidate?.outcome_samples || 0 }}</p></template><span v-else class="text-slate-600">-</span></td>
                <td class="px-4 py-4 text-xs text-slate-400">{{ formatTime(item.updated_at) }}</td>
                <td class="px-6 py-4"><div v-if="canAdmin && item.status === 'running'" class="flex justify-end gap-2"><button @click="evaluateExperiment(item)" :disabled="mutating" title="立即评估" class="flex h-8 w-8 items-center justify-center rounded-md border border-indigo-400/40 text-indigo-300 hover:bg-indigo-500/10 disabled:opacity-40"><ArrowPathIcon class="h-4 w-4" /></button><button @click="stopExperiment(item)" :disabled="mutating" title="停止实验" class="flex h-8 w-8 items-center justify-center rounded-md border border-rose-400/30 text-rose-300 hover:bg-rose-500/10 disabled:opacity-40"><XMarkIcon class="h-4 w-4" /></button></div></td>
              </tr>
              <tr v-if="!experiments.length"><td colspan="10" class="px-6 py-12 text-center text-sm text-slate-500">暂无实验记录</td></tr>
            </tbody>
          </table>
        </div>
      </section>

      <section v-if="activeTab === 'audits'" class="overflow-x-auto">
        <div class="border-b border-white/10 px-4 py-3 text-xs text-slate-500 sm:px-6">共 {{ auditTotal }} 条事件</div>
        <table class="w-full min-w-[980px] text-left text-sm"><thead class="border-b border-white/10 bg-slate-900/60 text-xs text-slate-500"><tr><th class="px-6 py-3">动作</th><th class="px-4 py-3">目标</th><th class="px-4 py-3">结果</th><th class="px-4 py-3">操作者</th><th class="px-4 py-3">Approval</th><th class="px-4 py-3">时间</th></tr></thead><tbody class="divide-y divide-white/10"><tr v-for="item in audits" :key="item.id" class="hover:bg-white/[0.025]"><td class="px-6 py-4 font-medium">{{ item.action }}</td><td class="px-4 py-4"><p>{{ item.profile_id }}<span v-if="item.version">@{{ item.version }}</span></p><p v-if="item.error_code" class="mt-1 text-xs text-rose-300">{{ item.error_code }}</p></td><td class="px-4 py-4"><span class="rounded px-2 py-1 text-xs" :class="statusClass(item.outcome)">{{ item.outcome }}</span></td><td class="px-4 py-4 font-mono text-xs">{{ item.actor_user_id }}</td><td class="max-w-48 truncate px-4 py-4 font-mono text-xs text-slate-400" :title="item.approval_id">{{ item.approval_id || '-' }}</td><td class="px-4 py-4 text-xs text-slate-400">{{ formatTime(item.created_at) }}</td></tr><tr v-if="!audits.length"><td colspan="6" class="px-6 py-12 text-center text-sm text-slate-500">暂无审计事件</td></tr></tbody></table>
      </section>

      <section v-if="activeTab === 'members' && canAdmin">
        <div class="flex flex-wrap items-center justify-between gap-3 border-b border-white/10 px-4 py-3 sm:px-6">
          <div>
            <p class="text-sm font-medium">动态成员角色</p>
            <p class="mt-1 text-xs text-slate-500">环境变量角色始终保留；只有根管理员能变更 admin。</p>
          </div>
          <button v-if="dynamicRBACEnabled" @click="showRoleForm ? resetRoleForm() : showRoleForm = true" class="inline-flex h-9 items-center gap-2 rounded-md bg-indigo-500 px-3 text-sm font-semibold hover:bg-indigo-400">
            <PlusIcon class="h-4 w-4" />新增成员
          </button>
        </div>

        <div v-if="!dynamicRBACEnabled" class="flex min-h-64 flex-col items-center justify-center gap-3 px-6 text-center">
          <UserGroupIcon class="h-10 w-10 text-slate-600" />
          <p class="text-sm font-medium">动态 RBAC 已关闭</p>
          <p class="text-xs text-slate-500">当前仅使用环境变量角色。</p>
        </div>

        <form v-if="showRoleForm && dynamicRBACEnabled" @submit.prevent="saveRoleBinding" class="flex flex-wrap items-end gap-4 border-b border-white/10 bg-slate-900/50 px-4 py-4 sm:px-6">
          <label class="w-full max-w-xs text-xs text-slate-400">用户 ID
            <input v-model.trim="roleForm.user_id" :disabled="roleForm.expected_revision > 0" inputmode="numeric" pattern="[0-9]+" required class="mt-1 h-9 w-full rounded-md border border-white/10 bg-slate-950 px-3 text-sm disabled:opacity-60" />
          </label>
          <fieldset class="min-w-0 flex-1">
            <legend class="mb-2 text-xs text-slate-400">角色</legend>
            <div class="flex flex-wrap gap-4">
              <label v-for="role in roleOptions" :key="role" class="inline-flex items-center gap-2 text-sm" :class="role === 'admin' && !rootAdmin ? 'text-slate-600' : 'text-slate-300'">
                <input v-model="roleForm.roles" type="checkbox" :value="role" :disabled="role === 'admin' && !rootAdmin" class="h-4 w-4 rounded border-white/20 bg-slate-950 text-indigo-500" />{{ role }}
              </label>
            </div>
          </fieldset>
          <div class="flex gap-2">
            <button type="submit" :disabled="mutating || !roleForm.roles.length" class="h-9 rounded-md bg-indigo-500 px-4 text-sm font-semibold hover:bg-indigo-400 disabled:opacity-50">保存</button>
            <button type="button" @click="resetRoleForm" class="h-9 rounded-md border border-white/10 px-4 text-sm hover:bg-slate-800">取消</button>
          </div>
        </form>

        <div v-if="dynamicRBACEnabled" class="overflow-x-auto">
          <div class="border-b border-white/10 px-4 py-3 text-xs text-slate-500 sm:px-6">共 {{ roleBindingTotal }} 个动态绑定</div>
          <table class="w-full min-w-[820px] text-left text-sm">
            <thead class="border-b border-white/10 bg-slate-900/60 text-xs text-slate-500"><tr><th class="px-6 py-3">用户</th><th class="px-4 py-3">角色</th><th class="px-4 py-3">Revision</th><th class="px-4 py-3">更新人</th><th class="px-4 py-3">更新时间</th><th class="px-6 py-3 text-right">操作</th></tr></thead>
            <tbody class="divide-y divide-white/10">
              <tr v-for="item in roleBindings" :key="item.user_id" class="hover:bg-white/[0.025]">
                <td class="px-6 py-4 font-mono">{{ item.user_id }}</td>
                <td class="px-4 py-4"><div class="flex flex-wrap gap-2"><span v-for="role in item.roles" :key="role" class="rounded bg-indigo-500/15 px-2 py-1 text-xs text-indigo-300">{{ role }}</span></div></td>
                <td class="px-4 py-4 font-mono text-xs">{{ item.revision }}</td>
                <td class="px-4 py-4 font-mono text-xs">{{ item.updated_by }}</td>
                <td class="px-4 py-4 text-xs text-slate-400">{{ formatTime(item.updated_at) }}</td>
                <td class="px-6 py-4"><div class="flex justify-end gap-2"><button @click="editRoleBinding(item)" title="编辑角色" class="flex h-8 w-8 items-center justify-center rounded-md border border-white/10 text-slate-300 hover:bg-slate-800"><PencilSquareIcon class="h-4 w-4" /></button><button @click="removeRoleBinding(item)" title="删除动态绑定" class="flex h-8 w-8 items-center justify-center rounded-md border border-rose-400/30 text-rose-300 hover:bg-rose-500/10"><TrashIcon class="h-4 w-4" /></button></div></td>
              </tr>
              <tr v-if="!roleBindings.length"><td colspan="6" class="px-6 py-12 text-center text-sm text-slate-500">暂无动态角色绑定</td></tr>
            </tbody>
          </table>
        </div>

        <div v-if="dynamicRBACEnabled" class="overflow-x-auto border-t border-white/10">
          <div class="px-4 py-3 text-xs text-slate-500 sm:px-6">最近 {{ Math.min(roleAudits.length, roleAuditTotal) }} / {{ roleAuditTotal }} 条角色审计</div>
          <table class="w-full min-w-[880px] text-left text-sm">
            <thead class="border-y border-white/10 bg-slate-900/60 text-xs text-slate-500"><tr><th class="px-6 py-3">动作</th><th class="px-4 py-3">目标用户</th><th class="px-4 py-3">角色</th><th class="px-4 py-3">结果</th><th class="px-4 py-3">操作者</th><th class="px-4 py-3">时间</th></tr></thead>
            <tbody class="divide-y divide-white/10"><tr v-for="item in roleAudits" :key="item.id" class="hover:bg-white/[0.025]"><td class="px-6 py-4 font-medium">{{ item.action }}</td><td class="px-4 py-4 font-mono text-xs">{{ item.subject_user_id }}</td><td class="px-4 py-4 text-xs text-slate-400">{{ item.roles?.join(', ') || '-' }}</td><td class="px-4 py-4"><span class="rounded px-2 py-1 text-xs" :class="statusClass(item.outcome)">{{ item.outcome }}</span><p v-if="item.error_code" class="mt-1 text-xs text-rose-300">{{ item.error_code }}</p></td><td class="px-4 py-4 font-mono text-xs">{{ item.actor_user_id }}</td><td class="px-4 py-4 text-xs text-slate-400">{{ formatTime(item.created_at) }}</td></tr><tr v-if="!roleAudits.length"><td colspan="6" class="px-6 py-10 text-center text-sm text-slate-500">暂无角色审计</td></tr></tbody>
          </table>
        </div>
      </section>
    </template>

    <div v-if="publishTarget" class="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4" @click.self="closePublish">
      <section role="dialog" aria-modal="true" class="w-full max-w-2xl rounded-lg border border-white/10 bg-slate-900 shadow-2xl">
        <header class="flex items-center justify-between border-b border-white/10 px-4 py-3"><div><h2 class="text-sm font-semibold">提交发布审批</h2><p class="mt-1 text-xs text-slate-500">{{ publishTarget.spec.profile_id }}@{{ publishTarget.spec.version }} · Revision {{ publishTarget.revision }}</p></div><button @click="closePublish" title="关闭" class="flex h-8 w-8 items-center justify-center rounded-md text-slate-400 hover:bg-slate-800"><XMarkIcon class="h-5 w-5" /></button></header>
        <div class="px-4 py-4">
          <label class="text-xs text-slate-400">Agent Eval 归档回执
            <textarea v-model="publishEvidenceText" rows="12" spellcheck="false" placeholder="粘贴 agent-task-eval 生成的 archive receipt JSON；是否必填由服务端发布策略决定" class="mt-1 w-full resize-y rounded-md border border-white/10 bg-slate-950 px-3 py-2 font-mono text-xs leading-5 text-slate-200 outline-none focus:border-indigo-400" />
          </label>
          <p class="mt-3 text-xs text-slate-500">服务端会读取 MinIO 精确版本，校验 HMAC、COMPLIANCE 保留期、Profile 版本与质量门禁。</p>
        </div>
        <footer class="flex justify-end gap-2 border-t border-white/10 px-4 py-3"><button @click="closePublish" class="h-9 rounded-md border border-white/10 px-4 text-sm hover:bg-slate-800">取消</button><button @click="requestPublish" :disabled="mutating" class="h-9 rounded-md bg-indigo-500 px-4 text-sm font-semibold text-white hover:bg-indigo-400 disabled:opacity-50">提交审批</button></footer>
      </section>
    </div>

    <div v-if="decisionTarget" class="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4" @click.self="decisionTarget = null">
      <section role="dialog" aria-modal="true" class="w-full max-w-lg rounded-lg border border-white/10 bg-slate-900 shadow-2xl">
        <header class="flex items-center justify-between border-b border-white/10 px-4 py-3"><div><h2 class="text-sm font-semibold">{{ decision === 'approved' ? '批准发布' : '拒绝发布' }}</h2><p class="mt-1 text-xs text-slate-500">{{ decisionTarget.profile_id }}@{{ decisionTarget.version }}</p></div><button @click="decisionTarget = null" title="关闭" class="flex h-8 w-8 items-center justify-center rounded-md text-slate-400 hover:bg-slate-800"><XMarkIcon class="h-5 w-5" /></button></header>
        <div class="px-4 py-4"><label class="text-xs text-slate-400">审批意见<textarea v-model="decisionReason" rows="4" class="mt-1 w-full rounded-md border border-white/10 bg-slate-950 px-3 py-2 text-sm" /></label><div v-if="decisionTarget.quality_evidence" class="mt-3 border-l-2 border-emerald-400/60 pl-3 text-xs text-slate-400"><p class="text-emerald-300">Eval {{ decisionTarget.quality_evidence.gate_status }} · {{ decisionTarget.quality_evidence.passed }}/{{ decisionTarget.quality_evidence.cases }}</p><p class="mt-1">任务 {{ formatBasisPoints(decisionTarget.quality_evidence.task_completion_rate_bps) }} · 工具 {{ formatBasisPoints(decisionTarget.quality_evidence.read_tool_selection_accuracy_bps) }} · 语义 {{ formatBasisPoints(decisionTarget.quality_evidence.semantic_pass_rate_bps) }}</p><p class="mt-1 font-mono text-[10px]">{{ shortHash(decisionTarget.quality_evidence.reference?.report_sha256) }}</p></div><p class="mt-3 flex items-center gap-2 text-xs text-slate-500"><ClockIcon class="h-4 w-4" />审批绑定 Revision {{ decisionTarget.expected_version_revision }} 与当前快照</p></div>
        <footer class="flex justify-end gap-2 border-t border-white/10 px-4 py-3"><button @click="decisionTarget = null" class="h-9 rounded-md border border-white/10 px-4 text-sm hover:bg-slate-800">取消</button><button @click="submitDecision" :disabled="mutating" class="h-9 rounded-md px-4 text-sm font-semibold text-white disabled:opacity-50" :class="decision === 'approved' ? 'bg-emerald-600 hover:bg-emerald-500' : 'bg-rose-600 hover:bg-rose-500'">确认</button></footer>
      </section>
    </div>
  </main>
</template>
