<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  ArrowPathIcon,
  CheckIcon,
  PencilSquareIcon,
  PlusIcon,
  TrashIcon,
  XMarkIcon,
} from '@heroicons/vue/24/outline'
import {
  createProviderConfig,
  listProviderConfigs,
  revokeProviderConfig,
  updateProviderConfig,
  type ProviderConfigView,
} from '../../api/agent'

type SearchProvider = 'brave' | 'qianfan'

interface SearchProviderOption {
  value: SearchProvider
  label: string
  defaultBaseURL: string
  apiKeyPlaceholder: string
}

const defaultProviderOption: SearchProviderOption = {
  value: 'brave',
  label: 'Brave Search',
  defaultBaseURL: 'https://api.search.brave.com/res/v1/web/search',
  apiKeyPlaceholder: 'Brave Search API Key',
}

const providerOptions: SearchProviderOption[] = [
  defaultProviderOption,
  {
    value: 'qianfan',
    label: '百度千帆搜索',
    defaultBaseURL: 'https://qianfan.baidubce.com/v2/ai_search/web_search',
    apiKeyPlaceholder: '百度千帆 API Key',
  },
]

const props = defineProps<{
  open: boolean
  modelValue: string
}>()

const emit = defineEmits<{
  (event: 'update:modelValue', value: string): void
  (event: 'close'): void
}>()

const configs = ref<ProviderConfigView[]>([])
const loading = ref(false)
const saving = ref(false)
const revokingId = ref('')
const errorMessage = ref('')
const configurationEnabled = ref(true)
const editingId = ref('')
const editingRevision = ref(0)
const editingProvider = ref<SearchProvider | null>(null)
const provider = ref<SearchProvider>('brave')
const name = ref('')
const baseURL = ref(defaultProviderOption.defaultBaseURL)
const apiKey = ref('')

const activeProvider = computed(
  () => providerOptions.find(option => option.value === provider.value) || defaultProviderOption,
)
const apiKeyRequired = computed(
  () => !editingId.value || editingProvider.value !== provider.value,
)

const providerLabel = (value: string) => (
  providerOptions.find(option => option.value === value)?.label || value
)

const resetForm = () => {
  editingId.value = ''
  editingRevision.value = 0
  editingProvider.value = null
  provider.value = 'brave'
  name.value = ''
  baseURL.value = defaultProviderOption.defaultBaseURL
  apiKey.value = ''
  errorMessage.value = ''
}

const providerConfigError = (error: any, fallback: string) => {
  const detail = String(error?.response?.data?.error || error?.message || '').trim()
  const normalized = detail.toLowerCase()
  if (
    normalized.includes('provider configuration is disabled') ||
    normalized.includes('web search provider configuration is disabled')
  ) {
    configurationEnabled.value = false
    return '当前部署未启用联网搜索配置。请开启 AGENT_WEB_SEARCH_ENABLED 后重试。'
  }
  if (normalized.includes('new api key is required when provider changes')) {
    return '切换搜索 Provider 时必须填写对应的新 API Key。'
  }
  if (normalized.includes('web search provider must be')) {
    return '当前仅支持 Brave Search 和百度千帆搜索。'
  }
  return detail || fallback
}

const loadConfigs = async () => {
  loading.value = true
  errorMessage.value = ''
  configurationEnabled.value = true
  try {
    const response = await listProviderConfigs({ page: 1, page_size: 100, kind: 'web_search' })
    configs.value = (response.data?.provider_configs || []).filter(item => item.status === 'active')
    if (props.modelValue && !configs.value.some(item => item.provider_config_id === props.modelValue)) {
      emit('update:modelValue', '')
    }
  } catch (error: any) {
    errorMessage.value = providerConfigError(error, '加载联网搜索配置失败')
  } finally {
    loading.value = false
  }
}

const editConfig = (config: ProviderConfigView) => {
  const nextProvider: SearchProvider = config.provider === 'qianfan' ? 'qianfan' : 'brave'
  editingId.value = config.provider_config_id
  editingRevision.value = config.revision
  editingProvider.value = nextProvider
  provider.value = nextProvider
  name.value = config.name
  baseURL.value = config.base_url
  apiKey.value = ''
  errorMessage.value = ''
}

const onProviderChange = () => {
  if (!editingId.value || provider.value !== editingProvider.value) {
    baseURL.value = activeProvider.value.defaultBaseURL
    apiKey.value = ''
  }
}

const saveConfig = async () => {
  if (
    !name.value.trim() ||
    !baseURL.value.trim() ||
    (apiKeyRequired.value && !apiKey.value.trim())
  ) return

  saving.value = true
  errorMessage.value = ''
  try {
    const payload = {
      kind: 'web_search' as const,
      name: name.value.trim(),
      provider: provider.value,
      base_url: baseURL.value.trim(),
      model: '',
      api_key: apiKey.value.trim() || undefined,
      revision: editingRevision.value || undefined,
    }
    const response = editingId.value
      ? await updateProviderConfig(editingId.value, payload)
      : await createProviderConfig(payload)
    const saved = response.data?.provider_config as ProviderConfigView | undefined
    if (saved?.provider_config_id) emit('update:modelValue', saved.provider_config_id)
    resetForm()
    await loadConfigs()
  } catch (error: any) {
    errorMessage.value = providerConfigError(error, '保存联网搜索配置失败')
  } finally {
    saving.value = false
  }
}

const revokeConfig = async (config: ProviderConfigView) => {
  if (!window.confirm(`撤销联网配置“${config.name}”吗？`)) return
  revokingId.value = config.provider_config_id
  errorMessage.value = ''
  try {
    await revokeProviderConfig(config.provider_config_id, config.revision)
    if (props.modelValue === config.provider_config_id) emit('update:modelValue', '')
    if (editingId.value === config.provider_config_id) resetForm()
    await loadConfigs()
  } catch (error: any) {
    errorMessage.value = providerConfigError(error, '撤销联网搜索配置失败')
  } finally {
    revokingId.value = ''
  }
}

watch(() => props.open, (open) => {
  if (open) void loadConfigs()
})
</script>

<template>
  <div
    v-if="open"
    class="fixed inset-0 z-[60] flex items-center justify-center bg-black/45 p-4"
    @click.self="emit('close')"
  >
    <div class="flex max-h-[90vh] w-full max-w-3xl flex-col overflow-hidden rounded-lg bg-white shadow-2xl dark:bg-gray-900">
      <div class="flex items-center justify-between border-b border-gray-200 px-5 py-4 dark:border-gray-700">
        <h2 class="text-base font-semibold text-gray-900 dark:text-white">联网搜索配置</h2>
        <button
          type="button"
          title="关闭"
          class="flex h-8 w-8 items-center justify-center rounded-md text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-800"
          @click="emit('close')"
        >
          <XMarkIcon class="h-5 w-5" />
        </button>
      </div>

      <div class="grid min-h-0 flex-1 overflow-y-auto md:grid-cols-[minmax(0,1fr)_minmax(280px,0.8fr)]">
        <div class="border-b border-gray-200 p-5 dark:border-gray-700 md:border-b-0 md:border-r">
          <div class="mb-3 flex items-center justify-between">
            <h3 class="text-sm font-semibold text-gray-800 dark:text-gray-100">搜索 Provider</h3>
            <button
              type="button"
              title="刷新"
              class="flex h-8 w-8 items-center justify-center rounded-md text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-800"
              @click="loadConfigs"
            >
              <ArrowPathIcon class="h-4 w-4" :class="loading ? 'animate-spin' : ''" />
            </button>
          </div>
          <p class="mb-4 text-xs leading-5 text-gray-500 dark:text-gray-400">
            可选择平台默认、Brave Search 或百度千帆搜索；站内推文检索不使用这里的配置。
          </p>
          <p
            v-if="!configurationEnabled"
            class="mb-4 rounded-md bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800 dark:bg-amber-950/30 dark:text-amber-200"
          >
            {{ errorMessage }}
          </p>

          <button
            type="button"
            class="mb-2 flex w-full items-center justify-between rounded-md border px-3 py-3 text-left text-sm transition-colors"
            :class="modelValue === ''
              ? 'border-primary bg-blue-50 text-primary dark:bg-blue-950/30'
              : 'border-gray-200 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800'"
            @click="emit('update:modelValue', '')"
          >
            <span>平台默认</span>
            <CheckIcon v-if="modelValue === ''" class="h-4 w-4" />
          </button>

          <div v-if="loading && configs.length === 0" class="flex justify-center py-10 text-gray-400">
            <ArrowPathIcon class="h-5 w-5 animate-spin" />
          </div>
          <div v-else class="space-y-2">
            <div
              v-for="config in configs"
              :key="config.provider_config_id"
              class="flex items-center gap-2 rounded-md border px-3 py-2.5"
              :class="modelValue === config.provider_config_id
                ? 'border-primary bg-blue-50 dark:bg-blue-950/30'
                : 'border-gray-200 dark:border-gray-700'"
            >
              <button
                type="button"
                class="min-w-0 flex-1 text-left"
                @click="emit('update:modelValue', config.provider_config_id)"
              >
                <span class="block truncate text-sm font-medium text-gray-800 dark:text-gray-100">
                  {{ config.name }}
                </span>
                <span class="block truncate text-xs text-gray-400">
                  {{ providerLabel(config.provider) }} · 密钥 v{{ config.credential_version }}
                </span>
              </button>
              <CheckIcon v-if="modelValue === config.provider_config_id" class="h-4 w-4 shrink-0 text-primary" />
              <button
                type="button"
                title="编辑"
                class="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-800"
                @click="editConfig(config)"
              >
                <PencilSquareIcon class="h-4 w-4" />
              </button>
              <button
                type="button"
                title="撤销"
                :disabled="revokingId === config.provider_config_id"
                class="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-red-500 hover:bg-red-50 disabled:opacity-50 dark:hover:bg-red-950/30"
                @click="revokeConfig(config)"
              >
                <ArrowPathIcon v-if="revokingId === config.provider_config_id" class="h-4 w-4 animate-spin" />
                <TrashIcon v-else class="h-4 w-4" />
              </button>
            </div>
          </div>
        </div>

        <form class="space-y-4 p-5" @submit.prevent="saveConfig">
          <div class="flex items-center justify-between">
            <h3 class="text-sm font-semibold text-gray-800 dark:text-gray-100">
              {{ editingId ? '编辑配置' : '新建配置' }}
            </h3>
            <button
              v-if="editingId"
              type="button"
              class="text-xs text-gray-500 hover:text-gray-800 dark:hover:text-gray-200"
              @click="resetForm"
            >
              取消编辑
            </button>
          </div>
          <label class="block space-y-1">
            <span class="text-xs font-medium text-gray-600 dark:text-gray-300">Provider</span>
            <select
              v-model="provider"
              class="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm outline-none focus:border-primary dark:border-gray-700 dark:bg-gray-950"
              @change="onProviderChange"
            >
              <option v-for="option in providerOptions" :key="option.value" :value="option.value">
                {{ option.label }}
              </option>
            </select>
          </label>
          <label class="block space-y-1">
            <span class="text-xs font-medium text-gray-600 dark:text-gray-300">名称</span>
            <input
              v-model="name"
              maxlength="80"
              required
              class="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm outline-none focus:border-primary dark:border-gray-700 dark:bg-gray-950"
            />
          </label>
          <label class="block space-y-1">
            <span class="text-xs font-medium text-gray-600 dark:text-gray-300">Base URL</span>
            <input
              v-model="baseURL"
              type="url"
              required
              class="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm outline-none focus:border-primary dark:border-gray-700 dark:bg-gray-950"
            />
          </label>
          <label class="block space-y-1">
            <span class="text-xs font-medium text-gray-600 dark:text-gray-300">API Key</span>
            <input
              v-model="apiKey"
              type="password"
              autocomplete="new-password"
              :required="apiKeyRequired"
              :placeholder="editingId && !apiKeyRequired ? '留空保留当前密钥' : activeProvider.apiKeyPlaceholder"
              class="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm outline-none focus:border-primary dark:border-gray-700 dark:bg-gray-950"
            />
          </label>
          <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">
            密钥仅发送到服务端并加密保存，前端不会再次读取明文。
          </p>
          <p v-if="errorMessage && configurationEnabled" class="text-sm text-red-600">{{ errorMessage }}</p>
          <button
            type="submit"
            :disabled="
              !configurationEnabled ||
              saving ||
              !name.trim() ||
              !baseURL.trim() ||
              (apiKeyRequired && !apiKey.trim())
            "
            class="inline-flex w-full items-center justify-center gap-2 rounded-md bg-primary px-4 py-2.5 text-sm font-medium text-white hover:bg-blue-600 disabled:cursor-not-allowed disabled:opacity-50"
          >
            <ArrowPathIcon v-if="saving" class="h-4 w-4 animate-spin" />
            <PlusIcon v-else-if="!editingId" class="h-4 w-4" />
            <CheckIcon v-else class="h-4 w-4" />
            {{ editingId ? '保存修改' : '添加配置' }}
          </button>
        </form>
      </div>
    </div>
  </div>
</template>
