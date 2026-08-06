<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  ArrowPathIcon,
  PlusIcon,
  TrashIcon,
  UserGroupIcon,
  XMarkIcon,
} from '@heroicons/vue/24/outline'
import {
  createAgentProject,
  listAllAgentProjects,
  removeAgentProjectMember,
  upsertAgentProjectMember,
  type AgentProjectRole,
  type AgentProjectView,
} from '../../api/agent'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{
  (event: 'close'): void
  (event: 'changed'): void
}>()

const projects = ref<AgentProjectView[]>([])
const selectedProjectId = ref('')
const loading = ref(false)
const saving = ref(false)
const memberAction = ref('')
const errorMessage = ref('')
const projectName = ref('')
const memberUserId = ref('')
const memberRole = ref<Exclude<AgentProjectRole, 'owner'>>('viewer')

const selectedProject = computed(() => (
  projects.value.find(project => project.project_id === selectedProjectId.value) || null
))

const canManageMembers = computed(() => selectedProject.value?.current_role === 'owner')

const apiError = (error: any, fallback: string) => {
  const status = Number(error?.response?.status || 0)
  if (status === 403) return `${fallback}：只有项目所有者可以管理成员`
  if (status === 409) return `${fallback}：项目状态已变化，请刷新后重试`
  if (status === 412) return '项目级 MCP 当前未启用'
  if (!status || status >= 500) return `${fallback}：服务暂不可用，请稍后重试`
  const detail = error?.response?.data?.error
  return typeof detail === 'string' && detail.trim() ? detail : fallback
}

const replaceProject = (project: AgentProjectView) => {
  const index = projects.value.findIndex(item => item.project_id === project.project_id)
  if (index >= 0) projects.value.splice(index, 1, project)
  else projects.value.unshift(project)
  selectedProjectId.value = project.project_id
}

const loadProjects = async () => {
  loading.value = true
  errorMessage.value = ''
  try {
    projects.value = await listAllAgentProjects()
    if (!projects.value.some(project => project.project_id === selectedProjectId.value)) {
      selectedProjectId.value = projects.value[0]?.project_id || ''
    }
  } catch (error: any) {
    projects.value = []
    selectedProjectId.value = ''
    errorMessage.value = apiError(error, '加载项目失败')
  } finally {
    loading.value = false
  }
}

const createProject = async () => {
  if (!projectName.value.trim()) return
  saving.value = true
  errorMessage.value = ''
  try {
    const response = await createAgentProject(projectName.value.trim())
    if (response.data?.project) replaceProject(response.data.project)
    projectName.value = ''
    emit('changed')
  } catch (error: any) {
    errorMessage.value = apiError(error, '创建项目失败')
  } finally {
    saving.value = false
  }
}

const saveMember = async () => {
  const project = selectedProject.value
  const userId = memberUserId.value.trim()
  if (!project || !canManageMembers.value || !/^\d+$/.test(userId) || userId === '0') return
  memberAction.value = `save:${userId}`
  errorMessage.value = ''
  try {
    const response = await upsertAgentProjectMember(
      project.project_id,
      userId,
      memberRole.value,
      project.revision,
    )
    if (response.data?.project) replaceProject(response.data.project)
    memberUserId.value = ''
    memberRole.value = 'viewer'
    emit('changed')
  } catch (error: any) {
    errorMessage.value = apiError(error, '保存成员失败')
  } finally {
    memberAction.value = ''
  }
}

const removeMember = async (userId: string) => {
  const project = selectedProject.value
  if (!project || !canManageMembers.value) return
  if (!window.confirm(`从项目中移除用户 ${userId}？移除后其项目 MCP 权限会立即失效。`)) return
  memberAction.value = `remove:${userId}`
  errorMessage.value = ''
  try {
    const response = await removeAgentProjectMember(project.project_id, userId, project.revision)
    if (response.data?.project) replaceProject(response.data.project)
    emit('changed')
  } catch (error: any) {
    errorMessage.value = apiError(error, '移除成员失败')
  } finally {
    memberAction.value = ''
  }
}

watch(() => props.open, (open) => {
  if (open) void loadProjects()
})
</script>

<template>
  <div
    v-if="open"
    class="fixed inset-0 z-[70] flex items-center justify-center bg-black/50 p-3 sm:p-4"
    @click.self="emit('close')"
  >
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="agent-project-dialog-title"
      class="flex max-h-[88vh] w-full max-w-4xl flex-col overflow-hidden rounded-lg bg-white shadow-2xl dark:bg-gray-900"
    >
      <header class="flex items-center justify-between border-b border-gray-200 px-5 py-4 dark:border-gray-700">
        <div class="flex min-w-0 items-center gap-2">
          <UserGroupIcon class="h-5 w-5 shrink-0 text-primary" />
          <h2 id="agent-project-dialog-title" class="truncate text-base font-semibold text-gray-900 dark:text-white">Agent 项目</h2>
        </div>
        <button type="button" title="关闭" class="flex h-8 w-8 items-center justify-center rounded-md text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-800" @click="emit('close')">
          <XMarkIcon class="h-5 w-5" />
        </button>
      </header>

      <div class="grid min-h-0 flex-1 overflow-y-auto md:grid-cols-[280px_minmax(0,1fr)]">
        <section class="border-b border-gray-200 p-4 dark:border-gray-700 md:border-b-0 md:border-r">
          <form class="mb-4 flex gap-2" @submit.prevent="createProject">
            <input
              v-model="projectName"
              maxlength="80"
              placeholder="新项目名称"
              class="min-w-0 flex-1 rounded-md border border-gray-300 bg-white px-3 py-2 text-sm outline-none focus:border-primary dark:border-gray-700 dark:bg-gray-950"
            />
            <button type="submit" title="创建项目" :disabled="saving || !projectName.trim()" class="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-primary text-white disabled:opacity-50">
              <ArrowPathIcon v-if="saving" class="h-4 w-4 animate-spin" />
              <PlusIcon v-else class="h-4 w-4" />
            </button>
          </form>

          <div v-if="loading" class="flex justify-center py-8 text-gray-400"><ArrowPathIcon class="h-5 w-5 animate-spin" /></div>
          <p v-else-if="projects.length === 0" class="py-8 text-center text-sm text-gray-400">暂无项目</p>
          <div v-else class="space-y-1">
            <button
              v-for="project in projects"
              :key="project.project_id"
              type="button"
              class="w-full rounded-md px-3 py-2 text-left"
              :class="selectedProjectId === project.project_id ? 'bg-blue-50 text-primary dark:bg-blue-950/30' : 'hover:bg-gray-50 dark:hover:bg-gray-800'"
              @click="selectedProjectId = project.project_id"
            >
              <span class="block truncate text-sm font-medium">{{ project.name }}</span>
              <span class="block text-xs text-gray-400">{{ project.current_role }} · {{ project.members.length }} 名成员</span>
            </button>
          </div>
        </section>

        <section class="min-h-[360px] p-4 sm:p-5">
          <div v-if="!selectedProject" class="flex h-full items-center justify-center text-sm text-gray-400">选择项目后查看成员</div>
          <template v-else>
            <div class="mb-4 flex flex-wrap items-start justify-between gap-2">
              <div class="min-w-0">
                <h3 class="truncate text-base font-semibold text-gray-900 dark:text-white">{{ selectedProject.name }}</h3>
                <p class="break-all text-xs text-gray-400">{{ selectedProject.project_id }} · v{{ selectedProject.revision }}</p>
              </div>
              <span class="rounded bg-gray-100 px-2 py-1 text-xs text-gray-600 dark:bg-gray-800 dark:text-gray-300">{{ selectedProject.current_role }}</span>
            </div>

            <form v-if="canManageMembers" class="mb-4 grid gap-2 sm:grid-cols-[minmax(0,1fr)_120px_auto]" @submit.prevent="saveMember">
              <input
                v-model="memberUserId"
                inputmode="numeric"
                placeholder="用户 ID"
                class="min-w-0 rounded-md border border-gray-300 bg-white px-3 py-2 text-sm outline-none focus:border-primary dark:border-gray-700 dark:bg-gray-950"
              />
              <select v-model="memberRole" class="rounded-md border border-gray-300 bg-white px-3 py-2 text-sm outline-none focus:border-primary dark:border-gray-700 dark:bg-gray-950">
                <option value="viewer">Viewer</option>
                <option value="editor">Editor</option>
              </select>
              <button type="submit" :disabled="Boolean(memberAction) || !/^\d+$/.test(memberUserId.trim()) || memberUserId.trim() === '0'" class="inline-flex items-center justify-center gap-1.5 rounded-md bg-primary px-3 py-2 text-sm font-medium text-white disabled:opacity-50">
                <ArrowPathIcon v-if="memberAction.startsWith('save:')" class="h-4 w-4 animate-spin" />
                <PlusIcon v-else class="h-4 w-4" />
                成员
              </button>
            </form>

            <div class="divide-y divide-gray-200 border-y border-gray-200 dark:divide-gray-700 dark:border-gray-700">
              <div v-for="member in selectedProject.members" :key="member.user_id" class="flex items-center gap-3 py-3">
                <div class="min-w-0 flex-1">
                  <p class="truncate text-sm font-medium text-gray-800 dark:text-gray-100">用户 {{ member.user_id }}</p>
                  <p class="text-xs text-gray-400">{{ member.role }}</p>
                </div>
                <button
                  v-if="canManageMembers && member.role !== 'owner'"
                  type="button"
                  title="移除成员"
                  :disabled="Boolean(memberAction)"
                  class="flex h-8 w-8 items-center justify-center rounded-md text-red-500 hover:bg-red-50 disabled:opacity-50 dark:hover:bg-red-950/30"
                  @click="removeMember(member.user_id)"
                >
                  <ArrowPathIcon v-if="memberAction === `remove:${member.user_id}`" class="h-4 w-4 animate-spin" />
                  <TrashIcon v-else class="h-4 w-4" />
                </button>
              </div>
            </div>
          </template>
          <p v-if="errorMessage" class="mt-4 text-sm text-red-600">{{ errorMessage }}</p>
        </section>
      </div>
    </div>
  </div>
</template>
