<script setup lang="ts">
import MainLayout from '../layout/MainLayout.vue'
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { HeartIcon } from '@heroicons/vue/24/outline'
import { getNotifications, markAllAsRead, type Notification } from '../api/notification'
import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'
import 'dayjs/locale/zh-cn'

dayjs.extend(relativeTime)
dayjs.locale('zh-cn')

const router = useRouter()

const notifications = ref<Notification[]>([])
const loading = ref(false)
const cursor = ref('0')
const hasMore = ref(true)

const typeInfo: Record<string, { icon: string, text: string }> = {
    like:    { icon: '❤️', text: '赞了你的推文' },
    comment: { icon: '💬', text: '评论了你的推文' },
    follow:  { icon: '👤', text: '关注了你' },
}

const fetchNotifications = async (refresh = false) => {
    if (loading.value) return
    loading.value = true
    try {
        const currentCursor = refresh ? '0' : cursor.value
        const res = await getNotifications(currentCursor, 20)
        const items = res.data.notifications || []
        if (refresh) {
            notifications.value = items
        } else {
            notifications.value.push(...items)
        }
        cursor.value = res.data.next_cursor || '0'
        hasMore.value = res.data.has_more || false

        // 标记所有已读 (无论是否加载完)
        markAllAsRead().then(() => {
            // 通知 NavBar 立即刷新未读计数
            window.dispatchEvent(new Event('notifications-read'))
        }).catch(() => {})
    } catch (error) {
        console.error('Failed to load notifications', error)
    } finally {
        loading.value = false
    }
}

const handleNotificationClick = (n: Notification) => {
    if (n.type === 'follow') {
        router.push(`/users/${n.actor.id}`)
    } else {
        router.push(`/tweets/${n.target_id}`)
    }
}

const formatTime = (ts: number) => {
    if (!ts) return ''
    return dayjs(ts).fromNow()
}

onMounted(() => {
    fetchNotifications(true)
})
</script>

<template>
  <MainLayout>
      <div class="sticky top-0 bg-white/80 dark:bg-black/80 backdrop-blur-md z-10 border-b border-gray-100 dark:border-gray-800 px-4 py-3">
          <h1 class="font-bold text-xl">通知</h1>
      </div>

      <!-- Loading -->
      <div v-if="loading && notifications.length === 0" class="p-8 text-center text-gray-500">加载中...</div>

      <!-- Notifications List -->
      <div v-for="n in notifications" :key="n.id"
        @click="handleNotificationClick(n)"
        class="px-4 py-3 border-b border-gray-100 dark:border-gray-800 hover:bg-gray-50 dark:hover:bg-gray-900/50 cursor-pointer transition-colors flex items-start space-x-3"
        :class="{ 'bg-blue-50/50 dark:bg-blue-900/10': !n.is_read }"
      >
          <!-- Icon -->
          <div class="w-10 h-10 rounded-full flex-shrink-0 flex items-center justify-center text-xl"
            :class="{
              'bg-pink-100 dark:bg-pink-900/30': n.type === 'like',
              'bg-blue-100 dark:bg-blue-900/30': n.type === 'comment',
              'bg-green-100 dark:bg-green-900/30': n.type === 'follow'
            }"
          >
              {{ typeInfo[n.type]?.icon || '🔔' }}
          </div>

          <!-- Content -->
          <div class="flex-1 min-w-0">
              <div class="flex items-center space-x-2">
                  <img
                    :src="n.actor?.avatar || 'https://abs.twimg.com/sticky/default_profile_images/default_profile_400x400.png'"
                    class="w-8 h-8 rounded-full object-cover"
                  />
                  <div class="text-sm">
                      <span class="font-bold text-gray-900 dark:text-white">{{ n.actor?.username || 'unknown' }}</span>
                      <span class="text-gray-500 ml-1">{{ typeInfo[n.type]?.text || '通知' }}</span>
                  </div>
              </div>
              <p v-if="n.content" class="text-sm text-gray-500 mt-1 line-clamp-2">{{ n.content }}</p>
              <span class="text-xs text-gray-400 mt-1 block">{{ formatTime(n.created_at) }}</span>
          </div>
      </div>

      <!-- Empty State -->
      <div v-if="notifications.length === 0 && !loading" class="flex flex-col items-center justify-center py-20 px-8 text-center">
          <HeartIcon class="w-16 h-16 text-gray-300 mb-6" />
          <h2 class="text-2xl font-bold text-gray-900 dark:text-white mb-2">暂无通知</h2>
          <p class="text-gray-500 max-w-sm">当有人关注你、点赞或评论你的推文时，通知将会显示在这里。</p>
      </div>

      <!-- Load More -->
      <div v-if="hasMore && notifications.length > 0" class="p-4 text-center">
          <button @click="fetchNotifications(false)" :disabled="loading" class="text-primary hover:underline">
              {{ loading ? '加载中...' : '加载更多' }}
          </button>
      </div>
  </MainLayout>
</template>
