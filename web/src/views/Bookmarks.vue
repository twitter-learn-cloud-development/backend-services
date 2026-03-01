<script setup lang="ts">
import MainLayout from '../layout/MainLayout.vue'
import TweetCard from '../components/TweetCard.vue'
import { ref, onMounted } from 'vue'
import request from '../utils/request'
import { BookmarkIcon } from '@heroicons/vue/24/outline'
import type { Tweet } from '../api/tweet'

const tweets = ref<Tweet[]>([])
const loading = ref(false)
const cursor = ref('0')
const hasMore = ref(true)

const fetchBookmarks = async (refresh = false) => {
    if (loading.value) return
    loading.value = true
    try {
        const currentCursor = refresh ? '0' : cursor.value
        const res = await request.get('/bookmarks', {
            params: { cursor: currentCursor, limit: 20 }
        })
        const items = (res.data.tweets || []).map((t: Tweet) => ({ ...t, is_bookmarked: true }))
        if (refresh) {
            tweets.value = items
        } else {
            tweets.value.push(...items)
        }
        cursor.value = res.data.next_cursor || '0'
        hasMore.value = res.data.has_more || false
    } catch (error) {
        console.error('Failed to load bookmarks', error)
    } finally {
        loading.value = false
    }
}

const handleTweetDeleted = (tweetId: string) => {
    tweets.value = tweets.value.filter(t => t.id !== tweetId)
}

onMounted(() => {
    fetchBookmarks(true)
})
</script>

<template>
  <MainLayout>
      <div class="sticky top-0 bg-white/80 dark:bg-black/80 backdrop-blur-md z-10 border-b border-gray-100 dark:border-gray-800 px-4 py-3">
          <h1 class="font-bold text-xl">书签</h1>
          <p class="text-xs text-gray-500 mt-0.5">@你 · 私密</p>
      </div>

      <!-- Loading -->
      <div v-if="loading && tweets.length === 0" class="p-8 text-center text-gray-500">加载中...</div>

      <!-- Bookmarked Tweets -->
      <div v-if="tweets.length > 0">
          <TweetCard v-for="tweet in tweets" :key="tweet.id" :tweet="tweet" @deleted="handleTweetDeleted" />
      </div>

      <!-- Empty State -->
      <div v-else-if="!loading" class="flex flex-col items-center justify-center py-20 px-8 text-center">
          <BookmarkIcon class="w-16 h-16 text-gray-300 mb-6" />
          <h2 class="text-2xl font-bold text-gray-900 dark:text-white mb-2">保存帖子以供稍后阅读</h2>
          <p class="text-gray-500 max-w-sm">在推文上点击书签图标即可将推文保存到这里。</p>
      </div>

      <!-- Load More -->
      <div v-if="hasMore && tweets.length > 0" class="p-4 text-center">
          <button @click="fetchBookmarks(false)" :disabled="loading" class="text-primary hover:underline">
              {{ loading ? '加载中...' : '加载更多' }}
          </button>
      </div>
  </MainLayout>
</template>
