<script setup lang="ts">
import MainLayout from '../layout/MainLayout.vue'
import TweetCard from '../components/TweetCard.vue'
import UserCard from '../components/UserCard.vue'
import { ref, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import request from '../utils/request'
import { listTweets, type Tweet } from '../api/tweet'
import { searchUsers } from '../api/user'

const route = useRoute()
const router = useRouter()
const tweets = ref<Tweet[]>([])
const users = ref<any[]>([])
const loading = ref(false)
const searchQuery = ref('')
const trends = ref<any[]>([])
const activeTab = ref<'trends' | 'latest'>('trends')
const searchType = ref<'latest' | 'people'>('latest') // 搜索状态下的 Tab

// 获取热搜
const fetchTrends = async () => {
    try {
        const res = await request.get('/trends')
        trends.value = res.data.topics || []
    } catch (e) {
        console.error(e)
    }
}

// 获取最新推文 (使用 listTweets 接口)
const fetchLatestTweets = async () => {
    loading.value = true
    try {
        const res = await listTweets('0')
        tweets.value = res.data.tweets || []
    } catch (e) {
        console.error(e)
    } finally {
        loading.value = false
    }
}

// 搜索 (推文或用户)
const executeSearch = async () => {
    if (!searchQuery.value.trim()) return
    
    loading.value = true
    tweets.value = []
    users.value = []
    
    try {
        if (searchType.value === 'latest') {
            const res = await request.get('/search', {
                params: { q: searchQuery.value, limit: 20 }
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
    // 只有按回车或点击标签时触发路由更新
    if (!searchQuery.value.trim()) {
        router.push('/explore')
    } else {
        router.push(`/explore?q=${encodeURIComponent(searchQuery.value)}&type=${searchType.value}`)
    }
}

// 格式化数字 (如 1.2k)
const formatNumber = (num: number) => {
    if (num >= 1000000) return (num / 1000000).toFixed(1) + 'M'
    if (num >= 1000) return (num / 1000).toFixed(1) + 'k'
    return num.toString()
}

// 初始化
onMounted(() => {
    fetchTrends()
    
    // 如果 URL 有查询参数
    if (route.query.q) {
        searchQuery.value = route.query.q as string
        searchType.value = (route.query.type as 'latest' | 'people') || 'latest'
        executeSearch()
    } else {
        // 默认加载最新
        activeTab.value = 'latest'
        fetchLatestTweets()
    }
})

// 监听 URL 变化
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
        fetchLatestTweets()
    }
}, { deep: true })

const goToTrend = (topic: string) => {
    router.push(`/explore?q=${encodeURIComponent(topic)}&type=latest`)
}

const switchTab = (tab: 'trends' | 'latest') => {
    activeTab.value = tab
    if (tab === 'latest' && tweets.value.length === 0) {
        fetchLatestTweets()
    }
}

const switchSearchType = (type: 'latest' | 'people') => {
    searchType.value = type
    triggerSearch() // 更新 URL，触发 executeSearch
}
</script>

<template>
  <MainLayout>
      <div class="sticky top-0 bg-white/80 dark:bg-black/80 backdrop-blur-md z-10 border-b border-gray-100 dark:border-gray-800">
           <!-- Search Input -->
           <div class="px-4 py-3">
               <div class="relative">
                  <input 
                    v-model="searchQuery"
                    @keyup.enter="triggerSearch"
                    type="text" 
                    placeholder="搜索 Twitter" 
                    class="w-full bg-gray-100 dark:bg-gray-900 rounded-full py-2 px-10 text-gray-900 dark:text-gray-100 focus:bg-white dark:focus:bg-black focus:ring-1 focus:ring-primary outline-none transition-all"
                  />
                  <div class="absolute left-3 top-2.5 text-gray-500">
                      🔍
                  </div>
               </div>
           </div>

          <!-- Tabs (Search Query Empty) -->
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

          <!-- Tabs (Search Query Active) -->
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

      <!-- Content -->
      <div>
          <!-- Trends List -->
          <div v-if="!searchQuery && activeTab === 'trends'" class="divide-y divide-gray-100 dark:divide-gray-800">
              <div 
                v-for="(trend, idx) in trends" 
                :key="idx"
                @click="goToTrend(trend.topic)"
                class="px-4 py-3 hover:bg-gray-50 dark:hover:bg-gray-900 cursor-pointer transition-colors group"
              >
                  <div class="flex justify-between items-start">
                      <div>
                          <div class="text-[13px] text-gray-500 mb-0.5">正在热搜</div>
                          <div class="font-bold text-gray-900 dark:text-gray-100 group-hover:underline">#{{ trend.topic }}</div>
                          <div class="text-[13px] text-gray-500 mt-0.5">{{ formatNumber(trend.score) }} 推文</div>
                      </div>
                      <div class="text-gray-400 group-hover:text-primary transition-colors">
                          <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 12h.01M12 12h.01M19 12h.01M6 12a1 1 0 11-2 0 1 1 0 012 0zm7 0a1 1 0 11-2 0 1 1 0 012 0zm7 0a1 1 0 11-2 0 1 1 0 012 0z" />
                          </svg>
                      </div>
                  </div>
              </div>
          </div>

          <!-- Latest Tweets (Default Feed) -->
          <div v-else-if="!searchQuery && activeTab === 'latest'">
              <div v-if="loading" class="p-8 text-center text-primary">加载中...</div>
              <div v-else-if="tweets.length > 0">
                   <TweetCard v-for="tweet in tweets" :key="tweet.id" :tweet="tweet" />
              </div>
              <div v-else class="p-8 text-center text-gray-500">暂无最新推文</div>
          </div>

          <!-- Search Results -->
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
