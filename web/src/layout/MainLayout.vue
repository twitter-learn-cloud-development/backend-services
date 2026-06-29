<script setup lang="ts">
import NavBar from '../components/NavBar.vue'
import { computed, ref, onMounted } from 'vue'
import request from '../utils/request'

interface TrendTopic {
  topic: string
  score: number
}

const trends = ref<TrendTopic[]>([])

const trendStopwords = new Set([
  '这次',
  '一个',
  '我们',
  '你们',
  '他们',
  '这个',
  '那个',
  '网友',
  '话题',
  '推文',
  '搜索',
  'twitter',
])

const compactTrendTopic = (topic: string) => {
  const raw = String(topic || '').trim()
  if (!raw) return ''

  const trimTopic = (value: string) => {
    let result = value.trim()
    for (const marker of ['话题', '真是', '真的', '网友', '我们', '你们', '他们', '一个', '这次', '这个', '那个']) {
      const idx = result.indexOf(marker)
      if (idx > 0) {
        result = result.slice(0, idx)
        break
      }
    }
    return result.slice(0, 16)
  }

  const hashtag = raw.match(/#([\u4e00-\u9fa5A-Za-z0-9_][\u4e00-\u9fa5A-Za-z0-9_-]{0,63})/)
  if (hashtag?.[1]) return trimTopic(hashtag[1])

  const cleaned = raw
    .replace(/https?:\/\/\S+/g, ' ')
    .replace(/[【】「」『』"'“”‘’。，、！？!?,.:;；：（）()\[\]{}<>]/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()

  const candidates = [
    ...(cleaned.match(/[A-Za-z][A-Za-z0-9_-]{1,23}/g) || []),
    ...(cleaned.match(/[\u4e00-\u9fa5]{2,12}/g) || []),
  ]
  const candidate = candidates.find(item => !trendStopwords.has(item.toLowerCase()))
  return trimTopic(candidate || cleaned)
}

const visibleTrends = computed(() => trends.value
  .map(trend => ({
    ...trend,
    label: compactTrendTopic(trend.topic),
  }))
  .filter(trend => trend.label && trend.label.length <= 16)
  .slice(0, 10))

const fetchTrends = async () => {
  try {
    const res = await request({ url: '/trends', method: 'get' })
    trends.value = res.data.topics || []
  } catch (error) {
    trends.value = []
  }
}

onMounted(() => {
  fetchTrends()
})
</script>

<template>
  <div class="min-h-screen bg-white dark:bg-black">
    <div class="container mx-auto max-w-7xl flex">
      <header class="hidden sm:flex flex-col w-20 xl:w-64 sticky top-0 h-screen">
        <NavBar />
      </header>

      <main class="flex-1 border-x border-gray-100 dark:border-gray-800 min-h-screen max-w-2xl">
        <slot />
      </main>

      <aside class="hidden lg:block w-80 pl-8 py-4 sticky top-0 h-screen">
        <div class="mb-4">
          <input
            type="text"
            placeholder="搜索 Twitter"
            @keyup.enter="$router.push('/explore?q=' + encodeURIComponent(($event.target as HTMLInputElement).value))"
            class="w-full bg-gray-100 dark:bg-gray-900 rounded-full py-3 px-5 text-gray-900 dark:text-gray-100 focus:bg-white dark:focus:bg-black focus:ring-1 focus:ring-primary outline-none transition-all"
          />
        </div>

        <div class="bg-gray-50 dark:bg-gray-900 rounded-2xl p-4">
          <h2 class="font-bold text-xl mb-4 text-gray-900 dark:text-white">推荐趋势</h2>

          <template v-if="visibleTrends.length > 0">
            <div
              v-for="(trend, index) in visibleTrends"
              :key="`${trend.label}-${index}`"
              class="py-3 hover:bg-gray-100 dark:hover:bg-gray-800 cursor-pointer transition-colors -mx-4 px-4"
              @click="$router.push('/explore?q=' + encodeURIComponent(trend.label) + '&type=latest')"
            >
              <div class="text-xs text-gray-500">趋势 · 第{{ index + 1 }}名</div>
              <div class="font-bold text-gray-900 dark:text-white truncate">#{{ trend.label }}</div>
              <div class="text-xs text-gray-500">热度 {{ trend.score }}</div>
            </div>
          </template>
          <template v-else>
            <div class="py-3 text-sm text-gray-500">暂无热门话题</div>
          </template>

          <div
            class="py-3 text-primary hover:bg-gray-100 dark:hover:bg-gray-800 cursor-pointer transition-colors -mx-4 px-4 rounded-b-2xl"
            @click="$router.push('/explore')"
          >
            显示更多
          </div>
        </div>
      </aside>
    </div>
  </div>
</template>
