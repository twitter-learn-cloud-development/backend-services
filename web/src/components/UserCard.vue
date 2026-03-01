<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useUserStore } from '../stores/user'
import { followUser, unfollowUser } from '../api/user'
import type { UserProfile } from '../api/user'

const props = defineProps<{
    user: UserProfile
}>()

const router = useRouter()
const userStore = useUserStore()
const loading = ref(false)

// Use local state for optimistic UI updates
const isFollowing = ref(props.user.is_following || false)

const isSelf = computed(() => {
    return userStore.user?.id === props.user.id
})

const handleFollow = async () => {
    if (loading.value) return
    loading.value = true
    try {
        if (isFollowing.value) {
            await unfollowUser(props.user.id)
            isFollowing.value = false
        } else {
            await followUser(props.user.id)
            isFollowing.value = true
        }
    } catch (error) {
        console.error('Failed to update follow status', error)
    } finally {
        loading.value = false
    }
}

const goToProfile = () => {
    router.push(`/users/${props.user.id}`)
}
</script>

<template>
    <div class="flex items-start space-x-3 p-4 hover:bg-gray-50 dark:hover:bg-gray-900/50 transition-colors cursor-pointer border-b border-gray-100 dark:border-gray-800" @click="goToProfile">
        <img
            :src="user.avatar || 'https://abs.twimg.com/sticky/default_profile_images/default_profile_400x400.png'"
            alt="Avatar"
            class="w-12 h-12 rounded-full object-cover flex-shrink-0"
        />
        <div class="flex-1 min-w-0">
            <div class="flex items-center justify-between">
                <div class="group">
                    <h4 class="font-bold text-gray-900 dark:text-white truncate group-hover:underline">{{ user.username }}</h4>
                    <div class="text-gray-500 text-sm truncate">@{{ user.username }}</div>
                </div>
                <!-- Follow Button -->
                <button
                    v-if="!isSelf"
                    @click.stop="handleFollow"
                    class="rounded-full px-4 py-1.5 text-sm font-bold transition-all duration-200 flex-shrink-0 ml-2"
                    :class="[
                        isFollowing
                            ? 'border border-gray-300 dark:border-gray-600 text-gray-900 dark:text-white hover:bg-red-50 hover:text-red-600 hover:border-red-600 group w-[100px]'
                            : 'bg-black dark:bg-white text-white dark:text-black hover:bg-gray-800 dark:hover:bg-gray-200'
                    ]"
                >
                    <span v-if="isFollowing" class="group-hover:hidden">已关注</span>
                    <span v-if="isFollowing" class="hidden group-hover:block">取消关注</span>
                    <span v-else>关注</span>
                </button>
            </div>
            <p v-if="user.bio" class="text-gray-900 dark:text-white mt-1 text-sm break-words line-clamp-2">{{ user.bio }}</p>
        </div>
    </div>
</template>
