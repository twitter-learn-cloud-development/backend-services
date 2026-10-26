<script setup lang="ts">
import NavBar from '../components/NavBar.vue'
import { computed, ref, onMounted, onUnmounted } from 'vue'
import request from '../utils/request'

interface TrendTopic {
  topic: string
  score: number
}

const trends = ref<TrendTopic[]>([])
let trendRequestID = 0
let trendRefreshInterval: ReturnType<typeof setInterval> | undefined
let postPublishRefresh: ReturnType<typeof setTimeout> | undefined

const visibleTrends = computed(() => trends.value
  .filter(trend => trend.topic && trend.score >= 1)
  .slice(0, 10))

const fetchTrends = async () => {
  const requestID = ++trendRequestID
  try {
    const res = await request({ url: '/trends', method: 'get' })
    if (requestID === trendRequestID) trends.value = res.data.topics || []
  } catch (error) {
    if (requestID === trendRequestID) trends.value = []
  }
}

const refreshAfterPublish = () => {
  fetchTrends()
  if (postPublishRefresh) clearTimeout(postPublishRefresh)
  postPublishRefresh = setTimeout(fetchTrends, 3000)
}

onMounted(() => {
  fetchTrends()
  trendRefreshInterval = setInterval(() => {
    if (!document.hidden) fetchTrends()
  }, 30000)
  window.addEventListener('tweet-created', refreshAfterPublish)
  window.addEventListener('focus', fetchTrends)
})

onUnmounted(() => {
  if (trendRefreshInterval) clearInterval(trendRefreshInterval)
  if (postPublishRefresh) clearTimeout(postPublishRefresh)
  window.removeEventListener('tweet-created', refreshAfterPublish)
  window.removeEventListener('focus', fetchTrends)
  trendRequestID++
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
              :key="`${trend.topic}-${index}`"
              class="py-3 hover:bg-gray-100 dark:hover:bg-gray-800 cursor-pointer transition-colors -mx-4 px-4"
              @click="$router.push('/explore?q=' + encodeURIComponent(trend.topic) + '&type=latest')"
            >
              <div class="text-xs text-gray-500">趋势 · 第{{ index + 1 }}名</div>
              <div class="font-bold text-gray-900 dark:text-white truncate">#{{ trend.topic }}</div>
              <div class="text-xs text-gray-500">近期热度 {{ trend.score }}</div>
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
