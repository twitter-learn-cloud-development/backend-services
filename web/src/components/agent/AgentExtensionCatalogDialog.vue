<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  ArrowPathIcon,
  CheckCircleIcon,
  CircleStackIcon,
  Cog6ToothIcon,
  CpuChipIcon,
  MagnifyingGlassIcon,
  PuzzlePieceIcon,
  ServerStackIcon,
  XMarkIcon,
} from '@heroicons/vue/24/outline'
import {
  getAgentSkill,
  listAgentExtensions,
  type AgentExtension,
  type AgentExtensionKind,
  type AgentExtensionSourceStatus,
  type AgentSkill,
} from '../../api/agent'

const props = defineProps<{ open: boolean }>()

const emit = defineEmits<{
  (event: 'close'): void
  (event: 'use-skill', skill: AgentSkill): void
  (event: 'manage-mcp', connectionId: string): void
}>()

type KindFilter = 'all' | AgentExtensionKind

const kindFilters: Array<{ value: KindFilter, label: string }> = [
  { value: 'all', label: '全部' },
  { value: 'capability', label: '能力' },
  { value: 'skill', label: 'Skill' },
  { value: 'mcp_tool', label: 'MCP' },
]

const selectedKind = ref<KindFilter>('all')
const searchInput = ref('')
const appliedSearch = ref('')
const entries = ref<AgentExtension[]>([])
const sources = ref<AgentExtensionSourceStatus[]>([])
const nextCursor = ref('')
const hasMore = ref(false)
const loading = ref(false)
const loadingMore = ref(false)
const errorMessage = ref('')
const actionError = ref('')
const activatingExtensionId = ref('')

let requestSequence = 0

const sourceLabels: Record<AgentExtensionSourceStatus['source'], string> = {
  built_in: '内建能力',
  workflow: '工作流 Skill',
  external_mcp: '外部 MCP',
}

const categoryLabels: Record<AgentExtension['category'], string> = {
  general: '通用',
  workflow: '工作流',
  read: '只读',
  write: '写入',
  risky: '高风险',
}

const scopeLabels: Record<AgentExtension['scope'], string> = {
  platform: '平台',
  user: '个人',
  project: '项目',
}

const availableEntries = computed(() => entries.value.filter(item => item.status === 'available').length)

const apiError = (error: any) => {
  const status = Number(error?.response?.status || 0)
  if (status === 412 || status === 422) return '扩展目录尚未启用'
  if (status === 401) return '登录状态已失效'
  const detail = error?.response?.data?.error
  if (typeof detail === 'string' && detail.trim()) return detail
  return '扩展目录加载失败，请稍后重试'
}

const loadPage = async (append: boolean) => {
  if (loading.value || loadingMore.value) return
  const sequence = ++requestSequence
  if (append) loadingMore.value = true
  else loading.value = true
  errorMessage.value = ''
  actionError.value = ''
  try {
    const response = await listAgentExtensions({
      kind: selectedKind.value === 'all' ? undefined : selectedKind.value,
      search: appliedSearch.value || undefined,
      after_cursor: append ? nextCursor.value : undefined,
      page_size: 20,
    })
    if (sequence !== requestSequence || !props.open) return
    const received = Array.isArray(response.data.extensions) ? response.data.extensions : []
    if (append) {
      const byID = new Map(entries.value.map(item => [item.extension_id, item]))
      received.forEach(item => byID.set(item.extension_id, item))
      entries.value = [...byID.values()]
    } else {
      entries.value = received
    }
    sources.value = Array.isArray(response.data.sources) ? response.data.sources : []
    nextCursor.value = String(response.data.next_cursor || '')
    hasMore.value = Boolean(response.data.has_more && nextCursor.value)
  } catch (error: any) {
    if (sequence !== requestSequence || !props.open) return
    if (append) actionError.value = apiError(error)
    else {
      entries.value = []
      errorMessage.value = apiError(error)
    }
  } finally {
    if (sequence === requestSequence) {
      loading.value = false
      loadingMore.value = false
    }
  }
}

const reload = () => {
  requestSequence += 1
  loading.value = false
  loadingMore.value = false
  entries.value = []
  sources.value = []
  nextCursor.value = ''
  hasMore.value = false
  void loadPage(false)
}

const applyKind = (kind: KindFilter) => {
  if (selectedKind.value === kind) return
  selectedKind.value = kind
  reload()
}

const submitSearch = () => {
  const normalized = searchInput.value.trim()
  if (appliedSearch.value === normalized && entries.value.length > 0) return
  appliedSearch.value = normalized
  reload()
}

const activateSkill = async (entry: AgentExtension) => {
  if (!entry.skill || entry.status !== 'available' || activatingExtensionId.value) return
  activatingExtensionId.value = entry.extension_id
  actionError.value = ''
  try {
    const response = await getAgentSkill(entry.skill.skill_id, entry.skill.version)
    if (!response.data.skill) throw new Error('empty skill response')
    emit('use-skill', response.data.skill)
  } catch (error: any) {
    actionError.value = apiError(error)
  } finally {
    activatingExtensionId.value = ''
  }
}

const close = () => emit('close')

const kindIcon = (kind: AgentExtensionKind) => {
  if (kind === 'skill') return PuzzlePieceIcon
  if (kind === 'mcp_tool') return ServerStackIcon
  return CpuChipIcon
}

watch(() => props.open, (open) => {
  if (!open) {
    requestSequence += 1
    loading.value = false
    loadingMore.value = false
    return
  }
  selectedKind.value = 'all'
  searchInput.value = ''
  appliedSearch.value = ''
  reload()
})
</script>

<template>
  <Teleport to="body">
    <div
      v-if="open"
      class="fixed inset-0 z-[70] flex items-center justify-center bg-black/45 p-3 sm:p-6"
      role="dialog"
      aria-modal="true"
      aria-labelledby="agent-extension-title"
      @click.self="close"
    >
      <section class="flex max-h-[88vh] w-full max-w-4xl flex-col overflow-hidden rounded-lg bg-white shadow-2xl dark:bg-gray-900">
        <header class="flex items-center justify-between border-b border-gray-200 px-4 py-3 dark:border-gray-700 sm:px-5">
          <div class="min-w-0">
            <h2 id="agent-extension-title" class="truncate text-base font-semibold text-gray-900 dark:text-white">
              扩展目录
            </h2>
            <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
              {{ availableEntries }} 项可用
            </p>
          </div>
          <button
            type="button"
            title="关闭"
            class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-gray-500 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800"
            @click="close"
          >
            <XMarkIcon class="h-5 w-5" />
          </button>
        </header>

        <div class="border-b border-gray-200 px-4 py-3 dark:border-gray-700 sm:px-5">
          <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div class="inline-flex w-full overflow-hidden rounded-lg border border-gray-200 dark:border-gray-700 sm:w-auto">
              <button
                v-for="filter in kindFilters"
                :key="filter.value"
                type="button"
                class="min-w-0 flex-1 border-r border-gray-200 px-3 py-2 text-xs font-medium last:border-r-0 dark:border-gray-700 sm:flex-none"
                :class="selectedKind === filter.value
                  ? 'bg-gray-900 text-white dark:bg-white dark:text-gray-900'
                  : 'bg-white text-gray-600 hover:bg-gray-50 dark:bg-gray-900 dark:text-gray-300 dark:hover:bg-gray-800'"
                @click="applyKind(filter.value)"
              >
                {{ filter.label }}
              </button>
            </div>
            <form class="flex min-w-0 flex-1 sm:max-w-sm" @submit.prevent="submitSearch">
              <label class="relative min-w-0 flex-1">
                <span class="sr-only">搜索扩展</span>
                <MagnifyingGlassIcon class="pointer-events-none absolute left-3 top-2.5 h-4 w-4 text-gray-400" />
                <input
                  v-model="searchInput"
                  type="search"
                  maxlength="120"
                  placeholder="搜索名称或能力"
                  class="h-9 w-full rounded-l-lg border border-gray-300 bg-white pl-9 pr-3 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary dark:border-gray-700 dark:bg-gray-950"
                />
              </label>
              <button
                type="submit"
                title="搜索"
                class="flex h-9 w-10 items-center justify-center rounded-r-lg bg-gray-900 text-white hover:bg-gray-700 dark:bg-white dark:text-gray-900 dark:hover:bg-gray-200"
              >
                <MagnifyingGlassIcon class="h-4 w-4" />
              </button>
            </form>
          </div>

          <div v-if="sources.length" class="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-500 dark:text-gray-400">
            <span v-for="source in sources" :key="source.source" class="inline-flex items-center gap-1.5">
              <CircleStackIcon class="h-3.5 w-3.5" />
              {{ sourceLabels[source.source] }}
              <span :class="source.state === 'ready' ? 'text-green-600 dark:text-green-400' : 'text-gray-400'">
                {{ source.state === 'ready' ? source.entry_count : '未启用' }}
              </span>
            </span>
          </div>
        </div>

        <main class="min-h-0 flex-1 overflow-y-auto px-4 py-3 sm:px-5">
          <div v-if="loading" class="flex min-h-64 items-center justify-center text-gray-400">
            <ArrowPathIcon class="h-6 w-6 animate-spin" />
          </div>

          <div v-else-if="errorMessage" class="flex min-h-64 flex-col items-center justify-center gap-3 text-center">
            <p class="text-sm text-red-600 dark:text-red-400">{{ errorMessage }}</p>
            <button
              type="button"
              class="inline-flex items-center gap-1.5 rounded-lg border border-gray-300 px-3 py-2 text-sm font-medium hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-800"
              @click="reload"
            >
              <ArrowPathIcon class="h-4 w-4" />
              重试
            </button>
          </div>

          <div v-else-if="entries.length === 0" class="flex min-h-64 flex-col items-center justify-center gap-2 text-gray-400">
            <PuzzlePieceIcon class="h-8 w-8" />
            <p class="text-sm">没有匹配的扩展</p>
          </div>

          <div v-else class="divide-y divide-gray-200 dark:divide-gray-800">
            <article v-for="entry in entries" :key="entry.extension_id" class="flex gap-3 py-4 first:pt-1">
              <div class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300">
                <component :is="kindIcon(entry.kind)" class="h-5 w-5" />
              </div>
              <div class="min-w-0 flex-1">
                <div class="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
                  <div class="min-w-0">
                    <div class="flex min-w-0 flex-wrap items-center gap-2">
                      <h3 class="truncate text-sm font-semibold text-gray-900 dark:text-white">{{ entry.display_name }}</h3>
                      <span
                        class="rounded border px-1.5 py-0.5 text-[11px]"
                        :class="entry.status === 'available'
                          ? 'border-green-200 bg-green-50 text-green-700 dark:border-green-900 dark:bg-green-950 dark:text-green-300'
                          : 'border-gray-200 bg-gray-50 text-gray-500 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-400'"
                      >
                        {{ entry.status === 'available' ? '可用' : '规划中' }}
                      </span>
                    </div>
                    <p class="mt-0.5 break-all font-mono text-[11px] text-gray-400">{{ entry.name }} · {{ entry.version }}</p>
                  </div>

                  <button
                    v-if="entry.kind === 'skill' && entry.skill"
                    type="button"
                    :disabled="entry.status !== 'available' || Boolean(activatingExtensionId)"
                    class="inline-flex h-8 shrink-0 items-center justify-center gap-1.5 rounded-lg bg-primary px-3 text-xs font-semibold text-white hover:bg-blue-600 disabled:cursor-not-allowed disabled:opacity-50"
                    @click="activateSkill(entry)"
                  >
                    <ArrowPathIcon v-if="activatingExtensionId === entry.extension_id" class="h-4 w-4 animate-spin" />
                    <CheckCircleIcon v-else class="h-4 w-4" />
                    使用
                  </button>
                  <button
                    v-else-if="entry.kind === 'mcp_tool' && entry.mcp"
                    type="button"
                    class="inline-flex h-8 shrink-0 items-center justify-center gap-1.5 rounded-lg border border-gray-300 px-3 text-xs font-semibold text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
                    @click="emit('manage-mcp', entry.mcp.connection_id)"
                  >
                    <Cog6ToothIcon class="h-4 w-4" />
                    管理
                  </button>
                </div>

                <p v-if="entry.description" class="mt-2 text-sm leading-6 text-gray-600 dark:text-gray-300">
                  {{ entry.description }}
                </p>
                <div class="mt-2 flex flex-wrap gap-1.5 text-[11px] text-gray-500 dark:text-gray-400">
                  <span class="rounded border border-gray-200 px-1.5 py-0.5 dark:border-gray-700">{{ categoryLabels[entry.category] }}</span>
                  <span class="rounded border border-gray-200 px-1.5 py-0.5 dark:border-gray-700">{{ scopeLabels[entry.scope] }}</span>
                  <span v-if="entry.approval_mode === 'required'" class="rounded border border-amber-200 px-1.5 py-0.5 text-amber-700 dark:border-amber-900 dark:text-amber-300">需审批</span>
                  <span v-if="entry.health_status === 'healthy'" class="rounded border border-green-200 px-1.5 py-0.5 text-green-700 dark:border-green-900 dark:text-green-300">健康</span>
                  <span v-else-if="entry.health_status === 'degraded' || entry.health_status === 'unhealthy'" class="rounded border border-red-200 px-1.5 py-0.5 text-red-700 dark:border-red-900 dark:text-red-300">连接异常</span>
                </div>
              </div>
            </article>
          </div>

          <p v-if="actionError" class="mt-3 text-center text-sm text-red-600 dark:text-red-400">{{ actionError }}</p>

          <div v-if="hasMore && !loading" class="flex justify-center py-4">
            <button
              type="button"
              :disabled="loadingMore"
              class="inline-flex items-center gap-1.5 rounded-lg border border-gray-300 px-4 py-2 text-sm font-medium hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-gray-700 dark:hover:bg-gray-800"
              @click="loadPage(true)"
            >
              <ArrowPathIcon v-if="loadingMore" class="h-4 w-4 animate-spin" />
              <span>{{ loadingMore ? '加载中' : '加载更多' }}</span>
            </button>
          </div>
        </main>
      </section>
    </div>
  </Teleport>
</template>
