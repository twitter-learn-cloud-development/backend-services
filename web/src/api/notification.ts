import request from '../utils/request'

export interface Notification {
    id: string
    type: 'like' | 'comment' | 'follow'
    target_id: string
    content: string
    is_read: boolean
    created_at: number
    actor: {
        id: string
        username: string
        avatar: string
    }
}

export interface NotificationListResponse {
    notifications: Notification[]
    next_cursor: string
    has_more: boolean
}

export const getNotifications = (cursor: string = '0', limit: number = 20) => {
    return request.get<NotificationListResponse>('/notifications', {
        params: { cursor, limit }
    })
}

export const markAsRead = (ids: number[]) => {
    return request.put('/notifications/read', { ids })
}

export const markAllAsRead = () => {
    return request.put('/notifications/read-all')
}

export const getUnreadCount = () => {
    return request.get<{ count: number }>('/notifications/unread-count')
}
