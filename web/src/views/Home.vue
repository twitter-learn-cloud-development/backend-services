<script setup lang="ts">
import MainLayout from '../layout/MainLayout.vue'
import ComposeBox from '../components/ComposeBox.vue'
import TweetCard from '../components/TweetCard.vue'
import ReplyModal from '../components/ReplyModal.vue'
import { ref, shallowRef, computed, watch, onMounted, onUnmounted } from 'vue'
import { getFeeds, listTweets, type Tweet } from '../api/tweet'
import { FeedPager, type FeedState } from '../utils/feedPager'
import { useUserStore } from '../stores/user'

const activeTab = ref<'following' | 'forYou'>('forYou')
const feedState = shallowRef<FeedState<Tweet>>({ items: [], cursor: '0', hasMore: true, loading: false, error: null, continuation: false })
const source = (tab: 'following' | 'forYou') => async (cursor: string) => {
    const response = tab === 'following' ? await getFeeds(cursor) : await listTweets(cursor)
    return response.data
}
const pager = new FeedPager<Tweet>(source(activeTab.value), state => { feedState.value = state })
const tweets = computed(() => feedState.value.items)
const loading = computed(() => feedState.value.loading)
const hasMore = computed(() => feedState.value.hasMore)
const userStore = useUserStore()
watch(() => userStore.token, () => { void pager.reset(source(activeTab.value)) })

const fetchTweets = async (refresh = false) => {
    if (refresh) await pager.refresh()
    else await pager.load()
}

const switchTab = (tab: 'following' | 'forYou') => {
    activeTab.value = tab
    void pager.reset(source(tab))
}

const handleTweetCreated = () => {
    fetchTweets(true)
}

const handleTweetDeleted = (tweetId: string) => {
    pager.updateItems(items => items.filter(t => t.id !== tweetId))
}

const showReplyModal = ref(false)
const replyToTweet = ref<Tweet | null>(null)

const handleReply = (tweet: Tweet) => {
    replyToTweet.value = tweet
    showReplyModal.value = true
}

const handleReplyCreated = () => {
    if (replyToTweet.value) {
        replyToTweet.value.comment_count++
    }
}

onMounted(() => {
    fetchTweets(true)
    window.addEventListener('scroll', handleScroll, { passive: true })
})

onUnmounted(() => {
    pager.dispose()
    window.removeEventListener('scroll', handleScroll)
})

const handleScroll = () => {
    const nearBottom = window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 360
    if (nearBottom && !feedState.value.error && !feedState.value.continuation) {
        fetchTweets(false)
    }
}
</script>

<template>
  <MainLayout>
      <!-- Header with Tabs -->
      <div class="sticky top-0 bg-white/80 dark:bg-black/80 backdrop-blur-md z-10 border-b border-gray-100 dark:border-gray-800">
          <h1 class="font-bold text-xl px-4 py-3">主页</h1>
          <div class="flex">
              <button
                @click="switchTab('forYou')"
                class="flex-1 text-center py-3 font-medium hover:bg-gray-50 dark:hover:bg-gray-900 transition-colors relative"
                :class="activeTab === 'forYou' ? 'text-gray-900 dark:text-white font-bold' : 'text-gray-500'"
              >
                  为你推荐
                  <div v-if="activeTab === 'forYou'" class="absolute bottom-0 left-1/2 -translate-x-1/2 w-14 h-1 bg-primary rounded-full"></div>
              </button>
              <button
                @click="switchTab('following')"
                class="flex-1 text-center py-3 font-medium hover:bg-gray-50 dark:hover:bg-gray-900 transition-colors relative"
                :class="activeTab === 'following' ? 'text-gray-900 dark:text-white font-bold' : 'text-gray-500'"
              >
                  正在关注
                  <div v-if="activeTab === 'following'" class="absolute bottom-0 left-1/2 -translate-x-1/2 w-14 h-1 bg-primary rounded-full"></div>
              </button>
          </div>
      </div>

      <!-- Compose Box -->
      <ComposeBox @tweet-created="handleTweetCreated" />





      <!-- Feed List -->
      <div v-if="tweets.length > 0">
          <TweetCard 
            v-for="tweet in tweets" 
            :key="tweet.id" 
            :tweet="tweet" 
            @deleted="handleTweetDeleted" 
            @reply="handleReply"
          />
      </div>

      <div v-if="loading" class="p-4 text-center text-gray-500" role="status">加载中...</div>
      <div v-else-if="feedState.error" class="p-4 text-center" role="alert">
          <p>{{ feedState.error }}</p>
          <button @click="fetchTweets(false)" class="mt-2 text-primary">重试</button>
      </div>
      <div v-else-if="feedState.continuation || hasMore" class="p-4 text-center">
          <button @click="fetchTweets(false)" class="text-primary">继续加载</button>
      </div>
      <div v-else class="p-4 text-center text-gray-400">{{ tweets.length ? '已经到底了' : '暂无推文' }}</div>

      <!-- ... -->

      <ReplyModal 
        :show="showReplyModal" 
        :replyTo="replyToTweet" 
        @close="showReplyModal = false" 
        @reply-created="handleReplyCreated" 
      />
  </MainLayout>
</template>
