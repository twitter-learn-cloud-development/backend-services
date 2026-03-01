<script setup lang="ts">
import NavBar from '../components/NavBar.vue'
import { ref, onMounted } from 'vue'
import request from '../utils/request'

interface TrendTopic {
    topic: string
    score: number
}

const trends = ref<TrendTopic[]>([])

const fetchTrends = async () => {
    try {
        const res = await request({ url: '/trends', method: 'get' })
        trends.value = res.data.topics || []
    } catch (error) {
        // 降级到空列表，不阻塞页面
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
      
      <!-- Left Sidebar (Nav) -->
      <header class="hidden sm:flex flex-col w-20 xl:w-64 sticky top-0 h-screen">
        <NavBar />
      </header>

      <!-- Main Content (Feed) -->
      <main class="flex-1 border-x border-gray-100 dark:border-gray-800 min-h-screen max-w-2xl">
        <slot />
      </main>

      <!-- Right Sidebar (Trending/Who to follow) -->
      <aside class="hidden lg:block w-80 pl-8 py-4 sticky top-0 h-screen">
        <!-- Search Bar -->
        <div class="mb-4">
          <input 
            type="text" 
            placeholder="搜索 Twitter" 
            @keyup.enter="$router.push('/explore?q=' + ($event.target as HTMLInputElement).value)"
            class="w-full bg-gray-100 dark:bg-gray-900 rounded-full py-3 px-5 text-gray-900 dark:text-gray-100 focus:bg-white dark:focus:bg-black focus:ring-1 focus:ring-primary outline-none transition-all"
          />
        </div>

        <!-- Trending Box -->
        <div class="bg-gray-50 dark:bg-gray-900 rounded-2xl p-4">
          <h2 class="font-bold text-xl mb-4 text-gray-900 dark:text-white">推荐趋势</h2>
          
          <template v-if="trends.length > 0">
            <div 
              v-for="(trend, index) in trends" 
              :key="index"
              class="py-3 hover:bg-gray-100 dark:hover:bg-gray-800 cursor-pointer transition-colors -mx-4 px-4"
              @click="$router.push('/explore?q=' + encodeURIComponent(trend.topic))"
            >
              <div class="text-xs text-gray-500">趋势 · 第{{ index + 1 }}名</div>
              <div class="font-bold text-gray-900 dark:text-white">{{ trend.topic }}</div>
              <div class="text-xs text-gray-500">热度 {{ trend.score }}</div>
            </div>
          </template>
          <template v-else>
            <div class="py-3 text-sm text-gray-500">暂无热门话题</div>
          </template>
          
          <div class="py-3 text-primary hover:bg-gray-100 dark:hover:bg-gray-800 cursor-pointer transition-colors -mx-4 px-4 rounded-b-2xl" @click="$router.push('/explore')">
              显示更多
          </div>
        </div>
      </aside>

    </div>
  </div>
</template>
