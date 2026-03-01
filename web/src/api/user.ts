import request from '../utils/request'

export interface UserProfile {
    id: string
    username: string
    email: string
    avatar: string
    bio: string
    cover_url?: string
    website?: string
    location?: string
    created_at: number
    follower_count?: number
    followee_count?: number
    is_following?: boolean
}

export const getUser = (id: string) => {
    return request({
        url: `/users/${id}`,
        method: 'get'
    })
}

export const getBatchUsers = (userIDs: string[]) => {
    return request({
        url: '/users/batch',
        method: 'post',
        data: { user_ids: userIDs }
    })
}

// 搜索用户
export const searchUsers = (query: string, cursor: string = '0', limit: number = 20) => {
    return request({
        url: '/users/search',
        method: 'get',
        params: { q: query, cursor, limit }
    })
}

export const getMe = () => {
    return request({
        url: '/users/me',
        method: 'get'
    })
}

export const updateProfile = (data: { bio?: string; avatar?: string; cover_url?: string; website?: string; location?: string }) => {
    return request({
        url: '/users/me',
        method: 'put',
        data
    })
}

export const getUserTimeline = (userId: string, cursor: string = '0', limit: number = 20) => {
    return request({
        url: `/users/${userId}/timeline`,
        method: 'get',
        params: { cursor, limit }
    })
}

export const getFollowStats = (userId: string) => {
    return request({
        url: `/users/${userId}/stats`,
        method: 'get'
    })
}

export const followUser = (userId: string) => {
    return request({
        url: '/follows',
        method: 'post',
        data: { followee_id: userId }
    })
}

export const unfollowUser = (userId: string) => {
    return request({
        url: `/follows/${userId}`,
        method: 'delete'
    })
}

export const isFollowing = (userId: string) => {
    return request({
        url: `/follows/${userId}/status`,
        method: 'get'
    })
}

// ==================== Profile Tabs ====================

export const getUserLikes = (userId: string, cursor: string = '0', limit: number = 20) => {
    return request({
        url: `/users/${userId}/likes`,
        method: 'get',
        params: { cursor, limit }
    })
}

export const getUserReplies = (userId: string, cursor: string = '0', limit: number = 20) => {
    return request({
        url: `/users/${userId}/replies`,
        method: 'get',
        params: { cursor, limit }
    })
}

export const getUserMedia = (userId: string, cursor: string = '0', limit: number = 20) => {
    return request({
        url: `/users/${userId}/media`,
        method: 'get',
        params: { cursor, limit }
    })
}

// ==================== Follows ====================

export const getFollowers = (userId: string, cursor: string = '0', limit: number = 20) => {
    return request({
        url: `/users/${userId}/followers`,
        method: 'get',
        params: { cursor, limit }
    })
}

export const getFollowees = (userId: string, cursor: string = '0', limit: number = 20) => {
    return request({
        url: `/users/${userId}/followees`,
        method: 'get',
        params: { cursor, limit }
    })
}
