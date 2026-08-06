<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  ArrowLeftIcon,
  ArrowPathIcon,
  ArrowUpTrayIcon,
  CheckCircleIcon,
  ExclamationTriangleIcon,
  KeyIcon,
  NoSymbolIcon,
  PlusIcon,
  ShieldCheckIcon,
  XMarkIcon,
} from '@heroicons/vue/24/outline'
import {
  getAgentMarketplaceManagementAccess,
  listAgentMarketplaceAuditEvents,
  listAgentMarketplaceManagedReleases,
  listAgentMarketplacePublishers,
  publishAgentMarketplaceRelease,
  registerAgentMarketplacePublisher,
  revokeAgentMarketplacePublisherKey,
  rotateAgentMarketplacePublisherKey,
  setAgentMarketplacePublisherVerification,
  withdrawAgentMarketplaceRelease,
  type AgentMarketplaceAuditEvent,
  type AgentMarketplaceManagedPublisher,
  type AgentMarketplaceManagedRelease,
  type AgentMarketplaceManagementAccess,
  type AgentMarketplacePublisherVerification,
  type AgentMarketplaceReleaseStatus,
} from '../../api/agent'

type Tab = 'publishers' | 'releases' | 'audits'
type Modal = 'register' | 'rotate' | 'publish' | 'withdraw' | null

const router = useRouter()
const activeTab = ref<Tab>('publishers')
const modal = ref<Modal>(null)
const access = ref<AgentMarketplaceManagementAccess | null>(null)
const publishers = ref<AgentMarketplaceManagedPublisher[]>([])
const releases = ref<AgentMarketplaceManagedRelease[]>([])
const audits = ref<AgentMarketplaceAuditEvent[]>([])
const selectedPublisher = ref<AgentMarketplaceManagedPublisher | null>(null)
const selectedRelease = ref<AgentMarketplaceManagedRelease | null>(null)
const publisherFilter = ref('')
const releaseStatus = ref<'' | AgentMarketplaceReleaseStatus>('')
const auditOutcome = ref('')
const loading = ref(true)
const saving = ref(false)
const errorMessage = ref('')
const actionError = ref('')
const page = ref(1)
const total = ref(0)
const pageSize = 20

const registerForm = reactive({
  publisherId: '', displayName: '', ownerUserIDs: '', keyId: '', publicKeyBase64: '',
})
const rotateForm = reactive({ keyId: '', publicKeyBase64: '' })
const publishForm = reactive({
  publisherId: '', packageId: '', kind: 'skill' as 'skill' | 'mcp_server', version: '',
  displayName: '', description: '', artifactDigest: '', capabilityIDs: '', permissions: '',
  signatureKeyId: '', signatureBase64: '',
})
const withdrawForm = reactive({ reasonCode: 'publisher_request' })

const tabs: Array<{ value: Tab, label: string }> = [
  { value: 'publishers', label: '发布者' },
  { value: 'releases', label: '版本' },
  { value: 'audits', label: '审计' },
]

const withdrawalReasons = [
  { value: 'publisher_request', label: '发布者请求' },
  { value: 'security_incident', label: '安全事件' },
  { value: 'policy_violation', label: '策略违规' },
  { value: 'superseded', label: '版本替代' },
  { value: 'artifact_unavailable', label: '制品不可用' },
]

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
const canManage = computed(() => Boolean(access.value?.platform_admin || access.value?.owned_publisher_ids?.length))
const activePublishers = computed(() => publishers.value.filter(item => item.verification === 'verified'))
const modalTitle = computed(() => ({
  register: '注册发布者', rotate: '轮换签名公钥', publish: '发布签名版本', withdraw: '撤回版本',
}[modal.value || 'register']))

const apiError = (error: any) => {
  const status = Number(error?.response?.status || 0)
  if (status === 403) return '当前账号没有此操作权限'
  if (status === 409) return '数据已被其他操作更新，请刷新后重试'
  if (status === 503) return '扩展市场管理尚未启用'
  const detail = error?.response?.data?.error
  return typeof detail === 'string' && detail.trim() ? detail : '请求失败，请稍后重试'
}

const splitValues = (value: string) => value.split(',').map(item => item.trim()).filter(Boolean)
const shortValue = (value: string, size = 18) => value.length > size ? `${value.slice(0, size)}...` : value
const formatTime = (value: number) => value > 0
  ? new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
  : '-'

const loadAccess = async () => {
  const response = await getAgentMarketplaceManagementAccess()
  access.value = response.data
}

const loadPublishers = async () => {
  const response = await listAgentMarketplacePublishers({ page: page.value, page_size: pageSize })
  publishers.value = Array.isArray(response.data.publishers) ? response.data.publishers : []
  total.value = Number(response.data.total || 0)
}

const loadReleases = async () => {
  const response = await listAgentMarketplaceManagedReleases({
    publisher_id: publisherFilter.value || undefined,
    status: releaseStatus.value || undefined,
    page: page.value,
    page_size: pageSize,
  })
  releases.value = Array.isArray(response.data.releases) ? response.data.releases : []
  total.value = Number(response.data.total || 0)
}

const loadAudits = async () => {
  const response = await listAgentMarketplaceAuditEvents({
    publisher_id: publisherFilter.value || undefined,
    outcome: auditOutcome.value || undefined,
    page: page.value,
    page_size: pageSize,
  })
  audits.value = Array.isArray(response.data.events) ? response.data.events : []
  total.value = Number(response.data.total || 0)
}

const reload = async () => {
  loading.value = true
  errorMessage.value = ''
  try {
    if (!access.value) await loadAccess()
    if (!canManage.value) {
      total.value = 0
      return
    }
    if (activeTab.value === 'publishers') await loadPublishers()
    else if (activeTab.value === 'releases') await loadReleases()
    else await loadAudits()
  } catch (error: any) {
    errorMessage.value = apiError(error)
  } finally {
    loading.value = false
  }
}

const resetModal = () => {
  modal.value = null
  selectedPublisher.value = null
  selectedRelease.value = null
  actionError.value = ''
}

const openRegister = () => {
  Object.assign(registerForm, { publisherId: '', displayName: '', ownerUserIDs: '', keyId: '', publicKeyBase64: '' })
  actionError.value = ''
  modal.value = 'register'
}

const openRotate = (publisher: AgentMarketplaceManagedPublisher) => {
  selectedPublisher.value = publisher
  Object.assign(rotateForm, { keyId: '', publicKeyBase64: '' })
  actionError.value = ''
  modal.value = 'rotate'
}

const openPublish = () => {
  const publisher = activePublishers.value.find(item => item.publisher_id === publisherFilter.value) || activePublishers.value[0]
  Object.assign(publishForm, {
    publisherId: publisher?.publisher_id || '', packageId: '', kind: 'skill', version: '',
    displayName: '', description: '', artifactDigest: '', capabilityIDs: '', permissions: '',
    signatureKeyId: publisher?.signing_keys.find(key => key.status === 'active')?.key_id || '', signatureBase64: '',
  })
  actionError.value = ''
  modal.value = 'publish'
}

const openWithdraw = (release: AgentMarketplaceManagedRelease) => {
  selectedRelease.value = release
  withdrawForm.reasonCode = 'publisher_request'
  actionError.value = ''
  modal.value = 'withdraw'
}

const submitRegister = async () => {
  await registerAgentMarketplacePublisher({
    publisher_id: registerForm.publisherId,
    display_name: registerForm.displayName,
    owner_user_ids: splitValues(registerForm.ownerUserIDs),
    initial_key_id: registerForm.keyId,
    public_key_base64: registerForm.publicKeyBase64,
  })
}

const submitRotate = async () => {
  if (!selectedPublisher.value) return
  await rotateAgentMarketplacePublisherKey(selectedPublisher.value.publisher_id, {
    key_id: rotateForm.keyId,
    public_key_base64: rotateForm.publicKeyBase64,
    expected_revision: selectedPublisher.value.revision,
  })
}

const submitPublish = async () => {
  const publisher = publishers.value.find(item => item.publisher_id === publishForm.publisherId)
  if (!publisher) throw new Error('publisher_not_loaded')
  await publishAgentMarketplaceRelease({
    manifest: {
      contract_version: 'agent.extension_manifest.v1',
      package_id: publishForm.packageId,
      kind: publishForm.kind,
      version: publishForm.version,
      publisher_id: publishForm.publisherId,
      display_name: publishForm.displayName,
      description: publishForm.description,
      artifact_digest_sha256: publishForm.artifactDigest,
      capability_ids: splitValues(publishForm.capabilityIDs),
      requested_permissions: splitValues(publishForm.permissions),
    },
    signature_key_id: publishForm.signatureKeyId,
    signature_base64: publishForm.signatureBase64,
    expected_publisher_revision: publisher.revision,
  })
}

const submitWithdraw = async () => {
  if (!selectedRelease.value) return
  await withdrawAgentMarketplaceRelease(selectedRelease.value.release_id, {
    reason_code: withdrawForm.reasonCode,
    expected_revision: selectedRelease.value.revision,
  })
}

const submitModal = async () => {
  saving.value = true
  actionError.value = ''
  try {
    if (modal.value === 'register') await submitRegister()
    else if (modal.value === 'rotate') await submitRotate()
    else if (modal.value === 'publish') await submitPublish()
    else if (modal.value === 'withdraw') await submitWithdraw()
    resetModal()
    await reload()
  } catch (error: any) {
    actionError.value = error?.message === 'publisher_not_loaded' ? '请先刷新发布者列表' : apiError(error)
  } finally {
    saving.value = false
  }
}

const revokeKey = async (publisher: AgentMarketplaceManagedPublisher, keyId: string) => {
  if (!window.confirm(`确认吊销签名公钥 ${keyId}？历史版本将不再通过验签。`)) return
  try {
    await revokeAgentMarketplacePublisherKey(publisher.publisher_id, keyId, publisher.revision)
    await reload()
  } catch (error: any) {
    errorMessage.value = apiError(error)
  }
}

const toggleVerification = async (publisher: AgentMarketplaceManagedPublisher) => {
  const verification: AgentMarketplacePublisherVerification = publisher.verification === 'verified' ? 'suspended' : 'verified'
  try {
    await setAgentMarketplacePublisherVerification(publisher.publisher_id, verification, publisher.revision)
    await reload()
  } catch (error: any) {
    errorMessage.value = apiError(error)
  }
}

const changePage = (next: number) => {
  if (next < 1 || next > totalPages.value) return
  page.value = next
  void reload()
}

watch(activeTab, () => {
  page.value = 1
  publisherFilter.value = ''
  void reload()
})

watch(() => publishForm.publisherId, (publisherID) => {
  const publisher = publishers.value.find(item => item.publisher_id === publisherID)
  publishForm.signatureKeyId = publisher?.signing_keys.find(key => key.status === 'active')?.key_id || ''
})

onMounted(() => { void reload() })
</script>

<template>
  <div class="min-h-screen bg-white text-gray-900 dark:bg-gray-950 dark:text-gray-100">
    <header class="border-b border-gray-200 dark:border-gray-800">
      <div class="mx-auto flex min-h-16 max-w-7xl items-center justify-between gap-3 px-4 sm:px-6">
        <div class="flex min-w-0 items-center gap-3">
          <button type="button" title="返回智能体" class="flex h-9 w-9 items-center justify-center rounded-lg text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-900" @click="router.push('/agent')">
            <ArrowLeftIcon class="h-5 w-5" />
          </button>
          <div class="min-w-0">
            <h1 class="truncate text-base font-semibold">扩展发布控制台</h1>
            <p class="truncate text-xs text-gray-500">签名发布与生命周期治理</p>
          </div>
        </div>
        <div v-if="access" class="flex items-center gap-2 text-xs">
          <span v-if="access.platform_admin" class="rounded border border-blue-200 bg-blue-50 px-2 py-1 text-blue-700 dark:border-blue-900 dark:bg-blue-950 dark:text-blue-300">平台管理员</span>
          <span v-else class="rounded border border-gray-200 px-2 py-1 text-gray-600 dark:border-gray-700 dark:text-gray-300">发布者所有者</span>
        </div>
      </div>
    </header>

    <main class="mx-auto max-w-7xl px-4 py-5 sm:px-6">
      <div v-if="loading" class="flex min-h-72 items-center justify-center text-gray-400"><ArrowPathIcon class="h-6 w-6 animate-spin" /></div>
      <div v-else-if="errorMessage && !access" class="flex min-h-72 flex-col items-center justify-center gap-3 text-center">
        <ExclamationTriangleIcon class="h-7 w-7 text-amber-500" />
        <p class="text-sm text-gray-600 dark:text-gray-300">{{ errorMessage }}</p>
        <button type="button" class="rounded-lg border border-gray-300 px-3 py-2 text-sm dark:border-gray-700" @click="reload">重试</button>
      </div>
      <div v-else-if="!canManage" class="flex min-h-72 flex-col items-center justify-center gap-2 text-center text-gray-500">
        <NoSymbolIcon class="h-8 w-8" />
        <p class="text-sm">当前账号没有可管理的发布者</p>
      </div>

      <template v-else>
        <div class="flex flex-col gap-4 border-b border-gray-200 pb-4 dark:border-gray-800 sm:flex-row sm:items-center sm:justify-between">
          <div class="inline-flex overflow-hidden rounded-lg border border-gray-200 dark:border-gray-700">
            <button v-for="tab in tabs" :key="tab.value" type="button" class="border-r border-gray-200 px-4 py-2 text-sm last:border-r-0 dark:border-gray-700" :class="activeTab === tab.value ? 'bg-gray-900 text-white dark:bg-white dark:text-gray-900' : 'text-gray-600 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-900'" @click="activeTab = tab.value">
              {{ tab.label }}
            </button>
          </div>
          <div class="flex items-center gap-2">
            <button type="button" title="刷新" class="flex h-9 w-9 items-center justify-center rounded-lg border border-gray-200 text-gray-500 hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-900" @click="reload"><ArrowPathIcon class="h-4 w-4" /></button>
            <button v-if="activeTab === 'publishers' && access?.platform_admin" type="button" class="inline-flex items-center gap-1.5 rounded-lg bg-gray-900 px-3 py-2 text-sm font-medium text-white dark:bg-white dark:text-gray-900" @click="openRegister"><PlusIcon class="h-4 w-4" />注册发布者</button>
            <button v-if="activeTab === 'releases'" type="button" class="inline-flex items-center gap-1.5 rounded-lg bg-gray-900 px-3 py-2 text-sm font-medium text-white dark:bg-white dark:text-gray-900" @click="openPublish"><ArrowUpTrayIcon class="h-4 w-4" />发布版本</button>
          </div>
        </div>

        <p v-if="errorMessage" class="mt-4 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">{{ errorMessage }}</p>

        <div v-if="activeTab !== 'publishers'" class="flex flex-wrap gap-2 border-b border-gray-200 py-4 dark:border-gray-800">
          <select v-model="publisherFilter" class="h-9 rounded-lg border border-gray-300 bg-white px-3 text-sm dark:border-gray-700 dark:bg-gray-950" @change="page = 1; reload()">
            <option value="">全部发布者</option>
            <option v-for="publisher in publishers" :key="publisher.publisher_id" :value="publisher.publisher_id">{{ publisher.display_name }}</option>
          </select>
          <select v-if="activeTab === 'releases'" v-model="releaseStatus" class="h-9 rounded-lg border border-gray-300 bg-white px-3 text-sm dark:border-gray-700 dark:bg-gray-950" @change="page = 1; reload()">
            <option value="">全部状态</option><option value="published">已发布</option><option value="withdrawn">已撤回</option>
          </select>
          <select v-else v-model="auditOutcome" class="h-9 rounded-lg border border-gray-300 bg-white px-3 text-sm dark:border-gray-700 dark:bg-gray-950" @change="page = 1; reload()">
            <option value="">全部结果</option><option value="requested">已请求</option><option value="succeeded">成功</option><option value="failed">失败</option>
          </select>
        </div>

        <section v-if="activeTab === 'publishers'" class="divide-y divide-gray-200 dark:divide-gray-800">
          <article v-for="publisher in publishers" :key="publisher.publisher_id" class="py-5">
            <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <h2 class="font-semibold">{{ publisher.display_name }}</h2>
                  <span class="rounded border px-1.5 py-0.5 text-[11px]" :class="publisher.verification === 'verified' ? 'border-green-200 text-green-700 dark:border-green-900 dark:text-green-300' : 'border-red-200 text-red-700 dark:border-red-900 dark:text-red-300'">{{ publisher.verification === 'verified' ? '已验证' : '已暂停' }}</span>
                  <span class="text-xs text-gray-400">rev {{ publisher.revision }}</span>
                </div>
                <p class="mt-1 font-mono text-xs text-gray-500">{{ publisher.publisher_id }}</p>
                <p class="mt-2 text-xs text-gray-500">所有者 {{ publisher.owner_user_ids.join(', ') }}</p>
              </div>
              <div class="flex shrink-0 gap-2">
                <button type="button" title="轮换公钥" class="flex h-9 w-9 items-center justify-center rounded-lg border border-gray-200 hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-900" @click="openRotate(publisher)"><KeyIcon class="h-4 w-4" /></button>
                <button v-if="access?.platform_admin" type="button" :title="publisher.verification === 'verified' ? '暂停发布者' : '恢复发布者'" class="flex h-9 w-9 items-center justify-center rounded-lg border border-gray-200 hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-900" @click="toggleVerification(publisher)"><component :is="publisher.verification === 'verified' ? NoSymbolIcon : ShieldCheckIcon" class="h-4 w-4" /></button>
              </div>
            </div>
            <div class="mt-4 overflow-x-auto">
              <table class="w-full min-w-[680px] text-left text-xs">
                <thead class="text-gray-400"><tr><th class="pb-2 font-medium">Key ID</th><th class="pb-2 font-medium">算法</th><th class="pb-2 font-medium">公钥</th><th class="pb-2 font-medium">状态</th><th class="pb-2 text-right font-medium">操作</th></tr></thead>
                <tbody>
                  <tr v-for="key in publisher.signing_keys" :key="key.key_id" class="border-t border-gray-100 dark:border-gray-900">
                    <td class="py-2 font-mono">{{ key.key_id }}</td><td class="py-2">{{ key.algorithm }}</td><td class="py-2 font-mono text-gray-500" :title="key.public_key_base64">{{ shortValue(key.public_key_base64, 30) }}</td><td class="py-2">{{ key.status }}</td>
                    <td class="py-2 text-right"><button v-if="key.status !== 'revoked'" type="button" class="text-red-600 hover:underline dark:text-red-400" @click="revokeKey(publisher, key.key_id)">吊销</button></td>
                  </tr>
                </tbody>
              </table>
            </div>
          </article>
          <p v-if="publishers.length === 0" class="py-20 text-center text-sm text-gray-400">暂无发布者</p>
        </section>

        <section v-else-if="activeTab === 'releases'" class="divide-y divide-gray-200 dark:divide-gray-800">
          <article v-for="release in releases" :key="release.release_id" class="flex flex-col gap-3 py-5 sm:flex-row sm:items-start sm:justify-between">
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2"><h2 class="font-semibold">{{ release.manifest.display_name }}</h2><span class="rounded border px-1.5 py-0.5 text-[11px]" :class="release.status === 'published' ? 'border-green-200 text-green-700 dark:border-green-900 dark:text-green-300' : 'border-gray-300 text-gray-500 dark:border-gray-700'">{{ release.status === 'published' ? '已发布' : '已撤回' }}</span><span class="text-xs text-gray-400">rev {{ release.revision }}</span></div>
              <p class="mt-1 break-all font-mono text-xs text-gray-500">{{ release.manifest.package_id }} · {{ release.manifest.version }}</p>
              <p class="mt-2 text-sm text-gray-600 dark:text-gray-300">{{ release.manifest.description }}</p>
              <div class="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-500"><span>Key {{ release.signature_key_id }}</span><span>SHA256 {{ shortValue(release.manifest.artifact_digest_sha256, 24) }}</span><span>{{ formatTime(release.published_at_unix_ms) }}</span></div>
            </div>
            <button v-if="release.status === 'published'" type="button" class="shrink-0 rounded-lg border border-red-200 px-3 py-2 text-sm text-red-600 hover:bg-red-50 dark:border-red-900 dark:text-red-400 dark:hover:bg-red-950" @click="openWithdraw(release)">撤回</button>
          </article>
          <p v-if="releases.length === 0" class="py-20 text-center text-sm text-gray-400">暂无版本</p>
        </section>

        <section v-else class="overflow-x-auto py-4">
          <table class="w-full min-w-[860px] text-left text-sm">
            <thead class="border-b border-gray-200 text-xs text-gray-400 dark:border-gray-800"><tr><th class="pb-3 font-medium">时间</th><th class="pb-3 font-medium">操作</th><th class="pb-3 font-medium">结果</th><th class="pb-3 font-medium">发布者 / 版本</th><th class="pb-3 font-medium">操作者</th><th class="pb-3 font-medium">错误码</th></tr></thead>
            <tbody class="divide-y divide-gray-100 dark:divide-gray-900"><tr v-for="event in audits" :key="event.event_id"><td class="py-3 text-xs text-gray-500">{{ formatTime(event.created_at_unix_ms) }}</td><td class="py-3 font-mono text-xs">{{ event.action }}</td><td class="py-3"><span :class="event.outcome === 'failed' ? 'text-red-600 dark:text-red-400' : event.outcome === 'succeeded' ? 'text-green-700 dark:text-green-300' : 'text-gray-500'">{{ event.outcome }}</span></td><td class="py-3"><div>{{ event.publisher_id }}</div><div v-if="event.package_id" class="font-mono text-xs text-gray-500">{{ event.package_id }} {{ event.version }}</div></td><td class="py-3 font-mono text-xs">{{ event.actor_user_id }}</td><td class="py-3 font-mono text-xs text-red-500">{{ event.error_code || '-' }}</td></tr></tbody>
          </table>
          <p v-if="audits.length === 0" class="py-20 text-center text-sm text-gray-400">暂无审计事件</p>
        </section>

        <footer v-if="total > pageSize" class="flex items-center justify-between border-t border-gray-200 py-4 text-sm dark:border-gray-800">
          <span class="text-gray-500">第 {{ page }} / {{ totalPages }} 页，共 {{ total }} 条</span>
          <div class="flex gap-2"><button type="button" :disabled="page <= 1" class="rounded-lg border border-gray-300 px-3 py-1.5 disabled:opacity-40 dark:border-gray-700" @click="changePage(page - 1)">上一页</button><button type="button" :disabled="page >= totalPages" class="rounded-lg border border-gray-300 px-3 py-1.5 disabled:opacity-40 dark:border-gray-700" @click="changePage(page + 1)">下一页</button></div>
        </footer>
      </template>
    </main>

    <Teleport to="body">
      <div v-if="modal" class="fixed inset-0 z-[80] flex items-center justify-center bg-black/45 p-4" @click.self="resetModal">
        <form class="max-h-[90vh] w-full max-w-2xl overflow-y-auto rounded-lg bg-white shadow-2xl dark:bg-gray-900" @submit.prevent="submitModal">
          <header class="flex items-center justify-between border-b border-gray-200 px-5 py-4 dark:border-gray-800"><h2 class="font-semibold">{{ modalTitle }}</h2><button type="button" title="关闭" class="flex h-8 w-8 items-center justify-center rounded-lg hover:bg-gray-100 dark:hover:bg-gray-800" @click="resetModal"><XMarkIcon class="h-5 w-5" /></button></header>
          <div class="grid gap-4 p-5 sm:grid-cols-2">
            <template v-if="modal === 'register'">
              <label class="text-sm">Publisher ID<input v-model="registerForm.publisherId" required maxlength="128" class="mt-1 h-10 w-full rounded-lg border border-gray-300 bg-transparent px-3 dark:border-gray-700" /></label>
              <label class="text-sm">显示名称<input v-model="registerForm.displayName" required maxlength="120" class="mt-1 h-10 w-full rounded-lg border border-gray-300 bg-transparent px-3 dark:border-gray-700" /></label>
              <label class="text-sm sm:col-span-2">所有者用户 ID（逗号分隔）<input v-model="registerForm.ownerUserIDs" required class="mt-1 h-10 w-full rounded-lg border border-gray-300 bg-transparent px-3 dark:border-gray-700" /></label>
              <label class="text-sm">初始 Key ID<input v-model="registerForm.keyId" required class="mt-1 h-10 w-full rounded-lg border border-gray-300 bg-transparent px-3 dark:border-gray-700" /></label>
              <label class="text-sm sm:col-span-2">Ed25519 公钥（Base64 Raw）<textarea v-model="registerForm.publicKeyBase64" required rows="3" class="mt-1 w-full rounded-lg border border-gray-300 bg-transparent px-3 py-2 font-mono text-xs dark:border-gray-700"></textarea></label>
            </template>
            <template v-else-if="modal === 'rotate'">
              <p class="sm:col-span-2 text-sm text-gray-500">{{ selectedPublisher?.display_name }} · rev {{ selectedPublisher?.revision }}</p>
              <label class="text-sm">新 Key ID<input v-model="rotateForm.keyId" required class="mt-1 h-10 w-full rounded-lg border border-gray-300 bg-transparent px-3 dark:border-gray-700" /></label>
              <label class="text-sm sm:col-span-2">Ed25519 公钥（Base64 Raw）<textarea v-model="rotateForm.publicKeyBase64" required rows="3" class="mt-1 w-full rounded-lg border border-gray-300 bg-transparent px-3 py-2 font-mono text-xs dark:border-gray-700"></textarea></label>
            </template>
            <template v-else-if="modal === 'publish'">
              <label class="text-sm">发布者<select v-model="publishForm.publisherId" required class="mt-1 h-10 w-full rounded-lg border border-gray-300 bg-transparent px-3 dark:border-gray-700"><option v-for="publisher in activePublishers" :key="publisher.publisher_id" :value="publisher.publisher_id">{{ publisher.display_name }}</option></select></label>
              <label class="text-sm">类型<select v-model="publishForm.kind" class="mt-1 h-10 w-full rounded-lg border border-gray-300 bg-transparent px-3 dark:border-gray-700"><option value="skill">Skill</option><option value="mcp_server">MCP Server</option></select></label>
              <label class="text-sm">Package ID<input v-model="publishForm.packageId" required class="mt-1 h-10 w-full rounded-lg border border-gray-300 bg-transparent px-3 dark:border-gray-700" /></label>
              <label class="text-sm">SemVer<input v-model="publishForm.version" required placeholder="1.0.0" class="mt-1 h-10 w-full rounded-lg border border-gray-300 bg-transparent px-3 dark:border-gray-700" /></label>
              <label class="text-sm">显示名称<input v-model="publishForm.displayName" required class="mt-1 h-10 w-full rounded-lg border border-gray-300 bg-transparent px-3 dark:border-gray-700" /></label>
              <label class="text-sm sm:col-span-2">描述<textarea v-model="publishForm.description" rows="2" class="mt-1 w-full rounded-lg border border-gray-300 bg-transparent px-3 py-2 dark:border-gray-700"></textarea></label>
              <label class="text-sm sm:col-span-2">制品 SHA-256<input v-model="publishForm.artifactDigest" required minlength="64" maxlength="64" class="mt-1 h-10 w-full rounded-lg border border-gray-300 bg-transparent px-3 font-mono text-xs dark:border-gray-700" /></label>
              <label class="text-sm">能力 ID（逗号分隔）<input v-model="publishForm.capabilityIDs" required class="mt-1 h-10 w-full rounded-lg border border-gray-300 bg-transparent px-3 dark:border-gray-700" /></label>
              <label class="text-sm">权限声明（逗号分隔）<input v-model="publishForm.permissions" class="mt-1 h-10 w-full rounded-lg border border-gray-300 bg-transparent px-3 dark:border-gray-700" /></label>
              <label class="text-sm">签名 Key ID<input v-model="publishForm.signatureKeyId" required readonly class="mt-1 h-10 w-full rounded-lg border border-gray-300 bg-gray-50 px-3 font-mono dark:border-gray-700 dark:bg-gray-950" /></label>
              <label class="text-sm sm:col-span-2">离线签名（Base64 Raw）<textarea v-model="publishForm.signatureBase64" required rows="3" class="mt-1 w-full rounded-lg border border-gray-300 bg-transparent px-3 py-2 font-mono text-xs dark:border-gray-700"></textarea></label>
            </template>
            <template v-else-if="modal === 'withdraw'">
              <p class="sm:col-span-2 text-sm text-gray-500">{{ selectedRelease?.manifest.package_id }} · {{ selectedRelease?.manifest.version }}</p>
              <label class="text-sm sm:col-span-2">原因代码<select v-model="withdrawForm.reasonCode" class="mt-1 h-10 w-full rounded-lg border border-gray-300 bg-transparent px-3 dark:border-gray-700"><option v-for="reason in withdrawalReasons" :key="reason.value" :value="reason.value">{{ reason.label }}</option></select></label>
            </template>
            <p v-if="actionError" class="sm:col-span-2 text-sm text-red-600 dark:text-red-400">{{ actionError }}</p>
          </div>
          <footer class="flex justify-end gap-2 border-t border-gray-200 px-5 py-4 dark:border-gray-800"><button type="button" class="rounded-lg border border-gray-300 px-4 py-2 text-sm dark:border-gray-700" @click="resetModal">取消</button><button type="submit" :disabled="saving" class="inline-flex items-center gap-1.5 rounded-lg bg-gray-900 px-4 py-2 text-sm font-medium text-white disabled:opacity-50 dark:bg-white dark:text-gray-900"><ArrowPathIcon v-if="saving" class="h-4 w-4 animate-spin" /><CheckCircleIcon v-else class="h-4 w-4" />确认</button></footer>
        </form>
      </div>
    </Teleport>
  </div>
</template>
