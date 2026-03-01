import request from '../utils/request'

export interface Tweet {
    id: string
    user_id: string
    content: string
    media_urls: string[]
    created_at: number
    like_count: number
    comment_count: number
    share_count: number
    retweet_count: number
    is_liked: boolean
    is_bookmarked?: boolean
    is_retweeted?: boolean
    is_retweeted_display?: boolean // 是否显示为转发 (Timeline 用)
    retweeted_at?: number        // 转发时间
    parent_id?: string           // 父推文ID
    user?: User
    poll?: Poll
}

export interface PollOption {
    id: string
    poll_id: string
    text: string
    vote_count: number
    percentage?: number
}

export interface Poll {
    id: string
    tweet_id: string
    question: string
    options: PollOption[]
    end_time: number
    is_expired: boolean
    is_voted: boolean
    voted_option_id?: string
    total_votes?: number
}

export interface User {
    id: string
    username: string
    avatar: string
    bio?: string
}

export interface Comment {
    id: string
    user_id: string
    tweet_id: string
    content: string
    created_at: number
    user?: {
        username: string
        nickname: string
        avatar_url: string
    }
}

// ==================== Feed / Timeline ====================

export const getFeeds = (cursor: string = '0', limit: number = 20) => {
    return request({
        url: '/feeds',
        method: 'get',
        params: { cursor, limit }
    })
}

export const listTweets = (cursor: string = '0', limit: number = 20) => {
    return request({
        url: '/tweets/public',
        method: 'get',
        params: { cursor, limit }
    })
}

// ==================== Tweet CRUD ====================

export const getTweet = (tweetId: string) => {
    return request({
        url: `/tweets/${tweetId}`,
        method: 'get'
    })
}

export const createTweet = (data: { content: string; media_urls?: string[]; parent_id?: string }) => {
    return request({
        url: '/tweets',
        method: 'post',
        data
    })
}

export const deleteTweet = (tweetId: string) => {
    return request({
        url: `/tweets/${tweetId}`,
        method: 'delete'
    })
}

// ==================== Like ====================

export const likeTweet = (tweetId: string) => {
    return request({
        url: `/tweets/${tweetId}/like`,
        method: 'post'
    })
}

export const unlikeTweet = (tweetId: string) => {
    return request({
        url: `/tweets/${tweetId}/like`,
        method: 'delete'
    })
}

// ==================== Comment ====================

export const getComments = (tweetId: string, cursor: string = '0', limit: number = 20) => {
    return request({
        url: `/tweets/${tweetId}/comments`,
        method: 'get',
        params: { cursor, limit }
    })
}

export const createComment = (tweetId: string, content: string, parentId?: string) => {
    return request({
        url: `/tweets/${tweetId}/comments`,
        method: 'post',
        data: { content, parent_id: parentId }
    })
}

export const deleteComment = (commentId: string) => {
    return request({
        url: `/comments/${commentId}`,
        method: 'delete'
    })
}

// ==================== Bookmark ====================

export const addBookmark = (tweetId: string) => {
    return request({
        url: `/tweets/${tweetId}/bookmark`,
        method: 'post'
    })
}

export const removeBookmark = (tweetId: string) => {
    return request({
        url: `/tweets/${tweetId}/bookmark`,
        method: 'delete'
    })
}

// ==================== Retweet ====================

export const retweetTweet = (tweetId: string) => {
    return request({
        url: `/tweets/${tweetId}/retweet`,
        method: 'post'
    })
}

export const unretweetTweet = (tweetId: string) => {
    return request({
        url: `/tweets/${tweetId}/retweet`,
        method: 'delete'
    })
}

export const getTweetReplies = (tweetId: string, cursor: string = '0', limit: number = 20) => {
    return request({
        url: `/tweets/${tweetId}/replies`,
        method: 'get',
        params: { cursor, limit }
    })
}

export const votePoll = (pollId: string, optionId: string) => {
    return request({
        url: '/polls/vote',
        method: 'post',
        data: { poll_id: pollId, option_id: optionId }
    })
}

