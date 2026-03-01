<script setup lang="ts">
import MainLayout from '../layout/MainLayout.vue'
import UserCard from '../components/UserCard.vue'
import { ref, onMounted, watch, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getFollowers, getFollowees, getBatchUsers, getUser } from '../api/user'
import type { UserProfile } from '../api/user'
import { ArrowLeftIcon } from '@heroicons/vue/24/outline'

const route = useRoute()
const router = useRouter()

const type = ref<'followers' | 'following'>((route.query.tab as 'followers' | 'following') || 'followers')
const userId = ref('')
const user = ref<UserProfile | null>(null)
const users = ref<UserProfile[]>([])
const loading = ref(false)
const finished = ref(false)
const cursor = ref('0')

// Tabs
const tabs: { key: 'followers' | 'following', label: string }[] = [
    { key: 'followers', label: '关注者' },
    { key: 'following', label: '正在关注' }
]

const activeTab = computed({
    get: () => type.value,
    set: (val) => {
        router.replace(`/users/${userId.value}/${val}`)
    }
})

const init = async () => {
    userId.value = route.params.id as string
    const pathType = route.path.split('/').pop()
    if (pathType === 'followers' || pathType === 'following') {
        type.value = pathType
    }

    // Load user info for header
    try {
        const res = await getUser(userId.value)
        user.value = res.data.user || res.data
    } catch (e) {
        console.error(e)
    }

    // Load list
    users.value = []
    cursor.value = '0'
    finished.value = false
    loadMore()
}

const loadMore = async () => {
    if (loading.value || finished.value) return
    loading.value = true

    try {
        let res
        if (type.value === 'followers') {
            res = await getFollowers(userId.value, cursor.value)
        } else {
            res = await getFollowees(userId.value, cursor.value)
        }

        const ids = type.value === 'followers' ? res.data.follower_ids : res.data.followee_ids
        
        if (!ids || ids.length === 0) {
            finished.value = true
        } else {
            // Batch fetch user details
            // The API requires strings, but IDs might be numbers or strings. Convert to strings.
            const strIds = ids.map((id: any) => String(id))
            const usersRes = await getBatchUsers(strIds)
            const newUsers = usersRes.data.users || []
            
            // Reorder to match ID order if necessary, but batch API usually returns random order
            // If order matters (e.g. recent follows), we might need to map them back.
            // For now, just append.
            users.value.push(...newUsers)
            
            cursor.value = res.data.next_cursor
            if (!res.data.has_more) {
                finished.value = true
            }
        }
    } catch (error) {
        console.error('Failed to load users', error)
        finished.value = true
    } finally {
        loading.value = false
    }
}

watch(() => route.path, () => {
    init()
})

onMounted(() => {
    init()
})
</script>

<template>
  <MainLayout>
      <!-- Header -->
      <div class="sticky top-0 bg-white/80 dark:bg-black/80 backdrop-blur-md z-10 border-b border-gray-100 dark:border-gray-800 px-4 py-2 flex items-center space-x-6">
          <button @click="router.back()" class="p-2 -ml-2 rounded-full hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors">
              <ArrowLeftIcon class="w-5 h-5" />
          </button>
          <div v-if="user">
              <h1 class="font-bold text-lg leading-tight">{{ user.username }}</h1>
              <div class="text-xs text-gray-500">
                  @{{ user.username }}
              </div>
          </div>
      </div>

      <!-- Tabs -->
      <div class="flex border-b border-gray-100 dark:border-gray-800">
           <div
             v-for="tab in tabs" :key="tab.key"
             class="flex-1 text-center py-4 hover:bg-gray-50 dark:hover:bg-gray-900 cursor-pointer transition-colors relative"
             :class="activeTab === tab.key ? 'font-bold text-gray-900 dark:text-white' : 'text-gray-500'"
             @click="activeTab = tab.key"
           >
               {{ tab.label }}
               <div v-if="activeTab === tab.key" class="absolute bottom-0 left-1/2 -translate-x-1/2 w-14 h-1 bg-primary rounded-full"></div>
           </div>
      </div>

      <!-- List -->
      <div>
          <UserCard v-for="u in users" :key="u.id" :user="u" />
          
          <div v-if="loading" class="p-4 text-center text-gray-500">加载中...</div>
          <div v-if="!loading && users.length === 0" class="p-8 text-center text-gray-500">
              这里好像什么都没有
          </div>
           <!-- Load More Trigger (Simplified) -->
           <div v-if="!finished && !loading && users.length > 0" class="p-4 text-center">
              <button @click="loadMore" class="text-primary hover:underline">加载更多</button>
           </div>
      </div>
  </MainLayout>
</template>
