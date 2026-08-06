<script setup lang="ts">
import { ref, watch } from 'vue'
import {
  ArrowPathIcon,
  BuildingStorefrontIcon,
  MagnifyingGlassIcon,
  PuzzlePieceIcon,
  ServerStackIcon,
  ShieldCheckIcon,
  XMarkIcon,
} from '@heroicons/vue/24/outline'
import {
  listAgentMarketplaceExtensions,
  type AgentMarketplaceExtension,
  type AgentMarketplaceExtensionKind,
} from '../../api/agent'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ (event: 'close'): void }>()

type KindFilter = 'all' | AgentMarketplaceExtensionKind

const kindFilters: Array<{ value: KindFilter, label: string }> = [
  { value: 'all', label: '全部' },
  { value: 'skill', label: 'Skill' },
  { value: 'mcp_server', label: 'MCP Server' },
]

const permissionLabels: Record<string, string> = {
  network: '网络访问',
  user_data_read: '读取用户数据',
  user_data_write: '写入用户数据',
  external_write: '外部写入',
  credential_reference: '凭据引用',
}

const selectedKind = ref<KindFilter>('all')
const searchInput = ref('')
const appliedSearch = ref('')
const releases = ref<AgentMarketplaceExtension[]>([])
const nextCursor = ref('')
const hasMore = ref(false)
const loading = ref(false)
const loadingMore = ref(false)
const errorMessage = ref('')
const loadMoreError = ref('')

let requestSequence = 0

const apiError = (error: any) => {
  const status = Number(error?.response?.status || 0)
  if (status === 412 || status === 422) return '扩展市场尚未启用'
  if (status === 401) return '登录状态已失效'
  const detail = error?.response?.data?.error
  if (typeof detail === 'string' && detail.trim()) return detail
  return '扩展市场加载失败，请稍后重试'
}

const loadPage = async (append: boolean) => {
  if (loading.value || loadingMore.value) return
  const sequence = ++requestSequence
  if (append) loadingMore.value = true
  else loading.value = true
  errorMessage.value = ''
  loadMoreError.value = ''
  try {
    const response = await listAgentMarketplaceExtensions({
      kind: selectedKind.value === 'all' ? undefined : selectedKind.value,
      search: appliedSearch.value || undefined,
      after_cursor: append ? nextCursor.value : undefined,
      page_size: 20,
    })
    if (sequence !== requestSequence || !props.open) return
    const received = Array.isArray(response.data.releases) ? response.data.releases : []
    if (append) {
      const byID = new Map(releases.value.map(item => [item.release_id, item]))
      received.forEach(item => byID.set(item.release_id, item))
      releases.value = [...byID.values()]
    } else {
      releases.value = received
    }
    nextCursor.value = String(response.data.next_cursor || '')
    hasMore.value = Boolean(response.data.has_more && nextCursor.value)
  } catch (error: any) {
    if (sequence !== requestSequence || !props.open) return
    if (append) loadMoreError.value = apiError(error)
    else {
      releases.value = []
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
  releases.value = []
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
  if (appliedSearch.value === normalized && releases.value.length > 0) return
  appliedSearch.value = normalized
  reload()
}

const close = () => emit('close')

const kindIcon = (kind: AgentMarketplaceExtensionKind) => (
  kind === 'mcp_server' ? ServerStackIcon : PuzzlePieceIcon
)

const permissionLabel = (permission: string) => permissionLabels[permission] || permission

const publishedAt = (timestamp: number) => {
  if (!Number.isFinite(timestamp) || timestamp <= 0) return ''
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric', month: '2-digit', day: '2-digit',
  }).format(new Date(timestamp))
}

const shortDigest = (digest: string) => digest.length > 16
  ? `${digest.slice(0, 12)}...${digest.slice(-4)}`
  : digest

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
      aria-labelledby="agent-marketplace-title"
      @click.self="close"
    >
      <section class="flex max-h-[88vh] w-full max-w-4xl flex-col overflow-hidden rounded-lg bg-white shadow-2xl dark:bg-gray-900">
        <header class="flex items-center justify-between border-b border-gray-200 px-4 py-3 dark:border-gray-700 sm:px-5">
          <div class="min-w-0">
            <div class="flex items-center gap-2">
              <BuildingStorefrontIcon class="h-5 w-5 text-gray-500 dark:text-gray-300" />
              <h2 id="agent-marketplace-title" class="truncate text-base font-semibold text-gray-900 dark:text-white">
                扩展市场
              </h2>
            </div>
            <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">已加载 {{ releases.length }} 个可信版本</p>
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
                <span class="sr-only">搜索市场扩展</span>
                <MagnifyingGlassIcon class="pointer-events-none absolute left-3 top-2.5 h-4 w-4 text-gray-400" />
                <input
                  v-model="searchInput"
                  type="search"
                  maxlength="120"
                  placeholder="搜索名称、描述或能力"
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

          <div v-else-if="releases.length === 0" class="flex min-h-64 flex-col items-center justify-center gap-2 text-gray-400">
            <BuildingStorefrontIcon class="h-8 w-8" />
            <p class="text-sm">没有匹配的公开扩展</p>
          </div>

          <div v-else class="divide-y divide-gray-200 dark:divide-gray-800">
            <article v-for="release in releases" :key="release.release_id" class="flex gap-3 py-4 first:pt-1">
              <div class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300">
                <component :is="kindIcon(release.kind)" class="h-5 w-5" />
              </div>
              <div class="min-w-0 flex-1">
                <div class="flex min-w-0 flex-wrap items-center gap-2">
                  <h3 class="min-w-0 break-words text-sm font-semibold text-gray-900 dark:text-white">{{ release.display_name }}</h3>
                  <span class="inline-flex items-center gap-1 rounded border border-green-200 bg-green-50 px-1.5 py-0.5 text-[11px] text-green-700 dark:border-green-900 dark:bg-green-950 dark:text-green-300">
                    <ShieldCheckIcon class="h-3.5 w-3.5" />
                    已验证
                  </span>
                </div>
                <p class="mt-0.5 break-all font-mono text-[11px] text-gray-400">{{ release.package_id }} · {{ release.version }}</p>
                <p v-if="release.description" class="mt-2 text-sm leading-6 text-gray-600 dark:text-gray-300">{{ release.description }}</p>

                <div class="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs text-gray-500 dark:text-gray-400">
                  <span>{{ release.publisher.display_name }}</span>
                  <span v-if="publishedAt(release.published_at_unix_ms)">{{ publishedAt(release.published_at_unix_ms) }}</span>
                  <span class="font-mono" :title="release.artifact_digest_sha256">SHA-256 {{ shortDigest(release.artifact_digest_sha256) }}</span>
                  <span class="font-mono">Key {{ release.signature_key_id }}</span>
                </div>

                <div v-if="release.capability_ids.length" class="mt-2 flex flex-wrap gap-1.5 text-[11px] text-gray-500 dark:text-gray-400">
                  <span v-for="capability in release.capability_ids" :key="capability" class="rounded border border-gray-200 px-1.5 py-0.5 dark:border-gray-700">
                    {{ capability }}
                  </span>
                </div>
                <div v-if="release.requested_permissions.length" class="mt-1.5 flex flex-wrap gap-1.5 text-[11px] text-amber-700 dark:text-amber-300">
                  <span v-for="permission in release.requested_permissions" :key="permission" class="rounded border border-amber-200 px-1.5 py-0.5 dark:border-amber-900">
                    {{ permissionLabel(permission) }}
                  </span>
                </div>
              </div>
            </article>
          </div>

          <p v-if="loadMoreError" class="mt-3 text-center text-sm text-red-600 dark:text-red-400">{{ loadMoreError }}</p>

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
