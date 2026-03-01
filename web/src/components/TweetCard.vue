<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { HeartIcon, ChatBubbleLeftIcon, ArrowPathRoundedSquareIcon, ShareIcon, TrashIcon, BookmarkIcon } from '@heroicons/vue/24/outline'
import { HeartIcon as HeartIconSolid, BookmarkIcon as BookmarkIconSolid } from '@heroicons/vue/24/solid'
import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'
import 'dayjs/locale/zh-cn'
import { type Tweet, likeTweet, unlikeTweet, deleteTweet, addBookmark, removeBookmark, retweetTweet, unretweetTweet, votePoll } from '../api/tweet'
import { useUserStore } from '../stores/user'
import { CheckCircleIcon } from '@heroicons/vue/24/solid'

dayjs.extend(relativeTime)
dayjs.locale('zh-cn')

const props = defineProps<{
  tweet: Tweet
}>()

const emit = defineEmits(['deleted', 'reply'])

const router = useRouter()
const userStore = useUserStore()

const isLiked = ref(props.tweet.is_liked)
const likeCount = ref(props.tweet.like_count)
const isBookmarked = ref(props.tweet.is_bookmarked || false)
const isRetweeted = ref(props.tweet.is_retweeted || false)
const retweetCount = ref(props.tweet.retweet_count || 0)
const poll = ref(props.tweet.poll)

watch(() => props.tweet.poll, (newVal) => {
    poll.value = newVal
})

const handleVote = async (optionId: string) => {
    if (!poll.value || poll.value.is_voted || poll.value.is_expired) return
    
    try {
        const res = await votePoll(poll.value.id, optionId)
        // 更新本地投票数据
        // 假设后端返回最新的 poll 对象
        if (res.data.poll) {
            poll.value = res.data.poll
        }
    } catch (error) {
        console.error('Failed to vote', error)
    }
}


const toggleLike = async () => {
  try {
    if (isLiked.value) {
      await unlikeTweet(props.tweet.id)
      likeCount.value--
      isLiked.value = false
    } else {
      await likeTweet(props.tweet.id)
      likeCount.value++
      isLiked.value = true
    }
  } catch (error) {
    console.error('Failed to toggle like', error)
  }
}

// 点击推文进入详情页
const goToDetail = () => {
  router.push(`/tweets/${props.tweet.id}`)
}



const handleReplyClick = () => {
  emit('reply', props.tweet)
}

// 转发
const handleRetweet = async () => {
  try {
    if (isRetweeted.value) {
      await unretweetTweet(props.tweet.id)
      retweetCount.value--
      isRetweeted.value = false
    } else {
      await retweetTweet(props.tweet.id)
      retweetCount.value++
      isRetweeted.value = true
    }
  } catch (error) {
    console.error('Failed to toggle retweet', error)
  }
}

// 分享 (复制链接)
const handleShare = () => {
  const url = `${window.location.origin}/tweets/${props.tweet.id}`
  navigator.clipboard.writeText(url).then(() => {
    alert('链接已复制到剪贴板')
  })
}

// 切换书签
const toggleBookmark = async () => {
  try {
    if (isBookmarked.value) {
      await removeBookmark(props.tweet.id)
      isBookmarked.value = false
    } else {
      await addBookmark(props.tweet.id)
      isBookmarked.value = true
    }
  } catch (error) {
    console.error('Failed to toggle bookmark', error)
  }
}

// 删除推文
const handleDelete = async () => {
  if (!confirm('确定要删除这条推文吗？')) return
  try {
    await deleteTweet(props.tweet.id)
    emit('deleted', props.tweet.id)
  } catch (error) {
    console.error('Failed to delete tweet', error)
    alert('删除失败')
  }
}

// 是否是自己的推文
const isSelf = () => {
  return userStore.user && props.tweet.user_id === userStore.user.id
}

// 格式化时间
const formattedTime = (() => {
  const ts = props.tweet.created_at
  if (!ts || ts <= 0) return ''
  return dayjs(ts).fromNow()
})()

// 媒体辅助函数
const isVideo = (url: string) => {
    const lower = url.toLowerCase()
    return lower.endsWith('.mp4') || lower.endsWith('.mov') || lower.endsWith('.avi') || lower.endsWith('.webm')
}

const getGridClass = (count: number) => {
    if (count === 1) return 'grid-cols-1'
    if (count === 2) return 'grid-cols-2'
    if (count === 3) return 'grid-cols-2' // TODO: optimizing 3 layout
    return 'grid-cols-2' // 4 -> 2x2
}

const previewImage = (url: string) => {
    // TODO: Implement light box
    window.open(url, '_blank')
}
</script>

<template>
    <div class="border-b border-gray-100 dark:border-gray-800 px-4 py-3 hover:bg-gray-50 dark:hover:bg-gray-900/50 transition-colors cursor-pointer" @click="goToDetail">
    <!-- Retweet Label -->
    <div v-if="tweet.is_retweeted_display" class="flex items-center space-x-2 text-gray-500 text-sm mb-1 ml-10">
        <ArrowPathRoundedSquareIcon class="w-4 h-4" />
        <span class="font-bold">你转发了</span>
    </div>
    
    <div class="flex space-x-3">
      <!-- Avatar -->
      <div class="flex-shrink-0">
        <img
          :src="tweet.user?.avatar || 'https://abs.twimg.com/sticky/default_profile_images/default_profile_400x400.png'"
          alt="Avatar"
          class="w-10 h-10 rounded-full object-cover"
          @click.stop="router.push(tweet.user?.id ? `/users/${tweet.user.id}` : '/explore')"
        />
      </div>

      <!-- Content -->
      <div class="flex-1 min-w-0">
        <!-- Header -->
        <div class="flex items-center justify-between">
          <div class="flex items-center space-x-1 text-sm min-w-0">
            <router-link
              :to="tweet.user?.id ? `/users/${tweet.user.id}` : '/explore'"
              class="font-bold text-gray-900 dark:text-white truncate hover:underline"
              @click.stop
            >{{ tweet.user?.username || 'Unknown' }}</router-link>
            <span class="text-gray-500 truncate">@{{ tweet.user?.username || 'unknown' }}</span>
            <span class="text-gray-500">·</span>
            <span class="text-gray-500 whitespace-nowrap hover:underline">{{ formattedTime }}</span>
          </div>
          <!-- Delete -->
          <button
            v-if="isSelf()"
            @click.stop="handleDelete"
            class="p-1.5 rounded-full hover:bg-red-50 dark:hover:bg-red-900/20 text-gray-400 hover:text-red-500 transition-colors opacity-0 group-hover:opacity-100"
            title="删除"
          >
            <TrashIcon class="w-4 h-4" />
          </button>
        </div>

        <!-- Text -->
        <p class="text-gray-900 dark:text-white mt-1 whitespace-pre-wrap break-words">{{ tweet.content }}</p>

        <!-- Media -->
        <!-- Media -->
        <div v-if="tweet.media_urls && tweet.media_urls.length > 0" class="mt-3 grid gap-1 rounded-2xl overflow-hidden" :class="getGridClass(tweet.media_urls.length)">
            <template v-for="(url, index) in tweet.media_urls" :key="index">
                <!-- Video detection (simple extension check) -->
                <video 
                    v-if="isVideo(url)" 
                    :src="url" 
                    controls 
                    class="w-full h-full object-cover max-h-96 bg-black"
                    @click.stop
                ></video>
                <!-- Image -->
                <img
                    v-else
                    :src="url"
                    class="w-full h-full object-cover max-h-80 hover:opacity-90 transition-opacity"
                    alt="Media"
                    @click.stop="previewImage(url)"
                />
            </template>
        </div>

        <!-- Poll Display -->
        <div v-if="poll" class="mt-3 grid gap-2">
            <div 
                v-for="opt in poll.options" 
                :key="opt.id"
                class="relative h-10 rounded-xl overflow-hidden cursor-pointer transition-all"
                :class="!poll.is_voted && !poll.is_expired ? 'hover:bg-gray-100 dark:hover:bg-gray-800' : ''"
                @click.stop="handleVote(opt.id)"
            >
                <!-- Background Bar -->
                <div v-if="poll.is_voted || poll.is_expired" class="absolute left-0 top-0 h-full bg-blue-100 dark:bg-blue-900/40 transition-all duration-500" :style="{ width: `${opt.percentage}%` }"></div>
                
                <!-- Content -->
                <div class="absolute left-0 top-0 w-full h-full flex items-center justify-between px-3 z-10 pointer-events-none">
                     <div class="flex items-center space-x-2">
                         <span class="font-medium text-gray-900 dark:text-white">{{ opt.text }}</span>
                         <CheckCircleIcon v-if="poll.is_voted && poll.voted_option_id === opt.id" class="w-5 h-5 text-primary" />
                     </div>
                     <span v-if="poll.is_voted || poll.is_expired" class="font-bold text-gray-900 dark:text-white">{{ Math.round(opt.percentage || 0) }}%</span>
                </div>
            </div>
            
            <div class="text-sm text-gray-500 mt-1">
                {{ poll.total_votes || 0 }} 票 · {{ poll.is_expired ? '已结束' : (dayjs(poll.end_time).fromNow(true) + '后结束') }}
            </div>
        </div>

        <!-- Actions -->
        <div class="flex justify-between mt-3 max-w-md text-gray-500">
            <!-- Comment -->
            <button @click.stop="handleReplyClick" class="flex items-center space-x-1 group hover:text-blue-500 transition-colors">
                <div class="p-1.5 rounded-full group-hover:bg-blue-50 dark:group-hover:bg-blue-900/20 transition-colors">
                    <ChatBubbleLeftIcon class="w-[18px] h-[18px]" />
                </div>
                <span class="text-xs">{{ tweet.comment_count || '' }}</span>
            </button>

            <!-- Retweet -->
            <button @click.stop="handleRetweet" class="flex items-center space-x-1 group transition-colors" :class="isRetweeted ? 'text-green-500' : 'hover:text-green-500'">
                <div class="p-1.5 rounded-full group-hover:bg-green-50 dark:group-hover:bg-green-900/20 transition-colors">
                    <ArrowPathRoundedSquareIcon class="w-[18px] h-[18px]" />
                </div>
                <span class="text-xs">{{ retweetCount || '' }}</span>
            </button>

            <!-- Like -->
            <button
              @click.stop="toggleLike"
              class="flex items-center space-x-1 group transition-colors"
              :class="isLiked ? 'text-pink-600' : 'hover:text-pink-600'"
            >
                <div class="p-1.5 rounded-full group-hover:bg-pink-50 dark:group-hover:bg-pink-900/20 transition-colors">
                    <component :is="isLiked ? HeartIconSolid : HeartIcon" class="w-[18px] h-[18px]" />
                </div>
                <span class="text-xs">{{ likeCount || '' }}</span>
            </button>

            <!-- Share -->
            <button @click.stop="handleShare" class="flex items-center group hover:text-blue-500 transition-colors">
                <div class="p-1.5 rounded-full group-hover:bg-blue-50 dark:group-hover:bg-blue-900/20 transition-colors">
                    <ShareIcon class="w-[18px] h-[18px]" />
                </div>
            </button>

            <!-- Bookmark -->
            <button
              @click.stop="toggleBookmark"
              class="flex items-center group transition-colors"
              :class="isBookmarked ? 'text-blue-500' : 'hover:text-blue-500'"
            >
                <div class="p-1.5 rounded-full group-hover:bg-blue-50 dark:group-hover:bg-blue-900/20 transition-colors">
                    <component :is="isBookmarked ? BookmarkIconSolid : BookmarkIcon" class="w-[18px] h-[18px]" />
                </div>
            </button>
        </div>
      </div>
    </div>
  </div>
</template>
