<script setup lang="ts">
const nodeTypes = [
  {
    type: 'start',
    preset: 'start',
    title: '开始节点 (Start)',
    description: '工作流触发起点，接收用户输入参数。',
    iconClass: 'bg-emerald-500/20 text-emerald-400 border-emerald-500/30',
  },
  {
    type: 'llm',
    preset: 'llm_chat',
    title: 'LLM 对话组件 (Chat)',
    description: '通用对话、问答、总结和文本理解。默认不会发推。',
    iconClass: 'bg-purple-500/20 text-purple-400 border-purple-500/30',
  },
  {
    type: 'llm',
    preset: 'llm_writer',
    title: 'LLM 创作组件 (Writer)',
    description: '面向推文、长文、改写等内容创作任务。',
    iconClass: 'bg-fuchsia-500/20 text-fuchsia-400 border-fuchsia-500/30',
  },
  {
    type: 'llm',
    preset: 'llm_planner',
    title: '任务规划器 (Planner)',
    description: '把复杂目标拆成有顺序、可验证的执行计划。',
    iconClass: 'bg-violet-500/20 text-violet-300 border-violet-500/30',
  },
  {
    type: 'agent',
    preset: 'react_agent',
    title: 'ReAct 智能体',
    description: '在迭代上限内自主判断、调用只读 MCP 工具并汇总结论。',
    iconClass: 'bg-sky-500/20 text-sky-300 border-sky-500/30',
  },
  {
    type: 'agent',
    preset: 'plan_executor',
    title: '计划执行器 (Plan-Execute)',
    description: '接收 Planner 输出，按计划调用工具并核验执行结果。',
    iconClass: 'bg-blue-500/20 text-blue-300 border-blue-500/30',
  },
  {
    type: 'tool',
    preset: 'publish_tweet',
    title: '发推工具 (PublishTweet)',
    description: '真实发布内容到平台；运行前需要明确接入。',
    iconClass: 'bg-amber-500/20 text-amber-400 border-amber-500/30',
  },
  {
    type: 'tool',
    preset: 'mcp_hybrid_tweet_search',
    title: 'MCP 混合推文检索',
    description: '调用平台 MCP，融合关键词与向量召回检索推文。',
    iconClass: 'bg-indigo-500/20 text-indigo-400 border-indigo-500/30',
  },
  {
    type: 'tool',
    preset: 'mcp_semantic_tweet_search',
    title: 'MCP 语义推文检索',
    description: '调用平台 MCP，按语义搜索相关推文。',
    iconClass: 'bg-indigo-500/20 text-indigo-300 border-indigo-500/30',
  },
  {
    type: 'tool',
    preset: 'mcp_search_users',
    title: 'MCP 用户搜索',
    description: '按关键词搜索平台用户及其简介。',
    iconClass: 'bg-teal-500/20 text-teal-300 border-teal-500/30',
  },
  {
    type: 'tool',
    preset: 'mcp_get_user_tweets',
    title: 'MCP 用户推文',
    description: '获取指定用户的历史推文作为分析上下文。',
    iconClass: 'bg-teal-500/20 text-teal-300 border-teal-500/30',
  },
  {
    type: 'tool',
    preset: 'mcp_get_tweets_by_ids',
    title: 'MCP 指定推文',
    description: '按推文 ID 列表读取真实内容。',
    iconClass: 'bg-teal-500/20 text-teal-300 border-teal-500/30',
  },
  {
    type: 'router',
    preset: 'router',
    title: '条件路由器 (Router)',
    description: '根据上游输入导向 True/False 分支。',
    iconClass: 'bg-cyan-500/20 text-cyan-400 border-cyan-500/30',
  },
  {
    type: 'wait',
    preset: 'wait',
    title: '人工审批 (Approve)',
    description: '执行到此暂停，等待人工确认后恢复。',
    iconClass: 'bg-yellow-500/20 text-yellow-400 border-yellow-500/30',
  },
  {
    type: 'end',
    preset: 'end',
    title: '结束节点 (End)',
    description: '工作流终点，输出最终黑板结果。',
    iconClass: 'bg-rose-500/20 text-rose-400 border-rose-500/30',
  },
]

const onDragStart = (event: DragEvent, nodeType: string, title: string, preset: string) => {
  if (event.dataTransfer) {
    event.dataTransfer.setData('application/vueflow-type', nodeType)
    event.dataTransfer.setData('application/vueflow-title', title)
    event.dataTransfer.setData('application/vueflow-preset', preset)
    event.dataTransfer.effectAllowed = 'move'
  }
}
</script>

<template>
  <div class="w-72 bg-slate-900 border-r border-white/10 p-4 flex flex-col h-full overflow-y-auto">
    <div class="mb-4">
      <h2 class="text-base font-bold text-white">智能体组件库</h2>
      <p class="text-xs text-gray-500 mt-1">拖拽组件到右侧画布开始编排</p>
    </div>

    <div class="space-y-3 flex-1">
      <div
        v-for="node in nodeTypes"
        :key="node.preset"
        class="flex flex-col p-3 rounded-lg border bg-slate-800/50 hover:bg-slate-800 cursor-grab active:cursor-grabbing border-white/5 hover:border-white/10 transition-all group"
        draggable="true"
        @dragstart="onDragStart($event, node.type, node.title, node.preset)"
      >
        <div class="flex items-center gap-2 mb-1.5">
          <span
            class="text-[10px] px-2 py-0.5 rounded-full border uppercase tracking-wider font-semibold"
            :class="node.iconClass"
          >
            {{ node.preset }}
          </span>
        </div>
        <h4 class="text-xs font-bold text-gray-200 group-hover:text-white">{{ node.title }}</h4>
        <p class="text-[10px] text-gray-500 mt-1 leading-normal">
          {{ node.description }}
        </p>
      </div>
    </div>
  </div>
</template>
