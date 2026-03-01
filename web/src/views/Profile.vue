<script setup lang="ts">
import MainLayout from '../layout/MainLayout.vue'
import TweetCard from '../components/TweetCard.vue'
import { ref, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useUserStore } from '../stores/user'
import { getUser, getUserTimeline, followUser, unfollowUser, getFollowStats, isFollowing as checkIsFollowing, updateProfile, getUserLikes, getUserReplies, getUserMedia } from '../api/user'
import type { Tweet } from '../api/tweet'
import type { UserProfile } from '../api/user'
import { ArrowLeftIcon, EnvelopeIcon } from '@heroicons/vue/24/outline'
import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'
import 'dayjs/locale/zh-cn'

dayjs.extend(relativeTime)
dayjs.locale('zh-cn')

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()

const user = ref<UserProfile | null>(null)
const tweets = ref<Tweet[]>([])
const likedTweets = ref<Tweet[]>([])
const replies = ref<any[]>([])
const mediaTweets = ref<Tweet[]>([])
const loading = ref(false)
const timelineLoading = ref(false)
const tabLoading = ref(false)
const isSelf = ref(false)
const isFollowing = ref(false)
const stats = ref<{follower_count: number, followee_count: number}>({ follower_count: 0, followee_count: 0 })

// Tab 切换
const activeTab = ref('tweets')
const tabs = [
    { key: 'tweets', label: '帖子' },
    { key: 'replies', label: '回复' },
    { key: 'media', label: '媒体' },
    { key: 'likes', label: '喜欢' },
]

// 编辑资料
const showEditModal = ref(false)
const editForm = ref({ bio: '', avatar: '', cover_url: '', website: '', location: '' })
const editLoading = ref(false)

// 获取用户ID: 路由参数 > 当前登录用户
const getTargetUserId = () => {
    const id = route.params.id as string
    if (id) return id
    return userStore.user?.id
}

const fetchUserData = async () => {
    const userId = getTargetUserId()
    if (!userId) return

    loading.value = true
    try {
        isSelf.value = userId == userStore.user?.id

        // 1. 获取用户信息
        const userRes = await getUser(userId)
        user.value = userRes.data.user || userRes.data

        // 2. 获取统计数据
        try {
            const statsRes = await getFollowStats(userId)
            stats.value = statsRes.data
        } catch {
            stats.value = { follower_count: 0, followee_count: 0 }
        }

        // 3. 检查关注状态 (如果不是自己且已登录)
        if (!isSelf.value && userStore.user) {
            try {
                const followRes = await checkIsFollowing(userId)
                isFollowing.value = followRes.data.is_following
            } catch {
                isFollowing.value = false
            }
        }

        // 4. 获取推文列表
        fetchTimeline(userId)

    } catch (error) {
        console.error('Failed to load profile', error)
    } finally {
        loading.value = false
    }
}

const fetchTimeline = async (userId: string) => {
    timelineLoading.value = true
    try {
        const res = await getUserTimeline(userId)
        tweets.value = res.data.tweets || []
    } catch (error) {
        console.error('Failed to load timeline', error)
    } finally {
        timelineLoading.value = false
    }
}

const handleMessage = () => {
    if (!user.value) return
    router.push({ path: '/messages', query: { peer_id: user.value.id } })
}

const handleFollow = async () => {
    if (!user.value) return
    try {
        if (isFollowing.value) {
            await unfollowUser(user.value.id)
            isFollowing.value = false
            stats.value.follower_count--
        } else {
            await followUser(user.value.id)
            isFollowing.value = true
            stats.value.follower_count++
        }
    } catch (error) {
        console.error('Failed to update follow status', error)
        alert('操作失败')
    }
}

const switchTab = (tabKey: string) => {
    activeTab.value = tabKey
    const userId = getTargetUserId()
    if (!userId) return

    if (tabKey === 'tweets') {
        fetchTimeline(userId)
    } else if (tabKey === 'likes') {
        fetchUserLikes(userId)
    } else if (tabKey === 'replies') {
        fetchUserReplies(userId)
    } else if (tabKey === 'media') {
        fetchUserMedia(userId)
    }
}

const fetchUserLikes = async (userId: string) => {
    tabLoading.value = true
    try {
        const res = await getUserLikes(userId)
        likedTweets.value = res.data.tweets || []
    } catch (error) {
        console.error('Failed to load likes', error)
    } finally {
        tabLoading.value = false
    }
}

const fetchUserReplies = async (userId: string) => {
    tabLoading.value = true
    try {
        const res = await getUserReplies(userId)
        replies.value = res.data.replies || []
    } catch (error) {
        console.error('Failed to load replies', error)
    } finally {
        tabLoading.value = false
    }
}

const fetchUserMedia = async (userId: string) => {
    tabLoading.value = true
    try {
        const res = await getUserMedia(userId)
        mediaTweets.value = res.data.tweets || []
    } catch (error) {
        console.error('Failed to load media', error)
    } finally {
        tabLoading.value = false
    }
}

const openEditModal = () => {
    if (user.value) {
        // 只编辑后端支持的字段：bio 和 avatar
        editForm.value = {
            bio: user.value.bio || '',
            avatar: user.value.avatar || '',
            cover_url: user.value.cover_url || '',
            website: user.value.website || '',
            location: user.value.location || ''
        }
    }
    showEditModal.value = true
}

const saveProfile = async () => {
    editLoading.value = true
    try {
        const res = await updateProfile({
            bio: editForm.value.bio,
            avatar: editForm.value.avatar,
            cover_url: editForm.value.cover_url,
            website: editForm.value.website,
            location: editForm.value.location
        })
        // 关键修复: 用后端返回的完整用户数据覆盖，而非手动赋值
        if (res.data.user) {
            user.value = res.data.user
        }
        showEditModal.value = false
        alert('资料更新成功')
    } catch (error) {
        console.error('Failed to update profile', error)
        alert('更新失败')
    } finally {
        editLoading.value = false
    }
}

const handleTweetDeleted = (tweetId: string) => {
    tweets.value = tweets.value.filter(t => t.id !== tweetId)
}

// 格式化加入时间
const formatJoinDate = (ts: number) => {
    if (!ts || ts <= 0) return '未知'
    return dayjs(ts).format('YYYY年MM月')
}

const formatReplyTime = (ts: number) => {
    if (!ts || ts <= 0) return ''
    return dayjs(ts).fromNow()
}

// 监听路由变化
watch(() => route.params.id, () => {
    fetchUserData()
})

onMounted(() => {
    fetchUserData()
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
              <div class="text-xs text-gray-500">{{ tweets.length }} 条帖子</div>
          </div>
      </div>

      <div v-if="loading" class="p-8 text-center">加载中...</div>

      <div v-else-if="user">
          <!-- Cover -->
          <div class="h-48 bg-gray-200 dark:bg-gray-800 relative bg-cover bg-center" :style="user.cover_url ? { backgroundImage: `url(${user.cover_url})` } : { background: 'linear-gradient(to right, #60a5fa, #3b82f6, #2563eb)' }">
               <img
                  :src="user.avatar || 'https://abs.twimg.com/sticky/default_profile_images/default_profile_400x400.png'"
                  class="w-[134px] h-[134px] rounded-full border-4 border-white dark:border-black absolute -bottom-[67px] left-4 object-cover bg-white"
                />
          </div>

          <!-- Actions -->
          <div class="flex justify-end p-4 h-[72px] space-x-3">
              <button
                v-if="isSelf"
                @click="openEditModal"
                class="border border-gray-300 dark:border-gray-600 font-bold px-4 py-1.5 rounded-full hover:bg-gray-100 dark:hover:bg-gray-900 transition-colors text-sm"
              >
                  编辑个人资料
              </button>
              <template v-else>
                   <button
                    @click="handleMessage"
                    class="w-[34px] h-[34px] flex items-center justify-center border border-gray-300 dark:border-gray-600 rounded-full hover:bg-gray-100 dark:hover:bg-gray-900 transition-colors"
                    title="发送私信"
                  >
                      <EnvelopeIcon class="w-5 h-5" />
                  </button>
                  <button
                    @click="handleFollow"
                    class="font-bold px-5 py-1.5 rounded-full transition-colors text-sm flex items-center justify-center min-w-[100px]"
                    :class="isFollowing
                        ? 'border border-gray-300 dark:border-gray-600 hover:bg-red-50 hover:text-red-500 hover:border-red-500 group'
                        : 'bg-gray-900 dark:bg-white text-white dark:text-black hover:bg-gray-800 dark:hover:bg-gray-200'"
                  >
                      <span v-if="isFollowing" class="group-hover:hidden">已关注</span>
                      <span v-if="isFollowing" class="hidden group-hover:block">取消关注</span>
                      <span v-else>关注</span>
                  </button>
              </template>
          </div>

          <!-- Info -->
          <div class="px-4 mt-1">
              <h1 class="font-extrabold text-xl">{{ user.username }}</h1>
              <div class="text-gray-500 text-sm">@{{ user.username }}</div>

              <div v-if="user.bio" class="mt-3 text-gray-900 dark:text-white whitespace-pre-wrap text-[15px]">{{ user.bio }}</div>

              <div class="mt-3 text-gray-500 flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
                  <div v-if="user.location" class="flex items-center space-x-1">
                      <svg viewBox="0 0 24 24" class="w-[18px] h-[18px] fill-current"><path d="M12 2C8.13 2 5 5.13 5 9c0 5.25 7 13 7 13s7-7.75 7-13c0-3.87-3.13-7-7-7zm0 9.5c-1.38 0-2.5-1.12-2.5-2.5s1.12-2.5 2.5-2.5 2.5 1.12 2.5 2.5-1.12 2.5-2.5 2.5z"></path></svg>
                      <span>{{ user.location }}</span>
                  </div>
                  <div v-if="user.website" class="flex items-center space-x-1">
                      <svg viewBox="0 0 24 24" class="w-[18px] h-[18px] fill-current"><path d="M11.96 14.945c-.067 0-.136-.01-.203-.027-1.13-.318-2.097-.986-2.795-1.932-.832-1.125-1.176-2.508-.968-3.893s.942-2.605 2.068-3.438l3.53-2.608c2.322-1.716 5.61-1.224 7.33 1.1.83 1.127 1.175 2.51.967 3.895s-.943 2.605-2.07 3.438l-1.48 1.09c-.333.246-.804.175-1.05-.158-.246-.334-.176-.804.158-1.05l1.48-1.09c.813-.6 1.36-1.477 1.51-2.473.15-.997-.097-1.995-.697-2.81-.697-.943-1.757-1.484-2.91-1.484-.5 0-.996.102-1.473.305-.332.142-.72.016-.89-.285-.17-.302-.072-.685.18-.87 3.903-2.887 9.156-1.583 11.232 2.792h.002c1.23 1.66 1.745 3.715 1.436 5.776-.43 2.872-2.875 5.163-5.857 5.753-.578 4.2-4.04 6.758-7.72 5.708zM6.46 15.83l-1.48 1.09c-1.126.833-1.86 2.054-2.068 3.438-.208 1.385.136 2.768.968 3.894 1.72 2.324 5.008 2.815 7.33 1.1l3.53-2.608c1.126-.832 1.86-2.053 2.068-3.437.208-1.385-.136-2.768-.968-3.894l-1.48-1.09c-.334-.246-.805-.175-1.05.158-.246.334-.176.804.158 1.05l1.48 1.09c.813.6 1.36 1.477 1.51 2.473.15.997-.097 1.995-.697 2.81-.697.943-1.758 1.484-2.91 1.484-2.522 0-4.717-1.844-5.184-4.32-.467-2.475.69-4.88 2.808-5.856l.002-.002.002.002.002-.002c.328-.145.485-.526.34-.857-.144-.33-.525-.486-.855-.342-3.175 1.464-4.72 5.09-3.965 8.358z"></path></svg>
                      <a :href="user.website" target="_blank" rel="noopener noreferrer" class="text-primary hover:underline truncate max-w-[200px]">{{ user.website.replace(/^https?:\/\//, '') }}</a>
                  </div>
                  <div class="flex items-center space-x-1">
                      <svg viewBox="0 0 24 24" class="w-[18px] h-[18px] fill-current"><path d="M7 4V3h2v1h6V3h2v1h1.5C19.89 4 21 5.12 21 6.5v12c0 1.38-1.11 2.5-2.5 2.5h-13C4.12 21 3 19.88 3 18.5v-12C3 5.12 4.12 4 5.5 4H7zm0 2H5.5c-.27 0-.5.22-.5.5v12c0 .28.23.5.5.5h13c.28 0 .5-.22.5-.5v-12c0-.28-.22-.5-.5-.5H17v1h-2V6H9v1H7V6zm0 6h2v2H7v-2zm4 0h2v2h-2v-2zm4 0h2v2h-2v-2z"></path></svg>
                      <span>{{ formatJoinDate(user.created_at) }} 加入</span>
                  </div>
              </div>

              <div class="mt-3 flex space-x-5 text-sm">
                  <div class="hover:underline cursor-pointer" @click="router.push(`/users/${user.id}/following`)">
                      <span class="font-bold text-gray-900 dark:text-white">{{ stats.followee_count }}</span>
                      <span class="text-gray-500 ml-1">正在关注</span>
                  </div>
                  <div class="hover:underline cursor-pointer" @click="router.push(`/users/${user.id}/followers`)">
                      <span class="font-bold text-gray-900 dark:text-white">{{ stats.follower_count }}</span>
                      <span class="text-gray-500 ml-1">关注者</span>
                  </div>
              </div>
          </div>

          <!-- Tabs -->
          <div class="flex border-b border-gray-100 dark:border-gray-800 mt-4">
               <button
                 v-for="tab in tabs" :key="tab.key"
                 class="flex-1 text-center py-4 hover:bg-gray-50 dark:hover:bg-gray-900 cursor-pointer transition-colors relative"
                 :class="activeTab === tab.key ? 'font-bold text-gray-900 dark:text-white' : 'text-gray-500'"
                 @click="switchTab(tab.key)"
               >
                   {{ tab.label }}
                   <div v-if="activeTab === tab.key" class="absolute bottom-0 left-1/2 -translate-x-1/2 w-14 h-1 bg-primary rounded-full"></div>
               </button>
          </div>

          <!-- Timeline (帖子 Tab) -->
          <div v-if="activeTab === 'tweets'">
               <div v-if="timelineLoading" class="p-8 text-center">加载推文中...</div>
               <div v-else>
                   <TweetCard v-for="tweet in tweets" :key="tweet.id" :tweet="tweet" @deleted="handleTweetDeleted" />
                   <div v-if="tweets.length === 0" class="p-8 text-center text-gray-500">
                       <span v-if="isSelf">你还没有发过帖子</span>
                       <span v-else>该用户还没有发过帖子</span>
                   </div>
               </div>
          </div>

          <!-- 喜欢 Tab -->
          <div v-else-if="activeTab === 'likes'">
               <div v-if="tabLoading" class="p-8 text-center">加载中...</div>
               <div v-else>
                   <TweetCard v-for="tweet in likedTweets" :key="tweet.id" :tweet="tweet" />
                   <div v-if="likedTweets.length === 0" class="p-8 text-center text-gray-500">暂无喜欢的帖子</div>
               </div>
          </div>

          <!-- 回复 Tab -->
          <div v-else-if="activeTab === 'replies'">
               <div v-if="tabLoading" class="p-8 text-center">加载中...</div>
               <div v-else>
                   <div v-for="reply in replies" :key="reply.id" class="border-b border-gray-100 dark:border-gray-800 px-4 py-3 hover:bg-gray-50 dark:hover:bg-gray-900/50 transition-colors">
                       <!-- 原推文引用 -->
                       <div v-if="reply.tweet" class="mb-2 pl-3 border-l-2 border-gray-200 dark:border-gray-700 text-sm text-gray-500">
                           <router-link
                             :to="reply.tweet.user?.id ? `/users/${reply.tweet.user.id}` : '/explore'"
                             class="font-semibold text-gray-700 dark:text-gray-300 hover:underline"
                             @click.stop
                           >{{ reply.tweet.user?.username || 'unknown' }}</router-link>
                           <span class="ml-1">的帖子：</span>
                           <router-link :to="`/tweets/${reply.tweet_id}`" class="hover:underline" @click.stop>
                             {{ reply.tweet.content?.substring(0, 80) }}{{ reply.tweet.content?.length > 80 ? '...' : '' }}
                           </router-link>
                       </div>
                       <!-- 回复内容 -->
                       <div class="flex space-x-3">
                           <img
                             :src="reply.user?.avatar || 'https://abs.twimg.com/sticky/default_profile_images/default_profile_400x400.png'"
                             alt="Avatar"
                             class="w-10 h-10 rounded-full object-cover flex-shrink-0"
                           />
                           <div class="flex-1 min-w-0">
                               <div class="flex items-center space-x-1 text-sm">
                                   <span class="font-bold text-gray-900 dark:text-white truncate">{{ reply.user?.username || 'unknown' }}</span>
                                   <span class="text-gray-500">·</span>
                                   <span class="text-gray-500 text-xs">{{ formatReplyTime(reply.created_at) }}</span>
                               </div>
                               <p class="text-gray-900 dark:text-white mt-1 whitespace-pre-wrap break-words">{{ reply.content }}</p>
                           </div>
                       </div>
                   </div>
                   <div v-if="replies.length === 0" class="p-8 text-center text-gray-500">暂无回复</div>
               </div>
          </div>

          <!-- 媒体 Tab -->
          <div v-else-if="activeTab === 'media'">
               <div v-if="tabLoading" class="p-8 text-center">加载中...</div>
               <div v-else>
                   <TweetCard v-for="tweet in mediaTweets" :key="tweet.id" :tweet="tweet" />
                   <div v-if="mediaTweets.length === 0" class="p-8 text-center text-gray-500">暂无媒体内容</div>
               </div>
          </div>
      </div>

      <div v-else class="p-10 text-center text-gray-500">
          <p class="text-xl font-bold mb-2">此账号不存在</p>
          <p class="mb-4">请检查链接是否正确</p>
          <button @click="$router.push('/')" class="bg-primary text-white px-5 py-2.5 rounded-full font-bold">
              返回首页
          </button>
      </div>

      <!-- Edit Profile Modal -->
      <div v-if="showEditModal" class="fixed inset-0 bg-black/50 z-50 flex items-start justify-center pt-12" @click.self="showEditModal = false">
          <div class="bg-white dark:bg-black rounded-2xl w-full max-w-xl mx-4 shadow-2xl max-h-[90vh] overflow-y-auto">
              <!-- Modal Header -->
              <div class="sticky top-0 bg-white/90 dark:bg-black/90 backdrop-blur-md flex items-center justify-between px-4 py-3 border-b border-gray-100 dark:border-gray-800 z-10">
                  <div class="flex items-center space-x-6">
                      <button @click="showEditModal = false" class="p-1 rounded-full hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors">
                          <svg viewBox="0 0 24 24" class="w-5 h-5 fill-current"><path d="M10.59 12L4.54 5.96l1.42-1.42L12 10.59l6.04-6.05 1.42 1.42L13.41 12l6.05 6.04-1.42 1.42L12 13.41l-6.04 6.05-1.42-1.42L10.59 12z"></path></svg>
                      </button>
                      <h3 class="font-bold text-xl">编辑个人资料</h3>
                  </div>
                  <button
                    @click="saveProfile"
                    :disabled="editLoading"
                    class="bg-gray-900 dark:bg-white text-white dark:text-black font-bold py-1.5 px-5 rounded-full text-sm disabled:opacity-50 hover:bg-gray-800 dark:hover:bg-gray-200 transition-colors"
                  >
                      {{ editLoading ? '保存中...' : '保存' }}
                  </button>
              </div>

              <!-- Banner + Avatar Preview -->
              <div class="h-48 bg-gray-200 dark:bg-gray-800 relative bg-cover bg-center" :style="editForm.cover_url ? { backgroundImage: `url(${editForm.cover_url})` } : { background: 'linear-gradient(to right, #60a5fa, #3b82f6, #2563eb)' }">
                  <img
                    v-if="editForm.avatar"
                    :src="editForm.avatar"
                    class="w-28 h-28 rounded-full border-4 border-white dark:border-black absolute -bottom-14 left-4 object-cover bg-white"
                  />
                  <div v-else class="w-28 h-28 rounded-full border-4 border-white dark:border-black absolute -bottom-14 left-4 bg-gray-200 flex items-center justify-center">
                      <svg viewBox="0 0 24 24" class="w-10 h-10 fill-gray-400"><path d="M12 11.816c1.355 0 2.872-.15 3.84-1.256.814-.93 1.078-2.368.806-4.392-.38-2.825-2.117-4.512-4.646-4.512S7.734 3.343 7.354 6.168c-.272 2.024-.008 3.462.806 4.392.968 1.107 2.485 1.256 3.84 1.256zM8.84 6.368c.162-1.2.787-3.212 3.16-3.212s2.998 2.013 3.16 3.212c.207 1.55.057 2.627-.45 3.205-.455.52-1.266.743-2.71.743s-2.255-.223-2.71-.743c-.507-.578-.657-1.656-.45-3.205zm11.44 12.868c-.877-3.526-4.282-5.99-8.28-5.99s-7.403 2.464-8.28 5.99c-.172.692-.028 1.4.395 1.94.408.52 1.04.82 1.733.82h12.304c.693 0 1.325-.3 1.733-.82.424-.54.567-1.247.394-1.94zm-1.576 1.016c-.126.16-.316.246-.552.246H5.848c-.235 0-.426-.085-.552-.246-.137-.174-.18-.412-.12-.654.71-2.855 3.517-4.85 6.824-4.85s6.114 1.994 6.824 4.85c.06.242.017.48-.12.654z"></path></svg>
                  </div>
              </div>

              <!-- Form -->
              <div class="px-4 pt-20 pb-6 space-y-5">
                  <div class="relative">
                      <label class="absolute left-3 top-2 text-xs text-gray-500">头像链接</label>
                      <input
                        v-model="editForm.avatar"
                        type="text"
                        class="w-full border border-gray-200 dark:border-gray-700 rounded-lg pt-7 pb-2 px-3 focus:ring-1 focus:ring-primary focus:border-primary outline-none dark:bg-black dark:text-white text-sm"
                        placeholder="https://example.com/avatar.jpg"
                      />
                  </div>
                  <div class="relative">
                      <label class="absolute left-3 top-2 text-xs text-gray-500">个人简介</label>
                      <textarea
                        v-model="editForm.bio"
                        class="w-full border border-gray-200 dark:border-gray-700 rounded-lg pt-7 pb-2 px-3 min-h-[80px] focus:ring-1 focus:ring-primary focus:border-primary outline-none dark:bg-black dark:text-white resize-none text-sm"
                        placeholder="介绍一下你自己"
                      ></textarea>
                      <div class="text-right text-xs text-gray-400 mt-1">{{ editForm.bio.length }} / 255</div>
                  </div>
                  <div class="relative">
                      <label class="absolute left-3 top-2 text-xs text-gray-500">封面图片</label>
                      <input
                        v-model="editForm.cover_url"
                        type="text"
                        class="w-full border border-gray-200 dark:border-gray-700 rounded-lg pt-7 pb-2 px-3 focus:ring-1 focus:ring-primary focus:border-primary outline-none dark:bg-black dark:text-white text-sm"
                        placeholder="https://example.com/cover.jpg"
                      />
                  </div>
                  <div class="relative">
                      <label class="absolute left-3 top-2 text-xs text-gray-500">个人网站</label>
                      <input
                        v-model="editForm.website"
                        type="text"
                        class="w-full border border-gray-200 dark:border-gray-700 rounded-lg pt-7 pb-2 px-3 focus:ring-1 focus:ring-primary focus:border-primary outline-none dark:bg-black dark:text-white text-sm"
                        placeholder="https://your-website.com"
                      />
                  </div>
                  <div class="relative">
                      <label class="absolute left-3 top-2 text-xs text-gray-500">地理位置</label>
                      <input
                        v-model="editForm.location"
                        type="text"
                        class="w-full border border-gray-200 dark:border-gray-700 rounded-lg pt-7 pb-2 px-3 focus:ring-1 focus:ring-primary focus:border-primary outline-none dark:bg-black dark:text-white text-sm"
                        placeholder="例如：北京, 中国"
                      />
                  </div>

              </div>
          </div>
      </div>
  </MainLayout>
</template>
