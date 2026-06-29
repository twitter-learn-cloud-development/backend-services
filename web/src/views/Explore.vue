<script setup lang="ts">
import MainLayout from '../layout/MainLayout.vue'
import TweetCard from '../components/TweetCard.vue'
import UserCard from '../components/UserCard.vue'
import { computed, ref, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import request from '../utils/request'
import { listTweets, type Tweet } from '../api/tweet'
import { searchUsers } from '../api/user'

interface TrendTopic {
  topic: string
  score: number
}

const route = useRoute()
const router = useRouter()
const tweets = ref<Tweet[]>([])
const users = ref<any[]>([])
const loading = ref(false)
const cursor = ref('0')
const hasMore = ref(true)
const searchQuery = ref('')
const trends = ref<TrendTopic[]>([])
const activeTab = ref<'trends' | 'latest'>('latest')
const searchType = ref<'latest' | 'people'>('latest')

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
  .filter(trend => trend.label && trend.label.length <= 16))

const fetchTrends = async () => {
  try {
    const res = await request.get('/trends')
    trends.value = res.data.topics || []
  } catch (e) {
    console.error(e)
    trends.value = []
  }
}

const fetchLatestTweets = async (refresh = false) => {
  if (loading.value) return
  if (!refresh && !hasMore.value) return

  loading.value = true
  try {
    const currentCursor = refresh ? '0' : cursor.value
    const res = await listTweets(currentCursor)
    const items = res.data.tweets || []
    tweets.value = refresh ? items : [...tweets.value, ...items]
    cursor.value = res.data.next_cursor || '0'
    hasMore.value = res.data.has_more || false
  } catch (e) {
    console.error(e)
  } finally {
    loading.value = false
  }
}

const executeSearch = async () => {
  if (!searchQuery.value.trim()) return

  loading.value = true
  tweets.value = []
  users.value = []
  cursor.value = '0'
  hasMore.value = true

  try {
    if (searchType.value === 'latest') {
      const res = await request.get('/search', {
        params: { q: searchQuery.value, limit: 20 },
      })
      tweets.value = res.data.tweets || []
    } else {
      const res = await searchUsers(searchQuery.value)
      users.value = res.data.users || []
    }
  } catch (error) {
    console.error('Failed to search', error)
  } finally {
    loading.value = false
  }
}

const triggerSearch = () => {
  if (!searchQuery.value.trim()) {
    router.push('/explore')
  } else {
    router.push(`/explore?q=${encodeURIComponent(searchQuery.value)}&type=${searchType.value}`)
  }
}

const formatNumber = (num: number) => {
  if (num >= 1000000) return (num / 1000000).toFixed(1) + 'M'
  if (num >= 1000) return (num / 1000).toFixed(1) + 'k'
  return String(num)
}

const handleScroll = () => {
  if (searchQuery.value || activeTab.value !== 'latest') return
  const nearBottom = window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 360
  if (nearBottom) fetchLatestTweets(false)
}

onMounted(() => {
  fetchTrends()
  window.addEventListener('scroll', handleScroll, { passive: true })

  if (route.query.q) {
    searchQuery.value = route.query.q as string
    searchType.value = (route.query.type as 'latest' | 'people') || 'latest'
    executeSearch()
  } else {
    activeTab.value = 'latest'
    fetchLatestTweets(true)
  }
})

onUnmounted(() => {
  window.removeEventListener('scroll', handleScroll)
})

watch(() => route.query, (newQuery) => {
  const q = newQuery.q as string
  const t = (newQuery.type as 'latest' | 'people') || 'latest'

  if (q) {
    searchQuery.value = q
    searchType.value = t
    executeSearch()
  } else {
    searchQuery.value = ''
    activeTab.value = 'latest'
    fetchLatestTweets(true)
  }
}, { deep: true })

const goToTrend = (topic: string) => {
  router.push(`/explore?q=${encodeURIComponent(topic)}&type=latest`)
}

const switchTab = (tab: 'trends' | 'latest') => {
  activeTab.value = tab
  if (tab === 'latest' && tweets.value.length === 0) {
    cursor.value = '0'
    hasMore.value = true
    fetchLatestTweets(true)
  }
}

const switchSearchType = (type: 'latest' | 'people') => {
  searchType.value = type
  triggerSearch()
}
</script>

<template>
  <MainLayout>
    <div class="sticky top-0 bg-white/80 dark:bg-black/80 backdrop-blur-md z-10 border-b border-gray-100 dark:border-gray-800">
      <div class="px-4 py-3">
        <div class="relative">
          <input
            v-model="searchQuery"
            @keyup.enter="triggerSearch"
            type="text"
            placeholder="搜索 Twitter"
            class="w-full bg-gray-100 dark:bg-gray-900 rounded-full py-2 px-10 text-gray-900 dark:text-gray-100 focus:bg-white dark:focus:bg-black focus:ring-1 focus:ring-primary outline-none transition-all"
          />
          <div class="absolute left-3 top-2.5 text-gray-500">⌕</div>
        </div>
      </div>

      <div v-if="!searchQuery" class="flex border-t border-gray-100 dark:border-gray-800">
        <div
          @click="switchTab('latest')"
          class="flex-1 text-center py-4 font-bold cursor-pointer transition-colors hover:bg-gray-50 dark:hover:bg-gray-900"
          :class="activeTab === 'latest' ? 'border-b-4 border-primary' : 'text-gray-500'"
        >
          最新推文
        </div>
        <div
          @click="switchTab('trends')"
          class="flex-1 text-center py-4 font-bold cursor-pointer transition-colors hover:bg-gray-50 dark:hover:bg-gray-900"
          :class="activeTab === 'trends' ? 'border-b-4 border-primary' : 'text-gray-500'"
        >
          趋势
        </div>
      </div>

      <div v-if="searchQuery" class="flex border-t border-gray-100 dark:border-gray-800">
        <div
          @click="switchSearchType('latest')"
          class="flex-1 text-center py-4 font-bold cursor-pointer transition-colors hover:bg-gray-50 dark:hover:bg-gray-900"
          :class="searchType === 'latest' ? 'border-b-4 border-primary' : 'text-gray-500'"
        >
          热门
        </div>
        <div
          @click="switchSearchType('people')"
          class="flex-1 text-center py-4 font-bold cursor-pointer transition-colors hover:bg-gray-50 dark:hover:bg-gray-900"
          :class="searchType === 'people' ? 'border-b-4 border-primary' : 'text-gray-500'"
        >
          用户
        </div>
      </div>
    </div>

    <div>
      <div v-if="!searchQuery && activeTab === 'trends'" class="divide-y divide-gray-100 dark:divide-gray-800">
        <div
          v-for="(trend, idx) in visibleTrends"
          :key="`${trend.label}-${idx}`"
          @click="goToTrend(trend.label)"
          class="px-4 py-3 hover:bg-gray-50 dark:hover:bg-gray-900 cursor-pointer transition-colors group"
        >
          <div class="flex justify-between items-start">
            <div class="min-w-0">
              <div class="text-[13px] text-gray-500 mb-0.5">正在热搜</div>
              <div class="font-bold text-gray-900 dark:text-gray-100 group-hover:underline truncate">#{{ trend.label }}</div>
              <div class="text-[13px] text-gray-500 mt-0.5">{{ formatNumber(trend.score) }} 条推文</div>
            </div>
          </div>
        </div>
        <div v-if="visibleTrends.length === 0" class="p-8 text-center text-gray-500">暂无热门话题</div>
      </div>

      <div v-else-if="!searchQuery && activeTab === 'latest'">
        <div v-if="loading && tweets.length === 0" class="p-8 text-center text-primary">加载中...</div>
        <div v-else-if="tweets.length > 0">
          <TweetCard v-for="tweet in tweets" :key="tweet.id" :tweet="tweet" />
          <div v-if="loading" class="p-4 text-center text-gray-500">加载更多...</div>
          <div v-else-if="!hasMore" class="p-4 text-center text-gray-400">已经到底了</div>
        </div>
        <div v-else class="p-8 text-center text-gray-500">暂无最新推文</div>
      </div>

      <div v-else-if="searchQuery">
        <div v-if="loading" class="p-8 text-center text-primary">搜索中...</div>

        <div v-else-if="searchType === 'latest'">
          <div v-if="tweets.length > 0">
            <TweetCard v-for="tweet in tweets" :key="tweet.id" :tweet="tweet" />
          </div>
          <div v-else class="p-8 text-center text-gray-500">
            未找到关于 "{{ searchQuery }}" 的推文
          </div>
        </div>

        <div v-else-if="searchType === 'people'">
          <div v-if="users.length > 0">
            <UserCard v-for="user in users" :key="user.id" :user="user" />
          </div>
          <div v-else class="p-8 text-center text-gray-500">
            未找到匹配 "{{ searchQuery }}" 的用户
          </div>
        </div>
      </div>
    </div>
  </MainLayout>
</template>
