<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import {
  ArrowPathIcon,
  CheckIcon,
  ClipboardDocumentCheckIcon,
  XMarkIcon,
} from '@heroicons/vue/24/outline'
import {
  decideToolApproval,
  getAgentRun,
  getWorkflowRun,
  issueAgentResumeGrant,
  issueWorkflowResumeGrant,
  listToolApprovals,
  resumeAgentRun,
  resumeWorkflowRun,
  type ToolApproval,
} from '../../api/agent'
import { clearWorkflowResume } from '../../utils/workflowResume'

const emit = defineEmits<{
  resumed: [payload: { response: string; run: Record<string, any> }]
}>()

const tabs = [
  { id: 'pending', label: '待处理' },
  { id: 'approved', label: '已批准' },
  { id: 'rejected', label: '已拒绝' },
  { id: 'consumed', label: '已执行' },
] as const

const isOpen = ref(false)
const activeStatus = ref<(typeof tabs)[number]['id']>('pending')
const approvals = ref<ToolApproval[]>([])
const pendingCount = ref(0)
const selected = ref<ToolApproval | null>(null)
const runDetail = ref<Record<string, any> | null>(null)
const rejectReason = ref('')
const isLoading = ref(false)
const actionApprovalId = ref('')
const notice = ref('')
let refreshTimer: number | undefined

const selectedInputs = computed(() => Object.entries(selected.value?.redacted_inputs || {}))
const isRuntimeApproval = (approval: ToolApproval) => approval.source === 'runtime'
const selectedRunIsSuspended = computed(() => {
  if (!selected.value || !runDetail.value) return false
  return isRuntimeApproval(selected.value)
    ? runDetail.value.status === 'approval_required'
    : runDetail.value.status === 'suspended'
})

const statusLabel = (status: string) => tabs.find(tab => tab.id === status)?.label || status

const formatTime = (timestamp?: number) => {
  if (!timestamp) return '-'
  return new Date(timestamp * 1000).toLocaleString('zh-CN', { hour12: false })
}

const formatJSON = (value: any) => {
  if (value === undefined || value === null) return '-'
  return JSON.stringify(value, null, 2)
}

const fetchPendingCount = async () => {
  try {
    const response = await listToolApprovals({ status: 'pending', page: 1, page_size: 1 })
    pendingCount.value = Number(response.data.total || 0)
  } catch (error) {
    console.error('Failed to load pending approval count:', error)
  }
}

const loadRunDetail = async (approval: ToolApproval) => {
  if (isRuntimeApproval(approval)) {
    const response = await getAgentRun(approval.run_id)
    return response.data || null
  }
  const response = await getWorkflowRun(approval.run_id)
  return response.data.run || null
}

const selectApproval = async (approval: ToolApproval) => {
  selected.value = approval
  rejectReason.value = ''
  notice.value = ''
  runDetail.value = null
  if (!approval.run_id) return
  try {
    runDetail.value = await loadRunDetail(approval)
  } catch (error) {
    console.error('Failed to load approval run:', error)
    notice.value = '运行详情加载失败。'
  }
}

const fetchApprovals = async () => {
  isLoading.value = true
  notice.value = ''
  try {
    const response = await listToolApprovals({ status: activeStatus.value, page: 1, page_size: 50 })
    approvals.value = response.data.approvals || []
    if (activeStatus.value === 'pending') pendingCount.value = Number(response.data.total || 0)
    const current = approvals.value.find(item => item.approval_id === selected.value?.approval_id)
    const nextApproval = current || approvals.value[0]
    if (nextApproval) {
      await selectApproval(nextApproval)
    } else {
      selected.value = null
      runDetail.value = null
    }
  } catch (error) {
    console.error('Failed to load approvals:', error)
    notice.value = '审批列表加载失败。'
  } finally {
    isLoading.value = false
  }
}

const openInbox = async () => {
  isOpen.value = true
  await fetchApprovals()
}

const switchStatus = async (status: (typeof tabs)[number]['id']) => {
  activeStatus.value = status
  selected.value = null
  runDetail.value = null
  await fetchApprovals()
}

const continueApprovedParentAgent = async (
  approval: ToolApproval,
  childRun: Record<string, any>,
  resumeToken: string,
) => {
  const parentRunId = String(childRun.parent_run_id || '')
  if (!parentRunId) throw new Error('子工作流缺少父 Agent 运行标识。')

  const parentResponse = await getAgentRun(parentRunId)
  const parentRun = parentResponse.data || {}
  if (parentRun.status !== 'approval_required' || parentRun.approval_id !== approval.approval_id) {
    throw new Error('父 Agent 已不再等待该子工作流审批。')
  }

  const response = await resumeAgentRun(parentRunId, {
    expected_revision: Number(parentRun.revision || 0),
    approval_id: approval.approval_id,
    resume_token: resumeToken,
  })
  clearWorkflowResume(approval.run_id)

  const refreshedResponse = await getAgentRun(parentRunId)
  const refreshedRun = refreshedResponse.data || parentRun
  runDetail.value = refreshedRun
  emit('resumed', {
    response: response.data.response || '父 Agent 已继续执行。',
    run: refreshedRun,
  })
  if (refreshedRun.status === 'completed') return '审批通过，子工作流与父 Agent 均已执行完成。'
  if (refreshedRun.status === 'approval_required') {
    return '父 Agent 已继续执行，并在下一个受控动作处再次等待审批。'
  }
  if (refreshedRun.status === 'awaiting_human') {
    return '子工作流已继续执行，父 Agent 正在等待补充信息。'
  }
  return '子工作流与父 Agent 已继续执行。'
}

const continueApprovedWorkflow = async (approval: ToolApproval) => {
  let grantResponse: any
  for (let attempt = 0; attempt < 2; attempt++) {
    const detailResponse = await getWorkflowRun(approval.run_id)
    const currentRun = detailResponse.data.run || {}
    runDetail.value = currentRun
    if (currentRun.status !== 'suspended' || currentRun.approval_request_id !== approval.approval_id) {
      throw new Error('该审批对应的工作流已不再等待恢复。')
    }
    try {
      grantResponse = await issueWorkflowResumeGrant(approval.approval_id, {
        expected_run_revision: Number(currentRun.revision || 0),
      })
      break
    } catch (error: any) {
      if (attempt === 1 || Number(error?.response?.status || 0) !== 409) throw error
    }
  }

  const grantedRun = grantResponse?.data?.run || {}
  const resumeToken = String(grantResponse?.data?.resume_token || '')
  if (!resumeToken) throw new Error('服务端未返回一次性恢复授权。')

  if (
    grantedRun.invocation_source === 'runtime'
    && String(grantedRun.parent_run_id || '')
  ) {
    return continueApprovedParentAgent(approval, grantedRun, resumeToken)
  }

  clearWorkflowResume(approval.run_id)
  const response = await resumeWorkflowRun(approval.run_id, {
    approval_id: approval.approval_id,
    resume_token: resumeToken,
    input: {},
  })
  clearWorkflowResume(approval.run_id)

  const resumedRun = response.data.run || grantedRun
  emit('resumed', {
    response: response.data.response || '工作流已继续执行。',
    run: resumedRun,
  })
  if (resumedRun.status === 'success') return '审批通过，工作流已执行完成。'
  if (resumedRun.status === 'suspended' && resumedRun.approval_request_id) {
    return '工作流已继续执行，并在下一个受控工具处再次挂起。'
  }
  return '工作流已继续执行。'
}

const continueApprovedAgentRun = async (approval: ToolApproval) => {
  let grantResponse: any
  for (let attempt = 0; attempt < 2; attempt++) {
    const detailResponse = await getAgentRun(approval.run_id)
    const currentRun = detailResponse.data || {}
    runDetail.value = currentRun
    if (currentRun.status !== 'approval_required' || currentRun.approval_id !== approval.approval_id) {
      throw new Error('该审批对应的 Agent 运行已不再等待恢复。')
    }
    try {
      grantResponse = await issueAgentResumeGrant(approval.approval_id, {
        expected_run_revision: Number(currentRun.revision || 0),
      })
      break
    } catch (error: any) {
      if (attempt === 1 || Number(error?.response?.status || 0) !== 409) throw error
    }
  }

  const grantedRun = grantResponse?.data?.run || {}
  const resumeToken = String(grantResponse?.data?.resume_token || '')
  if (!resumeToken) throw new Error('服务端未返回一次性恢复授权。')

  const response = await resumeAgentRun(approval.run_id, {
    expected_revision: Number(grantedRun.revision || 0),
    approval_id: approval.approval_id,
    resume_token: resumeToken,
  })
  const refreshedRun = await getAgentRun(approval.run_id)
  runDetail.value = refreshedRun.data || null
  emit('resumed', {
    response: response.data.response || 'Agent 已继续执行。',
    run: refreshedRun.data || grantedRun,
  })
  if (refreshedRun.data.status === 'completed') return '审批通过，Agent 已执行完成。'
  if (refreshedRun.data.status === 'approval_required') {
    return 'Agent 已继续执行，并在下一个受控工具处再次挂起。'
  }
  return 'Agent 已继续执行。'
}

const continueApprovedRun = (approval: ToolApproval) => (
  isRuntimeApproval(approval)
    ? continueApprovedAgentRun(approval)
    : continueApprovedWorkflow(approval)
)

const decide = async (decision: 'approved' | 'rejected') => {
  const approval = selected.value
  if (!approval || actionApprovalId.value) return
  actionApprovalId.value = approval.approval_id
  notice.value = ''
  let successNotice = ''
  try {
    await decideToolApproval(approval.approval_id, {
      decision,
      reason: rejectReason.value.trim(),
      expected_revision: approval.revision,
    })

    if (decision === 'rejected') {
      if (!isRuntimeApproval(approval)) clearWorkflowResume(approval.run_id)
      successNotice = isRuntimeApproval(approval)
        ? '已拒绝该工具调用，Agent 运行已终止。'
        : '已拒绝该工具调用，工作流已终止。'
    } else {
      successNotice = await continueApprovedRun(approval)
    }
    await fetchPendingCount()
    await fetchApprovals()
    notice.value = successNotice
  } catch (error: any) {
    console.error('Failed to decide approval:', error)
    notice.value = error?.response?.data?.error || error?.message || '审批操作失败。'
    await fetchPendingCount()
    await fetchApprovals()
  } finally {
    actionApprovalId.value = ''
  }
}

const resumeApproved = async () => {
  const approval = selected.value
  if (!approval || approval.status !== 'approved' || actionApprovalId.value) return
  actionApprovalId.value = approval.approval_id
  notice.value = ''
  try {
    const successNotice = await continueApprovedRun(approval)
    await fetchApprovals()
    notice.value = successNotice
  } catch (error: any) {
    console.error('Failed to resume approved run:', error)
    notice.value = error?.response?.data?.error || error?.message || '恢复运行失败。'
    await fetchApprovals()
  } finally {
    actionApprovalId.value = ''
  }
}

onMounted(() => {
  fetchPendingCount()
  refreshTimer = window.setInterval(fetchPendingCount, 30000)
})

onUnmounted(() => {
  if (refreshTimer) window.clearInterval(refreshTimer)
})
</script>

<template>
  <div>
    <button
      type="button"
      class="relative flex h-9 items-center gap-1.5 rounded-lg border border-gray-200 px-3 text-xs font-semibold text-gray-700 transition-colors hover:bg-gray-100 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
      title="工具审批"
      @click="openInbox"
    >
      <ClipboardDocumentCheckIcon class="h-4 w-4" />
      <span class="hidden sm:inline">审批</span>
      <span v-if="pendingCount" class="min-w-5 rounded-full bg-rose-500 px-1.5 py-0.5 text-center text-[10px] leading-4 text-white">
        {{ pendingCount > 99 ? '99+' : pendingCount }}
      </span>
    </button>

    <Teleport to="body">
      <div v-if="isOpen" class="fixed inset-0 z-40 bg-black/30" @click="isOpen = false"></div>
      <aside
        v-if="isOpen"
        class="fixed inset-y-0 right-0 z-50 flex w-full max-w-3xl flex-col bg-white shadow-2xl dark:bg-gray-950"
        aria-label="工具审批收件箱"
      >
        <header class="flex h-16 items-center justify-between border-b border-gray-200 px-5 dark:border-gray-800">
          <div>
            <h2 class="text-base font-bold">工具审批</h2>
            <p class="text-xs text-gray-500">检查挂起运行后，再决定是否执行受控操作</p>
          </div>
          <button
            type="button"
            class="flex h-9 w-9 items-center justify-center rounded-full text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-800"
            title="关闭"
            @click="isOpen = false"
          >
            <XMarkIcon class="h-5 w-5" />
          </button>
        </header>

        <div class="flex border-b border-gray-200 px-4 dark:border-gray-800">
          <button
            v-for="tab in tabs"
            :key="tab.id"
            type="button"
            class="border-b-2 px-3 py-3 text-sm font-medium transition-colors"
            :class="activeStatus === tab.id ? 'border-primary text-primary' : 'border-transparent text-gray-500 hover:text-gray-900 dark:hover:text-white'"
            @click="switchStatus(tab.id)"
          >
            {{ tab.label }}
          </button>
        </div>

        <div class="grid min-h-0 flex-1 grid-cols-1 md:grid-cols-[280px_minmax(0,1fr)]">
          <section class="min-h-0 overflow-y-auto border-r border-gray-200 p-3 dark:border-gray-800">
            <div v-if="isLoading" class="flex items-center justify-center py-12 text-gray-400">
              <ArrowPathIcon class="h-5 w-5 animate-spin" />
            </div>
            <p v-else-if="approvals.length === 0" class="px-3 py-12 text-center text-sm text-gray-400">
              暂无{{ statusLabel(activeStatus) }}审批
            </p>
            <template v-else>
              <button
                v-for="approval in approvals"
                :key="approval.approval_id"
                type="button"
                class="mb-2 w-full rounded-lg border p-3 text-left transition-colors"
                :class="selected?.approval_id === approval.approval_id ? 'border-primary bg-blue-50 dark:bg-blue-950/30' : 'border-gray-200 hover:bg-gray-50 dark:border-gray-800 dark:hover:bg-gray-900'"
                @click="selectApproval(approval)"
              >
                <div class="flex items-center justify-between gap-2">
                  <span class="truncate text-sm font-semibold">{{ approval.tool_name }}</span>
                  <span class="text-[11px] text-gray-500">{{ statusLabel(approval.status) }}</span>
                </div>
                <p class="mt-1 truncate text-xs text-gray-500">步骤 {{ approval.step_id || '-' }}</p>
                <p class="mt-2 text-[11px] text-gray-400">{{ formatTime(approval.created_at) }}</p>
              </button>
            </template>
          </section>

          <section class="min-h-0 overflow-y-auto">
            <div v-if="!selected" class="flex h-full items-center justify-center p-8 text-sm text-gray-400">
              选择一条审批查看运行详情
            </div>
            <template v-else>
              <div class="border-b border-gray-200 p-5 dark:border-gray-800">
                <div class="flex flex-wrap items-center justify-between gap-3">
                  <div>
                    <h3 class="font-bold">{{ selected.tool_name }}</h3>
                    <p class="mt-1 text-xs text-gray-500">Run {{ selected.run_id }}</p>
                  </div>
                  <span class="rounded-full bg-gray-100 px-2.5 py-1 text-xs font-medium dark:bg-gray-800">
                    {{ statusLabel(selected.status) }}
                  </span>
                </div>
                <dl class="mt-4 grid grid-cols-2 gap-x-4 gap-y-3 text-xs">
                  <div><dt class="text-gray-400">步骤</dt><dd class="mt-1 break-all">{{ selected.step_id }}</dd></div>
                  <div><dt class="text-gray-400">来源</dt><dd class="mt-1">{{ selected.source || '-' }}</dd></div>
                  <div><dt class="text-gray-400">过期时间</dt><dd class="mt-1">{{ formatTime(selected.expires_at) }}</dd></div>
                  <div>
                    <dt class="text-gray-400">挂起动作</dt>
                    <dd class="mt-1 break-all">{{ runDetail?.pending_action_id || runDetail?.waiting_node_id || '-' }}</dd>
                  </div>
                </dl>
              </div>

              <div class="border-b border-gray-200 p-5 dark:border-gray-800">
                <h4 class="text-sm font-semibold">工具参数</h4>
                <dl class="mt-3 space-y-2 text-xs">
                  <div v-for="[key, value] in selectedInputs" :key="key" class="grid grid-cols-[120px_minmax(0,1fr)] gap-3">
                    <dt class="truncate text-gray-400">{{ key }}</dt>
                    <dd class="break-words">{{ typeof value === 'string' ? value : formatJSON(value) }}</dd>
                  </div>
                </dl>
              </div>

              <div class="border-b border-gray-200 p-5 dark:border-gray-800">
                <h4 class="text-sm font-semibold">挂起状态快照</h4>
                <pre class="mt-3 max-h-72 overflow-auto whitespace-pre-wrap break-words bg-gray-50 p-3 text-xs leading-5 dark:bg-gray-900">{{ formatJSON(runDetail?.output || runDetail) }}</pre>
                <p v-if="runDetail?.error_message" class="mt-3 text-xs text-rose-600">{{ runDetail.error_message }}</p>
              </div>

              <div v-if="selected.status === 'pending'" class="p-5">
                <label class="text-xs font-medium text-gray-600 dark:text-gray-300" for="approval-reason">拒绝原因</label>
                <textarea
                  id="approval-reason"
                  v-model="rejectReason"
                  rows="3"
                  class="mt-2 w-full resize-none rounded-lg border border-gray-300 bg-transparent p-3 text-sm focus:border-primary focus:outline-none dark:border-gray-700"
                  placeholder="可选，说明拒绝原因"
                ></textarea>
                <div class="mt-3 flex justify-end gap-2">
                  <button
                    type="button"
                    class="flex h-9 items-center gap-1.5 rounded-lg border border-gray-300 px-3 text-sm font-medium hover:bg-gray-100 disabled:opacity-50 dark:border-gray-700 dark:hover:bg-gray-800"
                    :disabled="Boolean(actionApprovalId)"
                    @click="decide('rejected')"
                  >
                    <XMarkIcon class="h-4 w-4" />
                    拒绝
                  </button>
                  <button
                    type="button"
                    class="flex h-9 items-center gap-1.5 rounded-lg bg-primary px-3 text-sm font-semibold text-white hover:bg-blue-600 disabled:opacity-50"
                    :disabled="Boolean(actionApprovalId)"
                    @click="decide('approved')"
                  >
                    <ArrowPathIcon v-if="actionApprovalId" class="h-4 w-4 animate-spin" />
                    <CheckIcon v-else class="h-4 w-4" />
                    批准并继续
                  </button>
                </div>
              </div>
              <div v-else-if="selected.status === 'approved' && selectedRunIsSuspended" class="flex items-center justify-between gap-4 p-5">
                <p class="text-xs text-gray-500">该审批已通过，可签发短期一次性授权并继续运行。</p>
                <button
                  type="button"
                  class="flex h-9 shrink-0 items-center gap-1.5 rounded-lg bg-primary px-3 text-sm font-semibold text-white hover:bg-blue-600 disabled:opacity-50"
                  :disabled="Boolean(actionApprovalId)"
                  @click="resumeApproved"
                >
                  <ArrowPathIcon v-if="actionApprovalId" class="h-4 w-4 animate-spin" />
                  <CheckIcon v-else class="h-4 w-4" />
                  继续运行
                </button>
              </div>
            </template>
          </section>
        </div>

        <p v-if="notice" class="border-t border-gray-200 px-5 py-3 text-sm text-gray-700 dark:border-gray-800 dark:text-gray-200">
          {{ notice }}
        </p>
      </aside>
    </Teleport>
  </div>
</template>
