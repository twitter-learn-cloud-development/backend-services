<script setup lang="ts">
import { inject } from 'vue'
import { Handle, Position, useVueFlow } from '@vue-flow/core'

interface NodeProps {
  id: string
  type: string
  label?: string
  data?: {
    title?: string
    description?: string
    status?: 'idle' | 'running' | 'success' | 'failed' | 'skipped' | 'suspended'
  }
}

const props = defineProps<NodeProps>()

const emit = defineEmits<{
  (e: 'delete', id: string): void
}>()

const { removeNodes } = useVueFlow()
const deleteNode = inject<((id: string) => void) | undefined>('deleteNode', undefined)

const onDelete = () => {
  if (deleteNode) {
    deleteNode(props.id)
  } else {
    removeNodes(props.id)
  }
  emit('delete', props.id)
}

const getStatusBorderClass = () => {
  switch (props.data?.status) {
    case 'running':
      return 'border-blue-500 shadow-[0_0_15px_rgba(59,130,246,0.6)] animate-pulse'
    case 'success':
      return 'border-green-500 shadow-[0_0_15px_rgba(34,197,94,0.6)]'
    case 'failed':
      return 'border-red-500 shadow-[0_0_15px_rgba(239,68,68,0.6)]'
    case 'skipped':
      return 'border-gray-500 opacity-60'
    case 'suspended':
      return 'border-yellow-500 shadow-[0_0_15px_rgba(234,179,8,0.55)]'
    default:
      return 'border-white/20 hover:border-blue-400/50 shadow-lg'
  }
}

const getTypeHeaderColor = () => {
  switch (props.type) {
    case 'start':
      return 'from-emerald-500 to-teal-500'
    case 'end':
      return 'from-rose-500 to-pink-500'
    case 'llm':
      return 'from-purple-500 to-indigo-500'
    case 'tool':
      return 'from-amber-500 to-orange-500'
    case 'agent':
      return 'from-sky-500 to-blue-600'
    case 'router':
      return 'from-cyan-500 to-sky-500'
    case 'wait':
      return 'from-yellow-500 to-amber-600'
    default:
      return 'from-gray-500 to-slate-500'
  }
}
</script>

<template>
  <div
    class="relative flex flex-col w-[260px] rounded-xl backdrop-blur-md bg-slate-900/80 border transition-all duration-300 text-white select-none"
    :class="getStatusBorderClass()"
  >
    <Handle
      v-if="props.type !== 'start'"
      id="input"
      type="target"
      :position="Position.Left"
      class="workflow-handle workflow-handle-in !left-[-7px]"
    />

    <div
      class="h-2 rounded-t-xl bg-gradient-to-r"
      :class="getTypeHeaderColor()"
    ></div>

    <div class="p-3 relative">
      <button
        v-if="props.type !== 'start' && props.type !== 'end'"
        @click.stop="onDelete"
        class="absolute right-2 top-2 text-gray-500 hover:text-white transition-colors text-[11px] bg-slate-950/40 hover:bg-slate-950/90 rounded-full w-5 h-5 flex items-center justify-center z-20 font-bold"
        title="删除节点"
      >
        ×
      </button>
      <div class="flex items-center justify-between mb-1 pr-5">
        <span class="text-xs font-semibold uppercase tracking-wider text-gray-400">
          {{ props.type }}
        </span>
        <span
          v-if="props.data?.status"
          class="w-2 h-2 rounded-full"
          :class="{
            'bg-blue-500 animate-ping': props.data.status === 'running',
            'bg-green-500': props.data.status === 'success',
            'bg-red-500': props.data.status === 'failed',
            'bg-gray-500': props.data.status === 'skipped',
            'bg-yellow-500': props.data.status === 'suspended',
          }"
        ></span>
      </div>
      <h3 class="text-sm font-bold truncate">{{ props.data?.title || props.label || '节点' }}</h3>
      <p class="text-xs text-gray-500 mt-1 line-clamp-2 leading-relaxed">
        {{ props.data?.description || '暂无配置参数' }}
      </p>
    </div>

    <template v-if="props.type !== 'end' && props.type !== 'router'">
      <Handle
        id="output"
        type="source"
        :position="Position.Right"
        class="workflow-handle workflow-handle-out !right-[-7px]"
      />
    </template>

    <template v-if="props.type === 'router'">
      <Handle
        type="source"
        id="true"
        :position="Position.Right"
        class="workflow-handle workflow-handle-true !right-[-7px] !top-[35%]"
      />
      <span class="absolute right-4 top-[22%] text-[9px] text-emerald-400">True</span>

      <Handle
        type="source"
        id="false"
        :position="Position.Right"
        class="workflow-handle workflow-handle-false !right-[-7px] !top-[65%]"
      />
      <span class="absolute right-4 top-[52%] text-[9px] text-rose-400">False</span>
    </template>
  </div>
</template>

<style scoped>
.workflow-handle {
  width: 14px !important;
  height: 14px !important;
  border: 2px solid #0f172a !important;
  transition: transform 140ms ease, box-shadow 140ms ease;
}

.workflow-handle:hover {
  transform: scale(1.18);
  box-shadow: 0 0 0 5px rgba(99, 102, 241, 0.18);
}

.workflow-handle-in {
  background: #60a5fa !important;
}

.workflow-handle-out {
  background: #818cf8 !important;
}

.workflow-handle-true {
  background: #34d399 !important;
}

.workflow-handle-false {
  background: #fb7185 !important;
}
</style>
