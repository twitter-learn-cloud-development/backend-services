<script setup lang="ts">
import { HomeIcon, HashtagIcon, BellIcon, BookmarkIcon, UserIcon, ArrowLeftOnRectangleIcon, EnvelopeIcon } from '@heroicons/vue/24/outline'
import { HomeIcon as HomeIconSolid, HashtagIcon as HashtagIconSolid, BellIcon as BellIconSolid, BookmarkIcon as BookmarkIconSolid, UserIcon as UserIconSolid, EnvelopeIcon as EnvelopeIconSolid } from '@heroicons/vue/24/solid'
import { useRouter, useRoute } from 'vue-router'
import { useUserStore } from '../stores/user'
import { useMessengerStore } from '../stores/messenger'
import { computed, ref, onMounted, onUnmounted, watch } from 'vue'
import { getUnreadCount } from '../api/notification'

const router = useRouter()
const route = useRoute()
const userStore = useUserStore()
const messengerStore = useMessengerStore()

const unreadCount = ref(0)
let pollTimer: ReturnType<typeof setInterval> | null = null

const fetchUnreadCount = async () => {
    try {
        const res = await getUnreadCount()
        unreadCount.value = res.data.count || 0
    } catch {
        // 静默失败
    }
}

// 监听路由变化：离开通知页面时立即刷新计数
watch(() => route.path, (newPath, oldPath) => {
    if (oldPath === '/notifications' && newPath !== '/notifications') {
        fetchUnreadCount()
    }
})

// 监听自定义事件：通知标记已读后立即刷新
const onNotificationsRead = () => { fetchUnreadCount() }

onMounted(() => {
    fetchUnreadCount()
    pollTimer = setInterval(fetchUnreadCount, 30000)
    window.addEventListener('notifications-read', onNotificationsRead)
    
    // 连接 WebSocket
    if (userStore.token) {
        messengerStore.connect()
    }
})

onUnmounted(() => {
    if (pollTimer) clearInterval(pollTimer)
    window.removeEventListener('notifications-read', onNotificationsRead)
    messengerStore.disconnect()
})

const handleLogout = () => {
  if (confirm('确定要退出登录吗？')) {
      messengerStore.disconnect()
      userStore.logout()
  }
}

const navItems = [
  { name: '首页', icon: HomeIcon, activeIcon: HomeIconSolid, path: '/' },
  { name: '探索', icon: HashtagIcon, activeIcon: HashtagIconSolid, path: '/explore' },
  { name: '通知', icon: BellIcon, activeIcon: BellIconSolid, path: '/notifications' },
  { name: '私信', icon: EnvelopeIcon, activeIcon: EnvelopeIconSolid, path: '/messages' },
  { name: '书签', icon: BookmarkIcon, activeIcon: BookmarkIconSolid, path: '/bookmarks' },
  { name: '个人资料', icon: UserIcon, activeIcon: UserIconSolid, path: '/profile' },
]

const isActive = (path: string) => {
  if (path === '/') return route.path === '/'
  return route.path.startsWith(path)
}

const currentUser = computed(() => userStore.user)
</script>

<template>
  <nav class="flex flex-col h-full bg-white dark:bg-black">
    <!-- Logo -->
    <div class="px-4 py-3">
      <div class="w-12 h-12 rounded-full hover:bg-blue-50 dark:hover:bg-gray-900 flex items-center justify-center cursor-pointer transition-colors" @click="router.push('/')">
         <svg viewBox="0 0 24 24" class="w-7 h-7 fill-current text-primary">
           <path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-5.214-6.817L4.99 21.75H1.68l7.73-8.835L1.254 2.25H8.08l4.713 6.231zm-1.161 17.52h1.833L7.084 4.126H5.117z"></path>
         </svg>
      </div>
    </div>

    <!-- Nav Items -->
    <div class="flex-1 flex flex-col space-y-1 px-2">
      <router-link
        v-for="item in navItems"
        :key="item.name"
        :to="item.path"
        class="flex items-center space-x-4 px-4 py-3 rounded-full hover:bg-gray-100 dark:hover:bg-gray-900 transition-colors group"
      >
        <div class="relative">
          <component
            :is="isActive(item.path) ? item.activeIcon : item.icon"
            class="w-7 h-7 text-gray-900 dark:text-white"
          />
          <!-- 未读通知红点 -->
          <span
            v-if="item.name === '通知' && unreadCount > 0"
            class="absolute -top-1.5 -right-1.5 bg-red-500 text-white text-xs font-bold rounded-full min-w-[18px] h-[18px] flex items-center justify-center px-1 shadow-sm"
          >{{ unreadCount > 99 ? '99+' : unreadCount }}</span>
        </div>
        <span
          class="text-xl hidden xl:block text-gray-900 dark:text-white"
          :class="{ 'font-bold': isActive(item.path) }"
        >{{ item.name }}</span>
      </router-link>

      <!-- Tweet Button -->
      <button
        @click="router.push('/')"
        class="mt-4 bg-primary hover:bg-blue-600 text-white font-bold rounded-full transition-colors shadow-md"
      >
        <!-- Desktop -->
        <span class="hidden xl:block py-3 px-4 text-center text-lg">发推</span>
        <!-- Mobile/Tablet -->
        <span class="xl:hidden flex items-center justify-center w-12 h-12 mx-auto">
          <svg viewBox="0 0 24 24" class="w-6 h-6 fill-current">
            <path d="M23 3c-6.62-.1-10.38 2.421-13.424 6.054C7.593 11.466 6.775 13.88 5 16c-2.006 2.386-4.96 3.8-4.96 3.8s5.213.6 7.96-1.8c1.682-1.47 2.86-3.654 3.547-6.203C12.933 7.81 15.96 5.78 23 3z"></path>
          </svg>
        </span>
      </button>
    </div>

    <!-- Bottom: Current User + Logout -->
    <div class="border-t border-gray-100 dark:border-gray-800 mx-2">
      <!-- User Info -->
      <div
        v-if="currentUser"
        class="flex items-center space-x-3 p-3 my-2 rounded-full hover:bg-gray-100 dark:hover:bg-gray-900 cursor-pointer transition-colors"
        @click="router.push('/profile')"
      >
        <img
          :src="currentUser.avatar || 'https://abs.twimg.com/sticky/default_profile_images/default_profile_400x400.png'"
          class="w-10 h-10 rounded-full object-cover flex-shrink-0"
        />
        <div class="hidden xl:block min-w-0 flex-1">
          <div class="font-bold text-sm text-gray-900 dark:text-white truncate">{{ currentUser.username }}</div>
          <div class="text-sm text-gray-500 truncate">@{{ currentUser.username }}</div>
        </div>
      </div>

      <!-- Logout -->
      <button
        @click="handleLogout"
        class="flex items-center space-x-4 px-4 py-3 rounded-full hover:bg-red-50 dark:hover:bg-red-900/20 transition-colors group w-full mb-3"
      >
        <ArrowLeftOnRectangleIcon class="w-7 h-7 text-gray-500 group-hover:text-red-500" />
        <span class="text-xl hidden xl:block text-gray-500 group-hover:text-red-500">退出登录</span>
      </button>
    </div>
  </nav>
</template>
