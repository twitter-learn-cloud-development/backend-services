<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  ArrowPathIcon,
  CheckIcon,
  CloudArrowDownIcon,
  PencilSquareIcon,
  PlusIcon,
  ServerStackIcon,
  ShieldCheckIcon,
  TrashIcon,
  UserGroupIcon,
  XMarkIcon,
} from '@heroicons/vue/24/outline'
import {
  approveExternalMCPSnapshot,
  configureExternalMCPTool,
  createExternalMCPConnection,
  discoverExternalMCPTools,
  listAllAgentProjects,
  listExternalMCPConnections,
  listExternalMCPTools,
  revokeExternalMCPConnection,
  updateExternalMCPConnection,
  type ExternalMCPAuthType,
  type ExternalMCPConnectionView,
  type ExternalMCPToolSnapshotView,
  type ExternalMCPToolView,
  type ExternalMCPTransport,
  type AgentProjectView,
} from '../../api/agent'
import AgentProjectDialog from './AgentProjectDialog.vue'

const props = defineProps<{ open: boolean, initialConnectionId?: string }>()

const emit = defineEmits<{
  (event: 'close'): void
  (event: 'changed'): void
}>()

const connections = ref<ExternalMCPConnectionView[]>([])
const selectedConnectionId = ref('')
const tools = ref<ExternalMCPToolView[]>([])
const pendingSnapshot = ref<ExternalMCPToolSnapshotView | null>(null)
const loading = ref(false)
const loadingTools = ref(false)
const saving = ref(false)
const action = ref('')
const toolAction = ref('')
const errorMessage = ref('')
const editingId = ref('')
const editingRevision = ref(0)
const name = ref('')
const transport = ref<ExternalMCPTransport>('streamable_http')
const endpoint = ref('')
const authType = ref<ExternalMCPAuthType>('none')
const credentialSource = ref<'user' | 'managed'>('user')
const managedCredentialRef = ref('')
const bearerToken = ref('')
const connectionScope = ref<'user' | 'project'>('user')
const projectId = ref('')
const projects = ref<AgentProjectView[]>([])
const projectScopeAvailable = ref(false)
const projectDialogOpen = ref(false)
const projectNotice = ref('')

const selectedConnection = computed(() => (
  connections.value.find(connection => connection.connection_id === selectedConnectionId.value) || null
))

const editingConnection = computed(() => (
  connections.value.find(connection => connection.connection_id === editingId.value) || null
))

const requiresUserBearerToken = computed(() => (
  authType.value === 'bearer' && credentialSource.value === 'user' && (
    !editingId.value || editingConnection.value?.auth_type !== 'bearer' || editingConnection.value?.credential_source !== 'user'
  )
))

const readyToolCount = computed(() => (
  tools.value.filter(tool => tool.policy.enabled).length
))

const manageableProjects = computed(() => (
  projects.value.filter(project => project.current_role === 'owner' || project.current_role === 'editor')
))

const selectedConnectionProject = computed(() => (
  projects.value.find(project => project.project_id === selectedConnection.value?.project_id) || null
))

const canManageConnection = (connection: ExternalMCPConnectionView | null) => {
  if (!connection || connection.scope !== 'project') return true
  const role = projects.value.find(project => project.project_id === connection.project_id)?.current_role
  return role === 'owner' || role === 'editor'
}

const canManageSelectedConnection = computed(() => canManageConnection(selectedConnection.value))

const projectName = (value: string) => (
  projects.value.find(project => project.project_id === value)?.name || '项目连接'
)

const apiError = (error: any, fallback: string) => {
  const status = Number(error?.response?.status || 0)
  if (!status || status >= 500) return `${fallback}：服务暂不可用，请稍后重试`
  if (status === 401) return `${fallback}：登录状态已失效`
  if (status === 403) return `${fallback}：当前账号无权执行此操作`
  if (status === 409) return `${fallback}：连接状态已变化，请刷新后重试`

  const detail = error?.response?.data?.error
  return typeof detail === 'string' && detail.trim() ? detail : fallback
}

const resetForm = () => {
  editingId.value = ''
  editingRevision.value = 0
  name.value = ''
  transport.value = 'streamable_http'
  endpoint.value = ''
  authType.value = 'none'
  credentialSource.value = 'user'
  managedCredentialRef.value = ''
  bearerToken.value = ''
  connectionScope.value = 'user'
  projectId.value = ''
}

const loadProjects = async () => {
  projectNotice.value = ''
  try {
    projects.value = await listAllAgentProjects()
    projectScopeAvailable.value = true
    if (!manageableProjects.value.some(project => project.project_id === projectId.value)) {
      projectId.value = manageableProjects.value[0]?.project_id || ''
    }
  } catch (error: any) {
    projects.value = []
    projectScopeAvailable.value = false
    const status = Number(error?.response?.status || 0)
    if (status && status !== 412) projectNotice.value = apiError(error, '加载 Agent 项目失败')
    if (connectionScope.value === 'project') connectionScope.value = 'user'
    projectId.value = ''
  }
}

const replaceConnection = (connection: ExternalMCPConnectionView) => {
  const index = connections.value.findIndex(item => item.connection_id === connection.connection_id)
  if (index >= 0) connections.value.splice(index, 1, connection)
  else connections.value.unshift(connection)
}

const loadTools = async () => {
  const connection = selectedConnection.value
  tools.value = []
  pendingSnapshot.value = null
  if (!connection?.active_snapshot_id || connection.discovery_status !== 'ready') return
  loadingTools.value = true
  errorMessage.value = ''
  try {
    const response = await listExternalMCPTools(connection.connection_id)
    if (response.data?.connection) replaceConnection(response.data.connection)
    tools.value = response.data?.tools || []
  } catch (error: any) {
    errorMessage.value = apiError(error, '加载工具目录失败')
  } finally {
    loadingTools.value = false
  }
}

const selectConnection = async (connectionId: string) => {
  selectedConnectionId.value = connectionId
  await loadTools()
}

const loadConnections = async () => {
  loading.value = true
  errorMessage.value = ''
  try {
    await loadProjects()
    const response = await listExternalMCPConnections({ page: 1, page_size: 100 })
    connections.value = (response.data?.connections || []).filter(connection => connection.status === 'active')
    const preferredConnectionId = String(props.initialConnectionId || '').trim()
    if (connections.value.some(connection => connection.connection_id === preferredConnectionId)) {
      selectedConnectionId.value = preferredConnectionId
    }
    if (!connections.value.some(connection => connection.connection_id === selectedConnectionId.value)) {
      selectedConnectionId.value = connections.value[0]?.connection_id || ''
    }
    await loadTools()
  } catch (error: any) {
    connections.value = []
    selectedConnectionId.value = ''
    tools.value = []
    pendingSnapshot.value = null
    errorMessage.value = apiError(error, '加载 MCP 连接失败')
  } finally {
    loading.value = false
  }
}

const editConnection = (connection: ExternalMCPConnectionView) => {
  editingId.value = connection.connection_id
  editingRevision.value = connection.revision
  name.value = connection.name
  transport.value = connection.transport
  endpoint.value = connection.endpoint
  authType.value = connection.auth_type
  credentialSource.value = connection.credential_source || 'user'
  managedCredentialRef.value = connection.managed_credential_ref || ''
  bearerToken.value = ''
  connectionScope.value = connection.scope
  projectId.value = connection.project_id || ''
  errorMessage.value = ''
}

const saveConnection = async () => {
  if (!name.value.trim() || !endpoint.value.trim()) return
  if (credentialSource.value === 'managed' && (!projectId.value || !managedCredentialRef.value.trim())) return
  if (requiresUserBearerToken.value && !bearerToken.value.trim()) return
  if (connectionScope.value === 'project' && !projectId.value) return
  saving.value = true
  errorMessage.value = ''
  try {
    const payload = {
      scope: connectionScope.value,
      project_id: connectionScope.value === 'project' ? projectId.value : undefined,
      name: name.value.trim(),
      transport: transport.value,
      endpoint: endpoint.value.trim(),
      auth_type: authType.value,
      credential_source: credentialSource.value,
      managed_credential_ref: credentialSource.value === 'managed' ? managedCredentialRef.value.trim() : undefined,
      bearer_token: credentialSource.value === 'user' ? (bearerToken.value.trim() || undefined) : undefined,
      expected_revision: editingRevision.value || undefined,
    }
    const response = editingId.value
      ? await updateExternalMCPConnection(editingId.value, payload)
      : await createExternalMCPConnection(payload)
    const saved = response.data?.connection as ExternalMCPConnectionView | undefined
    if (saved) {
      replaceConnection(saved)
      selectedConnectionId.value = saved.connection_id
    }
    resetForm()
    emit('changed')
    await loadConnections()
  } catch (error: any) {
    errorMessage.value = apiError(error, '保存 MCP 连接失败')
  } finally {
    saving.value = false
  }
}

const revokeConnection = async (connection: ExternalMCPConnectionView) => {
  if (!window.confirm(`撤销 MCP 连接“${connection.name}”？`)) return
  action.value = `revoke:${connection.connection_id}`
  errorMessage.value = ''
  try {
    await revokeExternalMCPConnection(connection.connection_id, connection.revision)
    if (editingId.value === connection.connection_id) resetForm()
    if (selectedConnectionId.value === connection.connection_id) selectedConnectionId.value = ''
    emit('changed')
    await loadConnections()
  } catch (error: any) {
    errorMessage.value = apiError(error, '撤销 MCP 连接失败')
  } finally {
    action.value = ''
  }
}

const discoverTools = async () => {
  const connection = selectedConnection.value
  if (!connection) return
  action.value = 'discover'
  errorMessage.value = ''
  try {
    const response = await discoverExternalMCPTools(connection.connection_id, connection.revision)
    const updated = response.data?.connection
    const snapshot = response.data?.snapshot
    if (updated) replaceConnection(updated)
    if (
      updated?.discovery_status === 'review_required'
      && snapshot?.snapshot_id
      && updated.pending_snapshot_id === snapshot.snapshot_id
    ) {
      pendingSnapshot.value = snapshot
      tools.value = []
    } else {
      pendingSnapshot.value = null
      await loadTools()
    }
  } catch (error: any) {
    errorMessage.value = apiError(error, '发现 MCP 工具失败')
  } finally {
    action.value = ''
  }
}

const approveSnapshot = async () => {
  const connection = selectedConnection.value
  const snapshot = pendingSnapshot.value
  if (!connection || !snapshot) return
  action.value = 'approve'
  errorMessage.value = ''
  try {
    const response = await approveExternalMCPSnapshot(
      connection.connection_id,
      snapshot.snapshot_id,
      connection.revision,
    )
    if (response.data?.connection) replaceConnection(response.data.connection)
    pendingSnapshot.value = null
    emit('changed')
    await loadTools()
  } catch (error: any) {
    errorMessage.value = apiError(error, '审核 MCP Schema 失败')
  } finally {
    action.value = ''
  }
}

const reviewedToolCategory = (schema: ExternalMCPToolView['schema']): ExternalMCPToolView['policy']['category'] => {
  if (schema.declared_read_only) return 'read'
  if (schema.supports_write_idempotency) return 'write'
  return 'risky'
}

const reviewedToolLabel = (schema: ExternalMCPToolView['schema']) => {
  const category = reviewedToolCategory(schema)
  if (category === 'read') return '只读声明'
  if (category === 'write') return '幂等写入 · 需审批'
  return '高风险 · 需审批'
}

const reviewedToolBadge = (schema: ExternalMCPToolView['schema']) => {
  const category = reviewedToolCategory(schema)
  if (category === 'read') return 'bg-green-100 text-green-800 dark:bg-green-950/50 dark:text-green-200'
  if (category === 'write') return 'bg-blue-100 text-blue-800 dark:bg-blue-950/50 dark:text-blue-200'
  return 'bg-amber-100 text-amber-800 dark:bg-amber-950/50 dark:text-amber-200'
}

const activeToolLabel = (tool: ExternalMCPToolView) => {
  if (tool.policy.category === 'read') return '只读'
  if (tool.policy.category === 'write') return tool.policy.enabled ? '幂等写入 · 逐次审批' : '幂等写入'
  return tool.policy.enabled ? '高风险 · 逐次审批' : '高风险'
}

const setToolEnabled = async (tool: ExternalMCPToolView, enabled: boolean) => {
  const connection = selectedConnection.value
  if (!connection) return
  const category = reviewedToolCategory(tool.schema)
  if (
    enabled
    && category === 'risky'
    && !window.confirm(`启用高风险 MCP 工具“${tool.schema.name}”？每次工作流执行仍需人工审批。`)
  ) return
  if (
    enabled
    && category === 'write'
    && !window.confirm(`启用幂等写入 MCP 工具“${tool.schema.name}”？每次执行仍需人工审批，平台会覆盖注入幂等键。`)
  ) return
  toolAction.value = tool.schema.qualified_name
  errorMessage.value = ''
  try {
    const response = await configureExternalMCPTool(
      connection.connection_id,
      tool.schema.qualified_name,
      {
        snapshot_id: connection.active_snapshot_id,
        category,
        enabled,
        expected_revision: connection.revision,
      },
    )
    if (response.data?.connection) replaceConnection(response.data.connection)
    const updated = response.data?.tool
    if (updated) {
      const index = tools.value.findIndex(item => item.schema.qualified_name === updated.schema.qualified_name)
      if (index >= 0) tools.value.splice(index, 1, updated)
    }
    emit('changed')
  } catch (error: any) {
    errorMessage.value = apiError(error, '更新 MCP 工具策略失败')
  } finally {
    toolAction.value = ''
  }
}

const discoveryStatusLabel = (status: ExternalMCPConnectionView['discovery_status']) => {
  const labels: Record<ExternalMCPConnectionView['discovery_status'], string> = {
    unchecked: '未发现',
    ready: '已审核',
    review_required: '待审核',
    failed: '发现失败',
  }
  return labels[status]
}

const healthStatusLabel = (status: ExternalMCPConnectionView['health_status']) => {
  const labels: Record<ExternalMCPConnectionView['health_status'], string> = {
    unknown: '未检测',
    healthy: '健康',
    degraded: '波动',
    unhealthy: '不可用',
  }
  return labels[status]
}

const healthStatusClass = (status: ExternalMCPConnectionView['health_status']) => {
  if (status === 'healthy') return 'text-emerald-500'
  if (status === 'degraded') return 'text-amber-500'
  if (status === 'unhealthy') return 'text-red-500'
  return 'text-gray-400'
}

const healthStatusTitle = (connection: ExternalMCPConnectionView) => {
  if (!connection.last_health_checked_at) return '尚未完成主动健康检测'
  const checkedAt = new Date(connection.last_health_checked_at * 1000).toLocaleString()
  if (!connection.health_error_code) return `最近检测：${checkedAt}`
  return `最近检测：${checkedAt} · ${connection.health_error_code}`
}

watch(() => props.open, (open) => {
  if (open) void loadConnections()
  else projectDialogOpen.value = false
})

watch(connectionScope, (value) => {
  if (value === 'project' && !manageableProjects.value.some(project => project.project_id === projectId.value)) {
    projectId.value = manageableProjects.value[0]?.project_id || ''
  }
  if (value === 'user') {
    projectId.value = ''
    credentialSource.value = 'user'
    managedCredentialRef.value = ''
  }
})

watch(authType, (value) => {
  if (value === 'none') {
    credentialSource.value = 'user'
    managedCredentialRef.value = ''
    bearerToken.value = ''
  }
})

watch(credentialSource, (value) => {
  if (value === 'managed') bearerToken.value = ''
  else managedCredentialRef.value = ''
})
</script>

<template>
  <div
    v-if="open"
    class="fixed inset-0 z-[60] flex items-center justify-center bg-black/45 p-3 sm:p-4"
    @click.self="emit('close')"
  >
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="external-mcp-dialog-title"
      class="flex max-h-[92vh] w-full max-w-6xl flex-col overflow-hidden rounded-lg bg-white shadow-2xl dark:bg-gray-900"
    >
      <div class="flex items-center justify-between border-b border-gray-200 px-5 py-4 dark:border-gray-700">
        <div class="flex min-w-0 items-center gap-2">
          <ServerStackIcon class="h-5 w-5 shrink-0 text-primary" />
          <h2 id="external-mcp-dialog-title" class="truncate text-base font-semibold text-gray-900 dark:text-white">外部 MCP</h2>
          <span v-if="readyToolCount" class="rounded bg-green-50 px-2 py-1 text-xs text-green-700 dark:bg-green-950/30 dark:text-green-300">
            {{ readyToolCount }} 个已启用
          </span>
          <button
            v-if="projectScopeAvailable"
            type="button"
            title="管理 Agent 项目"
            class="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-800"
            @click="projectDialogOpen = true"
          >
            <UserGroupIcon class="h-4 w-4" />
          </button>
        </div>
        <button
          type="button"
          title="关闭"
          class="flex h-8 w-8 items-center justify-center rounded-full text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-800"
          @click="emit('close')"
        >
          <XMarkIcon class="h-5 w-5" />
        </button>
      </div>

      <div class="grid min-h-0 flex-1 overflow-y-auto lg:grid-cols-[280px_minmax(0,1fr)_320px]">
        <section class="border-b border-gray-200 p-4 dark:border-gray-700 lg:border-b-0 lg:border-r">
          <div class="mb-3 flex items-center justify-between">
            <h3 class="text-sm font-semibold text-gray-800 dark:text-gray-100">连接</h3>
            <button
              type="button"
              title="刷新连接"
              class="flex h-8 w-8 items-center justify-center rounded-md text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-800"
              @click="loadConnections"
            >
              <ArrowPathIcon class="h-4 w-4" :class="loading ? 'animate-spin' : ''" />
            </button>
          </div>

          <div v-if="loading && connections.length === 0" class="flex justify-center py-10 text-gray-400">
            <ArrowPathIcon class="h-5 w-5 animate-spin" />
          </div>
          <div v-else-if="connections.length === 0" class="py-10 text-center text-sm text-gray-400">暂无连接</div>
          <div v-else class="space-y-1">
            <div
              v-for="connection in connections"
              :key="connection.connection_id"
              class="group flex items-center gap-1 rounded-md border px-2 py-2"
              :class="selectedConnectionId === connection.connection_id
                ? 'border-primary bg-blue-50 dark:bg-blue-950/30'
                : 'border-transparent hover:bg-gray-50 dark:hover:bg-gray-800'"
            >
              <button type="button" class="min-w-0 flex-1 text-left" @click="selectConnection(connection.connection_id)">
                <span class="block truncate text-sm font-medium text-gray-800 dark:text-gray-100">{{ connection.name }}</span>
                <span class="block truncate text-xs text-gray-400">
                  {{ connection.scope === 'project' ? projectName(connection.project_id) : '个人' }} ·
                  {{ connection.credential_source === 'managed' ? '托管凭据' : '用户凭据' }} ·
                  {{ discoveryStatusLabel(connection.discovery_status) }} ·
                  <span :class="healthStatusClass(connection.health_status)" :title="healthStatusTitle(connection)">
                    {{ healthStatusLabel(connection.health_status) }}
                  </span>
                  · v{{ connection.revision }}
                </span>
              </button>
              <button
                v-if="canManageConnection(connection)"
                type="button"
                title="编辑连接"
                class="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-gray-500 hover:bg-white dark:hover:bg-gray-700"
                @click="editConnection(connection)"
              >
                <PencilSquareIcon class="h-4 w-4" />
              </button>
              <button
                v-if="canManageConnection(connection)"
                type="button"
                title="撤销连接"
                :disabled="action === `revoke:${connection.connection_id}`"
                class="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-red-500 hover:bg-red-50 disabled:opacity-50 dark:hover:bg-red-950/30"
                @click="revokeConnection(connection)"
              >
                <ArrowPathIcon v-if="action === `revoke:${connection.connection_id}`" class="h-4 w-4 animate-spin" />
                <TrashIcon v-else class="h-4 w-4" />
              </button>
            </div>
          </div>
        </section>

        <section class="min-h-[180px] border-b border-gray-200 p-4 dark:border-gray-700 sm:min-h-[240px] lg:min-h-[320px] lg:border-b-0 lg:border-r">
          <div v-if="!selectedConnection" class="flex h-full items-center justify-center text-sm text-gray-400">
            选择连接后查看工具
          </div>
          <template v-else>
            <div class="mb-4 flex flex-wrap items-center justify-between gap-2">
              <div class="min-w-0">
                <h3 class="truncate text-sm font-semibold text-gray-800 dark:text-gray-100">{{ selectedConnection.name }}</h3>
                <p class="truncate text-xs text-gray-400">{{ selectedConnection.endpoint }}</p>
                <p class="truncate text-xs text-gray-400">
                  {{ selectedConnection.scope === 'project' ? projectName(selectedConnection.project_id) : '个人连接' }}
                  <template v-if="selectedConnectionProject"> · {{ selectedConnectionProject.current_role }}</template>
                </p>
                <p
                  class="truncate text-xs"
                  :class="healthStatusClass(selectedConnection.health_status)"
                  :title="healthStatusTitle(selectedConnection)"
                >
                  {{ healthStatusLabel(selectedConnection.health_status) }}
                  <template v-if="selectedConnection.health_failure_count">
                    · 连续失败 {{ selectedConnection.health_failure_count }} 次
                  </template>
                </p>
              </div>
              <button
                type="button"
                :disabled="Boolean(action) || !canManageSelectedConnection"
                class="inline-flex items-center gap-2 rounded-md border border-gray-200 px-3 py-2 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
                @click="discoverTools"
              >
                <ArrowPathIcon v-if="action === 'discover'" class="h-4 w-4 animate-spin" />
                <CloudArrowDownIcon v-else class="h-4 w-4" />
                发现工具
              </button>
            </div>

            <div v-if="pendingSnapshot" class="mb-4 border-y border-amber-200 bg-amber-50 dark:border-amber-900 dark:bg-amber-950/20">
              <div class="flex items-center justify-between gap-3 px-3 py-3">
                <div class="min-w-0">
                  <p class="text-sm font-medium text-amber-800 dark:text-amber-200">待审核 Schema · {{ pendingSnapshot.tools.length }} 个工具</p>
                  <p class="truncate text-xs text-amber-700/70 dark:text-amber-300/70">{{ pendingSnapshot.schema_hash }}</p>
                </div>
                <button
                  type="button"
                  :disabled="Boolean(action)"
                  class="inline-flex shrink-0 items-center gap-1.5 rounded-md bg-amber-700 px-3 py-2 text-xs font-medium text-white hover:bg-amber-800 disabled:opacity-50"
                  @click="approveSnapshot"
                >
                  <ArrowPathIcon v-if="action === 'approve'" class="h-4 w-4 animate-spin" />
                  <ShieldCheckIcon v-else class="h-4 w-4" />
                  审核
                </button>
              </div>
              <div class="max-h-48 divide-y divide-amber-200 overflow-y-auto border-t border-amber-200 px-3 dark:divide-amber-900 dark:border-amber-900">
                <div v-for="tool in pendingSnapshot.tools" :key="tool.qualified_name" class="flex items-start justify-between gap-3 py-2.5">
                  <div class="min-w-0">
                    <p class="break-all text-xs font-medium text-amber-900 dark:text-amber-100">{{ tool.qualified_name }}</p>
                    <p v-if="tool.description" class="mt-0.5 line-clamp-2 text-xs leading-5 text-amber-800/70 dark:text-amber-200/70">{{ tool.description }}</p>
                  </div>
                  <span
                    class="shrink-0 rounded px-1.5 py-0.5 text-xs"
                    :class="reviewedToolBadge(tool)"
                  >
                    {{ reviewedToolLabel(tool) }}
                  </span>
                </div>
              </div>
            </div>

            <div v-if="loadingTools" class="flex justify-center py-12 text-gray-400">
              <ArrowPathIcon class="h-5 w-5 animate-spin" />
            </div>
            <div v-else-if="tools.length === 0" class="py-12 text-center text-sm text-gray-400">暂无已审核工具</div>
            <div v-else class="divide-y divide-gray-200 border-y border-gray-200 dark:divide-gray-700 dark:border-gray-700">
              <div v-for="tool in tools" :key="tool.schema.qualified_name" class="flex items-start gap-3 py-3">
                <div class="min-w-0 flex-1">
                  <div class="flex flex-wrap items-center gap-2">
                    <span class="break-all text-sm font-medium text-gray-800 dark:text-gray-100">{{ tool.schema.name }}</span>
                    <span
                      class="rounded px-1.5 py-0.5 text-xs"
                      :class="reviewedToolBadge(tool.schema)"
                    >
                      {{ activeToolLabel(tool) }}
                    </span>
                  </div>
                  <p v-if="tool.schema.description" class="mt-1 line-clamp-2 text-xs leading-5 text-gray-500 dark:text-gray-400">
                    {{ tool.schema.description }}
                  </p>
                  <p class="mt-1 break-all text-xs text-gray-400">{{ tool.schema.qualified_name }}</p>
                </div>
                <label class="relative mt-1 inline-flex cursor-pointer items-center">
                  <input
                    type="checkbox"
                    class="peer sr-only"
                    :checked="tool.policy.enabled"
                    :disabled="toolAction === tool.schema.qualified_name || !canManageSelectedConnection"
                    @change="setToolEnabled(tool, ($event.target as HTMLInputElement).checked)"
                  />
                  <span class="h-5 w-9 rounded-full bg-gray-300 transition-colors peer-checked:bg-primary peer-disabled:opacity-50 dark:bg-gray-700"></span>
                  <span class="absolute left-0.5 top-0.5 h-4 w-4 rounded-full bg-white transition-transform peer-checked:translate-x-4"></span>
                  <span class="sr-only">启用 {{ tool.schema.name }}</span>
                </label>
              </div>
            </div>
          </template>
        </section>

        <form class="space-y-4 p-4" @submit.prevent="saveConnection">
          <div class="flex items-center justify-between">
            <h3 class="text-sm font-semibold text-gray-800 dark:text-gray-100">{{ editingId ? '编辑连接' : '新建连接' }}</h3>
            <button v-if="editingId" type="button" class="text-xs text-gray-500 hover:text-gray-800 dark:hover:text-gray-200" @click="resetForm">
              取消编辑
            </button>
          </div>
          <label class="block space-y-1">
            <span class="text-xs font-medium text-gray-600 dark:text-gray-300">作用域</span>
            <select
              v-model="connectionScope"
              :disabled="Boolean(editingId)"
              class="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm outline-none focus:border-primary disabled:opacity-60 dark:border-gray-700 dark:bg-gray-950"
            >
              <option value="user">个人</option>
              <option v-if="projectScopeAvailable && manageableProjects.length" value="project">项目共享</option>
            </select>
          </label>
          <label v-if="connectionScope === 'project'" class="block space-y-1">
            <span class="text-xs font-medium text-gray-600 dark:text-gray-300">Agent 项目</span>
            <select
              v-model="projectId"
              :disabled="Boolean(editingId)"
              required
              class="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm outline-none focus:border-primary disabled:opacity-60 dark:border-gray-700 dark:bg-gray-950"
            >
              <option value="" disabled>选择项目</option>
              <option v-for="project in manageableProjects" :key="project.project_id" :value="project.project_id">
                {{ project.name }} · {{ project.current_role }}
              </option>
            </select>
          </label>
          <label class="block space-y-1">
            <span class="text-xs font-medium text-gray-600 dark:text-gray-300">名称</span>
            <input v-model="name" maxlength="80" required class="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm outline-none focus:border-primary dark:border-gray-700 dark:bg-gray-950" />
          </label>
          <label class="block space-y-1">
            <span class="text-xs font-medium text-gray-600 dark:text-gray-300">传输协议</span>
            <select v-model="transport" class="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm outline-none focus:border-primary dark:border-gray-700 dark:bg-gray-950">
              <option value="streamable_http">Streamable HTTP</option>
              <option value="sse">SSE</option>
            </select>
          </label>
          <label class="block space-y-1">
            <span class="text-xs font-medium text-gray-600 dark:text-gray-300">Endpoint</span>
            <input v-model="endpoint" type="url" required placeholder="https://mcp.example.com/mcp" class="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm outline-none focus:border-primary dark:border-gray-700 dark:bg-gray-950" />
          </label>
          <label class="block space-y-1">
            <span class="text-xs font-medium text-gray-600 dark:text-gray-300">认证</span>
            <select v-model="authType" class="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm outline-none focus:border-primary dark:border-gray-700 dark:bg-gray-950">
              <option value="none">无认证</option>
              <option value="bearer">Bearer Token</option>
            </select>
          </label>
          <label v-if="authType === 'bearer'" class="block space-y-1">
            <span class="text-xs font-medium text-gray-600 dark:text-gray-300">凭据来源</span>
            <select v-model="credentialSource" class="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm outline-none focus:border-primary dark:border-gray-700 dark:bg-gray-950">
              <option value="user">用户凭据</option>
              <option v-if="connectionScope === 'project'" value="managed">平台托管</option>
            </select>
          </label>
          <label v-if="authType === 'bearer' && credentialSource === 'managed'" class="block space-y-1">
            <span class="text-xs font-medium text-gray-600 dark:text-gray-300">托管凭据引用</span>
            <input
              v-model="managedCredentialRef"
              maxlength="96"
              required
              autocomplete="off"
              placeholder="team.research"
              class="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm outline-none focus:border-primary dark:border-gray-700 dark:bg-gray-950"
            />
          </label>
          <label v-if="authType === 'bearer' && credentialSource === 'user'" class="block space-y-1">
            <span class="text-xs font-medium text-gray-600 dark:text-gray-300">Bearer Token</span>
            <input
              v-model="bearerToken"
              type="password"
              autocomplete="new-password"
              :required="requiresUserBearerToken"
              :placeholder="requiresUserBearerToken ? 'Token' : '留空保留当前凭据'"
              class="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm outline-none focus:border-primary dark:border-gray-700 dark:bg-gray-950"
            />
          </label>
          <p v-if="projectNotice" class="text-xs text-amber-600">{{ projectNotice }}</p>
          <button
            v-if="projectScopeAvailable"
            type="button"
            class="inline-flex items-center gap-2 text-xs font-medium text-primary hover:text-blue-700"
            @click="projectDialogOpen = true"
          >
            <UserGroupIcon class="h-4 w-4" />
            管理项目与成员
          </button>
          <p v-if="errorMessage" class="text-sm text-red-600">{{ errorMessage }}</p>
          <button
            type="submit"
            :disabled="saving || !name.trim() || !endpoint.trim() || (connectionScope === 'project' && !projectId) || (credentialSource === 'managed' && !managedCredentialRef.trim()) || (requiresUserBearerToken && !bearerToken.trim())"
            class="inline-flex w-full items-center justify-center gap-2 rounded-md bg-primary px-4 py-2.5 text-sm font-medium text-white hover:bg-blue-600 disabled:cursor-not-allowed disabled:opacity-50"
          >
            <ArrowPathIcon v-if="saving" class="h-4 w-4 animate-spin" />
            <PlusIcon v-else-if="!editingId" class="h-4 w-4" />
            <CheckIcon v-else class="h-4 w-4" />
            {{ editingId ? '保存修改' : '添加连接' }}
          </button>
        </form>
      </div>
    </div>
  </div>
  <AgentProjectDialog
    :open="projectDialogOpen"
    @close="projectDialogOpen = false"
    @changed="loadProjects"
  />
</template>
