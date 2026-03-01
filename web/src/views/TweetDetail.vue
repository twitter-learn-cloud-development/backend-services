<script setup lang="ts">
import MainLayout from '../layout/MainLayout.vue'
import { ref, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useUserStore } from '../stores/user'

import TweetCard from '../components/TweetCard.vue'
import ReplyModal from '../components/ReplyModal.vue'
import { HeartIcon, ChatBubbleLeftIcon, ArrowPathRoundedSquareIcon, ShareIcon, ArrowLeftIcon, TrashIcon } from '@heroicons/vue/24/outline'
import { HeartIcon as HeartIconSolid, CheckCircleIcon } from '@heroicons/vue/24/solid'
import { getTweet, getComments, getTweetReplies, createComment, likeTweet, unlikeTweet, deleteTweet, votePoll, retweetTweet, unretweetTweet, type Tweet, type Comment } from '../api/tweet'
import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'
import 'dayjs/locale/zh-cn'

dayjs.extend(relativeTime)
dayjs.locale('zh-cn')

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()

const tweet = ref<Tweet | null>(null)
const comments = ref<Comment[]>([])
const loading = ref(false)
const commentsLoading = ref(false)
const commentContent = ref('')
const commentSubmitting = ref(false)
const isLiked = ref(false)
const likeCount = ref(0)
const isRetweeted = ref(false)
const retweetCount = ref(0)
const commentsCursor = ref('0')
const hasMoreComments = ref(true)

const tweetId = () => route.params.id as string

// 格式化时间
const formatTime = (ts: number) => {
    if (!ts || ts <= 0) return ''
    return dayjs(ts).format('YYYY年M月D日 HH:mm')
}
const formatRelativeTime = (ts: number) => {
    if (!ts || ts <= 0) return ''
    return dayjs(ts).fromNow()
}

// 获取推文详情
const fetchTweet = async () => {
    loading.value = true
    try {
        const res = await getTweet(tweetId())
        tweet.value = res.data.tweet || res.data
        isLiked.value = tweet.value!.is_liked
        likeCount.value = tweet.value!.like_count
        isRetweeted.value = tweet.value!.is_retweeted || false
        retweetCount.value = tweet.value!.retweet_count || 0
    } catch (error) {
        console.error('Failed to load tweet', error)
    } finally {
        loading.value = false
    }
}

// 获取评论列表
const fetchComments = async (refresh = false) => {
    if (commentsLoading.value) return
    commentsLoading.value = true
    try {
        const cursor = refresh ? '0' : commentsCursor.value
        const res = await getComments(tweetId(), cursor)
        const newComments = res.data.comments || []
        if (refresh) {
            comments.value = newComments
        } else {
            comments.value.push(...newComments)
        }
        commentsCursor.value = res.data.next_cursor || '0'
        hasMoreComments.value = res.data.has_more || false
    } catch (error) {
        console.error('Failed to load comments', error)
    } finally {
        commentsLoading.value = false
    }
}

const replies = ref<Tweet[]>([])
const repliesLoading = ref(false)
const repliesCursor = ref('0')
const hasMoreReplies = ref(true)

const fetchReplies = async (refresh = false) => {
    if (repliesLoading.value) return
    repliesLoading.value = true
    try {
        const cursor = refresh ? '0' : repliesCursor.value
        const res = await getTweetReplies(tweetId(), cursor)
        const newReplies = res.data.replies || []
        if (refresh) {
            replies.value = newReplies
        } else {
            replies.value.push(...newReplies)
        }
        repliesCursor.value = res.data.next_cursor || '0'
        hasMoreReplies.value = res.data.has_more || false
    } catch (error) {
        console.error('Failed to load replies', error)
    } finally {
        repliesLoading.value = false
    }
}

const parentTweet = ref<Tweet | null>(null)
const fetchParentTweet = async (parentId: string) => {
    try {
        const res = await getTweet(parentId)
        parentTweet.value = res.data.tweet || res.data
    } catch(e) {
        console.error('Failed to load parent tweet', e)
    }
}

const replyTarget = ref<Tweet | null>(null)
const showReplyModal = ref(false)
const handleReply = (target?: any) => {
    if (target && target.id) {
        replyTarget.value = target as Tweet
    } else if (tweet.value) {
        replyTarget.value = tweet.value
    }
    if (replyTarget.value) {
        showReplyModal.value = true
    }
}
const handleReplyCreated = () => {
    // 刷新回复列表
    fetchReplies(true)
    if (tweet.value) {
        tweet.value.comment_count++
    }
}

const replyToComment = ref<Comment | null>(null)
const handleReplyToComment = (c: Comment) => {
    replyToComment.value = c
    commentContent.value = `@${c.user?.username} `
    // Option to focus textarea could go here via a template ref
    const textarea = document.querySelector('textarea')
    if (textarea) textarea.focus()
}

// 发表评论
const handleSubmitComment = async () => {
    if (!commentContent.value.trim() || commentSubmitting.value) return
    commentSubmitting.value = true
    try {
        const parentId = replyToComment.value ? replyToComment.value.id : undefined;
        const res = await createComment(tweetId(), commentContent.value, parentId)
        // 添加到列表顶部
        const newComment = res.data.comment || res.data
        comments.value.unshift(newComment)
        commentContent.value = ''
        replyToComment.value = null
        // 更新推文的评论计数
        if (tweet.value) {
            tweet.value.comment_count++
        }
    } catch (error) {
        console.error('Failed to submit comment', error)
        alert('评论发送失败')
    } finally {
        commentSubmitting.value = false
    }
}

// 点赞
const toggleLike = async () => {
    try {
        if (isLiked.value) {
            await unlikeTweet(tweetId())
            likeCount.value--
            isLiked.value = false
        } else {
            await likeTweet(tweetId())
            likeCount.value++
            isLiked.value = true
        }
    } catch (error) {
        console.error('Failed to toggle like', error)
    }
}

// 删除推文
const handleDelete = async () => {
    if (!confirm('确定要删除这条推文吗？删除后无法恢复。')) return
    try {
        await deleteTweet(tweetId())
        router.replace('/')
    } catch (error) {
        console.error('Failed to delete tweet', error)
        alert('删除失败')
    }
}

// 转推
const handleRetweet = async () => {
    try {
        if (isRetweeted.value) {
            await unretweetTweet(tweetId())
            retweetCount.value--
            isRetweeted.value = false
        } else {
            await retweetTweet(tweetId())
            retweetCount.value++
            isRetweeted.value = true
        }
    } catch (error) {
        console.error('Failed to toggle retweet', error)
    }
}

// 分享
const handleShare = () => {
    const url = `${window.location.origin}/tweets/${tweetId()}`
    navigator.clipboard.writeText(url).then(() => {
        alert('链接已复制到剪贴板')
    })
}

const isSelf = () => {
    return tweet.value && userStore.user && tweet.value.user_id === userStore.user.id
}

const handleVote = async (optionId: string) => {
    if (!tweet.value?.poll || tweet.value.poll.is_voted || tweet.value.poll.is_expired) return
    
    try {
        const res = await votePoll(tweet.value.poll.id, optionId)
        if (res.data.poll && tweet.value) {
            tweet.value.poll = res.data.poll
        }
    } catch (error) {
        console.error('Failed to vote', error)
    }
}


// 监听路由变化
watch(() => route.params.id, () => {
    if (route.params.id) {
        fetchTweet().then(() => {
            if (tweet.value?.parent_id) {
                fetchParentTweet(tweet.value.parent_id)
            }
        })
        fetchComments(true)
        fetchReplies(true)
    }
})

onMounted(() => {
    fetchTweet().then(() => {
        if (tweet.value?.parent_id) {
            fetchParentTweet(tweet.value.parent_id)
        }
    })
    fetchComments(true)
    fetchReplies(true)
})
</script>

<template>
  <MainLayout>
      <!-- Header -->
      <div class="sticky top-0 bg-white/80 dark:bg-black/80 backdrop-blur-md z-10 border-b border-gray-100 dark:border-gray-800 px-4 py-3 flex items-center space-x-6">
          <button @click="router.back()" class="p-2 -ml-2 rounded-full hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors">
              <ArrowLeftIcon class="w-5 h-5" />
          </button>
          <h1 class="font-bold text-xl">帖子</h1>
      </div>

      <!-- Loading -->
      <div v-if="loading" class="p-8 text-center text-gray-500">加载中...</div>

      <!-- Parent Tweet -->
      <div v-else-if="parentTweet" class="border-b border-gray-100 dark:border-gray-800">
          <TweetCard :tweet="parentTweet" @reply="handleReply" />
          <div class="px-8 ml-4 border-l-2 border-gray-200 dark:border-gray-700 h-4"></div>
      </div>

      <!-- Tweet Detail -->
      <div v-if="tweet" class="border-b border-gray-100 dark:border-gray-800">
          <!-- Author -->
          <div class="px-4 pt-4 flex items-start space-x-3">
              <img
                :src="tweet.user?.avatar || 'https://abs.twimg.com/sticky/default_profile_images/default_profile_400x400.png'"
                class="w-12 h-12 rounded-full object-cover cursor-pointer"
                @click="router.push(tweet.user?.id ? `/users/${tweet.user.id}` : '/explore')"
              />
              <div class="flex-1 min-w-0">
                  <div class="flex items-center justify-between">
                      <div>
                          <router-link
                            :to="tweet.user?.id ? `/users/${tweet.user.id}` : '/explore'"
                            class="font-bold text-gray-900 dark:text-white hover:underline"
                          >{{ tweet.user?.username || 'Unknown' }}</router-link>
                          <div class="text-gray-500 text-sm">@{{ tweet.user?.username || 'unknown' }}</div>
                      </div>
                      <button
                        v-if="isSelf()"
                        @click="handleDelete"
                        class="p-2 rounded-full hover:bg-red-50 dark:hover:bg-red-900/20 text-gray-400 hover:text-red-500 transition-colors"
                        title="删除推文"
                      >
                          <TrashIcon class="w-5 h-5" />
                      </button>
                  </div>
              </div>
          </div>

          <!-- Content -->
          <div class="px-4 mt-3">
              <p class="text-xl text-gray-900 dark:text-white whitespace-pre-wrap break-words leading-relaxed">{{ tweet.content }}</p>
          </div>

          <!-- Media -->
          <div v-if="tweet.media_urls && tweet.media_urls.length > 0" class="px-4 mt-3">
              <div class="grid gap-2" :class="tweet.media_urls.length > 1 ? 'grid-cols-2' : 'grid-cols-1'">
                  <img
                    v-for="(url, index) in tweet.media_urls"
                    :key="index"
                    :src="url"
                    class="rounded-2xl border border-gray-100 dark:border-gray-800 w-full object-cover max-h-[500px]"
                  />
              </div>
          </div>

        <!-- Poll Display -->
        <div v-if="tweet.poll" class="px-4 mt-3 grid gap-2 max-w-lg">
            <div 
                v-for="opt in tweet.poll.options" 
                :key="opt.id"
                class="relative h-12 rounded-xl overflow-hidden cursor-pointer transition-all"
                :class="!tweet.poll.is_voted && !tweet.poll.is_expired ? 'hover:bg-gray-100 dark:hover:bg-gray-800' : ''"
                @click="handleVote(opt.id)"
            >
                <!-- Background Bar -->
                <div v-if="tweet.poll.is_voted || tweet.poll.is_expired" class="absolute left-0 top-0 h-full bg-blue-100 dark:bg-blue-900/40 transition-all duration-500" :style="{ width: `${opt.percentage}%` }"></div>
                
                <!-- Content -->
                <div class="absolute left-0 top-0 w-full h-full flex items-center justify-between px-4 z-10 pointer-events-none">
                     <div class="flex items-center space-x-2">
                         <span class="font-medium text-gray-900 dark:text-white text-lg">{{ opt.text }}</span>
                         <CheckCircleIcon v-if="tweet.poll.is_voted && tweet.poll.voted_option_id === opt.id" class="w-6 h-6 text-primary" />
                     </div>
                     <span v-if="tweet.poll.is_voted || tweet.poll.is_expired" class="font-bold text-gray-900 dark:text-white">{{ Math.round(opt.percentage || 0) }}%</span>
                </div>
            </div>
            
            <div class="text-gray-500 mt-1">
                {{ tweet.poll.total_votes || 0 }} 票 · {{ tweet.poll.is_expired ? '已结束' : (dayjs(tweet.poll.end_time).fromNow(true) + '后结束') }}
            </div>
        </div>

          <!-- Time -->
          <div class="px-4 mt-3 py-3 border-b border-gray-100 dark:border-gray-800">
              <span class="text-gray-500 text-sm">{{ formatTime(tweet.created_at) }}</span>
          </div>

          <!-- Stats -->
          <div class="px-4 py-3 border-b border-gray-100 dark:border-gray-800 flex space-x-6 text-sm">
              <div v-if="tweet.share_count" class="hover:underline cursor-pointer">
                  <span class="font-bold text-gray-900 dark:text-white">{{ tweet.share_count }}</span>
                  <span class="text-gray-500 ml-1">转推</span>
              </div>
              <div class="hover:underline cursor-pointer">
                  <span class="font-bold text-gray-900 dark:text-white">{{ likeCount }}</span>
                  <span class="text-gray-500 ml-1">喜欢</span>
              </div>
              <div>
                  <span class="font-bold text-gray-900 dark:text-white">{{ tweet.comment_count }}</span>
                  <span class="text-gray-500 ml-1">回复</span>
              </div>
          </div>

          <!--  Action Buttons -->
          <div class="px-4 py-2 border-b border-gray-100 dark:border-gray-800 flex justify-around text-gray-500">
              <button @click="handleReply" class="p-2 rounded-full hover:bg-blue-50 dark:hover:bg-blue-900/20 hover:text-blue-500 transition-colors" title="回复">
                  <ChatBubbleLeftIcon class="w-6 h-6" />
              </button>
              <button 
                @click="handleRetweet" 
                class="p-2 rounded-full transition-colors" 
                :class="isRetweeted ? 'text-green-500 hover:bg-green-50 dark:hover:bg-green-900/20' : 'hover:bg-green-50 dark:hover:bg-green-900/20 hover:text-green-500'" 
                title="转推"
              >
                  <ArrowPathRoundedSquareIcon class="w-6 h-6" />
              </button>
              <button
                @click="toggleLike"
                class="p-2 rounded-full transition-colors"
                :class="isLiked ? 'text-pink-600 hover:bg-pink-50 dark:hover:bg-pink-900/20' : 'hover:bg-pink-50 dark:hover:bg-pink-900/20 hover:text-pink-600'"
                title="喜欢"
              >
                  <component :is="isLiked ? HeartIconSolid : HeartIcon" class="w-6 h-6" />
              </button>
              <button @click="handleShare" class="p-2 rounded-full hover:bg-blue-50 dark:hover:bg-blue-900/20 hover:text-blue-500 transition-colors" title="分享">
                  <ShareIcon class="w-6 h-6" />
              </button>
          </div>
      </div>

      <!-- Not Found -->
      <div v-else class="p-8 text-center text-gray-500">
          <p class="text-lg font-bold">推文不存在</p>
          <p class="mt-2">可能已被删除或链接错误</p>
          <button @click="router.push('/')" class="mt-4 text-primary hover:underline">返回首页</button>
      </div>

      <!-- Thread Replies -->
      <div v-if="replies.length > 0" class="border-b border-gray-100 dark:border-gray-800">
           <TweetCard 
             v-for="reply in replies" 
             :key="reply.id" 
             :tweet="reply" 
             @reply="handleReply" 
           />
           <div v-if="hasMoreReplies" class="p-4 text-center">
               <button @click="fetchReplies(false)" :disabled="repliesLoading" class="text-primary hover:underline">
                   {{ repliesLoading ? '加载中...' : '加载更多回复' }}
               </button>
           </div>
      </div>

      <!-- Comment Input -->
      <div v-if="tweet" class="px-4 py-3 border-b border-gray-100 dark:border-gray-800">
          <div class="flex space-x-3">
              <img
                :src="userStore.user?.avatar || 'https://abs.twimg.com/sticky/default_profile_images/default_profile_400x400.png'"
                class="w-10 h-10 rounded-full object-cover flex-shrink-0"
              />
              <div class="flex-1">
                  <textarea
                    v-model="commentContent"
                    class="w-full border-none focus:ring-0 text-base placeholder-gray-500 dark:bg-black dark:text-white resize-none min-h-[60px] outline-none"
                    placeholder="发表你的回复..."
                    @keydown.ctrl.enter="handleSubmitComment"
                  ></textarea>
                  <div class="flex justify-end">
                      <button
                        @click="handleSubmitComment"
                        :disabled="commentSubmitting || !commentContent.trim()"
                        class="bg-primary hover:bg-blue-600 text-white font-bold py-1.5 px-5 rounded-full disabled:opacity-50 transition-colors text-sm"
                      >
                          {{ commentSubmitting ? '发送中...' : '回复' }}
                      </button>
                  </div>
              </div>
          </div>
      </div>

      <!-- Comments List -->
      <div v-if="tweet">
          <div v-if="commentsLoading && comments.length === 0" class="p-6 text-center text-gray-500">
              加载评论中...
          </div>

          <div v-for="comment in comments" :key="comment.id" class="px-4 py-3 border-b border-gray-100 dark:border-gray-800 hover:bg-gray-50 dark:hover:bg-gray-900/30 transition-colors">
              <div class="flex space-x-3">
                  <img
                    :src="comment.user?.avatar_url || 'https://abs.twimg.com/sticky/default_profile_images/default_profile_400x400.png'"
                    class="w-10 h-10 rounded-full object-cover flex-shrink-0"
                  />
                  <div class="flex-1 min-w-0">
                      <div class="flex items-center space-x-1 text-sm">
                          <span class="font-bold text-gray-900 dark:text-white truncate">{{ comment.user?.username || 'unknown' }}</span>
                          <span class="text-gray-500">@{{ comment.user?.username || 'unknown' }}</span>
                          <span class="text-gray-500">·</span>
                          <span class="text-gray-500">{{ formatRelativeTime(comment.created_at) }}</span>
                      </div>
                      <div class="flex items-center justify-between text-sm text-gray-500 mb-1">
                          <div>
                              回复 <span class="text-primary">@{{ tweet.user?.username || 'unknown' }}</span>
                          </div>
                          <button @click="handleReplyToComment(comment)" class="flex items-center space-x-1 hover:text-blue-500 transition-colors">
                              <ChatBubbleLeftIcon class="w-4 h-4" />
                              <span>回复</span>
                          </button>
                      </div>
                      <p class="text-gray-900 dark:text-white whitespace-pre-wrap break-words">{{ comment.content }}</p>
                  </div>
              </div>
          </div>

          <div v-if="comments.length === 0 && !commentsLoading" class="p-8 text-center text-gray-500">
              暂无回复
          </div>

          <div v-if="hasMoreComments && comments.length > 0" class="p-4 text-center">
              <button @click="fetchComments(false)" :disabled="commentsLoading" class="text-primary hover:underline">
                  {{ commentsLoading ? '加载中...' : '加载更多回复' }}
              </button>
          </div>
      </div>

      <ReplyModal 
        :show="showReplyModal" 
        :replyTo="tweet" 
        @close="showReplyModal = false" 
        @reply-created="handleReplyCreated" 
      />
  </MainLayout>
</template>
